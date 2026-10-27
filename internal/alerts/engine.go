// Package alerts implements the DDoS detection engine.
//
// The engine runs as a goroutine inside the collector. Every
// AlertEvalInterval (default 30s), it:
//  1. Resolves stale active alerts (last_seen older than ALERT_STALE_THRESHOLD)
//  2. Loads enabled rules from alert_rules
//  3. For each rule, runs the corresponding pre-aggregated query against
//     traffic_by_dst_1min or traffic_by_src_1min (port_scan then confirms its
//     candidates on flows_raw), and drops the violations of baseline rules
//     whose target is at its usual level (baseline.go)
//  4. For each violation: heartbeats the alert of that rule+target, re-opens
//     it when it resolved within the re-arm window, or creates a new one
//  5. Sends notifications via configured webhooks (new alerts only)
//
// The engine queries the HOT pre-aggregated tables, plus bounded flows_raw
// lookups for candidates only, so it scales to very high flow volumes.
package alerts

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/metrics"
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
	EvalSubnetFlood(ctx context.Context, thresholdBps, thresholdPps uint64, aggPrefix int, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalSMTPAbuse(ctx context.Context, thresholdPps, thresholdCount uint64, window uint32, localPrefixes []string) ([]store.AlertViolation, error)
	EvalDiskUsage(ctx context.Context, thresholdPct uint64) ([]store.AlertViolation, error)
	EvalLinkCapacity(ctx context.Context, thresholdPct uint64, window uint32) ([]store.AlertViolation, error)
	EvalAnomaly(ctx context.Context, k float64, linkFilter string) ([]store.AlertViolation, error)
	// ThreatBaselines is the Live Threats per-host history, reused by the
	// baseline mode of the volume rules (baseline.go).
	ThreatBaselines(ctx context.Context, direction string, anchor time.Time, probes []string) (map[string]store.ThreatHistory, error)

	ListHostgroups(ctx context.Context) ([]model.Hostgroup, error)
	TopSourcesForTarget(ctx context.Context, targetIP net.IP, window uint32, limit int) ([]string, error)
	AnomalyExplain(ctx context.Context, target string, from, to time.Time) (model.AnomalyExplanation, error)

	LatestAlert(ctx context.Context, ruleID string, targetIP net.IP) (store.AlertState, error)
	InsertAlert(ctx context.Context, a model.Alert) error
	ReopenAlert(ctx context.Context, id string, ts time.Time, value float64) error
	UpdateAlertLastSeen(ctx context.Context, id string, ts time.Time) error
	AutoResolveStaleAlerts(ctx context.Context, olderThan time.Duration) error
}

// Notifier sends an alert to one or more webhook destinations.
type Notifier interface {
	Notify(ctx context.Context, webhook model.WebhookConfig, alert model.Alert) error
}

// BlockerClient is the interface used by the engine to trigger BGP blackholes.
// The collector passes a RemoteBlocker (HTTP to API) or NoopBlocker.
type BlockerClient interface {
	Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error
}

// BlockStore is the subset of the store needed for block persistence.
type BlockStore interface {
	InsertBlock(ctx context.Context, b model.BGPBlock) error
	FindActiveBlock(ctx context.Context, ip string) (string, error)
}

// DefaultRearmWindow is the default ALERT_REARM_WINDOW: a violation of the
// same rule on the same target within this long after its last alert was
// last seen re-opens that alert instead of creating a new one.
const DefaultRearmWindow = time.Hour

// recentAlert is an alert this process inserted or re-opened, kept so the
// next cycles heartbeat it by id while the INSERT / mutation may not be
// visible to a read yet.
type recentAlert struct {
	id string
	at time.Time
}

// Engine evaluates alert rules against pre-aggregated hot tables.
type Engine struct {
	store         Store
	notifier      Notifier
	blocker       BlockerClient // BGP blocker (nil = no auto-blocking)
	blockStore    BlockStore    // persists block records (nil = skip)
	localPrefixes []string
	evalInterval  time.Duration
	staleAfter    time.Duration
	rearmWindow   time.Duration
	now           func() time.Time

	// rule_id|target_ip -> the alert this process last stored or re-opened.
	// Pruned by cooldownCleanupLoop (CLAUDE.md bug #3: never unbounded).
	mu     sync.Mutex
	recent map[string]recentAlert
}

