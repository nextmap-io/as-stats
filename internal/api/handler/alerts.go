package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/alerts"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/model"
)

// =============================================================================
// Alerts — list, acknowledge, action
// =============================================================================

// ListAlerts handles GET /api/v1/alerts?status=active|acknowledged|resolved
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	status := r.URL.Query().Get("status")
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	alerts, err := h.Store.ListAlerts(r.Context(), status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: alerts})
}

// AlertsSummary handles GET /api/v1/alerts/summary — counts by severity for active alerts
func (h *Handler) AlertsSummary(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	counts, err := h.Store.CountAlertsBySeverity(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	total := uint64(0)
	for _, c := range counts {
		total += c
	}

	writeJSON(w, http.StatusOK, Response{Data: map[string]any{
		"total":       total,
		"by_severity": counts,
	}})
}

// AcknowledgeAlert handles POST /api/v1/alerts/{id}/ack
func (h *Handler) AcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	id := chi.URLParam(r, "id")
	userEmail := ""
	if user := middleware.GetUser(r.Context()); user != nil {
		userEmail = user.Email
	}

	if err := h.Store.AcknowledgeAlert(r.Context(), id, userEmail); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"status": "acknowledged"}})
}

// ResolveAlert handles POST /api/v1/alerts/{id}/resolve
func (h *Handler) ResolveAlert(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.Store.ResolveAlert(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"status": "resolved"}})
}

