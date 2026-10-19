package store

import (
	"context"
	"strings"
	"testing"

	"github.com/nextmap-io/as-stats/internal/model"
)

// TestRetentionTablesWhitelist verifies the centralized table→TTL-column map
// matches the columns the migrations actually key their TTL on. Getting this
// wrong would make the reconciler issue ALTER MODIFY TTL against a non-existent
// column and silently fail every cycle.
func TestRetentionTablesWhitelist(t *testing.T) {
	want := map[string]struct {
		col  string
		expr string
		days uint32
	}{
		"flows_raw":              {"timestamp", "toDateTime(timestamp)", 3},
		"traffic_by_as":          {"ts", "ts", 90},
		"traffic_by_as_hourly":   {"ts", "ts", 730},
		"traffic_by_as_daily":    {"ts", "ts", 1825},
		"traffic_by_link":        {"ts", "ts", 90},
		"traffic_by_link_hourly": {"ts", "ts", 730},
		"traffic_by_link_daily":  {"ts", "ts", 1825},
		"traffic_by_ip":          {"ts", "ts", 14},
		"traffic_by_ip_as":       {"ts", "ts", 14},
		"traffic_by_prefix":      {"ts", "ts", 30},
		"flows_log":              {"ts", "ts", 180},
		"traffic_by_port":        {"ts", "ts", 365},
		"traffic_by_dst_1min":    {"ts", "ts", 7},
		"traffic_by_src_1min":    {"ts", "ts", 7},
		"alerts":                 {"triggered_at", "triggered_at", 365},
		"audit_log":              {"ts", "ts", 365},
		"bgp_blocks":             {"blocked_at", "blocked_at", 365},
	}

	if len(retentionTables) != len(want) {
		t.Fatalf("retentionTables has %d entries, want %d", len(retentionTables), len(want))
	}
	for table, exp := range want {
		got, ok := retentionTables[table]
		if !ok {
			t.Errorf("missing table %q in retentionTables", table)
			continue
		}
		if got.TTLColumn != exp.col {
			t.Errorf("%s: TTLColumn = %q, want %q", table, got.TTLColumn, exp.col)
		}
		if got.TTLExpr != exp.expr {
			t.Errorf("%s: TTLExpr = %q, want %q", table, got.TTLExpr, exp.expr)
		}
		if got.DefaultDays != exp.days {
			t.Errorf("%s: DefaultDays = %d, want %d", table, got.DefaultDays, exp.days)
		}
	}
}

