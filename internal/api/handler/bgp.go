package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/model"
)

// BGPStatus handles GET /api/v1/bgp/status. ?refresh=1 runs the daemon's
// status command now (the BGP page's "Check now").
func (h *Handler) BGPStatus(w http.ResponseWriter, r *http.Request) {
	if h.BGP == nil {
		writeJSON(w, http.StatusOK, Response{Data: bgp.StatusReport{State: "disabled", Communities: []string{}}})
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: h.BGP.Status(r.Context(), r.URL.Query().Get("refresh") == "1")})
}

// ListBGPBlocks handles GET /api/v1/bgp/blocks (active blocks).
func (h *Handler) ListBGPBlocks(w http.ResponseWriter, r *http.Request) {
	blocks, err := h.Store.ListActiveBlocks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: blocks})
}

// ListBGPBlockHistory handles GET /api/v1/bgp/blocks/history
func (h *Handler) ListBGPBlockHistory(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	blocks, err := h.Store.ListBlockHistory(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: blocks})
}

// blockBody is the body of POST /bgp/blocks and POST /alerts/{id}/block.
// "description" and "reason" are synonyms (the alert endpoint always took
// "reason").
type blockBody struct {
	IP              string `json:"ip"`
	DurationMinutes int    `json:"duration_minutes"`
	Description     string `json:"description"`
	Reason          string `json:"reason"`
}

func (b blockBody) text() string {
	if b.Description != "" {
		return b.Description
	}
	return b.Reason
}

func decodeBlockBody(w http.ResponseWriter, r *http.Request, required bool) (blockBody, bool) {
	var body blockBody
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && (required || !errors.Is(err, io.EOF)) {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return body, false
	}
	if body.DurationMinutes < 0 {
		writeError(w, http.StatusBadRequest, "duration_minutes must be positive")
		return body, false
	}
	return body, true
}

// CreateBGPBlock handles POST /api/v1/bgp/blocks (admin, CSRF).
//
// Body: {"ip": "192.0.2.1" | "192.0.2.0/30", "duration_minutes": 60,
// "description": "…"}. 201 with the new block, 200 with the existing one when
// the target is already blocked. Errors: 400 malformed, 409 BGP off,
// 422 refused by policy, 429 rail, 502 daemon command failed, 503 state not
// loaded yet.
func (h *Handler) CreateBGPBlock(w http.ResponseWriter, r *http.Request) {
	if !h.bgpBlockingEnabled() {
		writeError(w, http.StatusConflict, "BGP blackholing is not enabled")
		return
	}
	body, ok := decodeBlockBody(w, r, true)
	if !ok {
		return
	}
	block, created, err := h.BGP.Block(r.Context(), bgp.BlockRequest{
		Target:      body.IP,
		Duration:    time.Duration(body.DurationMinutes) * time.Minute,
		Source:      bgp.SourceManual,
		Description: body.text(),
		By:          actor(r),
		ClientIP:    middleware.ClientIP(r),
	})
	writeBlockResult(w, block, created, err)
}

// DeleteBGPBlock handles DELETE /api/v1/bgp/blocks/{ip}?prefix_len=N (admin,
// CSRF). Optional body: {"description": "why"}. 404 when nothing is active
// for the target (and the daemon does not carry it as an orphan).
func (h *Handler) DeleteBGPBlock(w http.ResponseWriter, r *http.Request) {
	if !h.bgpBlockingEnabled() {
		writeError(w, http.StatusConflict, "BGP blackholing is not enabled")
		return
	}
	prefixLen := 0
	if v := r.URL.Query().Get("prefix_len"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 128 {
			writeError(w, http.StatusBadRequest, "invalid prefix_len")
			return
		}
		prefixLen = n
	}
	target, err := bgp.TargetFromParts(chi.URLParam(r, "ip"), prefixLen)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, ok := decodeBlockBody(w, r, false)
	if !ok {
		return
	}
	reason := body.text()
	if reason == "" {
		reason = "withdrawn by an operator"
	}
	if err := h.BGP.Unblock(r.Context(), target, actor(r), middleware.ClientIP(r), reason); err != nil {
		writeBGPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"status": "withdrawn", "target": target.String()}})
}

// InternalAutoBlock handles POST /internal/bgp/blocks: the collector's alert
// engine (bgp.Client). Authenticated only by the shared BGP_API_TOKEN; not
// under /api/v1, so no session, CSRF cookie or OIDC is involved.
func (h *Handler) InternalAutoBlock(w http.ResponseWriter, r *http.Request) {
	if !h.validInternalToken(r) {
		writeError(w, http.StatusUnauthorized, "invalid or missing BGP API token")
		return
	}
	if !h.bgpBlockingEnabled() {
		writeError(w, http.StatusConflict, "BGP blackholing is not enabled")
		return
	}
	var req bgp.AutoBlockRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.DurationMinutes < 0 {
		writeError(w, http.StatusBadRequest, "duration_minutes must be positive")
		return
	}
	if len(req.TopSources) > 20 {
		req.TopSources = req.TopSources[:20]
	}
	block, created, err := h.BGP.Block(r.Context(), bgp.BlockRequest{
		Target:      req.IP,
		Duration:    time.Duration(req.DurationMinutes) * time.Minute,
		Source:      bgp.SourceAuto,
		Description: req.Description,
		By:          "engine",
		ClientIP:    middleware.ClientIP(r),
		AlertID:     validUUID(req.AlertID),
		RuleName:    req.RuleName,
		MetricValue: req.MetricValue,
		MetricType:  req.MetricType,
		Threshold:   req.Threshold,
		TopSources:  req.TopSources,
	})
	writeBlockResult(w, block, created, err)
}

