package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// ListAlertRules returns all non-deleted alert rules.
func (s *ClickHouseStore) ListAlertRules(ctx context.Context) ([]model.AlertRule, error) {
	query := `
		SELECT id, name, description, rule_type, enabled,
			threshold_bps, threshold_pps, threshold_count,
			window_seconds, cooldown_seconds, severity,
			target_filter, custom_sql, action, webhook_ids,
			created_at, updated_at,
			hostgroup_id, subnet_prefix_len, baseline_factor
		FROM alert_rules FINAL
		WHERE deleted = 0
		ORDER BY name
	`
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.AlertRule
	for rows.Next() {
		var r model.AlertRule
		var enabled uint8
		var webhookIDs []string
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Description, &r.RuleType, &enabled,
			&r.ThresholdBps, &r.ThresholdPps, &r.ThresholdCount,
			&r.WindowSeconds, &r.CooldownSeconds, &r.Severity,
			&r.TargetFilter, &r.CustomSQL, &r.Action, &webhookIDs,
			&r.CreatedAt, &r.UpdatedAt,
			&r.HostgroupID, &r.SubnetPrefixLen, &r.BaselineFactor,
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		r.WebhookIDs = webhookIDs
		// Normalize zero UUID to empty string for JSON serialization
		if r.HostgroupID == "00000000-0000-0000-0000-000000000000" {
			r.HostgroupID = ""
		}
		results = append(results, r)
	}
	return results, nil
}

// UpsertAlertRule inserts or updates a rule.
func (s *ClickHouseStore) UpsertAlertRule(ctx context.Context, r model.AlertRule) error {
	enabled := uint8(0)
	if r.Enabled {
		enabled = 1
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = r.UpdatedAt
	}
	hgID := r.HostgroupID
	if hgID == "" {
		hgID = "00000000-0000-0000-0000-000000000000"
	}
	webhookIDs := r.WebhookIDs
	if webhookIDs == nil {
		webhookIDs = []string{}
	}
	return s.insertRow(ctx, `
		INSERT INTO alert_rules (id, name, description, rule_type, enabled,
			threshold_bps, threshold_pps, threshold_count,
			window_seconds, cooldown_seconds, severity,
			target_filter, custom_sql, action, webhook_ids,
			created_at, updated_at, deleted,
			hostgroup_id, subnet_prefix_len, baseline_factor)`,
		r.ID, r.Name, r.Description, r.RuleType, enabled,
		r.ThresholdBps, r.ThresholdPps, r.ThresholdCount,
		r.WindowSeconds, r.CooldownSeconds, r.Severity,
		r.TargetFilter, r.CustomSQL, r.Action, webhookIDs,
		r.CreatedAt, r.UpdatedAt, uint8(0),
		hgID, r.SubnetPrefixLen, r.BaselineFactor,
	)
}

