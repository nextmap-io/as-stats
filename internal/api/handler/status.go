package handler

import (
	"net/http"
)

// Status handles GET /api/v1/status
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	type routerStatus struct {
		RouterIP  string `json:"router_ip"`
		LastSeen  string `json:"last_seen"`
		FlowCount uint64 `json:"flow_count"`
	}

	// Query latest flow per router
	rows, err := h.Store.Query(r.Context(), `
		SELECT toString(router_ip), max(timestamp), count()
		FROM flows_raw
		WHERE timestamp >= now() - INTERVAL 10 MINUTE
		GROUP BY router_ip
		ORDER BY count() DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	var routers []routerStatus
	for rows.Next() {
		var r routerStatus
		var ts string
		if err := rows.Scan(&r.RouterIP, &ts, &r.FlowCount); err != nil {
			continue
		}
		r.LastSeen = ts
		routers = append(routers, r)
	}

	// Total rows in DB
	var totalRows uint64
	_ = h.Store.QueryRow(r.Context(), `SELECT count() FROM flows_raw`).Scan(&totalRows)

	// DB size
	var dbSize uint64
	_ = h.Store.QueryRow(r.Context(), `
		SELECT sum(bytes_on_disk) FROM system.parts
		WHERE database = 'asstats' AND active
	`).Scan(&dbSize)

	writeJSON(w, http.StatusOK, Response{
		Data: map[string]any{
			"routers":    routers,
			"total_rows": totalRows,
			"db_size":    dbSize,
		},
	})
}
