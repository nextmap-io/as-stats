package migrate

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	historyTable = "schema_migrations"
	lockTable    = "schema_migration_lock"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ClickHouseStore persists append-only migration events and coordinates DDL.
type ClickHouseStore struct {
	conn     driver.Conn
	database string
	quotedDB string
}

// NewClickHouseStore creates a migration store for a validated database name.
func NewClickHouseStore(conn driver.Conn, database string) (*ClickHouseStore, error) {
	if conn == nil {
		return nil, fmt.Errorf("nil ClickHouse connection")
	}
	if !identifierPattern.MatchString(database) {
		return nil, fmt.Errorf("invalid ClickHouse database identifier %q", database)
	}
	return &ClickHouseStore{conn: conn, database: database, quotedDB: "`" + database + "`"}, nil
}

func (s *ClickHouseStore) table(name string) string { return s.quotedDB + ".`" + name + "`" }

// AcquireLock relies on atomic CREATE TABLE semantics. Unlike a row in a
// MergeTree, this has one unambiguous winner even when two migrators race.
// A crash intentionally leaves the lock behind for manual investigation.
func (s *ClickHouseStore) AcquireLock(ctx context.Context, owner string) error {
	query := fmt.Sprintf(`CREATE TABLE %s (
		owner String,
		acquired_at DateTime64(9, 'UTC')
	) ENGINE = TinyLog`, s.table(lockTable))
	if err := s.conn.Exec(ctx, query); err != nil {
		var currentOwner string
		var acquiredAt time.Time
		metaErr := s.conn.QueryRow(ctx, fmt.Sprintf("SELECT owner, acquired_at FROM %s LIMIT 1", s.table(lockTable))).Scan(&currentOwner, &acquiredAt)
		if metaErr == nil {
			return fmt.Errorf("lock held by %q since %s (a stale lock must be inspected and dropped manually): %w", currentOwner, acquiredAt.UTC().Format(time.RFC3339Nano), err)
		}
		return fmt.Errorf("exclusive lock table already exists or could not be created: %w", err)
	}
	if err := s.conn.Exec(ctx, fmt.Sprintf("INSERT INTO %s (owner, acquired_at) VALUES (?, ?)", s.table(lockTable)), owner, time.Now().UTC()); err != nil {
		_ = s.conn.Exec(context.WithoutCancel(ctx), fmt.Sprintf("DROP TABLE IF EXISTS %s", s.table(lockTable)))
		return fmt.Errorf("write lock metadata: %w", err)
	}
	return nil
}

func (s *ClickHouseStore) ReleaseLock(ctx context.Context, owner string) error {
	var currentOwner string
	if err := s.conn.QueryRow(ctx, fmt.Sprintf("SELECT owner FROM %s LIMIT 1", s.table(lockTable))).Scan(&currentOwner); err != nil {
		return fmt.Errorf("verify lock owner: %w", err)
	}
	if currentOwner != owner {
		return fmt.Errorf("refusing to release lock owned by %q", currentOwner)
	}
	return s.conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", s.table(lockTable)))
}

func (s *ClickHouseStore) EnsureHistory(ctx context.Context) error {
	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version UInt64,
		name String,
		checksum FixedString(64),
		status LowCardinality(String),
		owner String,
		recorded_at DateTime64(9, 'UTC'),
		error String
	) ENGINE = MergeTree
	ORDER BY (version, recorded_at)`, s.table(historyTable))
	return s.conn.Exec(ctx, query)
}

func (s *ClickHouseStore) History(ctx context.Context) ([]Record, error) {
	query := fmt.Sprintf(`SELECT version, name, checksum, status, error
	FROM %s
	ORDER BY version, recorded_at`, s.table(historyTable))
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	latest := make(map[uint64]Record)
	var order []uint64
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.Version, &record.Name, &record.Checksum, &record.Status, &record.Error); err != nil {
			return nil, err
		}
		if _, exists := latest[record.Version]; !exists {
			order = append(order, record.Version)
		}
		latest[record.Version] = record
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(order))
	for _, version := range order {
		out = append(out, latest[version])
	}
	return out, nil
}

func (s *ClickHouseStore) HasUserSchema(ctx context.Context) (bool, error) {
	var count uint64
	query := `SELECT count()
		FROM system.tables
		WHERE database = ?
		  AND name NOT IN (?, ?)
		  AND is_temporary = 0`
	if err := s.conn.QueryRow(ctx, query, s.database, historyTable, lockTable).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *ClickHouseStore) Append(ctx context.Context, record Record, owner string) error {
	query := fmt.Sprintf(`INSERT INTO %s
		(version, name, checksum, status, owner, recorded_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, s.table(historyTable))
	return s.conn.Exec(ctx, query, record.Version, record.Name, record.Checksum, record.Status, owner, time.Now().UTC(), record.Error)
}

// Exec rewrites only the historical hard-coded schema qualifier. Checksums are
// always calculated over the original embedded file, not this runtime form.
func (s *ClickHouseStore) Exec(ctx context.Context, statement string) error {
	statement = strings.ReplaceAll(statement, "asstats.", s.quotedDB+".")
	statement = strings.ReplaceAll(statement, "CREATE DATABASE IF NOT EXISTS asstats", "CREATE DATABASE IF NOT EXISTS "+s.quotedDB)
	return s.conn.Exec(ctx, statement)
}

type schemaProbe struct {
	version uint64
	label   string
	check   func(context.Context) (bool, error)
}

