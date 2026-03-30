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

	topAS, _ := h.Store.IPTopAS(r.Context(), ip, p)

	peerP := p
	if peerP.Limit == 0 || peerP.Limit > 20 {
		peerP.Limit = 20
	}
	peerIPs, _ := h.Store.IPPeerIPs(r.Context(), ip, peerP)

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"ip":          ip,
			"time_series": ts,
			"top_as":      topAS,
			"peer_ips":    peerIPs,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