func (h *Handler) validInternalToken(r *http.Request) bool {
	if len(h.BGPAPIToken) < bgp.MinAPITokenLen {
		return false
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(h.BGPAPIToken)) == 1
}

func validUUID(s string) string {
	if _, err := uuid.Parse(s); err != nil {
		return ""
	}
	return s
}

// BlockAlertBGP handles POST /api/v1/alerts/{id}/block (admin, CSRF): blackhole
// the alert's target with the alert's context recorded on the block.
//
// The answers are deliberately honest: 409 when no BGP backend is wired, 422
// when the alert has no single host to blackhole (link / disk / subnet alerts
// and their synthetic 0100::/64 ids) or the policy refuses it, 429 on a rail,
// 502 when the daemon refused. 201 / 200 as for POST /bgp/blocks.
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
	body, ok := decodeBlockBody(w, r, false)
	if !ok {
		return
	}

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
	desc := body.text()
	if desc == "" {
		desc = "Blocked from alert " + target.ruleName
	}
	block, created, err := h.BGP.Block(r.Context(), bgp.BlockRequest{
		Target:      ip.String(),
		Duration:    time.Duration(body.DurationMinutes) * time.Minute,
		Source:      bgp.SourceAlert,
		Description: desc,
		By:          actor(r),
		ClientIP:    middleware.ClientIP(r),
		AlertID:     id,
		RuleName:    target.ruleName,
		MetricValue: target.metricValue,
		MetricType:  target.metricType,
		Threshold:   target.threshold,
	})
	writeBlockResult(w, block, created, err)
}

func writeBlockResult(w http.ResponseWriter, block model.BGPBlock, created bool, err error) {
	if err != nil {
		writeBGPError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, Response{Data: block})
}

// writeBGPError maps bgp.Service errors to statuses. Daemon failures are a
// 502 whose message names the failing program and its exit status but never
// the command output (logged server-side).
func writeBGPError(w http.ResponseWriter, err error) {
	var pe *bgp.PolicyError
	var le *bgp.LimitError
	var ce *bgp.CommandError
	switch {
	case errors.Is(err, bgp.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.As(err, &pe):
		writeError(w, http.StatusUnprocessableEntity, pe.Error())
	case errors.As(err, &le):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, le.Error())
	case errors.Is(err, bgp.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, bgp.ErrNotReady):
		w.Header().Set("Retry-After", "15")
		writeJSON(w, http.StatusServiceUnavailable, Response{Error: err.Error()})
	case errors.As(err, &ce):
		log.Printf("bgp: %v", err)
		writeJSON(w, http.StatusBadGateway, Response{Error: "BGP daemon command failed: " + ce.Short() + " (details in the API log)"})
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// actor names the user for bgp_blocks.blocked_by and the audit log.
func actor(r *http.Request) string {
	if u := middleware.GetUser(r.Context()); u != nil {
		for _, s := range []string{u.Email, u.Name, u.Sub} {
			if s != "" {
				return s
			}
		}
	}
	return "anonymous"
}

// bgpBlockingEnabled reports whether a block request can reach a daemon.
func (h *Handler) bgpBlockingEnabled() bool {
	return h.FeatureBGP && h.BGP != nil
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
	ip          string // IPv4 as dotted quad, IPv6 as-is
	ruleType    string // "" when the rule has since been deleted
	ruleName    string
	metricValue float64
	metricType  string
	threshold   float64
}

// alertTarget resolves a single alert's blackhole target by id. Aggregating
// means the query always yields exactly one row, so "not found" is a count of
// zero rather than a driver-specific no-rows error.
func (h *Handler) alertTarget(ctx context.Context, id string) (alertTargetRow, bool, error) {
	var count uint64
	var target alertTargetRow
	var ruleID string
	err := h.Store.QueryRow(ctx, `
		SELECT count(),
			toString(argMax(a.target_ip, a.triggered_at)),
			toString(argMax(a.rule_id, a.triggered_at)),
			argMax(a.rule_name, a.triggered_at),
			argMax(a.metric_value, a.triggered_at),
			toString(argMax(a.metric_type, a.triggered_at)),
			argMax(a.threshold, a.triggered_at)
		FROM alerts a
		WHERE a.id = @id
	`, clickhouse.Named("id", id)).Scan(&count, &target.ip, &ruleID, &target.ruleName, &target.metricValue, &target.metricType, &target.threshold)
	if err != nil {
		return target, false, err
	}
	if count == 0 {
		return target, false, nil
	}
	target.ip = strings.TrimPrefix(target.ip, "::ffff:")
	// The rule type tells link/disk/subnet alerts apart from host alerts. A
	// deleted rule just leaves it empty; the address checks still apply.
	if err := h.Store.QueryRow(ctx, `
		SELECT toString(any(r.rule_type)) FROM alert_rules AS r FINAL
		WHERE r.id = toUUIDOrZero(@rule_id)
	`, clickhouse.Named("rule_id", ruleID)).Scan(&target.ruleType); err != nil {
		return target, false, err
	}
	return target, true, nil
}
