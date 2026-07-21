package handler

import (
	"net/http"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Status handles GET /api/v1/status.
//
// The router breakdown and the flow-row count are windowed by the request's
// time range (from/to, default last 24h) so the Status page's Ingestion card
// tracks the selected period. They read raw flows (`flows_raw`, 7-day TTL), so
// they only reflect recent windows. The DB size is point-in-time storage and
// stays window-independent.
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	p := parseQueryParams(r)

	type routerStatus struct {
		RouterIP  string `json:"router_ip"`
		LastSeen  string `json:"last_seen"`
		FlowCount uint64 `json:"flow_count"`
	}

	// Per-router flow volume within the selected window.
	rows, err := h.Store.Query(r.Context(), `
		SELECT toString(router_ip), max(timestamp), count()
		FROM flows_raw
		WHERE timestamp >= @from AND timestamp < @to
		GROUP BY router_ip
		ORDER BY count() DESC
	`,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = rows.Close() }()

	var routers []routerStatus
	for rows.Next() {
		var rs routerStatus
		var ts time.Time
		if err := rows.Scan(&rs.RouterIP, &ts, &rs.FlowCount); err != nil {
			continue
		}
		rs.LastSeen = ts.UTC().Format(time.RFC3339)
		routers = append(routers, rs)
	}

	// Flow rows ingested within the selected window.
	var totalRows uint64
	_ = h.Store.QueryRow(r.Context(), `
		SELECT count() FROM flows_raw
		WHERE timestamp >= @from AND timestamp < @to
	`,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	).Scan(&totalRows)

	// DB size — point-in-time storage, window-independent.
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
		Meta: &ResponseMeta{From: p.From, To: p.To},
	})
}
