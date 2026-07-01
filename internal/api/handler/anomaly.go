package handler

import (
	"net/http"
)

// AnomalyExplain handles GET /api/v1/anomaly/explain?target=<link tag>&from=&to=
// (Module E explainability). It decomposes a link's traffic over the window into
// its top contributing source ASes, source IPs, and destination ports by bytes,
// so an operator can see *why* a link's throughput was anomalous. Gated by
// FEATURE_ALERTS (registered only when the flag is on).
func (h *Handler) AnomalyExplain(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	target := r.URL.Query().Get("target")
	if target == "" {
		writeError(w, http.StatusBadRequest, "target (link tag) is required")
		return
	}

	p := parseQueryParams(r)

	expl, err := h.Store.AnomalyExplain(r.Context(), target, p.From, p.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// expl is a concrete model.AnomalyExplanation (no any in the payload).
	writeJSON(w, http.StatusOK, Response{
		Data: expl,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
