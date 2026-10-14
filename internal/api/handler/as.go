package handler

import (
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// ASDetail handles GET /api/v1/as/{asn}
//
// The eight queries behind the page are independent, so they run concurrently
// instead of back to back; only the main time series is fatal, every other
// panel degrades to its zero value on error, as before.
func (h *Handler) ASDetail(w http.ResponseWriter, r *http.Request) {
	asn, err := parseASN(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ASN")
		return
	}

	p := parseQueryParams(r)
	ctx := r.Context()

	var (
		wg                                   sync.WaitGroup
		ts                                   []model.TrafficPoint
		tsErr                                error
		v4Series, v6Series                   []model.LinkTimeSeries
		v4In, v4Out, v6In, v6Out             uint64
		p95v4In, p95v4Out, p95v6In, p95v6Out uint64
		asName, country, class               string
		bytesIn, bytesOut                    uint64
		ratio                                float64
	)
	wg.Go(func() { ts, tsErr = h.Store.ASTimeSeries(ctx, asn, p) })
	// Per-link series for IPv4 and IPv6
	wg.Go(func() {
		p4 := p
		p4.IPVersion = 4
		v4Series, _ = h.Store.ASLinkSeries(ctx, asn, p4)
	})
	wg.Go(func() {
		p6 := p
		p6.IPVersion = 6
		v6Series, _ = h.Store.ASLinkSeries(ctx, asn, p6)
	})
	// Totals and P95 per IP version
	wg.Go(func() { v4In, v4Out, v6In, v6Out, _ = h.Store.ASTotals(ctx, asn, p) })
	wg.Go(func() { p95v4In, p95v4Out, p95v6In, p95v6Out, _ = h.Store.ASP95(ctx, asn, p) })
	// AS name + country (country is best-effort; empty when not populated)
	wg.Go(func() { asName, _ = h.Store.GetASName(ctx, asn) })
	wg.Go(func() { country, _ = h.Store.GetASCountry(ctx, asn) })
	// In/out asymmetry (F2) — best-effort; zero values on error.
	wg.Go(func() { bytesIn, bytesOut, ratio, class, _ = h.Store.ASAsymmetry(ctx, asn, p) })
	wg.Wait()

	if tsErr != nil {
		writeError(w, http.StatusInternalServerError, tsErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"as_number":    asn,
			"as_name":      asName,
			"country":      country,
			"time_series":  ts,
			"v4_series":    v4Series,
			"v6_series":    v6Series,
			"v4_bytes_in":  v4In,
			"v4_bytes_out": v4Out,
			"v6_bytes_in":  v6In,
			"v6_bytes_out": v6Out,
			"p95_v4_in":    p95v4In,
			"p95_v4_out":   p95v4Out,
			"p95_v6_in":    p95v6In,
			"p95_v6_out":   p95v6Out,
			"bytes_in":     bytesIn,
			"bytes_out":    bytesOut,
			"ratio":        ratio,
			"class":        class,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To, StepSeconds: store.ASSeriesStepSeconds(p.From, p.To)},
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

	scope := r.URL.Query().Get("scope")

	var ips []model.IPTraffic
	if scope == "external" {
		// Remote IPs: source addresses whose flows carried this src_as
		// (traffic_by_ip rollup, no flows_raw scan).
		ips, err = h.Store.ASRemoteIPs(r.Context(), asn, p)
	} else {
		// Internal IPs: our IPs that communicate with this AS
		if h.LocalIPFilter != "" {
			p.LocalIPFilter = h.LocalIPFilter
		}
		ips, err = h.Store.ASTopIPs(r.Context(), asn, p)
	}
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
