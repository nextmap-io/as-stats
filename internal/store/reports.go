package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// =============================================================================
// Report schedules CRUD (Module D — scheduled reports)
//
// Mirrors the alert_rules / webhook_configs CRUD patterns in alerts.go:
// ReplacingMergeTree(updated_at), always read with FINAL, soft-delete via a
// deleted tombstone column, parameterised SQL via clickhouse.Named().
// =============================================================================

const reportScheduleColumns = `id, name, frequency, hour, day_of_week, day_of_month,
	recipients, sections, format, enabled, last_run_at, created_at, updated_at`

// scanReportSchedule scans a single row in reportScheduleColumns order.
func scanReportSchedule(rows interface {
	Scan(dest ...any) error
}) (model.ReportSchedule, error) {
	var r model.ReportSchedule
	var enabled uint8
	if err := rows.Scan(
		&r.ID, &r.Name, &r.Frequency, &r.Hour, &r.DayOfWeek, &r.DayOfMonth,
		&r.Recipients, &r.Sections, &r.Format, &enabled,
		&r.LastRunAt, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return model.ReportSchedule{}, err
	}
	r.Enabled = enabled == 1
	return r, nil
}

// ListReportSchedules returns all non-deleted report schedules, ordered by name.
func (s *ClickHouseStore) ListReportSchedules(ctx context.Context) ([]model.ReportSchedule, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM report_schedules FINAL
		WHERE deleted = 0
		ORDER BY name
	`, reportScheduleColumns)
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list report schedules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ReportSchedule
	for rows.Next() {
		r, err := scanReportSchedule(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// GetReportSchedule returns a single non-deleted schedule by ID, or an error
// wrapping "no rows" when it does not exist.
func (s *ClickHouseStore) GetReportSchedule(ctx context.Context, id string) (model.ReportSchedule, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM report_schedules FINAL
		WHERE deleted = 0 AND id = @id
		LIMIT 1
	`, reportScheduleColumns)
	row := s.conn.QueryRow(ctx, query, clickhouse.Named("id", id))
	r, err := scanReportSchedule(row)
	if err != nil {
		return model.ReportSchedule{}, fmt.Errorf("get report schedule %q: %w", id, err)
	}
	return r, nil
}

// upsertReportSchedule inserts a new version of a schedule (ReplacingMergeTree
// collapses on the latest updated_at). Shared by Create and Update.
func (s *ClickHouseStore) upsertReportSchedule(ctx context.Context, r model.ReportSchedule) error {
	enabled := uint8(0)
	if r.Enabled {
		enabled = 1
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.UpdatedAt
	}
	if r.LastRunAt.IsZero() {
		r.LastRunAt = time.Unix(0, 0).UTC()
	}
	return s.conn.Exec(ctx, `
		INSERT INTO report_schedules (id, name, frequency, hour, day_of_week, day_of_month,
			recipients, sections, format, enabled, last_run_at, deleted, created_at, updated_at)
		VALUES (@id, @name, @frequency, @hour, @day_of_week, @day_of_month,
			@recipients, @sections, @format, @enabled, @last_run_at, 0, @created_at, @updated_at)
	`,
		clickhouse.Named("id", r.ID),
		clickhouse.Named("name", r.Name),
		clickhouse.Named("frequency", r.Frequency),
		clickhouse.Named("hour", r.Hour),
		clickhouse.Named("day_of_week", r.DayOfWeek),
		clickhouse.Named("day_of_month", r.DayOfMonth),
		clickhouse.Named("recipients", r.Recipients),
		clickhouse.Named("sections", r.Sections),
		clickhouse.Named("format", r.Format),
		clickhouse.Named("enabled", enabled),
		clickhouse.Named("last_run_at", r.LastRunAt),
		clickhouse.Named("created_at", r.CreatedAt),
		clickhouse.Named("updated_at", r.UpdatedAt),
	)
}

// CreateReportSchedule inserts a new schedule.
func (s *ClickHouseStore) CreateReportSchedule(ctx context.Context, r model.ReportSchedule) error {
	return s.upsertReportSchedule(ctx, r)
}

// UpdateReportSchedule replaces an existing schedule. CreatedAt/LastRunAt are
// preserved by the caller (handler reloads the row first) or defaulted here.
func (s *ClickHouseStore) UpdateReportSchedule(ctx context.Context, r model.ReportSchedule) error {
	r.UpdatedAt = time.Now().UTC()
	return s.upsertReportSchedule(ctx, r)
}

// DeleteReportSchedule soft-deletes a schedule (deleted = 1, updated_at = now).
func (s *ClickHouseStore) DeleteReportSchedule(ctx context.Context, id string) error {
	return s.conn.Exec(ctx, `
		INSERT INTO report_schedules (id, name, frequency, format, deleted, updated_at)
		SELECT id, name, frequency, format, 1, now()
		FROM report_schedules FINAL
		WHERE id = @id
	`, clickhouse.Named("id", id))
}

// MarkReportRun stamps last_run_at for a schedule so the scheduler does not
// re-fire the same occurrence. Uses a lightweight in-place UPDATE mutation
// rather than a full re-insert to avoid clobbering concurrent edits.
func (s *ClickHouseStore) MarkReportRun(ctx context.Context, id string, ts time.Time) error {
	return s.conn.Exec(ctx,
		"ALTER TABLE report_schedules UPDATE last_run_at = @ts, updated_at = now() WHERE id = @id",
		clickhouse.Named("id", id),
		clickhouse.Named("ts", ts.UTC()),
	)
}
