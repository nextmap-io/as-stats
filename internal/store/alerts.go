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
			created_at, updated_at
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
		); err != nil {
			return nil, err
		}
		r.Enabled = enabled == 1
		r.WebhookIDs = webhookIDs
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
	return s.conn.Exec(ctx, `
		INSERT INTO alert_rules (id, name, description, rule_type, enabled,
			threshold_bps, threshold_pps, threshold_count,
			window_seconds, cooldown_seconds, severity,
			target_filter, custom_sql, action, webhook_ids,
			created_at, updated_at, deleted)
		VALUES (@id, @name, @description, @rule_type, @enabled,
			@threshold_bps, @threshold_pps, @threshold_count,
			@window_seconds, @cooldown_seconds, @severity,
			@target_filter, @custom_sql, @action, @webhook_ids,
			@created_at, @updated_at, 0)
	`,
		clickhouse.Named("id", r.ID),
		clickhouse.Named("name", r.Name),
		clickhouse.Named("description", r.Description),
		clickhouse.Named("rule_type", r.RuleType),
		clickhouse.Named("enabled", enabled),
		clickhouse.Named("threshold_bps", r.ThresholdBps),
		clickhouse.Named("threshold_pps", r.ThresholdPps),
		clickhouse.Named("threshold_count", r.ThresholdCount),
		clickhouse.Named("window_seconds", r.WindowSeconds),
		clickhouse.Named("cooldown_seconds", r.CooldownSeconds),
		clickhouse.Named("severity", r.Severity),
		clickhouse.Named("target_filter", r.TargetFilter),
		clickhouse.Named("custom_sql", r.CustomSQL),
		clickhouse.Named("action", r.Action),
		clickhouse.Named("webhook_ids", r.WebhookIDs),
		clickhouse.Named("created_at", r.CreatedAt),
		clickhouse.Named("updated_at", r.UpdatedAt),
	)
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
			action_taken, action_by, action_at
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
			&a.ActionTaken, &a.ActionBy, &actionAt,
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

// InsertAlert inserts a new alert or updates last_seen_at on an existing active one.
func (s *ClickHouseStore) InsertAlert(ctx context.Context, a model.Alert) error {
	return s.conn.Exec(ctx, `
		INSERT INTO alerts (id, rule_id, rule_name, severity,
			triggered_at, last_seen_at, target_ip, target_as, protocol,
			metric_value, threshold, metric_type, details, status)
		VALUES (@id, @rule_id, @rule_name, @severity,
			@triggered_at, @last_seen_at, @target_ip, @target_as, @protocol,
			@metric_value, @threshold, @metric_type, @details, 'active')
	`,
		clickhouse.Named("id", a.ID),
		clickhouse.Named("rule_id", a.RuleID),
		clickhouse.Named("rule_name", a.RuleName),
		clickhouse.Named("severity", a.Severity),
		clickhouse.Named("triggered_at", a.TriggeredAt),
		clickhouse.Named("last_seen_at", a.LastSeenAt),
		clickhouse.Named("target_ip", ipToIPv6(net.ParseIP(a.TargetIP))),
		clickhouse.Named("target_as", a.TargetAS),
		clickhouse.Named("protocol", a.Protocol),
		clickhouse.Named("metric_value", a.MetricValue),
		clickhouse.Named("threshold", a.Threshold),
		clickhouse.Named("metric_type", a.MetricType),
		clickhouse.Named("details", a.Details),
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

// FindActiveAlert returns the ID of an active alert matching rule+target, or "" if none.
// Used by the evaluator to avoid duplicate alerts during cooldown.
func (s *ClickHouseStore) FindActiveAlert(ctx context.Context, ruleID string, targetIP net.IP) (string, time.Time, error) {
	query := `
		SELECT id, triggered_at
		FROM alerts
		WHERE rule_id = @rule_id AND target_ip = @target_ip AND status = 'active'
		ORDER BY triggered_at DESC
		LIMIT 1
	`
	var id string
	var triggeredAt time.Time
	err := s.conn.QueryRow(ctx, query,
		clickhouse.Named("rule_id", ruleID),
		clickhouse.Named("target_ip", ipToIPv6(targetIP)),
	).Scan(&id, &triggeredAt)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", time.Time{}, nil
		}
		return "", time.Time{}, err
	}
	return id, triggeredAt, nil
}

// UpdateAlertLastSeen bumps last_seen_at on an active alert (heartbeat).
func (s *ClickHouseStore) UpdateAlertLastSeen(ctx context.Context, id string, ts time.Time) error {
	return s.conn.Exec(ctx,
		"ALTER TABLE alerts UPDATE last_seen_at = @ts WHERE id = @id",
		clickhouse.Named("id", id),
		clickhouse.Named("ts", ts),
	)
}

// AutoResolveStaleAlerts resolves active alerts whose last_seen_at is older than threshold.
func (s *ClickHouseStore) AutoResolveStaleAlerts(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().UTC().Add(-olderThan)
	return s.conn.Exec(ctx, `
		ALTER TABLE alerts UPDATE status = 'resolved', resolved_at = now()
		WHERE status = 'active' AND last_seen_at < @cutoff
	`, clickhouse.Named("cutoff", cutoff))
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
	return s.conn.Exec(ctx, `
		INSERT INTO webhook_configs (id, name, webhook_type, url, enabled, min_severity, headers, template, created_at, updated_at, deleted)
		VALUES (@id, @name, @webhook_type, @url, @enabled, @min_severity, @headers, @template, @created_at, @updated_at, 0)
	`,
		clickhouse.Named("id", w.ID),
		clickhouse.Named("name", w.Name),
		clickhouse.Named("webhook_type", w.WebhookType),
		clickhouse.Named("url", w.URL),
		clickhouse.Named("enabled", enabled),
		clickhouse.Named("min_severity", w.MinSeverity),
		clickhouse.Named("headers", w.Headers),
		clickhouse.Named("template", w.Template),
		clickhouse.Named("created_at", w.CreatedAt),
		clickhouse.Named("updated_at", w.UpdatedAt),
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
	return s.conn.Exec(ctx, `
		INSERT INTO audit_log (ts, user_sub, user_email, user_role, action, resource, params, client_ip, user_agent, result, error_message)
		VALUES (@ts, @user_sub, @user_email, @user_role, @action, @resource, @params, @client_ip, @user_agent, @result, @error_message)
	`,
		clickhouse.Named("ts", e.Timestamp),
		clickhouse.Named("user_sub", e.UserSub),
		clickhouse.Named("user_email", e.UserEmail),
		clickhouse.Named("user_role", e.UserRole),
		clickhouse.Named("action", e.Action),
		clickhouse.Named("resource", e.Resource),
		clickhouse.Named("params", e.Params),
		clickhouse.Named("client_ip", ipToIPv6(clientIP)),
		clickhouse.Named("user_agent", e.UserAgent),
		clickhouse.Named("result", e.Result),
		clickhouse.Named("error_message", e.ErrorMessage),
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
	TopSources   []string `json:"top_sources,omitempty"`
	UniqueCount  uint64   `json:"unique_count,omitempty"`
	WindowSecond uint32   `json:"window_seconds,omitempty"`
	Extra        map[string]any `json:"extra,omitempty"`
}

func MarshalAlertDetails(d AlertDetails) string {
	b, _ := json.Marshal(d)
	return string(b)
}