// New creates a new alert engine.
func New(s Store, notifier Notifier, localPrefixes []string, evalInterval, staleAfter time.Duration) *Engine {
	return &Engine{
		store:         s,
		notifier:      notifier,
		localPrefixes: localPrefixes,
		evalInterval:  evalInterval,
		staleAfter:    staleAfter,
		rearmWindow:   DefaultRearmWindow,
		now:           func() time.Time { return time.Now().UTC() },
		recent:        make(map[string]recentAlert),
	}
}

// SetBlocker configures the BGP blocker for auto-block rules. When set,
// rules with action="auto_block" will call the blocker on violation.
func (e *Engine) SetBlocker(b BlockerClient, bs BlockStore) {
	e.blocker = b
	e.blockStore = bs
}

// SetRearmWindow sets the minimum re-arm window (ALERT_REARM_WINDOW); a
// rule's cooldown_seconds raises it for that rule. 0 leaves only the rules'
// cooldowns.
func (e *Engine) SetRearmWindow(d time.Duration) {
	if d >= 0 {
		e.rearmWindow = d
	}
}

// SetClock replaces the engine's clock (tests and the replay harness).
func (e *Engine) SetClock(now func() time.Time) { e.now = now }

// rearmFor is the re-arm window of a rule: the longer of its cooldown and
// the engine-wide minimum.
func (e *Engine) rearmFor(rule model.AlertRule) time.Duration {
	return max(time.Duration(rule.CooldownSeconds)*time.Second, e.rearmWindow)
}

// recentTrust is how long an alert this process just inserted or re-opened
// is heartbeated by id without reading it back.
func (e *Engine) recentTrust() time.Duration {
	return max(2*e.evalInterval, time.Minute)
}

// Run blocks until ctx is cancelled, evaluating rules periodically.
func (e *Engine) Run(ctx context.Context) {
	log.Printf("alert engine: starting (eval=%s, stale=%s, rearm=%s, prefixes=%d)",
		e.evalInterval, e.staleAfter, e.rearmWindow, len(e.localPrefixes))

	// Background cleanup of the recent-alert map. Without this, the map grows
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

// cooldownCleanupLoop periodically drops recent-alert entries that are well
// past any use so the in-memory map cannot grow unbounded.
func (e *Engine) cooldownCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// An entry is only trusted for recentTrust (≥ 1 min); the DB is
			// the source of truth after that, so 1h is generous.
			e.cleanupCooldown(time.Hour)
		}
	}
}

func (e *Engine) cleanupCooldown(maxAge time.Duration) int {
	cutoff := e.now().Add(-maxAge)
	e.mu.Lock()
	defer e.mu.Unlock()
	removed := 0
	for k, r := range e.recent {
		if r.at.Before(cutoff) {
			delete(e.recent, k)
			removed++
		}
	}
	if removed > 0 {
		log.Printf("alert engine: recent-alert cleanup removed %d stale entries (remaining=%d)", removed, len(e.recent))
	}
	return removed
}

