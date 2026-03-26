package handler

import (
	"net/http"
)

// TopAS handles GET /api/v1/top/as
func (h *Handler) TopAS(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

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

// TopIP handles GET /api/v1/top/ip
func (h *Handler) TopIP(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

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

// TopPrefix handles GET /api/v1/top/prefix
func (h *Handler) TopPrefix(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

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
