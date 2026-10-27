package store

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/model"
)

// parseErrors is the server-wide count of fast-VALUES-parser rejections.
func parseErrors(t *testing.T, s *ClickHouseStore) uint64 {
	t.Helper()
	var n uint64
	if err := s.conn.QueryRow(context.Background(), `
		SELECT toUInt64(sum(value)) FROM system.errors WHERE name = 'CANNOT_PARSE_INPUT_ASSERTION_FAILED'
	`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The alert writes go through the native protocol: no INSERT … VALUES with
// rendered literals, so no CANNOT_PARSE_INPUT_ASSERTION_FAILED per insert
// (~16k a day in production). Then the lifecycle the engine relies on:
// LatestAlert sees the newest alert in any status, ReopenAlert re-arms it.
func TestIntegrationAlertWritesAndLifecycle(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()

	// What 3.6 did, for the record: one named-parameter INSERT … VALUES.
	before := parseErrors(t, s)
	if err := s.conn.Exec(ctx, `
		INSERT INTO alerts (id, rule_id, rule_name, severity, triggered_at, last_seen_at, target_ip,
			metric_value, threshold, metric_type, status)
		VALUES (@id, @rule_id, 'legacy', 'info', @ts, @ts, @ip, @v, @th, 'bps', 'resolved')
	`, clickhouse.Named("id", uuid.NewString()), clickhouse.Named("rule_id", uuid.NewString()),
		clickhouse.Named("ts", time.Now().UTC()), clickhouse.Named("ip", ipToIPv6(net.ParseIP("192.0.2.250"))),
		clickhouse.Named("v", 5e8), clickhouse.Named("th", 5e8)); err != nil {
		t.Fatal(err)
	}
	t.Logf("named INSERT … VALUES: CANNOT_PARSE_INPUT_ASSERTION_FAILED +%d", parseErrors(t, s)-before)

	ruleID := uuid.NewString()
	target := net.ParseIP("192.0.2.251")
	now := time.Now().UTC().Truncate(time.Second)
	before = parseErrors(t, s)
	a := model.Alert{
		ID: uuid.NewString(), RuleID: ruleID, RuleName: "it", Severity: "warning",
		TriggeredAt: now.Add(-10 * time.Minute), LastSeenAt: now.Add(-9 * time.Minute),
		TargetIP: target.String(), MetricValue: 6e8, Threshold: 5e8, MetricType: "bps", Details: `{"window_seconds":60}`,
	}
	if err := s.InsertAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	rule := model.AlertRule{ID: ruleID, Name: "it rule", RuleType: "volume_in", Enabled: true, ThresholdBps: 5e8,
		WindowSeconds: 60, CooldownSeconds: 300, Severity: "warning", Action: "notify", BaselineFactor: 2.5,
		CreatedAt: now, UpdatedAt: now}
	if err := s.UpsertAlertRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteAuditLog(ctx, model.AuditLogEntry{UserEmail: "it@example.org", Action: "rule_update", Resource: "rules/" + ruleID, Result: "success", ClientIP: "192.0.2.9"}); err != nil {
		t.Fatal(err)
	}
	// system.errors is server-wide: another test package running in parallel
	// (go test runs packages concurrently) can bump it. Retry with throwaway
	// writes and fail only if no attempt is clean.
	d := parseErrors(t, s) - before
	for attempt := 0; d != 0 && attempt < 3; attempt++ {
		time.Sleep(200 * time.Millisecond)
		before = parseErrors(t, s)
		probe := a
		probe.ID, probe.RuleID, probe.TargetIP = uuid.NewString(), uuid.NewString(), "192.0.2.252"
		if err := s.InsertAlert(ctx, probe); err != nil {
			t.Fatal(err)
		}
		if err := s.WriteAuditLog(ctx, model.AuditLogEntry{UserEmail: "it@example.org", Action: "rule_update", Resource: "rules/probe", Result: "success", ClientIP: "192.0.2.9"}); err != nil {
			t.Fatal(err)
		}
		d = parseErrors(t, s) - before
	}
	if d != 0 {
		t.Errorf("native inserts raised CANNOT_PARSE_INPUT_ASSERTION_FAILED %d times", d)
	}

	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rules {
		if r.ID == ruleID {
			found = true
			if r.BaselineFactor != 2.5 {
				t.Errorf("baseline_factor round trip: %v", r.BaselineFactor)
			}
		}
	}
	if !found {
		t.Error("upserted rule not listed")
	}

	st, err := s.LatestAlert(ctx, ruleID, target)
	if err != nil {
		t.Fatal(err)
	}
	if st.ID != a.ID || st.Status != "active" || st.MetricValue != 6e8 || !st.LastSeenAt.Equal(a.LastSeenAt) {
		t.Fatalf("LatestAlert = %+v, want the inserted alert", st)
	}
	if none, err := s.LatestAlert(ctx, ruleID, net.ParseIP("192.0.2.252")); err != nil || none.ID != "" {
		t.Fatalf("LatestAlert for another target = %+v, %v; want none", none, err)
	}

	// Auto-resolve (5 min stale), then re-open with a higher value.
	if err := s.AutoResolveStaleAlerts(ctx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	waitStatus := func(want string) AlertState {
		t.Helper()
		var st AlertState
		for i := 0; i < 100; i++ { // mutations are asynchronous
			if st, err = s.LatestAlert(ctx, ruleID, target); err != nil {
				t.Fatal(err)
			}
			if st.Status == want {
				return st
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("status %q, want %q", st.Status, want)
		return st
	}
	waitStatus("resolved")
	if err := s.ReopenAlert(ctx, a.ID, now, 7e8); err != nil {
		t.Fatal(err)
	}
	st = waitStatus("active")
	if st.MetricValue != 7e8 || !st.LastSeenAt.Equal(now) {
		t.Errorf("re-opened alert = %+v, want peak 7e8 and last_seen %v", st, now)
	}
	list, err := s.ListAlerts(ctx, "active", 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range list {
		if l.ID == a.ID && (l.ReopenCount != 1 || l.ResolvedAt != nil) {
			t.Errorf("listed alert: reopen_count %d resolved_at %v, want 1 and none", l.ReopenCount, l.ResolvedAt)
		}
	}
}
