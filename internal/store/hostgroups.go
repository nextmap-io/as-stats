package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// ListHostgroups returns all non-deleted hostgroups.
func (s *ClickHouseStore) ListHostgroups(ctx context.Context) ([]model.Hostgroup, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT id, name, description, cidrs, created_at, updated_at
		FROM hostgroups FINAL
		WHERE deleted = 0
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list hostgroups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Hostgroup
	for rows.Next() {
		var h model.Hostgroup
		if err := rows.Scan(&h.ID, &h.Name, &h.Description, &h.CIDRs, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, h)
	}
	return results, nil
}

// GetHostgroup returns a single hostgroup by ID.
func (s *ClickHouseStore) GetHostgroup(ctx context.Context, id string) (model.Hostgroup, error) {
	var h model.Hostgroup
	row := s.conn.QueryRow(ctx, `
		SELECT id, name, description, cidrs, created_at, updated_at
		FROM hostgroups FINAL
		WHERE id = @id AND deleted = 0
	`, clickhouse.Named("id", id))
	if err := row.Scan(&h.ID, &h.Name, &h.Description, &h.CIDRs, &h.CreatedAt, &h.UpdatedAt); err != nil {
		return h, fmt.Errorf("get hostgroup: %w", err)
	}
	return h, nil
}

// UpsertHostgroup inserts or updates a hostgroup. ReplacingMergeTree dedup
// keeps only the row with the latest updated_at for each id.
func (s *ClickHouseStore) UpsertHostgroup(ctx context.Context, h model.Hostgroup) error {
	now := time.Now().UTC()
	return s.conn.Exec(ctx, `
		INSERT INTO hostgroups (id, name, description, cidrs, created_at, updated_at, deleted)
		VALUES (@id, @name, @desc, @cidrs, @created, @updated, 0)
	`,
		clickhouse.Named("id", h.ID),
		clickhouse.Named("name", h.Name),
		clickhouse.Named("desc", h.Description),
		clickhouse.Named("cidrs", h.CIDRs),
		clickhouse.Named("created", h.CreatedAt),
		clickhouse.Named("updated", now),
	)
}

// DeleteHostgroup soft-deletes a hostgroup by inserting a row with deleted=1.
func (s *ClickHouseStore) DeleteHostgroup(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return s.conn.Exec(ctx, `
		INSERT INTO hostgroups (id, name, description, cidrs, created_at, updated_at, deleted)
		SELECT id, name, description, cidrs, created_at, @now, 1
		FROM hostgroups FINAL
		WHERE id = @id AND deleted = 0
	`,
		clickhouse.Named("now", now),
		clickhouse.Named("id", id),
	)
}
