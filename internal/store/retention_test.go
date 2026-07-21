package store

import (
	"context"
	"strings"
	"testing"
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
	stmt, ok := buildModifyTTLStatement("flows_raw", 7)
	if !ok {
		t.Fatal("expected ok for flows_raw")
	}
	if stmt != "ALTER TABLE flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 7 DAY" {
		t.Errorf("unexpected flows_raw stmt: %q", stmt)
	}

	// Regular table uses the bare column.
	stmt, ok = buildModifyTTLStatement("traffic_by_ip", 21)
	if !ok {
		t.Fatal("expected ok for traffic_by_ip")
	}
	if stmt != "ALTER TABLE traffic_by_ip MODIFY TTL ts + INTERVAL 21 DAY" {
		t.Errorf("unexpected traffic_by_ip stmt: %q", stmt)
	}

	// Unknown table must be rejected.
	if _, ok := buildModifyTTLStatement("not_a_table; DROP TABLE x", 1); ok {
		t.Error("expected ok=false for unknown table")
	}
}

// TestSetRetentionPolicyRejectsUnknownTable verifies the whitelist guard fires
// before any DB access (so it is safe to call with a nil connection).
func TestSetRetentionPolicyRejectsUnknownTable(t *testing.T) {
	s := &ClickHouseStore{} // nil conn — guard must short-circuit before use
	err := s.SetRetentionPolicy(context.Background(), "../etc/passwd", 30, true)
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
	err := s.SetRetentionPolicy(context.Background(), "flows_raw", 0, true)
	if err == nil {
		t.Fatal("expected error for zero days")
	}
	if !strings.Contains(err.Error(), "ttl_days") {
		t.Errorf("expected 'ttl_days' error, got %v", err)
	}
}