func TestBuildModifyTTLStatement(t *testing.T) {
	// flows_raw wraps its DateTime64 column in toDateTime().
	stmt, ok := buildModifyTTLStatement("flows_raw", 7, 0)
	if !ok {
		t.Fatal("expected ok for flows_raw")
	}
	if stmt != "ALTER TABLE flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 7 DAY SETTINGS materialize_ttl_after_modify = 0" {
		t.Errorf("unexpected flows_raw stmt: %q", stmt)
	}

	// Regular table uses the bare column.
	stmt, ok = buildModifyTTLStatement("traffic_by_ip", 21, 0)
	if !ok {
		t.Fatal("expected ok for traffic_by_ip")
	}
	if stmt != "ALTER TABLE traffic_by_ip MODIFY TTL ts + INTERVAL 21 DAY SETTINGS materialize_ttl_after_modify = 0" {
		t.Errorf("unexpected traffic_by_ip stmt: %q", stmt)
	}

	// Cold tier: the move clause comes first, the DELETE interval last.
	stmt, ok = buildModifyTTLStatement("flows_log", 180, 14)
	if !ok {
		t.Fatal("expected ok for flows_log with a cold tier")
	}
	if stmt != "ALTER TABLE flows_log MODIFY TTL ts + INTERVAL 14 DAY TO VOLUME 'cold', ts + INTERVAL 180 DAY SETTINGS materialize_ttl_after_modify = 0" {
		t.Errorf("unexpected flows_log cold stmt: %q", stmt)
	}
	stmt, ok = buildModifyTTLStatement("flows_raw", 3, 1)
	if !ok || stmt != "ALTER TABLE flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 1 DAY TO VOLUME 'cold', toDateTime(timestamp) + INTERVAL 3 DAY SETTINGS materialize_ttl_after_modify = 0" {
		t.Errorf("unexpected flows_raw cold stmt: %q ok=%v", stmt, ok)
	}

	// A cold tier that is not strictly shorter than the retention is refused.
	for _, cold := range []uint32{180, 181} {
		if _, ok := buildModifyTTLStatement("flows_log", 180, cold); ok {
			t.Errorf("cold_after_days=%d with ttl_days=180 must be refused", cold)
		}
	}

	// Regression guard: the setting is what keeps the reconciler from rewriting
	// whole tables (see buildModifyTTLStatement). Never drop it — with or
	// without a cold tier.
	for _, table := range []string{"flows_raw", "flows_log", "traffic_by_port"} {
		for _, cold := range []uint32{0, 7} {
			s, ok := buildModifyTTLStatement(table, 30, cold)
			if !ok {
				t.Fatalf("expected ok for %s (cold %d)", table, cold)
			}
			if !strings.HasSuffix(s, " SETTINGS materialize_ttl_after_modify = 0") {
				t.Errorf("%s: MODIFY TTL must pin materialize_ttl_after_modify=0, got %q", table, s)
			}
		}
	}

	// Unknown table must be rejected.
	if _, ok := buildModifyTTLStatement("not_a_table; DROP TABLE x", 1, 0); ok {
		t.Error("expected ok=false for unknown table")
	}
}

func TestValidateRetention(t *testing.T) {
	for _, tc := range []struct {
		days, cold uint32
		ok         bool
	}{
		{180, 0, true},
		{180, 1, true},
		{180, 179, true},
		{180, 180, false},
		{180, 365, false},
		{0, 0, false},
		{1, 0, true},
		{1, 1, false},
	} {
		err := validateRetention(tc.days, tc.cold)
		if (err == nil) != tc.ok {
			t.Errorf("validateRetention(%d, %d) = %v, want ok=%v", tc.days, tc.cold, err, tc.ok)
		}
	}
}

