package alerts

import (
	"context"
	"net"
	"strings"
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

	topSources     []string // returned by TopSourcesForTarget
	topSourceCalls int
	findTargets    []string // target IPs passed to FindActiveAlert
	activeAlertID  string   // returned by FindActiveAlert when non-empty
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
func (m *mockStore) EvalAmplification(ctx context.Context, _, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["amplification"], nil
}
func (m *mockStore) EvalPortScan(ctx context.Context, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["port_scan"], nil
}
func (m *mockStore) EvalProtocolFlood(ctx context.Context, proto uint8, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	switch proto {
	case 1:
		return m.violations["icmp_flood"], nil
	case 17:
		return m.violations["udp_flood"], nil
	}
	return nil, nil
}
func (m *mockStore) EvalConnectionFlood(ctx context.Context, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["connection_flood"], nil
}
func (m *mockStore) EvalSubnetFlood(ctx context.Context, _, _ uint64, _ int, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["subnet_flood"], nil
}
func (m *mockStore) EvalSMTPAbuse(ctx context.Context, _, _ uint64, _ uint32, _ []string) ([]store.AlertViolation, error) {
	return m.violations["smtp_abuse"], nil
}
func (m *mockStore) EvalDiskUsage(ctx context.Context, _ uint64) ([]store.AlertViolation, error) {
	return m.violations["disk_usage"], nil
}
func (m *mockStore) EvalLinkCapacity(ctx context.Context, _ uint64, _ uint32) ([]store.AlertViolation, error) {
	return m.violations["link_capacity"], nil
}
func (m *mockStore) EvalAnomaly(ctx context.Context, _ float64, _ string) ([]store.AlertViolation, error) {
	return m.violations["anomaly"], nil
}
func (m *mockStore) AnomalyExplain(ctx context.Context, target string, from, to time.Time) (model.AnomalyExplanation, error) {
	return model.AnomalyExplanation{Target: target, From: from, To: to}, nil
}
func (m *mockStore) ListHostgroups(ctx context.Context) ([]model.Hostgroup, error) {
	return nil, nil
}
func (m *mockStore) TopSourcesForTarget(ctx context.Context, _ net.IP, _ uint32, _ int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topSourceCalls++
	return m.topSources, nil
}
func (m *mockStore) FindActiveAlert(ctx context.Context, ruleID string, targetIP net.IP) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findTargets = append(m.findTargets, targetIP.String())
	return m.activeAlertID, time.Time{}, nil
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

func TestEngineProtocolFlood(t *testing.T) {
	cases := []struct {
		ruleType    string
		expectProto uint8
	}{
		{"icmp_flood", 1},
		{"udp_flood", 17},
	}

	for _, tc := range cases {
		t.Run(tc.ruleType, func(t *testing.T) {
			ms := &mockStore{
				rules: []model.AlertRule{
					{
						ID:            "r1",
						RuleType:      tc.ruleType,
						Enabled:       true,
						ThresholdPps:  100,
						WindowSeconds: 60,
						Severity:      "warning",
					},
				},
				violations: map[string][]alertViolation{
					tc.ruleType: {{TargetIP: net.ParseIP("10.0.0.42"), MetricValue: 1500, Protocol: tc.expectProto}},
				},
			}

			e := New(ms, nil, nil, time.Second, time.Minute)
			e.evaluateOnce(context.Background())

			ms.mu.Lock()
			defer ms.mu.Unlock()
			if len(ms.inserted) != 1 {
				t.Fatalf("expected 1 alert for %s, got %d", tc.ruleType, len(ms.inserted))
			}
			if got := ms.inserted[0].Protocol; got != tc.expectProto {
				t.Errorf("expected protocol %d, got %d", tc.expectProto, got)
			}
		})
	}
}

func TestEngineConnectionFlood(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{
				ID:             "r1",
				RuleType:       "connection_flood",
				Enabled:        true,
				ThresholdCount: 100_000,
				WindowSeconds:  60,
				Severity:       "warning",
			},
		},
		violations: map[string][]alertViolation{
			"connection_flood": {{TargetIP: net.ParseIP("10.0.0.7"), MetricValue: 250_000, UniqueCount: 250_000}},
		},
	}

	e := New(ms, nil, nil, time.Second, time.Minute)
	e.evaluateOnce(context.Background())

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.inserted) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(ms.inserted))
	}
	if ms.inserted[0].MetricType != "count" {
		t.Errorf("expected metric_type=count, got %s", ms.inserted[0].MetricType)
	}
}

