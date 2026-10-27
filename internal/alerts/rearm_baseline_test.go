package alerts

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

func volumeRule(factor float64) model.AlertRule {
	return model.AlertRule{ID: "r-vol", Name: "High inbound volume", RuleType: "volume_in", Enabled: true,
		ThresholdBps: 500_000_000, WindowSeconds: 60, CooldownSeconds: 300, Severity: "warning",
		BaselineFactor: factor, WebhookIDs: []string{"wh"}}
}

func rearmStore(latest *store.AlertState, value float64) (*mockStore, *mockNotifier) {
	return &mockStore{
		rules:    []model.AlertRule{volumeRule(0)},
		webhooks: []model.WebhookConfig{{ID: "wh", Enabled: true, MinSeverity: "info"}},
		violations: map[string][]alertViolation{
			"volume_in": {{TargetIP: net.ParseIP("192.0.2.10"), MetricValue: value, BPS: value}},
		},
		latest: latest,
	}, &mockNotifier{}
}

// Flapping (bug #24): a violation shortly after the alert resolved re-opens
// it — no new alert, no notification.
func TestEngineRearmReopensResolvedAlert(t *testing.T) {
	now := time.Now().UTC()
	ms, n := rearmStore(&store.AlertState{ID: "a1", Status: "resolved", LastSeenAt: now.Add(-20 * time.Minute), MetricValue: 600e6}, 700e6)
	e := New(ms, n, nil, time.Second, 5*time.Minute)
	e.evaluateOnce(context.Background())
	time.Sleep(50 * time.Millisecond)

	if len(ms.inserted) != 0 || len(ms.reopened) != 1 || ms.reopened[0] != "a1" {
		t.Fatalf("inserted %d, reopened %v; want a re-open of a1 only", len(ms.inserted), ms.reopened)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.notified) != 0 {
		t.Errorf("a re-open must not notify, got %d notifications", len(n.notified))
	}
	// The next cycle trusts the re-opened alert without reading it back.
	e.evaluateOnce(context.Background())
	if len(ms.reopened) != 1 || len(ms.heartbeats) != 1 {
		t.Errorf("second cycle: reopened %v heartbeats %v; want one heartbeat", ms.reopened, ms.heartbeats)
	}
}

// An attack resuming far stronger than the alert it would re-open is a new
// incident.
func TestEngineRearmEscalationCreatesNewAlert(t *testing.T) {
	now := time.Now().UTC()
	ms, n := rearmStore(&store.AlertState{ID: "a1", Status: "resolved", LastSeenAt: now.Add(-10 * time.Minute), MetricValue: 600e6}, 1300e6)
	e := New(ms, n, nil, time.Second, 5*time.Minute)
	e.evaluateOnce(context.Background())
	if len(ms.inserted) != 1 || len(ms.reopened) != 0 {
		t.Fatalf("inserted %d, reopened %v; want a new alert", len(ms.inserted), ms.reopened)
	}
}

// Past the re-arm window (max(cooldown, ALERT_REARM_WINDOW)) a new alert is
// created.
func TestEngineRearmWindowExpired(t *testing.T) {
	now := time.Now().UTC()
	ms, n := rearmStore(&store.AlertState{ID: "a1", Status: "resolved", LastSeenAt: now.Add(-2 * time.Hour), MetricValue: 600e6}, 650e6)
	e := New(ms, n, nil, time.Second, 5*time.Minute)
	e.evaluateOnce(context.Background())
	if len(ms.inserted) != 1 || len(ms.reopened) != 0 {
		t.Fatalf("inserted %d, reopened %v; want a new alert after the window", len(ms.inserted), ms.reopened)
	}

	// A shorter engine-wide window, the rule's 300 s cooldown dominating:
	// 20 minutes is now outside.
	ms2, n2 := rearmStore(&store.AlertState{ID: "a1", Status: "resolved", LastSeenAt: now.Add(-20 * time.Minute), MetricValue: 600e6}, 650e6)
	e2 := New(ms2, n2, nil, time.Second, 5*time.Minute)
	e2.SetRearmWindow(0)
	e2.evaluateOnce(context.Background())
	if len(ms2.inserted) != 1 {
		t.Fatalf("ALERT_REARM_WINDOW=0: inserted %d, want 1", len(ms2.inserted))
	}
}