// Real create_table_query values captured from clickhouse-server 24.3.18
// (system.tables, FORMAT TSVRaw) after the reconciler's ALTERs.
const (
	ch243FlowsLogCold  = "CREATE TABLE asstats.flows_log (`ts` DateTime('UTC'), `router_ip` IPv6, `link_tag` LowCardinality(String), `src_ip` IPv6, `dst_ip` IPv6, `src_as` UInt32, `dst_as` UInt32, `protocol` UInt8, `src_port` UInt16, `dst_port` UInt16, `tcp_flags` UInt8, `ip_version` UInt8, `bytes` UInt64, `packets` UInt64, `flow_count` UInt64, INDEX idx_src_ip src_ip TYPE bloom_filter(0.01) GRANULARITY 4, INDEX idx_dst_ip dst_ip TYPE bloom_filter(0.01) GRANULARITY 4, INDEX idx_src_as src_as TYPE minmax GRANULARITY 4, INDEX idx_dst_as dst_as TYPE minmax GRANULARITY 4) ENGINE = SummingMergeTree((bytes, packets, flow_count)) PARTITION BY toYYYYMMDD(ts) ORDER BY (ts, link_tag, protocol, dst_port, src_ip, dst_ip, src_as, dst_as, src_port, tcp_flags, ip_version) TTL ts + toIntervalDay(14) TO VOLUME 'cold', ts + toIntervalDay(180) SETTINGS index_granularity = 8192, storage_policy = 'hot_cold', ttl_only_drop_parts = 1"
	ch243FlowsRawCold  = "CREATE TABLE asstats.flows_raw (`timestamp` DateTime64(3, 'UTC'), `router_ip` IPv6, `link_tag` LowCardinality(String) DEFAULT '', `src_ip` IPv6, `dst_ip` IPv6, `ip_version` UInt8 DEFAULT 4, `src_as` UInt32 DEFAULT 0, `dst_as` UInt32 DEFAULT 0, `src_prefix` String DEFAULT '', `dst_prefix` String DEFAULT '', `protocol` UInt8 DEFAULT 0, `src_port` UInt16 DEFAULT 0, `dst_port` UInt16 DEFAULT 0, `tcp_flags` UInt8 DEFAULT 0, `bytes` UInt64, `packets` UInt64, `sampling_rate` UInt32 DEFAULT 1, `direction` LowCardinality(String) DEFAULT '', `flow_type` LowCardinality(String) DEFAULT 'netflow') ENGINE = MergeTree PARTITION BY toYYYYMMDD(timestamp) ORDER BY (timestamp, router_ip, src_as, dst_as) TTL toDateTime(timestamp) + toIntervalDay(1) TO VOLUME 'cold', toDateTime(timestamp) + toIntervalDay(3) SETTINGS index_granularity = 8192, storage_policy = 'hot_cold'"
	ch243TrafficByPort = "CREATE TABLE asstats.traffic_by_port (`ts` DateTime('UTC'), `link_tag` LowCardinality(String), `direction` LowCardinality(String), `protocol` UInt8, `port` UInt16, `ip_version` UInt8, `bytes` UInt64, `packets` UInt64, `flow_count` UInt64) ENGINE = SummingMergeTree((bytes, packets, flow_count)) PARTITION BY toYYYYMM(ts) ORDER BY (ts, link_tag, direction, protocol, port, ip_version) TTL ts + toIntervalDay(365) SETTINGS index_granularity = 8192"
	ch243Alerts        = "CREATE TABLE asstats.alerts (`id` UUID, `rule_id` UUID, `rule_name` String, `severity` LowCardinality(String), `triggered_at` DateTime('UTC'), `last_seen_at` DateTime('UTC'), `resolved_at` DateTime('UTC') DEFAULT toDateTime(0), `target_ip` IPv6, `target_as` UInt32 DEFAULT 0, `protocol` UInt8 DEFAULT 0, `metric_value` Float64, `threshold` Float64, `metric_type` LowCardinality(String), `details` String DEFAULT '', `status` LowCardinality(String) DEFAULT 'active', `acknowledged_by` String DEFAULT '', `acknowledged_at` DateTime('UTC') DEFAULT toDateTime(0), `action_taken` LowCardinality(String) DEFAULT 'none', `action_by` String DEFAULT '', `action_at` DateTime('UTC') DEFAULT toDateTime(0)) ENGINE = MergeTree PARTITION BY toYYYYMM(triggered_at) ORDER BY (triggered_at, severity, id) TTL triggered_at + toIntervalDay(365) SETTINGS index_granularity = 8192"
)

