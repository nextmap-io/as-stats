package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
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

	if err := h.Store.SetRetentionPolicy(r.Context(), table, body.TTLDays, body.Enabled); err != nil {
		// Unknown table is a client error (not in the whitelist).
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	updated := model.RetentionPolicy{
		TableName: table,
		TTLDays:   body.TTLDays,
		Enabled:   body.Enabled,
	}
	writeJSON(w, http.StatusOK, Response{Data: updated})
}