// An acknowledged incident that is still going on is heartbeated, not
// duplicated (3.6 created a new alert as soon as the active one was acked).
func TestEngineAcknowledgedAlertIsHeartbeated(t *testing.T) {
	now := time.Now().UTC()
	ms, n := rearmStore(&store.AlertState{ID: "a1", Status: "acknowledged", LastSeenAt: now.Add(-time.Minute), MetricValue: 600e6}, 650e6)
	e := New(ms, n, nil, time.Second, 5*time.Minute)
	e.evaluateOnce(context.Background())
	if len(ms.inserted) != 0 || len(ms.reopened) != 0 || len(ms.heartbeats) != 1 {
		t.Fatalf("inserted %d reopened %v heartbeats %v; want one heartbeat", len(ms.inserted), ms.reopened, ms.heartbeats)
	}
}

func TestBaselineVerdict(t *testing.T) {
	// 600 Mb/s now; history per minute in bytes (x 8 / 60 = bps).
	perMinute := func(bps float64) uint64 { return uint64(bps * 60 / 8) }
	v := store.AlertViolation{BPS: 600e6, PPS: 60_000}
	cases := []struct {
		name   string
		factor float64
		h      store.ThreatHistory
		keep   bool
	}{
		{"new host (no history)", 2, store.ThreatHistory{}, true},
		{"too little history", 2, store.ThreatHistory{Samples: store.BaselineMinSamples - 1, BytesP99: perMinute(900e6)}, true},
		{"busy host at its usual level", 2, store.ThreatHistory{Samples: 400, BytesP99: perMinute(800e6), PacketsP99: 80_000 * 60}, false},
		{"just under the factor", 2, store.ThreatHistory{Samples: 400, BytesP99: perMinute(301e6)}, false},
		{"twice its usual level", 2, store.ThreatHistory{Samples: 400, BytesP99: perMinute(300e6)}, true},
		{"quiet host, floor applied", 3, store.ThreatHistory{Samples: 400, BytesP99: 10}, true},
	}
	for _, c := range cases {
		keep, info := baselineVerdict(volumeRule(c.factor), v, c.h)
		if keep != c.keep {
			t.Errorf("%s: keep=%v want %v (info %v)", c.name, keep, c.keep, info)
		}
	}
	// pps-only rule: compared on packets.
	r := volumeRule(2)
	r.ThresholdBps, r.ThresholdPps = 0, 50_000
	if keep, _ := baselineVerdict(r, v, store.ThreatHistory{Samples: 400, BytesP99: 1, PacketsP99: 40_000 * 60}); keep {
		t.Errorf("pps rule: 60k pps vs usual 40k pps kept at factor 2")
	}
}

func TestEngineBaselineGate(t *testing.T) {
	perMinute := func(bps float64) uint64 { return uint64(bps * 60 / 8) }
	ms := &mockStore{
		rules: []model.AlertRule{volumeRule(2)},
		violations: map[string][]alertViolation{"volume_in": {
			{TargetIP: net.ParseIP("192.0.2.1"), MetricValue: 700e6, BPS: 700e6},   // busy, usual 800M
			{TargetIP: net.ParseIP("192.0.2.2"), MetricValue: 700e6, BPS: 700e6},   // no history
			{TargetIP: net.ParseIP("2001:db8::1"), MetricValue: 900e6, BPS: 900e6}, // usual 100M
		}},
		baselines: map[string]store.ThreatHistory{
			"192.0.2.1":            {Samples: 420, BytesP99: perMinute(800e6)},
			"2001:0db8:0000::0001": {Samples: 420, BytesP99: perMinute(100e6)}, // any spelling matches
		},
	}
	e := New(ms, nil, nil, time.Second, 5*time.Minute)
	e.evaluateOnce(context.Background())
	got := map[string]string{}
	for _, a := range ms.inserted {
		got[a.TargetIP] = a.Details
	}
	if len(ms.inserted) != 2 || got["192.0.2.2"] == "" || got["2001:db8::1"] == "" {
		t.Fatalf("inserted %v; want the new host and the IPv6 host at 9x", got)
	}
	if !strings.Contains(got["192.0.2.2"], `"new_host":true`) || !strings.Contains(got["2001:db8::1"], `"ratio":9`) {
		t.Errorf("details missing the usual level: %v", got)
	}
	if ms.baselineCalls != 1 {
		t.Errorf("baseline lookups = %d, want 1 per rule", ms.baselineCalls)
	}

	// A rule without a factor never reads the baseline.
	ms2 := &mockStore{rules: []model.AlertRule{volumeRule(0)}, violations: ms.violations}
	New(ms2, nil, nil, time.Second, 5*time.Minute).evaluateOnce(context.Background())
	if ms2.baselineCalls != 0 || len(ms2.inserted) != 3 {
		t.Errorf("absolute rule: %d lookups, %d alerts; want 0 and 3", ms2.baselineCalls, len(ms2.inserted))
	}
}