func (e *Engine) evaluateOnce(ctx context.Context) {
	start := time.Now()
	defer func() {
		d := time.Since(start)
		metrics.AlertEngineCycleDuration.Observe(d.Seconds())
		e.mu.Lock()
		n := len(e.recent)
		e.mu.Unlock()
		metrics.CooldownMapSize.Set(float64(n))
		if d > 5*time.Second {
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

	// 4. Load hostgroups (once per cycle, for rules scoped to a hostgroup)
	hostgroups, err := e.store.ListHostgroups(ctx)
	if err != nil {
		log.Printf("alert engine: list hostgroups error: %v", err)
		hostgroups = nil
	}
	hostgroupByID := make(map[string]model.Hostgroup, len(hostgroups))
	for _, hg := range hostgroups {
		hostgroupByID[hg.ID] = hg
	}

	// 5. Evaluate each enabled rule
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		e.evaluateRule(ctx, rule, webhookByID, hostgroupByID)
	}
}

func (e *Engine) evaluateRule(ctx context.Context, rule model.AlertRule, webhooks map[string]model.WebhookConfig, hostgroups map[string]model.Hostgroup) {
	violations, metricType, ok := e.ruleViolations(ctx, rule, hostgroups)
	if !ok {
		return
	}
	violations = e.applyBaseline(ctx, rule, violations)
	for i := range violations {
		e.handleViolation(ctx, rule, violations[i], metricType, webhooks)
	}
}

// ruleViolations runs a rule's evaluation query and returns its violations
// (before the baseline gate) and metric type; ok is false on error or for a
// rule type the engine does not evaluate.
func (e *Engine) ruleViolations(ctx context.Context, rule model.AlertRule, hostgroups map[string]model.Hostgroup) (violations []store.AlertViolation, metricType string, ok bool) {
	metrics.AlertEvaluations.WithLabelValues(rule.RuleType).Inc()

	var err error

	// Resolve target filter: hostgroup → rule-specific → global LOCAL_AS prefixes
	prefixes := e.localPrefixes
	if rule.HostgroupID != "" {
		if hg, ok := hostgroups[rule.HostgroupID]; ok {
			prefixes = hg.CIDRs
		} else {
			log.Printf("alert engine: rule %s references unknown hostgroup %s, falling back to global prefixes", rule.Name, rule.HostgroupID)
		}
	} else if rule.TargetFilter != "" {
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
	case "subnet_flood":
		metricType = "bps"
		violations, err = e.store.EvalSubnetFlood(ctx, rule.ThresholdBps, rule.ThresholdPps, int(rule.SubnetPrefixLen), rule.WindowSeconds, prefixes)
	case "smtp_abuse":
		metricType = "pps"
		violations, err = e.store.EvalSMTPAbuse(ctx, rule.ThresholdPps, rule.ThresholdCount, rule.WindowSeconds, prefixes)
	case "disk_usage":
		// Disk usage is host-level, not IP-scoped: ThresholdCount carries the
		// percentage threshold (0..100) and prefixes are irrelevant.
		metricType = "percent"
		violations, err = e.store.EvalDiskUsage(ctx, rule.ThresholdCount)
	case "link_capacity":
		// Link capacity is link-scoped, not IP-scoped: ThresholdCount carries
		// the utilization percentage threshold (0..100), same convention as
		// disk_usage. The violation carries the link tag in TargetLabel.
		metricType = "percent"
		violations, err = e.store.EvalLinkCapacity(ctx, rule.ThresholdCount, rule.WindowSeconds)
	case "anomaly":
		// Statistical baseline anomaly on per-link hourly throughput. The metric
		// is bps (current hour). Sensitivity k is carried in ThresholdCount as
		// k*10 (e.g. 25 → k=2.5), so the feature needs no schema change.
		// TargetFilter, if set, restricts evaluation to a single link tag.
		metricType = "bps"
		k := float64(rule.ThresholdCount) / 10.0
		violations, err = e.store.EvalAnomaly(ctx, k, rule.TargetFilter)
	default:
		// 'custom' not implemented in phase 1
		return nil, "", false
	}

	if err != nil {
		log.Printf("alert engine: rule %s (%s) error: %v", rule.Name, rule.RuleType, err)
		return nil, "", false
	}
	return violations, metricType, true
}

// enrichViolation attaches best-effort triage context to a violation.
//
// Both lookups scan flows_raw — by far the largest table in the system — so
// this runs only on the path that actually stores a new alert. A sustained
// attack produces the same violation on every evaluation cycle; enriching
// before the cooldown / dedup checks meant paying for those scans dozens of
// times per incident and throwing the result away every time but the first.
func (e *Engine) enrichViolation(ctx context.Context, rule model.AlertRule, v *store.AlertViolation) {
	// Top src IPs so the alert payload (and the dashboard) can show *who* is
	// hitting the target without making the operator run a separate flow
	// search. Inbound rules look at attacker source IPs; outbound (volume_out,
	// port_scan) would be better served by top destinations, but flows_raw
	// lookups stay symmetric enough for now and return src IPs for all rule
	// types.
	if v.TargetIP != nil {
		if srcs, terr := e.store.TopSourcesForTarget(ctx, v.TargetIP, rule.WindowSeconds, 5); terr == nil && len(srcs) > 0 {
			v.TopSources = srcs
		}
	}

	// Anomaly rules target a link (no IP): enrich with the top-contributor
	// breakdown over the last complete hour so triage sees *why* the link
	// spiked. Attached into details.Extra alongside the baseline stats.
	if rule.RuleType == "anomaly" && v.TargetLabel != "" {
		to := time.Now().UTC().Truncate(time.Hour)
		from := to.Add(-time.Hour)
		if expl, eerr := e.store.AnomalyExplain(ctx, v.TargetLabel, from, to); eerr == nil {
			if v.Extra == nil {
				v.Extra = map[string]any{}
			}
			v.Extra["explanation"] = expl
		} else {
			log.Printf("alert engine: anomaly explain error for %s: %v", v.TargetLabel, eerr)
		}
	}
}

// labelTargetIP maps a non-IP target identity (a link tag for link_capacity /
// anomaly rules) onto a stable synthetic address.
//
// The alerts table stores target_ip as IPv6 and dedup keys on
// (rule_id, target_ip), so every label-targeted violation used to resolve to
// the zero IP: two saturated links collapsed into a single alert row and
// heartbeats landed on whichever of them was inserted first. Hashing the label
// gives each one its own row. The address is built inside 0100::/64 — the
// RFC 6666 discard-only prefix — so it can never collide with an address seen
// in real traffic nor be mistaken for a routable target. The human-readable
// label remains authoritative in details.extra.target.
func labelTargetIP(label string) net.IP {
	h := fnv.New64a()
	_, _ = h.Write([]byte(label))
	ip := make(net.IP, net.IPv6len)
	ip[0] = 0x01 // 0100::/64
	binary.BigEndian.PutUint64(ip[8:], h.Sum64())
	return ip
}

// handleViolation turns one violation into a heartbeat, a re-opened alert or
// a new alert.
//
// Lifecycle (CLAUDE.md "Alert engine", bug #24): an alert auto-resolves after
// ALERT_STALE_THRESHOLD without a violation. A target hovering around a
// threshold used to resolve and re-fire — one new alert and one notification
// every ~20 minutes all day. Now the most recent alert of the rule for the
// target decides, if it was last seen within the re-arm window
// (max(cooldown_seconds, ALERT_REARM_WINDOW)):
//
//   - active / acknowledged / muted: heartbeat it (an acknowledged incident
//     no longer spawns a duplicate alert while it is still going on);
//   - resolved: re-open it (status active, peak metric, reopen_count + 1),
//     without a new notification; auto_block rules re-announce, since the
//     previous block may have expired — unless the value is at least
//     RearmEscalation times the alert's peak, which is a new incident.
//
// Otherwise a new alert is stored, enriched and notified.
func (e *Engine) handleViolation(ctx context.Context, rule model.AlertRule, v store.AlertViolation, metricType string, webhooks map[string]model.WebhookConfig) {
	now := e.now()
	// Identity of this violation. Non-IP targets (link_capacity, anomaly) carry
	// theirs in TargetLabel and get a synthetic address (see labelTargetIP) so
	// that the recent-alert key, the DB lookup, the heartbeat and the stored
	// row all agree on one identity per label.
	targetIP := v.TargetIP
	if targetIP == nil && v.TargetLabel != "" {
		targetIP = labelTargetIP(v.TargetLabel)
	}
	target := targetIP.String()
	// Logs stay readable: show the link tag rather than its synthetic address.
	display := target
	if v.TargetLabel != "" {
		display = v.TargetLabel
	}
	key := rule.ID + "|" + target

	threshold := float64(rule.ThresholdBps)
	switch metricType {
	case "pps":
		threshold = float64(rule.ThresholdPps)
	case "count":
		threshold = float64(rule.ThresholdCount)
	case "percent":
		threshold = float64(rule.ThresholdCount)
	}

	// Fast path: this process stored or re-opened the alert moments ago and
	// the INSERT / mutation may not be visible to a read yet.
	e.mu.Lock()
	r, ok := e.recent[key]
	e.mu.Unlock()
	if ok && now.Sub(r.at) < e.recentTrust() {
		e.touch(ctx, r.id, now)
		return
	}

	last, err := e.store.LatestAlert(ctx, rule.ID, targetIP)
	if err != nil {
		log.Printf("alert engine: latest alert lookup error: %v", err)
		return
	}
	if last.ID != "" && now.Sub(last.LastSeenAt) < e.rearmFor(rule) {
		if last.Status != "resolved" {
			e.touch(ctx, last.ID, now)
			return
		}
		if v.MetricValue >= RearmEscalation*last.MetricValue && last.MetricValue > 0 {
			// Much worse than the incident it would re-open: a new incident,
			// stored and notified as such (below).
			log.Printf("alert engine: %s [%s] %s escalated to %.2f %s (previous peak %.2f): new alert instead of a re-open",
				rule.Severity, rule.Name, display, v.MetricValue, metricType, last.MetricValue)
		} else {
			e.reopen(ctx, rule, v, last, key, target, display, metricType, threshold, now)
			return
		}
	}

	// A new alert is going to be stored — only now is the enrichment worth its
	// flows_raw scans.
	e.enrichViolation(ctx, rule, &v)

	// Create new alert
	e.insertNew(ctx, rule, v, key, target, display, metricType, threshold, now, webhooks)
}

// RearmEscalation: a violation at least this many times the peak of the
// resolved alert it would re-open is a new incident (new alert, notified),
// not a re-open — the re-arm window must not hide an attack that resumes ten
// times stronger.
const RearmEscalation = 2.0

// reopen re-arms a resolved alert of the same rule and target (see
// handleViolation).
func (e *Engine) reopen(ctx context.Context, rule model.AlertRule, v store.AlertViolation, last store.AlertState, key, target, display, metricType string, threshold float64, now time.Time) {
	if err := e.store.ReopenAlert(ctx, last.ID, now, v.MetricValue); err != nil {
		log.Printf("alert engine: re-open alert error: %v", err)
		return
	}
	metrics.AlertsReopened.WithLabelValues(rule.Severity).Inc()
	e.remember(key, last.ID, now)
	log.Printf("alert engine: re-armed %s [%s] %s — %.2f %s (threshold %.2f), resolved %s ago",
		rule.Severity, rule.Name, display, v.MetricValue, metricType, threshold, now.Sub(last.LastSeenAt).Round(time.Second))
	// The previous block may have expired: auto_block rules announce again
	// (safeAutoBlock skips a target that is still blocked).
	if rule.Action == "auto_block" && e.blocker != nil && v.TargetIP != nil {
		go e.safeAutoBlock(rule, model.Alert{
			ID: last.ID, RuleID: rule.ID, RuleName: rule.Name, Severity: rule.Severity,
			TargetIP: target, MetricValue: v.MetricValue, Threshold: threshold, MetricType: metricType,
		}, v)
	}
}

// insertNew stores, notifies and (auto_block) blocks a new alert. v has
// already been enriched.
func (e *Engine) insertNew(ctx context.Context, rule model.AlertRule, v store.AlertViolation, key, target, display, metricType string, threshold float64, now time.Time, webhooks map[string]model.WebhookConfig) {
	details := store.AlertDetails{
		UniqueCount:  v.UniqueCount,
		WindowSecond: rule.WindowSeconds,
		TopSources:   v.TopSources,
	}
	if v.TargetLabel != "" {
		details.Extra = map[string]any{"target": v.TargetLabel}
	}
	// Merge rule-type-specific extras (e.g. anomaly baseline/current/deviation
	// and the top-contributor explanation) into the details payload.
	for k, val := range v.Extra {
		if details.Extra == nil {
			details.Extra = map[string]any{}
		}
		details.Extra[k] = val
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

	metrics.AlertsFired.WithLabelValues(rule.Severity).Inc()

	e.remember(key, alert.ID, now)

	log.Printf("alert engine: 🚨 %s [%s] %s — %.2f %s (threshold %.2f)",
		rule.Severity, rule.Name, display, v.MetricValue, metricType, threshold)

	// Dispatch webhooks (async, don't block evaluation). Notifications are read
	// by humans, so they carry the link tag rather than the synthetic address
	// that identifies a label-targeted alert in the database.
	if e.notifier != nil {
		notified := alert
		notified.TargetIP = display
		for _, webhookID := range rule.WebhookIDs {
			if wh, ok := webhooks[webhookID]; ok && wh.Enabled {
				if !severityMeets(alert.Severity, wh.MinSeverity) {
					continue
				}
				go e.safeNotify(wh, notified)
			}
		}
	}

	// Auto-block if rule action is "auto_block" and a blocker is configured.
	// Label-targeted rules are excluded: their stored target is a synthetic
	// address standing in for a link tag, and announcing a blackhole for it
	// would be meaningless at best.
	if rule.Action == "auto_block" && e.blocker != nil && v.TargetIP != nil {
		go e.safeAutoBlock(rule, alert, v)
	}
}

func (e *Engine) safeAutoBlock(rule model.AlertRule, alert model.Alert, v store.AlertViolation) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("alert engine: auto-block panic: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ip := net.ParseIP(alert.TargetIP)
	if ip == nil {
		return
	}

	// Skip if already blocked
	if e.blockStore != nil {
		if existing, _ := e.blockStore.FindActiveBlock(ctx, alert.TargetIP); existing != "" {
			return
		}
	}

	// Auto-generate description with alert context
	topSrc := ""
	if len(v.TopSources) > 0 {
		topSrc = fmt.Sprintf(" Top sources: %s", strings.Join(v.TopSources, ", "))
	}
	description := fmt.Sprintf("Auto-blocked by rule '%s': %.2f %s exceeded threshold %.2f.%s",
		rule.Name, alert.MetricValue, alert.MetricType, alert.Threshold, topSrc)

	duration := time.Hour // 1h default for auto-blocks

	if err := e.blocker.Announce(ctx, ip, duration, description); err != nil {
		log.Printf("alert engine: auto-block FAILED for %s: %v", alert.TargetIP, err)
		return
	}

	log.Printf("alert engine: AUTO-BLOCKED %s (rule=%s, duration=1h)", alert.TargetIP, rule.Name)

	// Persist to ClickHouse
	if e.blockStore != nil {
		now := time.Now().UTC()
		expiresAt := now.Add(duration)
		prefixLen := bgp.HostPrefixLen(ip)
		block := model.BGPBlock{
			ID:              uuid.NewString(),
			IP:              alert.TargetIP,
			PrefixLen:       prefixLen,
			Reason:          "auto_block",
			Description:     description,
			Status:          "active",
			BlockedBy:       "engine",
			BlockedAt:       now,
			AlertID:         alert.ID,
			RuleName:        rule.Name,
			MetricValue:     alert.MetricValue,
			MetricType:      alert.MetricType,
			Threshold:       alert.Threshold,
			TopSources:      v.TopSources,
			DurationSeconds: uint32(duration.Seconds()),
			ExpiresAt:       &expiresAt,
		}
		if err := e.blockStore.InsertBlock(ctx, block); err != nil {
			log.Printf("alert engine: auto-block CH insert failed for %s: %v", alert.TargetIP, err)
		}
	}
}

// touch heartbeats an alert (UpdateAlertLastSeen throttles the writes).
func (e *Engine) touch(ctx context.Context, id string, now time.Time) {
	if err := e.store.UpdateAlertLastSeen(ctx, id, now); err != nil {
		log.Printf("alert engine: heartbeat error: %v", err)
	}
}

// remember records an alert this process just inserted or re-opened.
func (e *Engine) remember(key, id string, now time.Time) {
	e.mu.Lock()
	e.recent[key] = recentAlert{id: id, at: now}
	e.mu.Unlock()
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

// CooldownSnapshot returns rule_id|target → when this process last stored or
// re-opened an alert for it (for debugging and tests).
func (e *Engine) CooldownSnapshot() map[string]time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]time.Time, len(e.recent))
	for k, r := range e.recent {
		out[k] = r.at
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
			Description:     descHighInbound,
			RuleType:        "volume_in",
			Enabled:         true,
			ThresholdBps:    500_000_000,
			BaselineFactor:  factorHighVolume,
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
			Description:     descPortScan,
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
			Description:     descHighOutbound,
			RuleType:        "volume_out",
			Enabled:         true,
			ThresholdBps:    500_000_000,
			BaselineFactor:  factorHighVolume,
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
			Description:     descExfil,
			RuleType:        "volume_out",
			Enabled:         true,
			ThresholdBps:    50_000_000,
			BaselineFactor:  factorExfil,
			WindowSeconds:   300,
			CooldownSeconds: 1800,
			Severity:        "info",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Carpet bombing ───────────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "Carpet bomb /24 (warning)",
			Description:     "Aggregate traffic to any /24 in our prefixes exceeds 2 Gbps for 60s — distributed attack",
			RuleType:        "subnet_flood",
			Enabled:         true,
			ThresholdBps:    2_000_000_000,
			SubnetPrefixLen: 24,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		// ── SMTP abuse / compromised mail relay ───────────────────
		{
			ID:              uuid.NewString(),
			Name:            "SMTP abuse (compromised relay)",
			Description:     "An internal host is sending > 50 pps to SMTP ports (25/465/587) for 5 min — likely a compromised spam relay",
			RuleType:        "smtp_abuse",
			Enabled:         true,
			ThresholdPps:    50,
			WindowSeconds:   300,
			CooldownSeconds: 1800,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Storage / disk usage ─────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "Disk usage high",
			Description:     "A ClickHouse data disk is over 80% full — review retention before it fills",
			RuleType:        "disk_usage",
			Enabled:         true,
			ThresholdCount:  80, // percent (carried in threshold_count for disk_usage)
			WindowSeconds:   60,
			CooldownSeconds: 3600,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		{
			ID:              uuid.NewString(),
			Name:            "Disk usage critical",
			Description:     "A ClickHouse data disk is over 90% full — imminent ingestion failure risk",
			RuleType:        "disk_usage",
			Enabled:         true,
			ThresholdCount:  90, // percent (carried in threshold_count for disk_usage)
			WindowSeconds:   60,
			CooldownSeconds: 3600,
			Severity:        "critical",
			Action:          "ack_required",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Capacity planning ────────────────────────────────────
		{
			ID:              uuid.NewString(),
			Name:            "Link capacity high",
			Description:     "A link's 95th-percentile utilization exceeded 80% of its configured capacity over the last 24h — plan an upgrade",
			RuleType:        "link_capacity",
			Enabled:         true,
			ThresholdCount:  80, // percent (carried in threshold_count for link_capacity)
			WindowSeconds:   86400,
			CooldownSeconds: 21600,
			Severity:        "warning",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		// ── Statistical anomaly (disabled by default) ────────────
		{
			ID:          uuid.NewString(),
			Name:        "Link throughput anomaly (example)",
			Description: "Fires when a link's current-hour throughput exceeds median + k·MAD of the same hour-of-week over the last 8 weeks. Disabled by default: needs per-deployment tuning of sensitivity (threshold_count carries k·10, so 25 → k=2.5) and only fires once enough hourly history has accumulated.",
			RuleType:    "anomaly",
			Enabled:     false, // disabled: baseline needs weeks of history + tuning
			// ThresholdCount carries k*10 for anomaly rules (25 → k=2.5).
			ThresholdCount:  25,
			WindowSeconds:   3600,
			CooldownSeconds: 3600,
			Severity:        "info",
			Action:          "notify",
			CreatedAt:       now,
			UpdatedAt:       now,
		},

		{
			ID:              uuid.NewString(),
			Name:            "Carpet bomb /24 (critical)",
			Description:     "Aggregate traffic to any /24 in our prefixes exceeds 5 Gbps for 60s — major distributed attack",
			RuleType:        "subnet_flood",
			Enabled:         true,
			ThresholdBps:    5_000_000_000,
			SubnetPrefixLen: 24,
			WindowSeconds:   60,
			CooldownSeconds: 300,
			Severity:        "critical",
			Action:          "ack_required",
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