// insertRow writes one row through the native protocol (PrepareBatch +
// Append). An INSERT … VALUES with named parameters reaches the server with
// every value rendered as a SQL literal or expression (cast(5e+08,
// 'Float64'), toDateTime('…')): the fast VALUES parser rejects it and the
// server falls back to the full SQL parser — harmless, but each insert
// bumped CANNOT_PARSE_INPUT_ASSERTION_FAILED in system.errors (~16k a day in
// production, mostly alert inserts).
func (s *ClickHouseStore) insertRow(ctx context.Context, insert string, values ...any) error {
	batch, err := s.conn.PrepareBatch(ctx, insert)
	if err != nil {
		return err
	}
	if err := batch.Append(values...); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

// DeleteAlertRule soft-deletes a rule.
func (s *ClickHouseStore) DeleteAlertRule(ctx context.Context, id string) error {
	return s.conn.Exec(ctx, `
		INSERT INTO alert_rules (id, name, rule_type, deleted, updated_at)
		SELECT id, name, rule_type, 1, now()
		FROM alert_rules FINAL
		WHERE id = @id
	`, clickhouse.Named("id", id))
}

// ListAlerts returns alerts with optional status filter.
func (s *ClickHouseStore) ListAlerts(ctx context.Context, status string, limit int) ([]model.Alert, error) {
	where := "1=1"
	args := []any{}
	if status != "" {
		where = "status = @status"
		args = append(args, clickhouse.Named("status", status))
	}
	if limit <= 0 {
		limit = 100
	}
	args = append(args, clickhouse.Named("limit", limit))

	query := fmt.Sprintf(`
		SELECT id, rule_id, rule_name, severity,
			triggered_at, last_seen_at, resolved_at,
			toString(target_ip) AS target_ip, target_as, protocol,
			metric_value, threshold, metric_type, details,
			status, acknowledged_by, acknowledged_at,
			action_taken, action_by, action_at, reopen_count
		FROM alerts
		WHERE %s
		ORDER BY triggered_at DESC
		LIMIT @limit
	`, where)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Alert
	for rows.Next() {
		var a model.Alert
		var resolvedAt, ackedAt, actionAt time.Time
		if err := rows.Scan(
			&a.ID, &a.RuleID, &a.RuleName, &a.Severity,
			&a.TriggeredAt, &a.LastSeenAt, &resolvedAt,
			&a.TargetIP, &a.TargetAS, &a.Protocol,
			&a.MetricValue, &a.Threshold, &a.MetricType, &a.Details,
			&a.Status, &a.AcknowledgedBy, &ackedAt,
			&a.ActionTaken, &a.ActionBy, &actionAt, &a.ReopenCount,
		); err != nil {
			return nil, err
		}
		a.TargetIP = cleanIPv4Mapped(a.TargetIP)
		if !resolvedAt.IsZero() && resolvedAt.Unix() > 0 {
			a.ResolvedAt = &resolvedAt
		}
		if !ackedAt.IsZero() && ackedAt.Unix() > 0 {
			a.AcknowledgedAt = &ackedAt
		}
		if !actionAt.IsZero() && actionAt.Unix() > 0 {
			a.ActionAt = &actionAt
		}
		results = append(results, a)
	}
	return results, nil
}

// CountAlertsBySeverity returns counts by severity for active alerts.
func (s *ClickHouseStore) CountAlertsBySeverity(ctx context.Context) (map[string]uint64, error) {
	query := `
		SELECT severity, count() AS cnt
		FROM alerts
		WHERE status = 'active'
		GROUP BY severity
	`
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]uint64)
	for rows.Next() {
		var sev string
		var cnt uint64
		if err := rows.Scan(&sev, &cnt); err != nil {
			return nil, err
		}
		counts[sev] = cnt
	}
	return counts, nil
}

// InsertAlert stores a new alert (status active). See insertRow for why it
// does not use INSERT … VALUES.
func (s *ClickHouseStore) InsertAlert(ctx context.Context, a model.Alert) error {
	return s.insertRow(ctx, `
		INSERT INTO alerts (id, rule_id, rule_name, severity,
			triggered_at, last_seen_at, target_ip, target_as, protocol,
			metric_value, threshold, metric_type, details, status)`,
		a.ID, a.RuleID, a.RuleName, a.Severity,
		a.TriggeredAt, a.LastSeenAt, ipToIPv6(net.ParseIP(a.TargetIP)), a.TargetAS, a.Protocol,
		a.MetricValue, a.Threshold, a.MetricType, a.Details, "active",
	)
}

// AcknowledgeAlert marks an alert as acknowledged by a user.
func (s *ClickHouseStore) AcknowledgeAlert(ctx context.Context, id, userEmail string) error {
	return s.updateAlertStatus(ctx, id, "acknowledged", "acknowledged_by", userEmail, "acknowledged_at")
}

// ResolveAlert marks an alert as resolved.
func (s *ClickHouseStore) ResolveAlert(ctx context.Context, id string) error {
	return s.updateAlertStatus(ctx, id, "resolved", "", "", "resolved_at")
}

func (s *ClickHouseStore) updateAlertStatus(ctx context.Context, id, newStatus, userField, userValue, tsField string) error {
	setParts := []string{fmt.Sprintf("status = '%s'", newStatus), fmt.Sprintf("%s = now()", tsField)}
	args := []any{clickhouse.Named("id", id)}
	if userField != "" {
		setParts = append(setParts, fmt.Sprintf("%s = @user", userField))
		args = append(args, clickhouse.Named("user", userValue))
	}
	query := fmt.Sprintf("ALTER TABLE alerts UPDATE %s WHERE id = @id", strings.Join(setParts, ", "))
	return s.conn.Exec(ctx, query, args...)
}

