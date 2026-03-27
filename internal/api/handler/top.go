package handler

import (
	"net/http"
)

// TopAS handles GET /api/v1/top/as
func (h *Handler) TopAS(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)
	p.ExcludeAS = h.LocalAS

	results, totalBytes, err := h.Store.TopAS(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{
			From:       p.From,
			To:         p.To,
			TotalBytes: totalBytes,
			Limit:      p.Limit,
			Offset:     p.Offset,
		},
	})
}

// TopASTraffic handles GET /api/v1/top/as/traffic
func (h *Handler) TopASTraffic(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)
	p.ExcludeAS = h.LocalAS
	if p.Limit == 0 || p.Limit > 50 {
		p.Limit = 50
	}

	results, err := h.Store.TopASTrafficSeries(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

// TopIP handles GET /api/v1/top/ip?scope=internal|external
func (h *Handler) TopIP(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	scope := r.URL.Query().Get("scope")
	if scope == "internal" && h.LocalIPFilter != "" {
		p.LocalIPFilter = h.LocalIPFilter
	} else if scope == "external" && h.LocalIPFilter != "" {
		p.LocalIPFilter = "NOT " + h.LocalIPFilter
	}

	results, _, err := h.Store.TopIP(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{
			From:   p.From,
			To:     p.To,
			Limit:  p.Limit,
			Offset: p.Offset,
		},
	})
}

// TopPrefix handles GET /api/v1/top/prefix?scope=internal|external
func (h *Handler) TopPrefix(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	scope := r.URL.Query().Get("scope")
	if scope == "internal" && len(h.LocalPrefixes) > 0 {
		p.LocalPrefixes = h.LocalPrefixes
		p.PrefixScope = "internal"
	} else if scope == "external" && len(h.LocalPrefixes) > 0 {
		p.LocalPrefixes = h.LocalPrefixes
		p.PrefixScope = "external"
	}

	results, _, err := h.Store.TopPrefix(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{
			From:   p.From,
			To:     p.To,
			Limit:  p.Limit,
			Offset: p.Offset,
		},
	})
}
