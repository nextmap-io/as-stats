package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
)

// LinksCapacity handles GET /api/v1/links/capacity — per-link utilization and
// saturation forecast over the selected window.
func (h *Handler) LinksCapacity(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	caps, err := h.Store.LinksCapacity(r.Context(), p.From, p.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if caps == nil {
		caps = []model.LinkCapacity{}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: caps,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}

// LinkLoadCurve handles GET /api/v1/link/{tag}/load-curve — the load-duration
// curve (sorted-descending throughput, quantiles, histogram) for one link.
func (h *Handler) LinkLoadCurve(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "missing link tag")
		return
	}

	p := parseQueryParams(r)

	curve, err := h.Store.LinkLoadCurve(r.Context(), tag, p.From, p.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: curve,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
