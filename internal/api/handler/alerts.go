package handler

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
		"total":      total,
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
func (h *Handler) BlockAlertBGP(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	if h.BGPBlocker == nil {
		writeError(w, http.StatusServiceUnavailable, "BGP blocker not configured")
		return
	}

	id := chi.URLParam(r, "id")

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

	// Load alert to get target IP
	alerts, err := h.Store.ListAlerts(r.Context(), "", 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target model.Alert
	for _, a := range alerts {
		if a.ID == id {
			target = a
			break
		}
	}
	if target.ID == "" {
		writeError(w, http.StatusNotFound, "alert not found")
		return
	}

	ip := net.ParseIP(target.TargetIP)
	if ip == nil {
		writeError(w, http.StatusBadRequest, "invalid target IP")
		return
	}

	duration := time.Duration(body.DurationMinutes) * time.Minute
	if err := h.BGPBlocker.Announce(r.Context(), ip, duration, body.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: map[string]any{
		"status":           "blackholed",
		"target":           target.TargetIP,
		"duration_minutes": body.DurationMinutes,
	}})
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
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusBadRequest, err.Error())
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
		"amplification": true, "port_scan": true, "custom": true,
	}
	if !validTypes[r.RuleType] {
		return errBadField("rule_type must be one of: volume_in, volume_out, syn_flood, amplification, port_scan, custom")
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
	return nil
}

type validationError struct{ msg string }

func (e validationError) Error() string { return e.msg }

func errBadField(msg string) error { return validationError{msg: msg} }

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
	// Mask URLs for non-admin users if needed (future)
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
