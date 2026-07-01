package store

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// retentionTable describes a TTL-bearing table: the timestamp column its TTL is
// keyed on, the expression used in `ALTER TABLE ... MODIFY TTL <expr> + INTERVAL`
// (usually identical to the column, but flows_raw wraps a DateTime64 column in
// toDateTime()), and the default retention in days as encoded in the migrations.
type retentionTable struct {
	TTLColumn   string // raw column name persisted in retention_policies.ttl_column
	TTLExpr     string // expression used when issuing ALTER ... MODIFY TTL
	DefaultDays uint32
}

// retentionTables is the single source of truth for which tables are under
// retention management. It doubles as a whitelist: SetRetentionPolicy and
// ReconcileRetention only ever interpolate table/column names that originate
// here, never raw user input. Values mirror the TTLs in migrations 000001,
// 000004, 000005, 000007, 000008, 000009 and 000011.
var retentionTables = map[string]retentionTable{
	// Core tables (migrations 000001 / 000004 / 000005)
	"flows_raw":              {TTLColumn: "timestamp", TTLExpr: "toDateTime(timestamp)", DefaultDays: 3},
	"traffic_by_as":          {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 90},
	"traffic_by_as_hourly":   {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 730},
	"traffic_by_as_daily":    {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 1825},
	"traffic_by_link":        {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 90},
	"traffic_by_link_hourly": {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 730},
	"traffic_by_link_daily":  {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 1825},
	"traffic_by_ip":          {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 14},
	"traffic_by_ip_as":       {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 14},
	"traffic_by_prefix":      {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 30},

	// Optional / feature-gated tables (migrations 000007 / 000008 / 000009 / 000011)
	"flows_log":           {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 180}, // seed overridden by FLOW_LOG_RETENTION_DAYS
	"traffic_by_port":     {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 365},
	"traffic_by_dst_1min": {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 7},
	"traffic_by_src_1min": {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 7},
	"alerts":              {TTLColumn: "triggered_at", TTLExpr: "triggered_at", DefaultDays: 365},
	"audit_log":           {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 365},
	"bgp_blocks":          {TTLColumn: "blocked_at", TTLExpr: "blocked_at", DefaultDays: 365},
}

// buildModifyTTLStatement constructs the ALTER ... MODIFY TTL statement for a
// table in the whitelist. Both the table name and the TTL expression come from
// the hardcoded retentionTables map (never user input); `days` is an integer so
// it cannot carry injection. Returns ok=false for unknown tables.
func buildModifyTTLStatement(table string, days uint32) (string, bool) {
	rt, ok := retentionTables[table]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s + INTERVAL %d DAY", table, rt.TTLExpr, days), true
}

// EnsureRetentionPolicies seeds retention_policies with one row per TTL-bearing
// table, using the defaults encoded in the migrations. It is idempotent: rows
// are only inserted when the table is currently empty, so operator edits made
// via the API are never clobbered on restart. flows_log is seeded with
// flowLogDays (from FLOW_LOG_RETENTION_DAYS).
func (s *ClickHouseStore) EnsureRetentionPolicies(ctx context.Context, flowLogDays int) error {
	var count uint64
	if err := s.conn.QueryRow(ctx, "SELECT count() FROM retention_policies FINAL").Scan(&count); err != nil {
		return fmt.Errorf("count retention_policies: %w", err)
	}
	if count > 0 {
		return nil // already seeded — never overwrite operator edits
	}

	now := time.Now().UTC()
	for table, rt := range retentionTables {
		days := rt.DefaultDays
		if table == "flows_log" && flowLogDays > 0 {
			days = uint32(flowLogDays)
		}
		if err := s.conn.Exec(ctx, `
			INSERT INTO retention_policies (table_name, ttl_column, ttl_days, enabled, updated_at)
			VALUES (@table_name, @ttl_column, @ttl_days, 1, @updated_at)
		`,
			clickhouse.Named("table_name", table),
			clickhouse.Named("ttl_column", rt.TTLColumn),
			clickhouse.Named("ttl_days", days),
			clickhouse.Named("updated_at", now),
		); err != nil {
			return fmt.Errorf("seed retention policy %q: %w", table, err)
		}
	}
	log.Printf("retention: seeded %d default retention policies", len(retentionTables))
	return nil
}

// ListRetentionPolicies returns the current retention policy for every managed
// table, ordered by table name. Only tables in the whitelist are returned.
func (s *ClickHouseStore) ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table_name, ttl_column, ttl_days, enabled, updated_at
		FROM retention_policies FINAL
		ORDER BY table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list retention policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.RetentionPolicy
	for rows.Next() {
		var p model.RetentionPolicy
		var enabled uint8
		if err := rows.Scan(&p.TableName, &p.TTLColumn, &p.TTLDays, &enabled, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		results = append(results, p)
	}
	return results, nil
}

