package handler

import (
	"net"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// IPDetail handles GET /api/v1/ip/{ip}
//
// The four queries are independent and run concurrently. Reverse DNS is not
// resolved here any more: a synchronous net.LookupAddr held the response for
// the resolver's full timeout on IPs without PTR records, and the frontend
// already resolves names through /dns/ptr (cached, rate-limited). "ptr" stays
// in the payload, always empty, for compatibility.
func (h *Handler) IPDetail(w http.ResponseWriter, r *http.Request) {
	parsed := net.ParseIP(chi.URLParam(r, "ip"))
	if parsed == nil {
		writeError(w, http.StatusBadRequest, "invalid IP address")
		return
	}
	// Canonical text form: dotted quad for IPv4 (even if given as ::ffff:…),
	// compressed form for IPv6. The store compares it with toIPv6(), which
	// maps IPv4 onto the ::ffff: form the columns hold.
	ip := parsed.String()

	p := parseQueryParams(r)
	ctx := r.Context()

	var (
		wg      sync.WaitGroup
		ts      []model.TrafficPoint
		tsErr   error
		topAS   []model.ASTraffic
		peerIPs []model.IPTraffic
		asn     uint32
		asName  string
		prefix  string
	)
	wg.Go(func() { ts, tsErr = h.Store.IPTimeSeries(ctx, ip, p) })
	// Secondary panels are best-effort: an error leaves them empty.
	wg.Go(func() { topAS, _ = h.Store.IPTopAS(ctx, ip, p) })
	wg.Go(func() {
		peerP := p
		if peerP.Limit == 0 || peerP.Limit > 20 {
			peerP.Limit = 20
		}
		peerIPs, _ = h.Store.IPPeerIPs(ctx, ip, peerP)
	})
	wg.Go(func() { asn, asName, prefix, _ = h.Store.IPInfo(ctx, ip) })
	wg.Wait()

	if tsErr != nil {
		writeError(w, http.StatusInternalServerError, tsErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"ip":          ip,
			"as_number":   asn,
			"as_name":     asName,
			"prefix":      prefix,
			"ptr":         "",
			"time_series": ts,
			"top_as":      topAS,
			"peer_ips":    peerIPs,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To, StepSeconds: store.IPSeriesStepSeconds(p.From, p.To)},
	})
}