// TestParseTableTTL pins the divergence parser on real 24.3 normalised create
// queries: the DELETE interval is the one without TO VOLUME (liveTTLDays must
// never read the move interval), and matches() compares both intervals and the
// move clause.
func TestParseTableTTL(t *testing.T) {
	tests := []struct {
		name               string
		query              string
		deleteDays, coldDs uint32
		matchDays, matchC  uint32
	}{
		{"flows_log cold", ch243FlowsLogCold, 180, 14, 180, 14},
		{"flows_raw cold", ch243FlowsRawCold, 3, 1, 3, 1},
		{"traffic_by_port plain", ch243TrafficByPort, 365, 0, 365, 0},
		{"alerts plain", ch243Alerts, 365, 0, 365, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTableTTL(tc.query)
			if !ok {
				t.Fatal("no TTL found")
			}
			if got.DeleteDays != tc.deleteDays || got.ColdDays != tc.coldDs || got.Deletes != 1 || got.Others != 0 {
				t.Fatalf("parseTableTTL = %+v, want delete %d cold %d", got, tc.deleteDays, tc.coldDs)
			}
			if !got.matches(tc.matchDays, tc.matchC) {
				t.Errorf("matches(%d, %d) = false, want true", tc.matchDays, tc.matchC)
			}
		})
	}

	cold, _ := parseTableTTL(ch243FlowsLogCold)
	plain, _ := parseTableTTL(ch243TrafficByPort)
	for _, tc := range []struct {
		name       string
		ttl        tableTTL
		days, cold uint32
	}{
		{"delete interval changed", cold, 90, 14},
		{"cold interval changed", cold, 180, 30},
		{"cold tier removed", cold, 180, 0},
		{"cold tier added", plain, 365, 30},
		{"plain interval changed", plain, 30, 0},
		// The move interval must never be taken for the retention.
		{"move interval is not the retention", cold, 14, 0},
	} {
		if tc.ttl.matches(tc.days, tc.cold) {
			t.Errorf("%s: matches(%d, %d) = true, want divergence", tc.name, tc.days, tc.cold)
		}
	}

	// Anything the reconciler would not have written counts as divergence.
	other := strings.Replace(ch243FlowsLogCold, "TO VOLUME 'cold'", "TO DISK 'cold'", 1)
	if got, ok := parseTableTTL(other); !ok || got.Others != 1 || got.matches(180, 14) || got.matches(180, 0) {
		t.Errorf("TO DISK clause: %+v ok=%v, want Others=1 and no match", got, ok)
	}
	if _, ok := parseTableTTL(strings.Replace(ch243TrafficByPort, " TTL ts + toIntervalDay(365)", "", 1)); ok {
		t.Error("query without TTL must report ok=false")
	}
	// A column-level TTL (inside the column list) is not the table TTL.
	colTTL := "CREATE TABLE x (`ts` DateTime, `v` UInt8 TTL ts + toIntervalDay(1)) ENGINE = MergeTree ORDER BY ts SETTINGS index_granularity = 8192"
	if _, ok := parseTableTTL(colTTL); ok {
		t.Error("column TTL must not be read as the table TTL")
	}
}

func TestSplitTopLevel(t *testing.T) {
	got := splitTopLevel("toDateTime(a, 'x,y') + toIntervalDay(1) TO VOLUME 'c,d', b")
	if len(got) != 2 || got[1] != " b" {
		t.Fatalf("splitTopLevel = %q", got)
	}
}

func TestFullestDisk(t *testing.T) {
	disks := []model.DiskStats{
		{Name: "cold", TotalBytes: 300, UsedPercent: 91},
		{Name: "default", TotalBytes: 100, UsedPercent: 40},
		{Name: "s3", TotalBytes: 0, UsedPercent: 0},
	}
	d, ok := fullestDisk(disks)
	if !ok || d.Name != "cold" {
		t.Fatalf("fullestDisk = %+v ok=%v, want the cold disk", d, ok)
	}
	if _, ok := fullestDisk([]model.DiskStats{{Name: "s3"}}); ok {
		t.Error("no sized disk must report ok=false")
	}
}

func TestEffectiveColdDays(t *testing.T) {
	s := &ClickHouseStore{}
	p := model.RetentionPolicy{TableName: "flows_log", TTLDays: 180, ColdAfterDays: 14}
	if got := s.effectiveColdDays(p, []string{"cold"}); got != 14 {
		t.Errorf("with a cold volume: %d, want 14", got)
	}
	if got := s.effectiveColdDays(p, nil); got != 0 {
		t.Errorf("without a cold volume: %d, want 0", got)
	}
	if !s.coldWarned["flows_log"] {
		t.Error("missing cold volume must be logged (once)")
	}
	p.ColdAfterDays = 180
	if got := s.effectiveColdDays(p, []string{"cold"}); got != 0 {
		t.Errorf("cold >= ttl: %d, want 0", got)
	}
}

