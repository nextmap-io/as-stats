package handler

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
)

// Links handles GET /api/v1/links
func (h *Handler) Links(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	links, err := h.Store.LinkList(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: links,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}

// LinkDetail handles GET /api/v1/link/{tag}
func (h *Handler) LinkDetail(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "missing link tag")
		return
	}

	p := parseQueryParams(r)

	ts, err := h.Store.LinkTimeSeries(r.Context(), tag, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	topAS, _, err := h.Store.LinkTopAS(r.Context(), tag, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"tag":         tag,
			"time_series": ts,
			"top_as":      topAS,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}

// LinkConfig represents a link configuration for CRUD operations.
type LinkConfig struct {
	Tag          string `json:"tag"`
	RouterIP     string `json:"router_ip"`
	SNMPIndex    uint32 `json:"snmp_index"`
	Description  string `json:"description"`
	CapacityMbps uint32 `json:"capacity_mbps"`
}

// LinksAdmin handles GET /api/v1/admin/links (list all configured links)
func (h *Handler) LinksAdmin(w http.ResponseWriter, r *http.Request) {
	links, err := h.Store.ListLinks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: links})
}

// LinkCreate handles POST /api/v1/admin/links
func (h *Handler) LinkCreate(w http.ResponseWriter, r *http.Request) {
	var cfg LinkConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if cfg.Tag == "" {
		writeError(w, http.StatusBadRequest, "tag is required")
		return
	}

	ip := net.ParseIP(cfg.RouterIP)
	if ip == nil {
		writeError(w, http.StatusBadRequest, "invalid router_ip")
		return
	}

	link := linkConfigToModel(cfg, ip)
	if err := h.Store.UpsertLink(r.Context(), link); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, Response{Data: cfg})
}

// LinkDelete handles DELETE /api/v1/admin/links/{tag}
func (h *Handler) LinkDelete(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "missing link tag")
		return
	}

	if err := h.Store.DeleteLink(r.Context(), tag); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func linkConfigToModel(cfg LinkConfig, ip net.IP) model.Link {
	return model.Link{
		Tag:          cfg.Tag,
		RouterIP:     ip,
		SNMPIndex:    cfg.SNMPIndex,
		Description:  cfg.Description,
		CapacityMbps: cfg.CapacityMbps,
	}
}
