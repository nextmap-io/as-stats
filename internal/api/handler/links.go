package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

const maxLinkBodySize = 10 * 1024 // 10KB

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
//
// The page's five queries are independent and run concurrently; the series
// and the top-AS table are fatal, the rest degrade to zero values.
func (h *Handler) LinkDetail(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		writeError(w, http.StatusBadRequest, "missing link tag")
		return
	}

	p := parseQueryParams(r)
	ctx := r.Context()
	step := store.LinkSeriesStepSeconds(p.From, p.To)

	var (
		wg            sync.WaitGroup
		ts            []model.TrafficPoint
		topAS         []model.ASTraffic
		tsErr, topErr error
		inPct, outPct model.Percentiles
		asSeries      []model.ASTrafficDetail
		capacityMbps  uint32
	)
	wg.Go(func() { ts, tsErr = h.Store.LinkTimeSeries(ctx, tag, p) })
	wg.Go(func() { topAS, _, topErr = h.Store.LinkTopAS(ctx, tag, p) })
	// p50/p95/p99 of per-bucket bytes (Module D). p95_in/p95_out come from
	// the same pass — the separate LinkP95 query computed the identical value.
	wg.Go(func() { inPct, outPct, _ = h.Store.LinkPercentiles(ctx, tag, p) })
	// Per-AS time series on this link
	wg.Go(func() { asSeries, _ = h.Store.LinkASTimeSeries(ctx, tag, p) })
	wg.Go(func() { capacityMbps, _ = h.Store.LinkCapacityMbps(ctx, tag) })
	wg.Wait()

	for _, err := range []error{tsErr, topErr} {
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// Capacity + utilization. A link is full duplex, so utilization_pct is the
	// p95 of the BUSIER direction against capacity — the same definition as
	// the Capacity page — never in+out. Nil when capacity is unset.
	var utilizationPct *float64
	if capacityMbps > 0 && step > 0 {
		busier := max(inPct.P95, outPct.P95)
		u := float64(busier) * 8 / float64(step) / (float64(capacityMbps) * 1e6) * 100
		utilizationPct = &u
	}

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"tag":             tag,
			"time_series":     ts,
			"top_as":          topAS,
			"as_series":       asSeries,
			"p95_in":          inPct.P95,
			"p95_out":         outPct.P95,
			"p50_in":          inPct.P50,
			"p50_out":         outPct.P50,
			"p99_in":          inPct.P99,
			"p99_out":         outPct.P99,
			"capacity_mbps":   capacityMbps,
			"utilization_pct": utilizationPct,
		},
		Meta: &ResponseMeta{From: p.From, To: p.To, StepSeconds: step},
	})
}

// LinkConfig represents a link configuration for CRUD operations.
type LinkConfig struct {
	Tag          string `json:"tag"`
	RouterIP     string `json:"router_ip"`
	SNMPIndex    uint32 `json:"snmp_index"`
	Description  string `json:"description"`
	CapacityMbps uint32 `json:"capacity_mbps"`
	Color        string `json:"color"`
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
	r.Body = http.MaxBytesReader(w, r.Body, maxLinkBodySize)

	var cfg LinkConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if cfg.Tag == "" || len(cfg.Tag) > 128 {
		writeError(w, http.StatusBadRequest, "tag is required and must be <= 128 chars")
		return
	}
	if len(cfg.Description) > 500 {
		writeError(w, http.StatusBadRequest, "description must be <= 500 chars")
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

// LinksTraffic handles GET /api/v1/links/traffic
func (h *Handler) LinksTraffic(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	series, err := h.Store.LinksTrafficSeries(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: series,
		Meta: &ResponseMeta{From: p.From, To: p.To, StepSeconds: store.LinkSeriesStepSeconds(p.From, p.To)},
	})
}

func linkConfigToModel(cfg LinkConfig, ip net.IP) model.Link {
	return model.Link{
		Tag:          cfg.Tag,
		RouterIP:     ip,
		SNMPIndex:    cfg.SNMPIndex,
		Description:  cfg.Description,
		CapacityMbps: cfg.CapacityMbps,
		Color:        cfg.Color,
	}
}
