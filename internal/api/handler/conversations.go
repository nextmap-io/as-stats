package handler

import (
	"net/http"
)

// conversationDims is the set of accepted `dim` values for GET /conversations.
// It mirrors the store-side whitelist (store.convDimensions) and exists here so
// the handler can return a clean 400 for an unknown dimension instead of a 500.
var conversationDims = map[string]bool{
	"src_dst_ip":     true,
	"src_dst_as":     true,
	"dst_port_proto": true,
}

// Conversations handles GET /api/v1/conversations — bidirectional top-talker
// rows (F3). Query params: dim (src_dst_ip|src_dst_as|dst_port_proto, default
// src_dst_ip), plus the standard from/to/period/link/limit. Gated behind
// FEATURE_FLOW_SEARCH; when enabled it reads flows_log, else flows_raw.
func (h *Handler) Conversations(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureFlowSearch {
		writeError(w, http.StatusNotFound, "flow search feature disabled")
		return
	}

	p := parseQueryParams(r)

	dim := r.URL.Query().Get("dim")
	if dim == "" {
		dim = "src_dst_ip"
	}
	if !conversationDims[dim] {
		writeError(w, http.StatusBadRequest, "invalid dim (want src_dst_ip|src_dst_as|dst_port_proto)")
		return
	}

	results, err := h.Store.Conversations(r.Context(), p, dim, h.FeatureFlowSearch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: results,
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}