// DetectBaseline recognizes every released schema level using objects or
// columns introduced by that migration. All preceding probes must also match;
// holes and partial migrations are rejected as ambiguous.
func (s *ClickHouseStore) DetectBaseline(ctx context.Context) (uint64, error) {
	objects := func(names ...string) func(context.Context) (bool, error) {
		return func(ctx context.Context) (bool, error) {
			for _, name := range names {
				exists, err := s.hasTable(ctx, name)
				if err != nil || !exists {
					return false, err
				}
			}
			return true, nil
		}
	}
	columns := func(table string, names ...string) func(context.Context) (bool, error) {
		return func(ctx context.Context) (bool, error) {
			for _, name := range names {
				exists, err := s.hasColumn(ctx, table, name)
				if err != nil || !exists {
					return false, err
				}
			}
			return true, nil
		}
	}
	all := func(checks ...func(context.Context) (bool, error)) func(context.Context) (bool, error) {
		return func(ctx context.Context) (bool, error) {
			for _, check := range checks {
				ok, err := check(ctx)
				if err != nil || !ok {
					return false, err
				}
			}
			return true, nil
		}
	}

	probes := []schemaProbe{
		{1, "core schema", objects("flows_raw", "traffic_by_as", "traffic_by_as_out_mv", "traffic_by_as_in_mv", "traffic_by_ip", "traffic_by_ip_in_mv", "traffic_by_ip_out_mv", "traffic_by_prefix", "traffic_by_prefix_in_mv", "traffic_by_prefix_out_mv", "traffic_by_link", "traffic_by_link_mv", "traffic_by_as_hourly", "traffic_by_as_hourly_out_mv", "traffic_by_as_hourly_in_mv", "traffic_by_ip_as", "traffic_by_ip_as_in_mv", "traffic_by_ip_as_out_mv")},
		{2, "configuration tables", objects("links", "as_names")},
		{3, "link IP version", columns("traffic_by_link", "ip_version")},
		{4, "progressive retention rollups", objects("traffic_by_link_hourly", "traffic_by_link_hourly_mv", "traffic_by_as_daily", "traffic_by_as_daily_out_mv", "traffic_by_as_daily_in_mv", "traffic_by_link_daily", "traffic_by_link_daily_mv")},
		{5, "AS IP version", all(columns("traffic_by_as", "ip_version"), columns("traffic_by_as_hourly", "ip_version"), columns("traffic_by_as_daily", "ip_version"))},
		{6, "link colour", columns("links", "color")},
		{7, "flow log and port statistics", objects("flows_log", "flows_log_mv", "traffic_by_port", "traffic_by_port_in_mv", "traffic_by_port_out_mv")},
		{8, "hot aggregates", objects("traffic_by_dst_1min", "traffic_by_dst_1min_mv", "traffic_by_src_1min", "traffic_by_src_1min_mv")},
		{9, "alerts and audit", objects("alert_rules", "alerts", "audit_log", "webhook_configs")},
		{10, "host groups", all(objects("hostgroups"), columns("alert_rules", "hostgroup_id", "subnet_prefix_len"))},
		{11, "BGP blocks", objects("bgp_blocks")},
		{12, "retention policies", objects("retention_policies")},
		{13, "report schedules", objects("report_schedules")},
		{14, "API tokens", objects("api_tokens")},
		{15, "summable hot aggregate counters", s.hotCountersFixed},
	}

	var baseline uint64
	missing := false
	for _, probe := range probes {
		ok, err := probe.check(ctx)
		if err != nil {
			return 0, fmt.Errorf("probe %06d (%s): %w", probe.version, probe.label, err)
		}
		if !ok {
			missing = true
			continue
		}
		if missing {
			return 0, fmt.Errorf("schema is non-contiguous or partially applied: migration %06d marker %q exists after an earlier marker is missing", probe.version, probe.label)
		}
		baseline = probe.version
	}
	return baseline, nil
}

func (s *ClickHouseStore) hasTable(ctx context.Context, table string) (bool, error) {
	var count uint64
	err := s.conn.QueryRow(ctx, `SELECT count() FROM system.tables WHERE database = ? AND name = ?`, s.database, table).Scan(&count)
	return count == 1, err
}

func (s *ClickHouseStore) hasColumn(ctx context.Context, table, column string) (bool, error) {
	var count uint64
	err := s.conn.QueryRow(ctx, `SELECT count() FROM system.columns WHERE database = ? AND table = ? AND name = ?`, s.database, table, column).Scan(&count)
	return count == 1, err
}

func (s *ClickHouseStore) hotCountersFixed(ctx context.Context) (bool, error) {
	checks := map[string][]string{
		"traffic_by_dst_1min": {"bytes", "packets", "flow_count", "syn_count"},
		"traffic_by_src_1min": {"bytes", "packets", "flow_count"},
	}
	fixed, total := 0, 0
	for table, columns := range checks {
		for _, column := range columns {
			var columnType string
			var count uint64
			err := s.conn.QueryRow(ctx, `SELECT any(type), count() FROM system.columns WHERE database = ? AND table = ? AND name = ?`, s.database, table, column).Scan(&columnType, &count)
			if err != nil {
				return false, err
			}
			if count != 1 {
				return false, nil
			}
			total++
			if columnType == "SimpleAggregateFunction(sum, UInt64)" {
				fixed++
			}
		}
	}
	if fixed > 0 && fixed < total {
		return false, fmt.Errorf("migration 000015 is partially applied: %d/%d counter columns have the new type", fixed, total)
	}
	return fixed == total, nil
}
