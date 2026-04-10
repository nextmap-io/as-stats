package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/model"
)

// BGPStatus handles GET /api/v1/bgp/status
func (h *Handler) BGPStatus(w http.ResponseWriter, r *http.Request) {
	if h.BGPBlocker == nil {
		writeJSON(w, http.StatusOK, Response{Data: map[string]any{"enabled": false, "state": "disabled"}})
		return
	}
	status, err := h.BGPBlocker.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: status})
}

// ListBGPBlocks handles GET /api/v1/bgp/blocks
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

// CreateBGPBlock handles POST /api/v1/bgp/blocks
//
// Request body: { "ip": "1.2.3.4", "duration_minutes": 60, "description": "..." }
func (h *Handler) CreateBGPBlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP              string `json:"ip"`
		DurationMinutes int    `json:"duration_minutes"`
		Description     string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ip := net.ParseIP(strings.TrimSpace(req.IP))
	if ip == nil {
		writeError(w, http.StatusBadRequest, "invalid IP address")
		return
	}

	// Check for existing active block
	if existing, _ := h.Store.FindActiveBlock(r.Context(), ip.String()); existing != "" {
		writeError(w, http.StatusConflict, "IP is already blocked")
		return
	}

	// Clamp duration
	dur := time.Duration(req.DurationMinutes) * time.Minute
	if dur <= 0 {
		dur = time.Hour
	}
	if dur > 24*time.Hour {
		dur = 24 * time.Hour
	}

	userEmail := "anonymous"
	if user := middleware.GetUser(r.Context()); user != nil {
		userEmail = user.Email
	}

	now := time.Now().UTC()
	expiresAt := now.Add(dur)

	block := model.BGPBlock{
		ID:              uuid.NewString(),
		IP:              ip.String(),
		PrefixLen:       32,
		Reason:          "manual",
		Description:     req.Description,
		Status:          "active",
		BlockedBy:       userEmail,
		BlockedAt:       now,
		DurationSeconds: uint32(dur.Seconds()),
		ExpiresAt:       &expiresAt,
	}

	if err := h.BGPBlocker.Announce(r.Context(), ip, dur, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("BGP announce failed: %v", err))
		return
	}

	if err := h.Store.InsertBlock(r.Context(), block); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, Response{Data: block})
}

// DeleteBGPBlock handles DELETE /api/v1/bgp/blocks/{ip}
//
// Optional request body: { "description": "reason for unblock" }
func (h *Handler) DeleteBGPBlock(w http.ResponseWriter, r *http.Request) {
	ipStr := chi.URLParam(r, "ip")
	ip := net.ParseIP(ipStr)
	if ip == nil {
		writeError(w, http.StatusBadRequest, "invalid IP address")
		return
	}

	var req struct {
		Description string `json:"description"`
	}
	// Body is optional — ignore decode errors
	_ = json.NewDecoder(r.Body).Decode(&req)

	userEmail := "anonymous"
	if user := middleware.GetUser(r.Context()); user != nil {
		userEmail = user.Email
	}

	if err := h.BGPBlocker.Withdraw(r.Context(), ip); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("BGP withdraw failed: %v", err))
		return
	}

	if err := h.Store.WithdrawBlock(r.Context(), ip.String(), userEmail, req.Description); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: "unblocked"})
}
