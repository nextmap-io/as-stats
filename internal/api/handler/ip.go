package handler

import (
	"net"
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

	// Get IP metadata: AS, prefix, reverse DNS
	asn, asName, prefix, _ := h.Store.IPInfo(r.Context(), ip)
	ptr := ""
	if names, err := net.LookupAddr(ip); err == nil && len(names) > 0 {
		ptr = names[0]
		if len(ptr) > 0 && ptr[len(ptr)-1] == '.' {
			ptr = ptr[:len(ptr)-1]
		}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"ip":          ip,
			"as_number":   asn,
			"as_name":     asName,
			"prefix":      prefix,
			"ptr":         ptr,
			"time_series": ts,
			"top_as":      topAS,
			"peer_ips":    peerIPs,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