// AlertState is the most recent alert of one (rule, target): what the engine
// needs to heartbeat, re-open or replace it. ID is "" when there is none.
type AlertState struct {
	ID          string
	Status      string // active | acknowledged | resolved | muted
	TriggeredAt time.Time
	LastSeenAt  time.Time
	MetricValue float64 // peak value (re-opens keep the highest)
}

// LatestAlert returns the most recent alert of a rule for a target, in any
// status. The engine decides from it whether a violation heartbeats that
// alert, re-opens it (resolved within the re-arm window) or starts a new one.
// The alerts table is small (a year of alerts, partitioned by month).
func (s *ClickHouseStore) LatestAlert(ctx context.Context, ruleID string, targetIP net.IP) (AlertState, error) {
	var (
		st    AlertState
		count uint64
	)
	// An aggregate always yields one row: count() = 0 means no alert.
	err := s.conn.QueryRow(ctx, `
		SELECT count(),
			toString(argMax(a.id, a.triggered_at)),
			argMax(a.status, a.triggered_at),
			max(a.triggered_at),
			argMax(a.last_seen_at, a.triggered_at),
			argMax(a.metric_value, a.triggered_at)
		FROM alerts AS a
		WHERE a.rule_id = @rule_id AND a.target_ip = @target_ip
	`,
		clickhouse.Named("rule_id", ruleID),
		clickhouse.Named("target_ip", ipToIPv6(targetIP)),
	).Scan(&count, &st.ID, &st.Status, &st.TriggeredAt, &st.LastSeenAt, &st.MetricValue)
	if err != nil {
		return AlertState{}, fmt.Errorf("latest alert: %w", err)
	}
	if count == 0 {
		return AlertState{}, nil
	}
	return st, nil
}

// ReopenAlert re-arms a resolved alert instead of creating a new one: status
// back to active, last_seen_at = ts, metric_value raised to the new value when
// higher (it reads as the peak), reopen_count + 1. One mutation per re-open;
// the engine never issues it twice for the same alert within a cycle or two
// (see Engine.recent).
func (s *ClickHouseStore) ReopenAlert(ctx context.Context, id string, ts time.Time, value float64) error {
	err := s.conn.Exec(ctx, `
		ALTER TABLE alerts UPDATE
			status = 'active',
			resolved_at = toDateTime(0),
			last_seen_at = toDateTime(@ts, 'UTC'),
			metric_value = greatest(metric_value, @value),
			reopen_count = reopen_count + 1
		WHERE id = @id
	`,
		clickhouse.Named("id", id),
		clickhouse.Named("ts", uint32(ts.Unix())),
		clickhouse.Named("value", value),
	)
	if err == nil {
		// last_seen_at was just written: the next heartbeat can be throttled.
		s.hbMu.Lock()
		if s.hbWritten != nil {
			s.hbWritten[id] = ts
		}
		s.hbMu.Unlock()
	}
	return err
}

// Heartbeat throttling constants. ClickHouse mutations (ALTER ... UPDATE) are
// asynchronous, rewrite whole parts and queue globally per table; issuing them
// at the alert engine's cycle rate, once per active alert, is a reliable way to
// saturate a server. A heartbeat only has to be fresher than the auto-resolve
// threshold, so it is safe to persist it far less often than it is computed.
const (
	// alertHeartbeatDivisor derives the throttle window from the auto-resolve
	// threshold the engine passes to AutoResolveStaleAlerts. A third leaves ~2x
	// headroom over the persisted staleness (window + one eval interval) before
	// a still-firing alert could ever look stale, so the observable lifecycle is
	// unchanged.
	alertHeartbeatDivisor = 3
	// alertHeartbeatMinWindow keeps very short stale thresholds writing through
	// unthrottled rather than risking alert flapping to save a few mutations.
	alertHeartbeatMinWindow = 15 * time.Second
	// alertHeartbeatMaxIdle bounds the throttle map: entries for alerts that
	// stopped firing are dropped, so it cannot grow with attacker-driven IDs.
	alertHeartbeatMaxIdle = time.Hour
)

