package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// IPDetail handles GET /api/v1/ip/{ip}
func (h *Handler) IPDetail(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		writeError(w, http.StatusBadRequest, "missing IP address")
		return
	}

	p := parseQueryParams(r)

	ts, err := h.Store.IPTimeSeries(r.Context(), ip, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"ip":          ip,
			"time_series": ts,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