// SetRetentionPolicy upserts the retention policy for a table. The table must be
// in the retentionTables whitelist; unknown tables are rejected so the API can
// never write a policy for an arbitrary name. The TTL is not applied here — the
// reconciler picks it up on its next cycle.
func (s *ClickHouseStore) SetRetentionPolicy(ctx context.Context, table string, days uint32, enabled bool) error {
	rt, ok := retentionTables[table]
	if !ok {
		return fmt.Errorf("unknown table %q", table)
	}
	if days < 1 {
		return fmt.Errorf("ttl_days must be >= 1")
	}
	en := uint8(0)
	if enabled {
		en = 1
	}
	return s.conn.Exec(ctx, `
		INSERT INTO retention_policies (table_name, ttl_column, ttl_days, enabled, updated_at)
		VALUES (@table_name, @ttl_column, @ttl_days, @enabled, @updated_at)
	`,
		clickhouse.Named("table_name", table),
		clickhouse.Named("ttl_column", rt.TTLColumn),
		clickhouse.Named("ttl_days", days),
		clickhouse.Named("enabled", en),
		clickhouse.Named("updated_at", time.Now().UTC()),
	)
}

// presentTables returns the set of tables that actually exist in the current
// database, so the reconciler / stats can skip feature-gated tables that were
// never created.
func (s *ClickHouseStore) presentTables(ctx context.Context) (map[string]bool, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT name FROM system.tables WHERE database = currentDatabase()
	`)
	if err != nil {
		return nil, fmt.Errorf("list system.tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	return present, nil
}

// ReconcileRetention applies each enabled policy's desired TTL to its table when
// it diverges from the live TTL. The change is metadata-only and idempotent.
// Tables absent from system.tables (e.g. feature-gated ones that were never
// created) are skipped. Applied changes are logged.
func (s *ClickHouseStore) ReconcileRetention(ctx context.Context) error {
	policies, err := s.ListRetentionPolicies(ctx)
	if err != nil {
		return err
	}
	present, err := s.presentTables(ctx)
	if err != nil {
		return err
	}

	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if _, ok := retentionTables[p.TableName]; !ok {
			continue // ignore policies for tables outside the whitelist
		}
		if !present[p.TableName] {
			continue // feature-gated table not created — nothing to do
		}

		applied, err := s.applyTTLIfDiverged(ctx, p.TableName, p.TTLDays)
		if err != nil {
			log.Printf("retention: reconcile %s failed: %v", p.TableName, err)
			continue
		}
		if applied {
			log.Printf("retention: applied TTL %d day(s) to %s", p.TTLDays, p.TableName)
		}
	}
	return nil
}

// applyTTLIfDiverged issues the MODIFY TTL only if the live table definition
// does not already encode the desired retention. ClickHouse normalizes TTL in
// create_table_query to `... + toIntervalDay(N)`, so we look for that token; if
// present, the table is already at the desired retention and we skip the ALTER
// to avoid log noise every cycle. Returns true when an ALTER was issued.
func (s *ClickHouseStore) applyTTLIfDiverged(ctx context.Context, table string, days uint32) (bool, error) {
	stmt, ok := buildModifyTTLStatement(table, days)
	if !ok {
		return false, fmt.Errorf("unknown table %q", table)
	}

	var createQuery string
	err := s.conn.QueryRow(ctx, `
		SELECT create_table_query FROM system.tables
		WHERE database = currentDatabase() AND name = @name
	`, clickhouse.Named("name", table)).Scan(&createQuery)
	if err != nil {
		return false, fmt.Errorf("read create query: %w", err)
	}

	desired := fmt.Sprintf("toIntervalDay(%d)", days)
	if strings.Contains(createQuery, desired) {
		return false, nil // already at desired retention
	}

	if err := s.conn.Exec(ctx, stmt); err != nil {
		return false, fmt.Errorf("modify ttl: %w", err)
	}
	return true, nil
}

// StorageStats returns per-table storage observability (size, parts, rows, data
// window, configured retention, pending mutations) plus per-disk usage. All
// interpolated values are bound parameters; the database is the connection's
// currentDatabase().
func (s *ClickHouseStore) StorageStats(ctx context.Context) (model.StorageStats, error) {
	var out model.StorageStats

	// Configured retention, keyed by table.
	policies, err := s.ListRetentionPolicies(ctx)
	if err != nil {
		return out, err
	}
	policyByTable := make(map[string]model.RetentionPolicy, len(policies))
	for _, p := range policies {
		policyByTable[p.TableName] = p
	}

	// Pending (not-done) mutations per table.
	pending, err := s.pendingMutationsByTable(ctx)
	if err != nil {
		return out, err
	}

	// Per-table aggregates from active parts.
	rows, err := s.conn.Query(ctx, `
		SELECT
			table,
			sum(data_compressed_bytes)   AS compressed,
			sum(data_uncompressed_bytes) AS uncompressed,
			count()                      AS parts,
			sum(rows)                    AS row_count,
			min(min_time)                AS oldest,
			max(max_time)                AS newest
		FROM system.parts
		WHERE database = currentDatabase() AND active
		GROUP BY table
		ORDER BY compressed DESC
	`)
	if err != nil {
		return out, fmt.Errorf("query system.parts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var t model.TableStorageStats
		var oldest, newest time.Time
		if err := rows.Scan(
			&t.Table, &t.CompressedBytes, &t.UncompressedBytes,
			&t.Parts, &t.Rows, &oldest, &newest,
		); err != nil {
			return out, err
		}
		if !oldest.IsZero() && oldest.Unix() > 0 {
			o := oldest
			t.OldestData = &o
		}
		if !newest.IsZero() && newest.Unix() > 0 {
			n := newest
			t.NewestData = &n
		}
		if p, ok := policyByTable[t.Table]; ok {
			t.TTLDays = p.TTLDays
			t.TTLEnabled = p.Enabled
		}
		t.PendingMutations = pending[t.Table]
		out.Tables = append(out.Tables, t)
	}

	disks, err := s.diskStats(ctx)
	if err != nil {
		return out, err
	}
	out.Disks = disks
	return out, nil
}

func (s *ClickHouseStore) pendingMutationsByTable(ctx context.Context) (map[string]uint64, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table, count() AS cnt
		FROM system.mutations
		WHERE database = currentDatabase() AND is_done = 0
		GROUP BY table
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.mutations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]uint64)
	for rows.Next() {
		var table string
		var cnt uint64
		if err := rows.Scan(&table, &cnt); err != nil {
			return nil, err
		}
		out[table] = cnt
	}
	return out, nil
}

func (s *ClickHouseStore) diskStats(ctx context.Context) ([]model.DiskStats, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT name, free_space, total_space
		FROM system.disks
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.disks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var disks []model.DiskStats
	for rows.Next() {
		var d model.DiskStats
		if err := rows.Scan(&d.Name, &d.FreeBytes, &d.TotalBytes); err != nil {
			return nil, err
		}
		if d.TotalBytes >= d.FreeBytes {
			d.UsedBytes = d.TotalBytes - d.FreeBytes
		}
		if d.TotalBytes > 0 {
			d.UsedPercent = float64(d.UsedBytes) / float64(d.TotalBytes) * 100
		}
		disks = append(disks, d)
	}
	return disks, nil
}

// softDeleteTables lists the ReplacingMergeTree config tables that carry a
// `deleted` tombstone column and can therefore be physically purged.
var softDeleteTables = []string{"alert_rules", "webhook_configs", "hostgroups", "report_schedules"}

// PurgeSoftDeleted physically removes tombstoned (deleted = 1) rows older than
// `days` from the config tables via lightweight ALTER ... DELETE mutations.
// OPTIMIZE FINAL is intentionally not forced (expensive). Tables absent from the
// database (feature-gated) are skipped. Affected row counts are logged.
func (s *ClickHouseStore) PurgeSoftDeleted(ctx context.Context, days int) error {
	if days < 1 {
		return fmt.Errorf("days must be >= 1")
	}
	present, err := s.presentTables(ctx)
	if err != nil {
		return err
	}

	for _, table := range softDeleteTables {
		if !present[table] {
			continue
		}

		// Count matching tombstones first (FINAL so superseded versions don't
		// inflate the count) for an informative log line.
		var n uint64
		countQ := fmt.Sprintf(`
			SELECT count() FROM %s FINAL
			WHERE deleted = 1 AND updated_at < now() - INTERVAL %d DAY
		`, table, days)
		if err := s.conn.QueryRow(ctx, countQ).Scan(&n); err != nil {
			log.Printf("retention: purge %s count failed: %v", table, err)
			continue
		}
		if n == 0 {
			continue
		}

		delQ := fmt.Sprintf(`
			ALTER TABLE %s DELETE
			WHERE deleted = 1 AND updated_at < now() - INTERVAL %d DAY
		`, table, days)
		if err := s.conn.Exec(ctx, delQ); err != nil {
			log.Printf("retention: purge %s failed: %v", table, err)
			continue
		}
		log.Printf("retention: purged %d tombstoned row(s) from %s (older than %d day(s))", n, table, days)
	}
	return nil
}
