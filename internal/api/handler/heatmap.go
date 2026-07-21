package handler

import "net/http"

// TrafficHeatmap handles GET /api/v1/traffic/heatmap — a 7×24 day-of-week ×
// hour-of-day throughput grid (U8) built from the hourly link rollup. Honours the
// link and direction filters. Cells are always dense (every slot present,
// zero-filled where there is no data).
func (h *Handler) TrafficHeatmap(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	data, err := h.Store.TrafficHeatmap(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Data: data,
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
