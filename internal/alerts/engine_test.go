package alerts

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// mockStore implements the Store interface for testing the engine.
type mockStore struct {
	mu            sync.Mutex
	rules         []model.AlertRule
	webhooks      []model.WebhookConfig
	violations    map[string][]store.AlertViolation // rule_type -> violations
	inserted      []model.Alert
	heartbeats    []string
	staleResolved int
}

func (m *mockStore) ListAlertRules(ctx context.Context) ([]model.AlertRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rules, nil
}
func (m *mockStore) ListWebhooks(ctx context.Context) ([]model.WebhookConfig, error) {
	return m.webhooks, nil
}
func (m *mockStore) EvalVolumeInbound(ctx context.Context, _, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["volume_in"], nil
}
func (m *mockStore) EvalVolumeOutbound(ctx context.Context, _, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["volume_out"], nil
}
func (m *mockStore) EvalSynFlood(ctx context.Context, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["syn_flood"], nil
}
func (m *mockStore) EvalAmplification(ctx context.Context, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["amplification"], nil
}
func (m *mockStore) EvalPortScan(ctx context.Context, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["port_scan"], nil
}
func (m *mockStore) FindActiveAlert(ctx context.Context, ruleID string, _ net.IP) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (m *mockStore) InsertAlert(ctx context.Context, a model.Alert) error {
	m.mu.Lock()
	m.inserted = append(m.inserted, a)
	m.mu.Unlock()
	return nil
}
func (m *mockStore) UpdateAlertLastSeen(ctx context.Context, id string, _ time.Time) error {
	m.mu.Lock()
	m.heartbeats = append(m.heartbeats, id)
	m.mu.Unlock()
	return nil
}
func (m *mockStore) AutoResolveStaleAlerts(ctx context.Context, _ time.Duration) error {
	m.mu.Lock()
	m.staleResolved++
	m.mu.Unlock()
	return nil
}

func TestEngineEvaluateVolumeInbound(t *testing.T) {
	store := &mockStore{
		rules: []model.AlertRule{
			{
				ID:            "rule-1",
				Name:          "High inbound",
				RuleType:      "volume_in",
				Enabled:       true,
				ThresholdBps:  1_000_000_000,
				WindowSeconds: 60,
				Severity:      "warning",
				Action:        "notify",
			},
		},
		violations: map[string][]alertViolation{
			"volume_in": {
				{
					TargetIP:    net.ParseIP("10.0.0.1"),
					MetricValue: 2_500_000_000,
				},
			},
		},
	}

	e := New(store, nil, nil, 100*time.Millisecond, 5*time.Minute)
	e.evaluateOnce(context.Background())

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 alert inserted, got %d", len(store.inserted))
	}
	a := store.inserted[0]
	if a.RuleID != "rule-1" {
		t.Errorf("expected rule-1, got %s", a.RuleID)
	}
	if a.TargetIP != "10.0.0.1" {
		t.Errorf("expected target 10.0.0.1, got %s", a.TargetIP)
	}
	if a.MetricValue != 2_500_000_000 {
		t.Errorf("expected 2.5 Gbps, got %f", a.MetricValue)
	}
}

func TestEngineDisabledRulesSkipped(t *testing.T) {
	store := &mockStore{
		rules: []model.AlertRule{
			{ID: "r1", RuleType: "volume_in", Enabled: false, WindowSeconds: 60},
		},
		violations: map[string][]alertViolation{
			"volume_in": {{TargetIP: net.ParseIP("10.0.0.1"), MetricValue: 999}},
		},
	}

	e := New(store, nil, nil, time.Second, time.Minute)
	e.evaluateOnce(context.Background())

	if len(store.inserted) != 0 {
		t.Errorf("disabled rules should not produce alerts, got %d", len(store.inserted))
	}
}

func TestEngineCooldown(t *testing.T) {
	store := &mockStore{
		rules: []model.AlertRule{
			{
				ID:              "r1",
				RuleType:        "volume_in",
				Enabled:         true,
				ThresholdBps:    100,
				WindowSeconds:   60,
				CooldownSeconds: 300,
				Severity:        "warning",
			},
		},
		violations: map[string][]alertViolation{
			"volume_in": {{TargetIP: net.ParseIP("10.0.0.1"), MetricValue: 1000}},
		},
	}

	e := New(store, nil, nil, time.Second, time.Minute)

	// First evaluation: should insert
	e.evaluateOnce(context.Background())
	if len(store.inserted) != 1 {
		t.Fatalf("expected 1 alert after first eval, got %d", len(store.inserted))
	}

	// Second evaluation: should NOT insert (cooldown), only heartbeat
	e.evaluateOnce(context.Background())
	if len(store.inserted) != 1 {
		t.Errorf("expected still 1 alert (cooldown), got %d", len(store.inserted))
	}
}

func TestSeverityMeets(t *testing.T) {
	cases := []struct {
		alert, min string
		want       bool
	}{
		{"info", "info", true},
		{"warning", "info", true},
		{"critical", "info", true},
		{"info", "warning", false},
		{"warning", "warning", true},
		{"critical", "warning", true},
		{"info", "critical", false},
		{"warning", "critical", false},
		{"critical", "critical", true},
	}
	for _, c := range cases {
		if got := severityMeets(c.alert, c.min); got != c.want {
			t.Errorf("severityMeets(%s, %s) = %v, want %v", c.alert, c.min, got, c.want)
		}
	}
}

// Type alias to avoid importing store in the test's violation map literal
type alertViolation = store.AlertViolation
