package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ASDetail handles GET /api/v1/as/{asn}
func (h *Handler) ASDetail(w http.ResponseWriter, r *http.Request) {
	asn, err := parseASN(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ASN")
		return
	}

	p := parseQueryParams(r)

	ts, err := h.Store.ASTimeSeries(r.Context(), asn, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Per-link series for IPv4 and IPv6
	p4 := p
	p4.IPVersion = 4
	v4Series, _ := h.Store.ASLinkSeries(r.Context(), asn, p4)

	p6 := p
	p6.IPVersion = 6
	v6Series, _ := h.Store.ASLinkSeries(r.Context(), asn, p6)

	// Totals per IP version
	v4In, v4Out, v6In, v6Out, _ := h.Store.ASTotals(r.Context(), asn, p)

	// Get AS name
	asName, _ := h.Store.GetASName(r.Context(), asn)

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"as_number":   asn,
			"as_name":     asName,
			"time_series": ts,
			"v4_series":   v4Series,
			"v6_series":   v6Series,
			"v4_bytes_in": v4In,
			"v4_bytes_out": v4Out,
			"v6_bytes_in": v6In,
			"v6_bytes_out": v6Out,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}

// ASPeers handles GET /api/v1/as/{asn}/peers
func (h *Handler) ASPeers(w http.ResponseWriter, r *http.Request) {
	asn, err := parseASN(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ASN")
		return
	}

	p := parseQueryParams(r)

	peers, err := h.Store.ASPeers(r.Context(), asn, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: peers,
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

// ASTopIPs handles GET /api/v1/as/{asn}/ips
func (h *Handler) ASTopIPs(w http.ResponseWriter, r *http.Request) {
	asn, err := parseASN(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ASN")
		return
	}

	p := parseQueryParams(r)
	p.LocalIPFilter = h.LocalIPFilter

	ips, err := h.Store.ASTopIPs(r.Context(), asn, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: ips,
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

func parseASN(r *http.Request) (uint32, error) {
	asnStr := chi.URLParam(r, "asn")
	n, err := strconv.ParseUint(asnStr, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}