// Enrichment scans flows_raw, so it must happen only when its result is
// actually stored — not on every cycle of a sustained attack.
func TestEngineEnrichmentOnlyOnInsert(t *testing.T) {
	ms := &mockStore{
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
		topSources: []string{"198.51.100.7"},
	}

	e := New(ms, nil, nil, time.Second, time.Minute)
	e.evaluateOnce(context.Background())
	e.evaluateOnce(context.Background()) // in cooldown: heartbeat only

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.topSourceCalls != 1 {
		t.Errorf("expected 1 enrichment call, got %d", ms.topSourceCalls)
	}
	if len(ms.inserted) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(ms.inserted))
	}
	if !strings.Contains(ms.inserted[0].Details, "198.51.100.7") {
		t.Errorf("enrichment missing from stored details: %s", ms.inserted[0].Details)
	}
}

func TestEngineNoEnrichmentWhenAlertAlreadyActive(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{ID: "r1", RuleType: "volume_in", Enabled: true, ThresholdBps: 100, WindowSeconds: 60, Severity: "warning"},
		},
		violations: map[string][]alertViolation{
			"volume_in": {{TargetIP: net.ParseIP("10.0.0.1"), MetricValue: 1000}},
		},
		topSources:    []string{"198.51.100.7"},
		activeAlertID: "existing-alert",
	}

	e := New(ms, nil, nil, time.Second, time.Minute)
	e.evaluateOnce(context.Background())

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.topSourceCalls != 0 {
		t.Errorf("expected no enrichment when the alert already exists, got %d calls", ms.topSourceCalls)
	}
	if len(ms.inserted) != 0 {
		t.Errorf("expected no insert, got %d", len(ms.inserted))
	}
	if len(ms.heartbeats) != 1 {
		t.Errorf("expected 1 heartbeat, got %d", len(ms.heartbeats))
	}
}

// Label-targeted rules (link_capacity, anomaly) have no target IP: each label
// must still get its own dedup identity, otherwise distinct links collapse
// into a single alert row.
func TestEngineLabelTargetsDedupPerLabel(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{
				ID:             "r-cap",
				Name:           "Link capacity high",
				RuleType:       "link_capacity",
				Enabled:        true,
				ThresholdCount: 80,
				WindowSeconds:  86400,
				Severity:       "warning",
				WebhookIDs:     []string{"wh1"},
			},
		},
		webhooks: []model.WebhookConfig{
			{ID: "wh1", Name: "slack", Enabled: true, MinSeverity: "info"},
		},
		violations: map[string][]alertViolation{
			"link_capacity": {
				{TargetLabel: "transit-1", MetricValue: 91},
				{TargetLabel: "transit-2", MetricValue: 85},
			},
		},
	}

	notifier := &mockNotifier{}
	e := New(ms, notifier, nil, time.Second, time.Minute)
	e.evaluateOnce(context.Background())

	// Notifications are dispatched in goroutines.
	time.Sleep(200 * time.Millisecond)

	// Operators must still see the link tag, not the synthetic address.
	notifier.mu.Lock()
	notifiedTargets := make([]string, 0, len(notifier.notified))
	for _, a := range notifier.notified {
		notifiedTargets = append(notifiedTargets, a.TargetIP)
	}
	notifier.mu.Unlock()
	if len(notifiedTargets) != 2 {
		t.Errorf("expected 2 notifications, got %v", notifiedTargets)
	}
	for _, tgt := range notifiedTargets {
		if !strings.HasPrefix(tgt, "transit-") {
			t.Errorf("notification target %q should be the link tag", tgt)
		}
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if len(ms.inserted) != 2 {
		t.Fatalf("expected 2 alerts (one per link), got %d", len(ms.inserted))
	}
	if ms.inserted[0].TargetIP == ms.inserted[1].TargetIP {
		t.Errorf("both links stored under the same target %q", ms.inserted[0].TargetIP)
	}
	for _, a := range ms.inserted {
		if net.ParseIP(a.TargetIP) == nil {
			t.Errorf("stored target %q is not a valid address", a.TargetIP)
		}
		if a.TargetIP == "::" {
			t.Error("label target collapsed onto the zero IP")
		}
	}
	// The DB dedup lookup must see the same distinct identities.
	if len(ms.findTargets) != 2 || ms.findTargets[0] == ms.findTargets[1] {
		t.Errorf("expected 2 distinct dedup lookups, got %v", ms.findTargets)
	}
	// The link tag stays authoritative in the details payload.
	if !strings.Contains(ms.inserted[0].Details, "transit-1") {
		t.Errorf("link tag missing from details: %s", ms.inserted[0].Details)
	}
}

