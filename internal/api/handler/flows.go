package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/services"
)

const (
	flowSearchDefaultLimit = 100
	flowSearchMaxLimit     = 1000
	flowExportMaxRows      = 100000
)

// FlowSearch handles GET /api/v1/flows/search
// Query params: from, to (RFC3339), src_ip, dst_ip, src_as, dst_as,
// protocol, src_port, dst_port, link, min_bytes, ip_version, limit, offset,
// order_by, format (json|csv)
func (h *Handler) FlowSearch(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureFlowSearch {
		writeError(w, http.StatusNotFound, "flow search feature disabled")
		return
	}

	filters, err := parseFlowSearchFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format == "csv" {
		// CSV export: cap at flowExportMaxRows for safety
		filters.Limit = flowExportMaxRows
		filters.Offset = 0
		h.streamFlowSearchCSV(w, r, filters)
		return
	}

	// Cap limit for JSON responses
	if filters.Limit > flowSearchMaxLimit {
		filters.Limit = flowSearchMaxLimit
	}

	results, err := h.Store.SearchFlowLog(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Enrich with protocol/service names
	for i := range results {
		results[i].ProtocolName = services.ProtocolName(results[i].Protocol)
		if svc := services.ServiceName(results[i].Protocol, results[i].DstPort); svc != "" {
			results[i].Service = svc
		}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{
			From:  filters.From,
			To:    filters.To,
			Limit: filters.Limit,
		},
	})
}

// FlowTimeSeries handles GET /api/v1/flows/timeseries
// Drill-down time series for a specific flow tuple.
func (h *Handler) FlowTimeSeries(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureFlowSearch {
		writeError(w, http.StatusNotFound, "flow search feature disabled")
		return
	}

	filters, err := parseFlowSearchFilters(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	points, err := h.Store.FlowLogTimeSeries(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: points,
		Meta: &ResponseMeta{From: filters.From, To: filters.To},
	})
}

func (h *Handler) streamFlowSearchCSV(w http.ResponseWriter, r *http.Request, filters model.FlowSearchFilters) {
	results, err := h.Store.SearchFlowLog(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="flows-%s.csv"`, time.Now().UTC().Format("20060102-150405"),
	))
	if len(results) >= flowExportMaxRows {
		w.Header().Set("X-Truncated", "true")
	}

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header row
	_ = cw.Write([]string{
		"ts", "link_tag", "src_ip", "dst_ip", "src_as", "dst_as",
		"protocol", "protocol_name", "src_port", "dst_port", "service",
		"tcp_flags", "ip_version", "bytes", "packets", "flow_count",
	})

	for _, e := range results {
		protoName := services.ProtocolName(e.Protocol)
		svc := services.ServiceName(e.Protocol, e.DstPort)
		_ = cw.Write([]string{
			e.Timestamp.Format(time.RFC3339),
			e.LinkTag,
			e.SrcIP,
			e.DstIP,
			strconv.FormatUint(uint64(e.SrcAS), 10),
			strconv.FormatUint(uint64(e.DstAS), 10),
			strconv.Itoa(int(e.Protocol)),
			protoName,
			strconv.Itoa(int(e.SrcPort)),
			strconv.Itoa(int(e.DstPort)),
			svc,
			strconv.Itoa(int(e.TCPFlags)),
			strconv.Itoa(int(e.IPVersion)),
			strconv.FormatUint(e.Bytes, 10),
			strconv.FormatUint(e.Packets, 10),
			strconv.FormatUint(e.FlowCount, 10),
		})
	}
}

// TopProtocols handles GET /api/v1/top/protocol
func (h *Handler) TopProtocols(w http.ResponseWriter, r *http.Request) {
	if !h.FeaturePortStats {
		writeError(w, http.StatusNotFound, "port stats feature disabled")
		return
	}

	p := parseQueryParams(r)

	results, err := h.Store.TopProtocols(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for i := range results {
		results[i].ProtocolName = services.ProtocolName(results[i].Protocol)
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}

// TopPortsHandler handles GET /api/v1/top/port
func (h *Handler) TopPortsHandler(w http.ResponseWriter, r *http.Request) {
	if !h.FeaturePortStats {
		writeError(w, http.StatusNotFound, "port stats feature disabled")
		return
	}

	p := parseQueryParams(r)

	var protocol uint8
	if v := r.URL.Query().Get("protocol"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n < 256 {
			protocol = uint8(n)
		}
	}

	results, err := h.Store.TopPorts(r.Context(), p, protocol)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for i := range results {
		results[i].ProtocolName = services.ProtocolName(results[i].Protocol)
		if svc := services.ServiceName(results[i].Protocol, results[i].Port); svc != "" {
			results[i].Service = svc
		}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

// parseFlowSearchFilters extracts FlowSearchFilters from query params.
func parseFlowSearchFilters(r *http.Request) (model.FlowSearchFilters, error) {
	q := r.URL.Query()

	var f model.FlowSearchFilters

	// Time range: default last 1 hour, max 30 days
	now := time.Now().UTC()
	f.To = now
	f.From = now.Add(-1 * time.Hour)
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("invalid 'from' timestamp: %w", err)
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("invalid 'to' timestamp: %w", err)
		}
		f.To = t
	}
	if period := q.Get("period"); period != "" {
		f.To = now
		switch period {
		case "1h":
			f.From = now.Add(-1 * time.Hour)
		case "6h":
			f.From = now.Add(-6 * time.Hour)
		case "24h":
			f.From = now.Add(-24 * time.Hour)
		case "7d":
			f.From = now.Add(-7 * 24 * time.Hour)
		case "30d":
			f.From = now.Add(-30 * 24 * time.Hour)
		}
	}
	if f.To.Sub(f.From) > 30*24*time.Hour {
		return f, fmt.Errorf("time range cannot exceed 30 days")
	}

	f.SrcIP = q.Get("src_ip")
	f.DstIP = q.Get("dst_ip")
	f.LinkTag = q.Get("link")
	f.OrderBy = q.Get("order_by")

	if v := q.Get("src_as"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return f, fmt.Errorf("invalid src_as: %w", err)
		}
		f.SrcAS = uint32(n)
	}
	if v := q.Get("dst_as"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return f, fmt.Errorf("invalid dst_as: %w", err)
		}
		f.DstAS = uint32(n)
	}
	if v := q.Get("protocol"); v != "" {
		n, err := strconv.ParseUint(v, 10, 8)
		if err != nil {
			return f, fmt.Errorf("invalid protocol: %w", err)
		}
		f.Protocol = uint8(n)
	}
	if v := q.Get("src_port"); v != "" {
		n, err := strconv.ParseUint(v, 10, 16)
		if err != nil {
			return f, fmt.Errorf("invalid src_port: %w", err)
		}
		f.SrcPort = uint16(n)
	}
	if v := q.Get("dst_port"); v != "" {
		n, err := strconv.ParseUint(v, 10, 16)
		if err != nil {
			return f, fmt.Errorf("invalid dst_port: %w", err)
		}
		f.DstPort = uint16(n)
	}
	if v := q.Get("min_bytes"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return f, fmt.Errorf("invalid min_bytes: %w", err)
		}
		f.MinBytes = n
	}
	if v := q.Get("ip_version"); v == "4" || v == "6" {
		n, _ := strconv.Atoi(v)
		f.IPVersion = uint8(n)
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			f.Limit = n
		}
	}
	if f.Limit == 0 {
		f.Limit = flowSearchDefaultLimit
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n >= 0 {
			f.Offset = n
		}
	}

	return f, nil
}