// UpdateAlertLastSeen bumps last_seen_at on an active alert (heartbeat).
// Writes are throttled per alert (see the constants above); a skipped call is a
// no-op success, since the previously persisted timestamp is still fresh enough
// for AutoResolveStaleAlerts.
func (s *ClickHouseStore) UpdateAlertLastSeen(ctx context.Context, id string, ts time.Time) error {
	if !s.heartbeatDue(id, ts) {
		return nil
	}
	err := s.conn.Exec(ctx,
		"ALTER TABLE alerts UPDATE last_seen_at = @ts WHERE id = @id",
		clickhouse.Named("id", id),
		clickhouse.Named("ts", ts),
	)
	if err != nil {
		// The write did not land — forget the bookkeeping so the next cycle
		// retries instead of throttling away a heartbeat that never happened.
		s.forgetHeartbeat(id)
	}
	return err
}

// heartbeatDue records the intent to persist a heartbeat for id at ts and
// reports whether the write should actually be issued.
func (s *ClickHouseStore) heartbeatDue(id string, ts time.Time) bool {
	s.hbMu.Lock()
	defer s.hbMu.Unlock()

	if s.hbWindow <= 0 {
		return true // threshold unknown yet — never throttle blindly
	}
	if s.hbWritten == nil {
		s.hbWritten = make(map[string]time.Time)
	} else if last, ok := s.hbWritten[id]; ok && ts.Sub(last) < s.hbWindow {
		return false
	}
	for k, v := range s.hbWritten {
		if ts.Sub(v) > alertHeartbeatMaxIdle {
			delete(s.hbWritten, k)
		}
	}
	s.hbWritten[id] = ts
	return true
}

func (s *ClickHouseStore) forgetHeartbeat(id string) {
	s.hbMu.Lock()
	delete(s.hbWritten, id)
	s.hbMu.Unlock()
}

// AutoResolveStaleAlerts resolves active alerts whose last_seen_at is older than threshold.
// The resolving mutation is only issued when there is actually something to
// resolve: the engine calls this every cycle, and unconditionally queueing an
// ALTER ... UPDATE meant a permanent mutation stream on a system with no alerts
// at all. The count probe is a cheap read over a small, partitioned table.
//
// This is also where the heartbeat throttle window is derived: the engine passes
// its own auto-resolve threshold here at the start of every cycle, before any
// heartbeat, so the two can never disagree about what "stale" means.
func (s *ClickHouseStore) AutoResolveStaleAlerts(ctx context.Context, olderThan time.Duration) error {
	s.setHeartbeatWindow(olderThan)
	now := s.now()
	cutoff := clickhouse.Named("cutoff", uint32(now.Add(-olderThan).Unix()))

	var stale uint64
	if err := s.conn.QueryRow(ctx, `
		SELECT count() FROM alerts
		WHERE status = 'active' AND last_seen_at < toDateTime(@cutoff, 'UTC')
	`, cutoff).Scan(&stale); err != nil {
		return fmt.Errorf("count stale alerts: %w", err)
	}
	if stale == 0 {
		return nil
	}

	return s.conn.Exec(ctx, `
		ALTER TABLE alerts UPDATE status = 'resolved', resolved_at = toDateTime(@now, 'UTC')
		WHERE status = 'active' AND last_seen_at < toDateTime(@cutoff, 'UTC')
	`, cutoff, clickhouse.Named("now", uint32(now.Unix())))
}

// setHeartbeatWindow updates the throttle window from the caller's auto-resolve
// threshold. A threshold too short to throttle safely disables throttling.
func (s *ClickHouseStore) setHeartbeatWindow(olderThan time.Duration) {
	window := olderThan / alertHeartbeatDivisor
	if window < alertHeartbeatMinWindow {
		window = 0
	}
	s.hbMu.Lock()
	s.hbWindow = window
	s.hbMu.Unlock()
}

// =============================================================================
// Webhooks
// =============================================================================