func TestLabelTargetIP(t *testing.T) {
	a := labelTargetIP("transit-1")
	if !a.Equal(labelTargetIP("transit-1")) {
		t.Error("labelTargetIP must be deterministic")
	}
	if a.Equal(labelTargetIP("transit-2")) {
		t.Error("distinct labels must map to distinct addresses")
	}
	// Must land in the RFC 6666 discard prefix 0100::/64 so it can never be
	// confused with an address observed in real traffic.
	if !strings.HasPrefix(a.String(), "100:") {
		t.Errorf("expected an address in 0100::/64, got %s", a)
	}
	if a.To4() != nil {
		t.Errorf("expected an IPv6 address, got %s", a)
	}
}

func TestCleanupCooldown(t *testing.T) {
	e := New(&mockStore{}, nil, nil, time.Second, time.Minute)

	old := time.Now().Add(-2 * time.Hour)
	recent := time.Now().Add(-30 * time.Minute)

	e.mu.Lock()
	e.cooldown["rule1|10.0.0.1"] = old
	e.cooldown["rule1|10.0.0.2"] = old
	e.cooldown["rule2|10.0.0.3"] = recent
	e.mu.Unlock()

	removed := e.cleanupCooldown(time.Hour)
	if removed != 2 {
		t.Errorf("expected 2 entries removed, got %d", removed)
	}

	snap := e.CooldownSnapshot()
	if len(snap) != 1 {
		t.Errorf("expected 1 entry remaining, got %d", len(snap))
	}
	if _, ok := snap["rule2|10.0.0.3"]; !ok {
		t.Error("recent entry should have been preserved")
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

// mockNotifier records the alerts handed to webhook delivery.
type mockNotifier struct {
	mu       sync.Mutex
	notified []model.Alert
}

func (n *mockNotifier) Notify(ctx context.Context, _ model.WebhookConfig, a model.Alert) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notified = append(n.notified, a)
	return nil
}

// ---------------------------------------------------------------------------
// Mock types for auto-block tests
// ---------------------------------------------------------------------------

// mockBlocker records Announce calls.
type mockBlocker struct {
	mu        sync.Mutex
	announced []mockAnnounceCall
}

type mockAnnounceCall struct {
	Target   net.IP
	Duration time.Duration
	Reason   string
}

func (m *mockBlocker) Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.announced = append(m.announced, mockAnnounceCall{Target: target, Duration: duration, Reason: reason})
	return nil
}

// mockBlockStore records InsertBlock calls and can fake FindActiveBlock results.
type mockBlockStore struct {
	mu             sync.Mutex
	insertedBlocks []model.BGPBlock
	activeBlockID  string // returned by FindActiveBlock when non-empty
}

func (m *mockBlockStore) InsertBlock(ctx context.Context, b model.BGPBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertedBlocks = append(m.insertedBlocks, b)
	return nil
}

func (m *mockBlockStore) FindActiveBlock(ctx context.Context, ip string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeBlockID, nil
}

// ---------------------------------------------------------------------------
// Auto-block tests
// ---------------------------------------------------------------------------

