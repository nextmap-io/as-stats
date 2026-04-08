// Package alerts implements the DDoS detection engine.
//
// The engine runs as a goroutine inside the collector. Every
// AlertEvalInterval (default 30s), it:
//  1. Loads enabled rules from alert_rules
//  2. For each rule, runs the corresponding pre-aggregated query
//     against traffic_by_dst_1min or traffic_by_src_1min
//  3. For each violation: creates a new Alert (or heartbeats existing one)
//  4. Resolves stale active alerts (last_seen older than threshold)
//  5. Sends notifications via configured webhooks
//
// The engine only queries HOT pre-aggregated tables, not flows_raw or
// flows_log, so it scales to very high flow volumes.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Store is the subset of the store interface that the engine needs.
// Kept minimal for easy mocking in tests.
type Store interface {
	ListAlertRules(ctx context.Context) ([]model.AlertRule, error)
	ListWebhooks(ctx context.Context) ([]model.WebhookConfig, error)

	EvalVolumeInbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalVolumeOutbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalSynFlood(ctx context.Context, thresholdPps uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalAmplification(ctx context.Context, thresholdCount, minBps uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalPortScan(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalProtocolFlood(ctx context.Context, protocol uint8, thresholdPps uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalConnectionFlood(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)

	TopSourcesForTarget(ctx context.Context, targetIP net.IP, window uint32, limit int) ([]string, error)

	FindActiveAlert(ctx context.Context, ruleID string, targetIP net.IP) (string, time.Time, error)
	InsertAlert(ctx context.Context, a model.Alert) error
	UpdateAlertLastSeen(ctx context.Context, id string, ts time.Time) error
	AutoResolveStaleAlerts(ctx context.Context, olderThan time.Duration) error
}

// Notifier sends an alert to one or more webhook destinations.
type Notifier interface {
	Notify(ctx context.Context, webhook model.WebhookConfig, alert model.Alert) error
}

// Engine evaluates alert rules against pre-aggregated hot tables.
type Engine struct {
	store         Store
	notifier      Notifier
	localPrefixes []string
	evalInterval  time.Duration
	staleAfter    time.Duration

	// In-memory cooldown tracker: rule_id|target_ip -> last trigger time
	mu       sync.Mutex
	cooldown map[string]time.Time
}

// New creates a new alert engine.
func New(s Store, notifier Notifier, localPrefixes []string, evalInterval, staleAfter time.Duration) *Engine {
	return &Engine{
		store:         s,
		notifier:      notifier,
		localPrefixes: localPrefixes,
		evalInterval:  evalInterval,
		staleAfter:    staleAfter,
		cooldown:      make(map[string]time.Time),
	}
}

// Run blocks until ctx is cancelled, evaluating rules periodically.
func (e *Engine) Run(ctx context.Context) {
	log.Printf("alert engine: starting (eval=%s, stale=%s, prefixes=%d)",
		e.evalInterval, e.staleAfter, len(e.localPrefixes))

	// Background cleanup of the cooldown map. Without this, the map grows
	// unboundedly: every (rule_id, target_ip) pair that ever fires keeps an
	// entry forever. Triggering thousands of unique attacker IPs over weeks
	// would slowly leak memory.
	go e.cooldownCleanupLoop(ctx)

	// Initial delay to let ClickHouse accumulate some data on startup
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(e.evalInterval)
	defer ticker.Stop()

	for {
		e.evaluateOnce(ctx)

		select {
		case <-ticker.C:
		case <-ctx.Done():
			log.Println("alert engine: stopping")
			return
		}
	}
}

// cooldownCleanupLoop periodically drops cooldown entries that are well past
// their expiration so the in-memory map cannot grow unbounded.
func (e *Engine) cooldownCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 1h is intentionally generous: even the longest-cooldown default
			// rule (10 min) is comfortably below this floor, so an entry that
			// hasn't been refreshed in 1h cannot still be in cooldown.
			e.cleanupCooldown(time.Hour)
		}
	}
}

func (e *Engine) cleanupCooldown(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge)
	e.mu.Lock()
	defer e.mu.Unlock()
	removed := 0
	for k, t := range e.cooldown {
		if t.Before(cutoff) {
			delete(e.cooldown, k)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("alert engine: cooldown cleanup removed %d stale entries (remaining=%d)", removed, len(e.cooldown))
	}
	return removed
}

func (e *Engine) evaluateOnce(ctx context.Context) {
	start := time.Now()
	defer func() {
		if d := time.Since(start); d > 5*time.Second {
			log.Printf("alert engine: evaluation took %s (slow!)", d)
		}
	}()

	// 1. Auto-resolve stale alerts first
	if err := e.store.AutoResolveStaleAlerts(ctx, e.staleAfter); err != nil {
		log.Printf("alert engine: auto-resolve error: %v", err)
	}

	// 2. Load enabled rules
	rules, err := e.store.ListAlertRules(ctx)
	if err != nil {
		log.Printf("alert engine: list rules error: %v", err)
		return
	}

	if len(rules) == 0 {
		return
	}

	// 3. Load webhooks (once per evaluation cycle)
	webhooks, err := e.store.ListWebhooks(ctx)
	if err != nil {
		log.Printf("alert engine: list webhooks error: %v", err)
		webhooks = nil
	}
	webhookByID := make(map[string]model.WebhookConfig, len(webhooks))
	for _, w := range webhooks {
		webhookByID[w.ID] = w
	}

	// 4. Evaluate each enabled rule
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		e.evaluateRule(ctx, rule, webhookByID)
	}
}

