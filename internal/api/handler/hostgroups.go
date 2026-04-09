package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/model"
)

// ListHostgroups handles GET /api/v1/admin/hostgroups
func (h *Handler) ListHostgroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.Store.ListHostgroups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: groups})
}

// CreateHostgroup handles POST /api/v1/admin/hostgroups
func (h *Handler) CreateHostgroup(w http.ResponseWriter, r *http.Request) {
	var hg model.Hostgroup
	if err := json.NewDecoder(r.Body).Decode(&hg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if hg.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(hg.CIDRs) == 0 {
		writeError(w, http.StatusBadRequest, "at least one CIDR is required")
		return
	}
	// Validate and normalize each CIDR
	clean := make([]string, 0, len(hg.CIDRs))
	for _, cidr := range hg.CIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			if ip := net.ParseIP(cidr); ip != nil {
				if ip.To4() != nil {
					cidr += "/32"
				} else {
					cidr += "/128"
				}
			} else {
				writeError(w, http.StatusBadRequest, "invalid CIDR: "+cidr)
				return
			}
		}
		clean = append(clean, cidr)
	}
	hg.CIDRs = clean
	hg.ID = uuid.NewString()

	if err := h.Store.UpsertHostgroup(r.Context(), hg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, Response{Data: hg})
}

// UpdateHostgroup handles PUT /api/v1/admin/hostgroups/{id}
func (h *Handler) UpdateHostgroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var hg model.Hostgroup
	if err := json.NewDecoder(r.Body).Decode(&hg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	hg.ID = id
	if err := h.Store.UpsertHostgroup(r.Context(), hg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: hg})
}

// DeleteHostgroup handles DELETE /api/v1/admin/hostgroups/{id}
func (h *Handler) DeleteHostgroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.Store.DeleteHostgroup(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: "deleted"})
}