func TestEngineAutoBlock(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{
				ID:            "rule-ab",
				Name:          "Auto block rule",
				RuleType:      "volume_in",
				Enabled:       true,
				ThresholdBps:  1_000_000_000,
				WindowSeconds: 60,
				Severity:      "critical",
				Action:        "auto_block",
			},
		},
		violations: map[string][]alertViolation{
			"volume_in": {
				{
					TargetIP:    net.ParseIP("192.0.2.10"),
					MetricValue: 5_000_000_000,
					TopSources:  []string{"198.51.100.1", "198.51.100.2"},
				},
			},
		},
	}

	blocker := &mockBlocker{}
	blockStore := &mockBlockStore{}

	e := New(ms, nil, nil, 100*time.Millisecond, 5*time.Minute)
	e.SetBlocker(blocker, blockStore)

	e.evaluateOnce(context.Background())

	// safeAutoBlock runs in a goroutine, give it a moment to complete.
	time.Sleep(200 * time.Millisecond)

	blocker.mu.Lock()
	defer blocker.mu.Unlock()
	if len(blocker.announced) != 1 {
		t.Fatalf("expected 1 Announce call, got %d", len(blocker.announced))
	}
	if blocker.announced[0].Target.String() != "192.0.2.10" {
		t.Errorf("expected target 192.0.2.10, got %s", blocker.announced[0].Target)
	}

	blockStore.mu.Lock()
	defer blockStore.mu.Unlock()
	if len(blockStore.insertedBlocks) != 1 {
		t.Fatalf("expected 1 InsertBlock call, got %d", len(blockStore.insertedBlocks))
	}
	b := blockStore.insertedBlocks[0]
	if b.IP != "192.0.2.10" {
		t.Errorf("expected block IP 192.0.2.10, got %s", b.IP)
	}
	if b.Reason != "auto_block" {
		t.Errorf("expected reason %q, got %q", "auto_block", b.Reason)
	}
	if !strings.Contains(b.Description, "Auto block rule") {
		t.Errorf("expected description to contain rule name, got %q", b.Description)
	}
}

func TestEngineAutoBlockSkipsDuplicate(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{
				ID:            "rule-dup",
				Name:          "Dup block rule",
				RuleType:      "volume_in",
				Enabled:       true,
				ThresholdBps:  1_000_000_000,
				WindowSeconds: 60,
				Severity:      "critical",
				Action:        "auto_block",
			},
		},
		violations: map[string][]alertViolation{
			"volume_in": {
				{
					TargetIP:    net.ParseIP("192.0.2.20"),
					MetricValue: 5_000_000_000,
				},
			},
		},
	}

	blocker := &mockBlocker{}
	blockStore := &mockBlockStore{activeBlockID: "existing-block-id"}

	e := New(ms, nil, nil, 100*time.Millisecond, 5*time.Minute)
	e.SetBlocker(blocker, blockStore)

	e.evaluateOnce(context.Background())

	// Give the goroutine time to run.
	time.Sleep(200 * time.Millisecond)

	blocker.mu.Lock()
	defer blocker.mu.Unlock()
	if len(blocker.announced) != 0 {
		t.Errorf("expected 0 Announce calls (duplicate skipped), got %d", len(blocker.announced))
	}
}

func TestEngineAutoBlockNilBlocker(t *testing.T) {
	ms := &mockStore{
		rules: []model.AlertRule{
			{
				ID:            "rule-nil",
				Name:          "Nil blocker rule",
				RuleType:      "volume_in",
				Enabled:       true,
				ThresholdBps:  1_000_000_000,
				WindowSeconds: 60,
				Severity:      "critical",
				Action:        "auto_block",
			},
		},
		violations: map[string][]alertViolation{
			"volume_in": {
				{
					TargetIP:    net.ParseIP("192.0.2.30"),
					MetricValue: 5_000_000_000,
				},
			},
		},
	}

	e := New(ms, nil, nil, 100*time.Millisecond, 5*time.Minute)
	e.SetBlocker(nil, nil) // explicitly nil — should not panic

	// This must not panic.
	e.evaluateOnce(context.Background())

	// Give any potential goroutine time to run (it should not be started).
	time.Sleep(100 * time.Millisecond)

	ms.mu.Lock()
	defer ms.mu.Unlock()
	// The alert should still be inserted even though blocking is not available.
	if len(ms.inserted) != 1 {
		t.Fatalf("expected 1 alert inserted despite nil blocker, got %d", len(ms.inserted))
	}
}