func (e *Engine) evaluateRule(ctx context.Context, rule model.AlertRule, webhooks map[string]model.WebhookConfig) {
	var violations []store.AlertViolation
	var err error
	var metricType string

	// Resolve target filter: rule-specific OR global LOCAL_AS prefixes
	prefixes := e.localPrefixes
	if rule.TargetFilter != "" {
		prefixes = []string{rule.TargetFilter}
	}

	switch rule.RuleType {
	case "volume_in":
		metricType = "bps"
		violations, err = e.store.EvalVolumeInbound(ctx, rule.ThresholdBps, rule.ThresholdPps, rule.WindowSeconds, prefixes)
	case "volume_out":
		metricType = "bps"
		violations, err = e.store.EvalVolumeOutbound(ctx, rule.ThresholdBps, rule.ThresholdPps, rule.WindowSeconds, prefixes)
	case "syn_flood":
		metricType = "pps"
		violations, err = e.store.EvalSynFlood(ctx, rule.ThresholdPps, rule.WindowSeconds, prefixes)
	case "amplification":
		metricType = "count"
		// ThresholdBps is reused as a "minimum sustained bps floor" — without
		// it, every scanner that touches one of our IPs from many sources at
		// trivial volume produces a constant amplification false positive.
		violations, err = e.store.EvalAmplification(ctx, rule.ThresholdCount, rule.ThresholdBps, rule.WindowSeconds, prefixes)
	case "port_scan":
		metricType = "count"
		violations, err = e.store.EvalPortScan(ctx, rule.ThresholdCount, rule.WindowSeconds, prefixes)
	case "icmp_flood":
		metricType = "pps"
		violations, err = e.store.EvalProtocolFlood(ctx, 1, rule.ThresholdPps, rule.WindowSeconds, prefixes)
	case "udp_flood":
		metricType = "pps"
		violations, err = e.store.EvalProtocolFlood(ctx, 17, rule.ThresholdPps, rule.WindowSeconds, prefixes)
	case "connection_flood":
		metricType = "count"
		violations, err = e.store.EvalConnectionFlood(ctx, rule.ThresholdCount, rule.WindowSeconds, prefixes)
	default:
		// 'custom' not implemented in phase 1
		return
	}

	if err != nil {
		log.Printf("alert engine: rule %s (%s) error: %v", rule.Name, rule.RuleType, err)
		return
	}

	for i := range violations {
		// Best-effort enrichment: pull the top src IPs from flows_raw so the
		// alert payload (and the dashboard) can show *who* is hitting the
		// target without making the operator run a separate flow search.
		// Inbound rules look at attacker source IPs; outbound (volume_out,
		// port_scan) get their context from outside this loop, since their
		// "target" is already the local source — top destinations would be
		// the meaningful enrichment, but flows_raw lookups stay symmetric
		// enough for now and return src IPs for all rule types.
		if violations[i].TargetIP != nil {
			if srcs, terr := e.store.TopSourcesForTarget(ctx, violations[i].TargetIP, rule.WindowSeconds, 5); terr == nil && len(srcs) > 0 {
				violations[i].TopSources = srcs
			}
		}
		e.handleViolation(ctx, rule, violations[i], metricType, webhooks)
	}
}