func (s *ClickHouseStore) ListWebhooks(ctx context.Context) ([]model.WebhookConfig, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT id, name, webhook_type, url, enabled, min_severity, headers, template, created_at, updated_at
		FROM webhook_configs FINAL
		WHERE deleted = 0
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []model.WebhookConfig
	for rows.Next() {
		var w model.WebhookConfig
		var enabled uint8
		if err := rows.Scan(
			&w.ID, &w.Name, &w.WebhookType, &w.URL, &enabled,
			&w.MinSeverity, &w.Headers, &w.Template,
			&w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, err
		}
		w.Enabled = enabled == 1
		results = append(results, w)
	}
	return results, nil
}

func (s *ClickHouseStore) UpsertWebhook(ctx context.Context, w model.WebhookConfig) error {
	enabled := uint8(0)
	if w.Enabled {
		enabled = 1
	}
	if w.UpdatedAt.IsZero() {
		w.UpdatedAt = time.Now().UTC()
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = w.UpdatedAt
	}
	return s.insertRow(ctx, `
		INSERT INTO webhook_configs (id, name, webhook_type, url, enabled, min_severity, headers, template, created_at, updated_at, deleted)`,
		w.ID, w.Name, w.WebhookType, w.URL, enabled, w.MinSeverity, w.Headers, w.Template,
		w.CreatedAt, w.UpdatedAt, uint8(0),
	)
}

func (s *ClickHouseStore) DeleteWebhook(ctx context.Context, id string) error {
	return s.conn.Exec(ctx, `
		INSERT INTO webhook_configs (id, name, webhook_type, url, deleted, updated_at)
		SELECT id, name, webhook_type, url, 1, now()
		FROM webhook_configs FINAL WHERE id = @id
	`, clickhouse.Named("id", id))
}

// =============================================================================
// Audit log
// =============================================================================

func (s *ClickHouseStore) WriteAuditLog(ctx context.Context, e model.AuditLogEntry) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	clientIP := net.ParseIP(e.ClientIP)
	if clientIP == nil {
		clientIP = net.IPv6zero
	}
	return s.insertRow(ctx, `
		INSERT INTO audit_log (ts, user_sub, user_email, user_role, action, resource, params, client_ip, user_agent, result, error_message)`,
		e.Timestamp, e.UserSub, e.UserEmail, e.UserRole, e.Action, e.Resource, e.Params,
		ipToIPv6(clientIP), e.UserAgent, e.Result, e.ErrorMessage,
	)
}

func (s *ClickHouseStore) ListAuditLog(ctx context.Context, from, to time.Time, userEmail, action string, limit int) ([]model.AuditLogEntry, error) {
	where := []string{"ts >= @from AND ts < @to"}
	args := []any{
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	}
	if userEmail != "" {
		where = append(where, "user_email = @user_email")
		args = append(args, clickhouse.Named("user_email", userEmail))
	}
	if action != "" {
		where = append(where, "action = @action")
		args = append(args, clickhouse.Named("action", action))
	}
	if limit <= 0 {
		limit = 500
	}
	args = append(args, clickhouse.Named("limit", limit))

	query := fmt.Sprintf(`
		SELECT ts, user_sub, user_email, user_role, action, resource, params,
			toString(client_ip) AS client_ip, user_agent, result, error_message
		FROM audit_log
		WHERE %s
		ORDER BY ts DESC
		LIMIT @limit
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []model.AuditLogEntry
	for rows.Next() {
		var e model.AuditLogEntry
		if err := rows.Scan(
			&e.Timestamp, &e.UserSub, &e.UserEmail, &e.UserRole,
			&e.Action, &e.Resource, &e.Params,
			&e.ClientIP, &e.UserAgent, &e.Result, &e.ErrorMessage,
		); err != nil {
			return nil, err
		}
		e.ClientIP = cleanIPv4Mapped(e.ClientIP)
		results = append(results, e)
	}
	return results, nil
}

// DetailsJSON helpers — for building alert.details blobs
type AlertDetails struct {
	TopSources   []string       `json:"top_sources,omitempty"`
	UniqueCount  uint64         `json:"unique_count,omitempty"`
	WindowSecond uint32         `json:"window_seconds,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

func MarshalAlertDetails(d AlertDetails) string {
	b, _ := json.Marshal(d)
	return string(b)
}
