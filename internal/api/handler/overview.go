package handler

import (
	"net/http"
)

// Overview handles GET /api/v1/overview
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	ov, err := h.Store.Overview(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: ov,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