// BlockAlertBGP handles POST /api/v1/alerts/{id}/block
// Uses the BGP blocker to announce a blackhole for the alert's target IP.
//
// The answers are deliberately honest: 409 when no real BGP backend is wired
// (the NoopBlocker only logs, and used to answer "blackholed" for an
// announcement that never happened), 422 when the alert has no single host to
// blackhole (link / disk / subnet alerts and their synthetic 0100::/64 ids).
func (h *Handler) BlockAlertBGP(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	if !h.bgpBlockingEnabled() {
		writeError(w, http.StatusConflict, "BGP blackholing is not enabled")
		return
	}

	id := chi.URLParam(r, "id")
	// alerts.id is a UUID column; reject anything else before it reaches SQL so
	// a malformed path segment is a 400 rather than a ClickHouse-level 500.
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid alert id")
		return
	}

	var body struct {
		DurationMinutes int    `json:"duration_minutes"`
		Reason          string `json:"reason"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DurationMinutes <= 0 {
		body.DurationMinutes = 60
	}
	if body.DurationMinutes > 24*60 {
		body.DurationMinutes = 24 * 60 // cap at 24h
	}
	if len(body.Reason) > 500 {
		body.Reason = body.Reason[:500]
	}

	// Load the alert's target IP by id.
	target, found, err := h.alertTarget(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "alert not found")
		return
	}

	ip := net.ParseIP(target.ip)
	if !isHostTarget(ip, target.ruleType) {
		writeError(w, http.StatusUnprocessableEntity, "this alert does not target a single IP")
		return
	}

	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := h.BGPBlocker.Announce(r.Context(), ip, duration, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: map[string]any{
		"status":           "blackholed",
		"target":           ip.String(),
		"duration_minutes": body.DurationMinutes,
	}})
}

// bgpBlockingEnabled reports whether a block request can actually reach a
// router: BGP must be enabled and the blocker must be a real backend, not the
// log-only NoopBlocker the router falls back to when BGP init fails.
func (h *Handler) bgpBlockingEnabled() bool {
	if !h.FeatureBGP || h.BGPBlocker == nil {
		return false
	}
	_, noop := h.BGPBlocker.(*bgp.NoopBlocker)
	return !noop
}

// labelTargetPrefix is 0100::/64 (RFC 6666 discard-only), where the alert
// engine maps non-IP targets (link tags) onto synthetic target_ip values.
var labelTargetPrefix = &net.IPNet{IP: net.ParseIP("100::"), Mask: net.CIDRMask(64, 128)}

// nonHostRuleTypes are the rule types whose target_ip is not a single host:
// link-level rules (synthetic 0100::/64 ids), disk usage (the zero address)
// and subnet_flood (the network address of an aggregate — blackholing its
// first address would protect nothing).
var nonHostRuleTypes = map[string]bool{
	"link_capacity": true,
	"anomaly":       true,
	"disk_usage":    true,
	"subnet_flood":  true,
}

// isHostTarget reports whether an alert's stored target is a real host
// address that a /32 or /128 blackhole would protect.
func isHostTarget(ip net.IP, ruleType string) bool {
	if ip == nil || ip.IsUnspecified() || nonHostRuleTypes[ruleType] {
		return false
	}
	return !labelTargetPrefix.Contains(ip)
}

// alertTargetRow is what the block action needs to know about one alert.
type alertTargetRow struct {
	ip       string // IPv4 as dotted quad, IPv6 as-is
	ruleType string // "" when the rule has since been deleted
}

// alertTarget resolves a single alert's blackhole target by id.
//
// This used to page the 1000 most recent alerts and scan them in memory, so
// blocking anything older than that page 404'd even though the row was right
// there. Aggregating means the query always yields exactly one row, so "not
// found" is a count of zero rather than a driver-specific no-rows error.
func (h *Handler) alertTarget(ctx context.Context, id string) (alertTargetRow, bool, error) {
	var count uint64
	var target alertTargetRow
	var ruleID string
	err := h.Store.QueryRow(ctx, `
		SELECT count(), toString(argMax(a.target_ip, a.triggered_at)), toString(argMax(a.rule_id, a.triggered_at))
		FROM alerts a
		WHERE a.id = @id
	`, clickhouse.Named("id", id)).Scan(&count, &target.ip, &ruleID)
	if err != nil {
		return target, false, err
	}
	if count == 0 {
		return target, false, nil
	}
	target.ip = strings.TrimPrefix(target.ip, "::ffff:")
	// The rule type tells link/disk/subnet alerts apart from host alerts. A
	// deleted rule just leaves it empty; the address checks still apply.
	// (An aggregate without GROUP BY always yields one row: '' when absent.)
	if err := h.Store.QueryRow(ctx, `
		SELECT toString(any(r.rule_type)) FROM alert_rules AS r FINAL
		WHERE r.id = toUUIDOrZero(@rule_id)
	`, clickhouse.Named("rule_id", ruleID)).Scan(&target.ruleType); err != nil {
		return target, false, err
	}
	return target, true, nil
}

// =============================================================================
// Alert rules CRUD (admin only)
// =============================================================================

func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	rules, err := h.Store.ListAlertRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: rules})
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	var rule model.AlertRule
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateRule(rule); err != nil {
		writeError(w, ruleErrStatus(err), err.Error())
		return
	}
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if err := h.Store.UpsertAlertRule(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, Response{Data: rule})
}

func (h *Handler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	var rule model.AlertRule
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	rule.ID = id
	if err := validateRule(rule); err != nil {
		writeError(w, ruleErrStatus(err), err.Error())
		return
	}
	if err := h.Store.UpsertAlertRule(r.Context(), rule); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: rule})
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Store.DeleteAlertRule(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateRule(r model.AlertRule) error {
	validTypes := map[string]bool{
		"volume_in": true, "volume_out": true, "syn_flood": true,
		"amplification": true, "port_scan": true, "icmp_flood": true,
		"udp_flood": true, "connection_flood": true, "subnet_flood": true,
		"smtp_abuse": true, "disk_usage": true, "link_capacity": true,
		"anomaly": true,
	}
	if r.RuleType == "custom" {
		// Accepted by the schema but never evaluated by the engine: storing it
		// would give the operator a rule that silently never fires.
		return errUnprocessable("rule_type custom is not supported: the alert engine cannot evaluate it")
	}
	if !validTypes[r.RuleType] {
		return errBadField("rule_type must be one of: volume_in, volume_out, syn_flood, amplification, port_scan, icmp_flood, udp_flood, connection_flood, subnet_flood, smtp_abuse, disk_usage, link_capacity, anomaly")
	}
	if r.Name == "" {
		return errBadField("name is required")
	}
	if r.WindowSeconds == 0 {
		return errBadField("window_seconds must be > 0")
	}
	if r.Severity == "" {
		r.Severity = "warning"
	}
	switch r.Severity {
	case "info", "warning", "critical":
	default:
		return errBadField("severity must be info|warning|critical")
	}
	if r.BaselineFactor != 0 {
		if !alerts.BaselineCapable(r.RuleType) {
			return errBadField("baseline_factor is only supported by volume_in and volume_out rules")
		}
		if math.IsNaN(r.BaselineFactor) || r.BaselineFactor < 1 || r.BaselineFactor > 100 {
			return errBadField("baseline_factor must be 0 (absolute threshold) or between 1 and 100")
		}
	}
	return nil
}

type validationError struct {
	msg    string
	status int
}

func (e validationError) Error() string { return e.msg }

func errBadField(msg string) error { return validationError{msg: msg, status: http.StatusBadRequest} }

// errUnprocessable is a well-formed request the server cannot act on (422).
func errUnprocessable(msg string) error {
	return validationError{msg: msg, status: http.StatusUnprocessableEntity}
}

// ruleErrStatus maps a validateRule error to its HTTP status (400 default).
func ruleErrStatus(err error) int {
	var ve validationError
	if errors.As(err, &ve) && ve.status != 0 {
		return ve.status
	}
	return http.StatusBadRequest
}

// =============================================================================
// Webhooks CRUD (admin only)
// =============================================================================

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	webhooks, err := h.Store.ListWebhooks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Mask webhook URLs — they contain secrets (e.g. Slack token in path)
	for i := range webhooks {
		webhooks[i].URL = maskWebhookURL(webhooks[i].URL)
	}
	writeJSON(w, http.StatusOK, Response{Data: webhooks})
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	var wh model.WebhookConfig
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&wh); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if wh.Name == "" || wh.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if err := validateWebhookURL(wh.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if wh.WebhookType == "" {
		wh.WebhookType = "generic"
	}
	if wh.MinSeverity == "" {
		wh.MinSeverity = "warning"
	}
	if wh.ID == "" {
		wh.ID = uuid.NewString()
	}
	if err := h.Store.UpsertWebhook(r.Context(), wh); err != nil {
		log.Printf("upsert webhook error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save webhook")
		return
	}
	writeJSON(w, http.StatusCreated, Response{Data: wh})
}

func (h *Handler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	var wh model.WebhookConfig
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&wh); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	wh.ID = id
	if wh.URL != "" {
		if err := validateWebhookURL(wh.URL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.Store.UpsertWebhook(r.Context(), wh); err != nil {
		log.Printf("upsert webhook error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to save webhook")
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: wh})
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Store.DeleteWebhook(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =============================================================================
// Audit log viewer
// =============================================================================

// ListAuditLog handles GET /api/v1/admin/audit?from=&to=&user=&action=&limit=
func (h *Handler) ListAuditLog(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from := now.Add(-7 * 24 * time.Hour)
	to := now
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	userEmail := r.URL.Query().Get("user")
	action := r.URL.Query().Get("action")
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}

	entries, err := h.Store.ListAuditLog(r.Context(), from, to, userEmail, action, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{
		Data: entries,
		Meta: &ResponseMeta{From: from, To: to, Limit: limit},
	})
}

// BGPBlocker is populated at router wiring time.
// Defined as interface field on Handler for dependency injection.
var _ = bgp.Blocker(nil) // reference the type so Go doesn't complain about unused import