// TestSetRetentionPolicyRejectsUnknownTable verifies the whitelist guard fires
// before any DB access (so it is safe to call with a nil connection).
func TestSetRetentionPolicyRejectsUnknownTable(t *testing.T) {
	s := &ClickHouseStore{} // nil conn — guard must short-circuit before use
	_, err := s.SetRetentionPolicy(context.Background(), "../etc/passwd", 30, true, nil)
	if err == nil {
		t.Fatal("expected error for unknown table")
	}
	if !strings.Contains(err.Error(), "unknown table") {
		t.Errorf("expected 'unknown table' error, got %v", err)
	}
}

// TestSetRetentionPolicyRejectsZeroDays verifies the days floor is validated
// before DB access.
func TestSetRetentionPolicyRejectsZeroDays(t *testing.T) {
	s := &ClickHouseStore{}
	_, err := s.SetRetentionPolicy(context.Background(), "flows_raw", 0, true, nil)
	if err == nil {
		t.Fatal("expected error for zero days")
	}
	if !strings.Contains(err.Error(), "ttl_days") {
		t.Errorf("expected 'ttl_days' error, got %v", err)
	}
}

// TestSetRetentionPolicyRejectsColdNotBelowTTL: an explicit cold tier is
// validated before any DB access.
func TestSetRetentionPolicyRejectsColdNotBelowTTL(t *testing.T) {
	s := &ClickHouseStore{}
	cold := uint32(30)
	_, err := s.SetRetentionPolicy(context.Background(), "flows_log", 30, true, &cold)
	if err == nil || !strings.Contains(err.Error(), "cold_after_days") {
		t.Fatalf("expected a cold_after_days error, got %v", err)
	}
}

// TestBuildDropPartitionStatement guards the only place a partition ID is
// interpolated into DDL: unknown tables and anything that is not a plain
// YYYYMM / YYYYMMDD id must be refused.
func TestBuildDropPartitionStatement(t *testing.T) {
	stmt, ok := buildDropPartitionStatement("flows_log", "20260630")
	if !ok || stmt != "ALTER TABLE flows_log DROP PARTITION ID '20260630'" {
		t.Fatalf("daily partition: got %q ok=%v", stmt, ok)
	}
	if _, ok := buildDropPartitionStatement("traffic_by_as", "202604"); !ok {
		t.Error("monthly partition id should be accepted")
	}
	for _, bad := range []string{"", "2026", "20260630'; DROP TABLE x --", "all", "tuple()", "202606301"} {
		if _, ok := buildDropPartitionStatement("flows_log", bad); ok {
			t.Errorf("partition id %q must be refused", bad)
		}
	}
	if _, ok := buildDropPartitionStatement("not_a_table", "20260630"); ok {
		t.Error("unknown table must be refused")
	}
}

// TestBuildMovePartitionToColdStatement: same whitelist as DROP PARTITION —
// the partition id is the only interpolated value besides whitelisted names.
func TestBuildMovePartitionToColdStatement(t *testing.T) {
	stmt, ok := buildMovePartitionToColdStatement("flows_log", "20260630")
	if !ok || stmt != "ALTER TABLE flows_log MOVE PARTITION ID '20260630' TO VOLUME 'cold'" {
		t.Fatalf("daily partition: got %q ok=%v", stmt, ok)
	}
	if stmt, ok := buildMovePartitionToColdStatement("traffic_by_port", "202604"); !ok || stmt != "ALTER TABLE traffic_by_port MOVE PARTITION ID '202604' TO VOLUME 'cold'" {
		t.Errorf("monthly partition: got %q ok=%v", stmt, ok)
	}
	for _, bad := range []string{"", "2026", "20260630' TO VOLUME 'x'; DROP TABLE x --", "all", "tuple()", "202606301"} {
		if _, ok := buildMovePartitionToColdStatement("flows_log", bad); ok {
			t.Errorf("partition id %q must be refused", bad)
		}
	}
	if _, ok := buildMovePartitionToColdStatement("system.tables", "20260630"); ok {
		t.Error("unknown table must be refused")
	}
}