// ruleStore is an in-memory rule table for ReconcileSeededRules.
type ruleStore struct{ rules []model.AlertRule }

func (r *ruleStore) ListAlertRules(context.Context) ([]model.AlertRule, error) { return r.rules, nil }
func (r *ruleStore) UpsertAlertRule(_ context.Context, x model.AlertRule) error {
	for i := range r.rules {
		if r.rules[i].ID == x.ID {
			r.rules[i] = x
			return nil
		}
	}
	r.rules = append(r.rules, x)
	return nil
}

func TestReconcileSeededRules(t *testing.T) {
	var seeds []model.AlertRule
	for i, up := range seededUpgrades {
		r := up.was
		r.ID = string(rune('a' + i))
		r.Enabled, r.CooldownSeconds, r.Action, r.WebhookIDs = true, 300, "notify", []string{"wh-1"}
		seeds = append(seeds, r)
	}
	edited := seeds[0]
	edited.ID, edited.ThresholdBps = "edited", 800_000_000 // operator-tuned threshold
	disabled := seeds[1]
	disabled.ID, disabled.Enabled = "disabled", false // only toggled: still a seed
	rs := &ruleStore{rules: append(append([]model.AlertRule{}, seeds...), edited, disabled)}

	n, err := ReconcileSeededRules(context.Background(), rs)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(seeds)+1 {
		t.Fatalf("upgraded %d rules, want %d", n, len(seeds)+1)
	}
	byID := map[string]model.AlertRule{}
	for _, r := range rs.rules {
		byID[r.ID] = r
	}
	if r := byID["a"]; r.BaselineFactor != factorHighVolume || r.Description != descHighInbound || len(r.WebhookIDs) != 1 || !r.Enabled {
		t.Errorf("high inbound not upgraded or lost operator fields: %+v", r)
	}
	if r := byID["c"]; r.BaselineFactor != factorExfil {
		t.Errorf("exfil factor = %v", r.BaselineFactor)
	}
	if r := byID["d"]; r.BaselineFactor != 0 || r.Description != descPortScan {
		t.Errorf("port scan: factor %v description %q", r.BaselineFactor, r.Description)
	}
	if r := byID["edited"]; r.BaselineFactor != 0 || r.Description != seededUpgrades[0].was.Description {
		t.Errorf("operator-edited rule was touched: %+v", r)
	}
	if r := byID["disabled"]; r.BaselineFactor != factorHighVolume || r.Enabled {
		t.Errorf("disabled seed: %+v", r)
	}

	// Idempotent; an operator setting the factor back to 0 later is kept.
	r := byID["a"]
	r.BaselineFactor = 0
	_ = rs.UpsertAlertRule(context.Background(), r)
	if n, err := ReconcileSeededRules(context.Background(), rs); err != nil || n != 0 {
		t.Fatalf("second run upgraded %d (%v), want 0", n, err)
	}
}

// The seeds and the upgrade agree: a fresh install gets exactly what an
// upgraded one gets.
func TestDefaultRulesMatchUpgrades(t *testing.T) {
	sink := &ruleSink{}
	if err := EnsureDefaultRules(context.Background(), sink); err != nil {
		t.Fatal(err)
	}
	for _, up := range seededUpgrades {
		found := false
		for _, r := range sink.rules {
			if r.Name != up.was.Name {
				continue
			}
			found = true
			if r.BaselineFactor != up.factor || r.Description != up.description ||
				r.ThresholdBps != up.was.ThresholdBps || r.ThresholdCount != up.was.ThresholdCount ||
				r.WindowSeconds != up.was.WindowSeconds || r.Severity != up.was.Severity {
				t.Errorf("seed %q differs from its upgrade: %+v", r.Name, r)
			}
		}
		if !found {
			t.Errorf("no seed named %q", up.was.Name)
		}
	}
}
