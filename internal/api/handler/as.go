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

	// Get AS name
	asName, _ := h.Store.GetASName(r.Context(), asn)

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"as_number":   asn,
			"as_name":     asName,
			"time_series": ts,
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
