package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// StorageStatus handles GET /api/v1/admin/storage — per-table size/rows/parts,
// configured retention, pending mutations and per-disk usage. Admin-only
// (gated by the /admin route group).
func (h *Handler) StorageStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Store.StorageStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: stats})
}

// SetRetention handles PUT /api/v1/admin/retention/{table} — set the desired
// retention (ttl_days) and enabled flag for a table. The table must be in the
// store's retention whitelist. The change is persisted to retention_policies and
// applied by the reconciler on its next cycle. Admin-only + CSRF (gated by the
// /admin route group); audited via the audit middleware.
func (h *Handler) SetRetention(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	if table == "" {
		writeError(w, http.StatusBadRequest, "table is required")
		return
	}

	var body struct {
		TTLDays uint32 `json:"ttl_days"`
		Enabled bool   `json:"enabled"`
		// Optional: omitted keeps the table's current cold tier; 0 removes it;
		// otherwise 1 <= cold_after_days < ttl_days.
		ColdAfterDays *uint32 `json:"cold_after_days"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.TTLDays < 1 {
		writeError(w, http.StatusBadRequest, "ttl_days must be >= 1")
		return
	}
	if c := body.ColdAfterDays; c != nil && *c > 0 && *c >= body.TTLDays {
		writeError(w, http.StatusBadRequest, "cold_after_days must be 0 (no cold tier) or lower than ttl_days")
		return
	}

	updated, err := h.Store.SetRetentionPolicy(r.Context(), table, body.TTLDays, body.Enabled, body.ColdAfterDays)
	if err != nil {
		// Unknown table (not in the whitelist) and an inconsistent cold tier
		// are client errors.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: updated})
}