func (e *Engine) handleViolation(ctx context.Context, rule model.AlertRule, v store.AlertViolation, metricType string, webhooks map[string]model.WebhookConfig) {
	now := time.Now().UTC()
	target := v.TargetIP.String()
	cooldownKey := rule.ID + "|" + target

	// Check in-memory cooldown (fast path)
	e.mu.Lock()
	lastTrig, inCooldown := e.cooldown[cooldownKey]
	if inCooldown && now.Sub(lastTrig) < time.Duration(rule.CooldownSeconds)*time.Second {
		// Still in cooldown — update last_seen of existing alert and exit
		e.mu.Unlock()
		e.heartbeat(ctx, rule, v.TargetIP, now)
		return
	}
	e.mu.Unlock()

	// Check for existing active alert (DB-level dedup)
	existingID, _, err := e.store.FindActiveAlert(ctx, rule.ID, v.TargetIP)
	if err != nil {
		log.Printf("alert engine: find active alert error: %v", err)
		return
	}

	if existingID != "" {
		// Active alert exists — just heartbeat
		if err := e.store.UpdateAlertLastSeen(ctx, existingID, now); err != nil {
			log.Printf("alert engine: heartbeat error: %v", err)
		}
		return
	}

	// Create new alert
	threshold := float64(rule.ThresholdBps)
	switch metricType {
	case "pps":
		threshold = float64(rule.ThresholdPps)
	case "count":
		threshold = float64(rule.ThresholdCount)
	}

	details := store.AlertDetails{
		UniqueCount:  v.UniqueCount,
		WindowSecond: rule.WindowSeconds,
		TopSources:   v.TopSources,
	}
	detailsJSON := store.MarshalAlertDetails(details)

	alert := model.Alert{
		ID:          uuid.NewString(),
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Severity:    rule.Severity,
		TriggeredAt: now,
		LastSeenAt:  now,
		TargetIP:    target,
		Protocol:    v.Protocol,
		MetricValue: v.MetricValue,
		Threshold:   threshold,
		MetricType:  metricType,
		Details:     detailsJSON,
		Status:      "active",
	}

	if err := e.store.InsertAlert(ctx, alert); err != nil {
		log.Printf("alert engine: insert alert error: %v", err)
		return
	}

	// Track cooldown
	e.mu.Lock()
	e.cooldown[cooldownKey] = now
	e.mu.Unlock()

	log.Printf("alert engine: 🚨 %s [%s] %s — %.2f %s (threshold %.2f)",
		rule.Severity, rule.Name, target, v.MetricValue, metricType, threshold)

	// Dispatch webhooks (async, don't block evaluation)
	if e.notifier != nil {
		for _, webhookID := range rule.WebhookIDs {
			if wh, ok := webhooks[webhookID]; ok && wh.Enabled {
				if !severityMeets(alert.Severity, wh.MinSeverity) {
					continue
				}
				go e.safeNotify(wh, alert)
			}
		}
	}
}

func (e *Engine) heartbeat(ctx context.Context, rule model.AlertRule, targetIP net.IP, now time.Time) {
	id, _, err := e.store.FindActiveAlert(ctx, rule.ID, targetIP)
	if err != nil || id == "" {
		return
	}
	if err := e.store.UpdateAlertLastSeen(ctx, id, now); err != nil {
		log.Printf("alert engine: heartbeat error: %v", err)
	}
}

func (e *Engine) safeNotify(wh model.WebhookConfig, alert model.Alert) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("alert engine: webhook panic: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.notifier.Notify(ctx, wh, alert); err != nil {
		log.Printf("alert engine: webhook %s (%s) error: %v", wh.Name, wh.WebhookType, err)
	}
}

// severityMeets returns true if alertSev >= minSev.
func severityMeets(alertSev, minSev string) bool {
	order := map[string]int{"info": 0, "warning": 1, "critical": 2}
	return order[alertSev] >= order[minSev]
}

// CooldownSnapshot returns a copy of the current cooldown map (for debugging).
func (e *Engine) CooldownSnapshot() map[string]time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]time.Time, len(e.cooldown))
	for k, v := range e.cooldown {
		out[k] = v
	}
	return out
}

// JSONEncode helper for tests / debugging.
func JSONEncode(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// EnsureDefaultRules creates a set of sensible default rules if none exist.
// Called once at collector startup when FEATURE_ALERTS is enabled.
//
// The defaults intentionally cover several attack archetypes:
//   - bulk volumetric (volume_in)
//   - SYN flood (syn_flood) — TCP state-table abuse
//   - reflection / amplification (amplification with bps floor)
//   - protocol-specific floods (icmp_flood, udp_flood)
//   - connection-rate abuse (connection_flood) — Slowloris-class
//   - lateral compromise (port_scan, volume_out)
//   - slow exfiltration (volume_out at lower threshold + longer window)
//
// All rules are enabled by default. Operators are expected to tune thresholds
// to their environment via the Admin UI; the goal of these defaults is to
// surface anything obviously hostile without flooding the dashboard with
// false positives.
func EnsureDefaultRules(ctx context.Context, s interface {
	ListAlertRules(ctx context.Context) ([]model.AlertRule, error)
	UpsertAlertRule(ctx context.Context, r model.AlertRule) error
}) error {
	existing, err := s.ListAlertRules(ctx)
	if err != nil {
		return fmt.Errorf("list existing rules: %w", err)
	}
	if len(existing) > 0 {
		return nil // already seeded
	}

	now := time.Now().UTC()
	defaults := []model.AlertRule{
		// ── Volumetric ───────────────────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "High inbound volume",
			Description:     "Warning when a single IP receives > 500 Mbps of inbound traffic for 60s",
			RuleType:        "volume_in",
			Enabled:         true,
			ThresholdBps:    500_000_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "Critical inbound volume",
			Description:     "Critical when a single IP receives > 2 Gbps of inbound traffic (likely DDoS)",
			RuleType:        "volume_in",
			Enabled:         true,
			ThresholdBps:    2_000_000_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "critical",
			Action:          "ack_required",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── TCP state-table abuse ────────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "SYN flood",
			Description:     "TCP SYN-only packets > 50k/s sustained for 60s — TCP state-table attack",
			RuleType:        "syn_flood",
			Enabled:         true,
			ThresholdPps:    50_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "critical",
			Action:          "ack_required",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "Connection-rate flood",
			Description:     "More than 200k distinct flows hit one destination in 60s — Slowloris/half-open scan signature",
			RuleType:        "connection_flood",
			Enabled:         true,
			ThresholdCount:  200_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Reflection / amplification ───────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "Reflection/amplification attack",
			Description:     "> 10k unique source IPs hit one destination AND sustained ≥ 100 Mbps over 60s",
			RuleType:        "amplification",
			Enabled:         true,
			ThresholdCount:  10_000,
			ThresholdBps:    100_000_000, // floor to filter out low-volume scanners
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "critical",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Protocol-specific floods ─────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "ICMP flood",
			Description:     "ICMP packets > 20k/s to one destination — almost never legitimate at this rate",
			RuleType:        "icmp_flood",
			Enabled:         true,
			ThresholdPps:    20_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "UDP flood",
			Description:     "UDP packets > 100k/s to one destination — DNS query flood / NTP query flood signature",
			RuleType:        "udp_flood",
			Enabled:         true,
			ThresholdPps:    100_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Lateral / outbound abuse ─────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "Port scan from internal host",
			Description:     "An internal host hit > 1000 unique destination ports in 60s (likely compromised)",
			RuleType:        "port_scan",
			Enabled:         true,
			ThresholdCount:  1_000,
			WindowSeconds:   60,
			CooldownSeconds: 600,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "High outbound volume (compromised host)",
			Description:     "An internal host is sending > 500 Mbps outbound (DDoS source / data dump)",
			RuleType:        "volume_out",
			Enabled:         true,
			ThresholdBps:    500_000_000,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "Sustained outbound exfiltration",
			Description:     "An internal host has been sending > 50 Mbps outbound for 5 minutes (slow exfil)",
			RuleType:        "volume_out",
			Enabled:         true,
			ThresholdBps:    50_000_000,
			WindowSeconds:   300,
			CooldownSeconds: 1800,
			Severity:        "info",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
	}

	for _, r := range defaults {
		if err := s.UpsertAlertRule(ctx, r); err != nil {
			return fmt.Errorf("seed rule %q: %w", r.Name, err)
		}
		log.Printf("alert engine: seeded default rule %q", r.Name)
	}

	return nil
}
