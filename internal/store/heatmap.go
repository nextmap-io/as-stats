package store

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// TrafficHeatmap aggregates link throughput into a 7×24 day-of-week × hour-of-day
// grid (U8) from the hourly link rollup — hourly is the natural granularity for a
// per-hour heatmap. For each (day, hour) slot it returns the mean and peak bits
// per second across all the hourly buckets that landed in that slot over the
// window.
//
// bps derivation: each hourly bucket already stores sampling-corrected bytes, so
// the bucket throughput is (sum(bytes) * 8) / 3600. We sum() per bucket (rows may
// be unmerged in the SummingMergeTree), then avg()/max() those per-bucket rates
// across the buckets sharing a (day, hour) slot.
//
// Alias-shadowing rule: the inner query aggregates t.bytes_* grouped by the
// source column it also filters on, so the table is aliased (t) and ts is
// qualified (t.ts) in WHERE, and the per-bucket timestamp is re-exposed under a
// distinct name (bucket_ts) to keep the outer aggregation clear of the source
// column.
//
// Direction: the hourly link table carries throughput as separate bytes_in /
// bytes_out columns rather than a direction row dimension, so the direction
// filter selects the byte column expression instead of adding a WHERE predicate
// (buildDirectionFilter, which emits t.direction, does not apply to this table).
// The link filter reuses buildLinkFilter (t.link_tag).
func (s *ClickHouseStore) TrafficHeatmap(ctx context.Context, p QueryParams) (model.HeatmapData, error) {
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	bytesExpr := "t.bytes_in + t.bytes_out"
	switch p.Direction {
	case "in":
		bytesExpr = "t.bytes_in"
	case "out":
		bytesExpr = "t.bytes_out"
	}

	ipvFilter := ""
	var ipvArgs []any
	if p.IPVersion == 4 || p.IPVersion == 6 {
		ipvFilter = "AND t.ip_version = @ipv"
		ipvArgs = append(ipvArgs, clickhouse.Named("ipv", p.IPVersion))
	}

	query := fmt.Sprintf(`
		SELECT
			toDayOfWeek(bucket_ts) AS day,
			toHour(bucket_ts) AS hour,
			avg(bucket_bps) AS mean_bps,
			max(bucket_bps) AS peak_bps
		FROM (
			SELECT
				t.ts AS bucket_ts,
				(sum(%s) * 8) / 3600 AS bucket_bps
			FROM traffic_by_link_hourly t
			WHERE t.ts >= @from AND t.ts < @to
			  AND t.link_tag != ''
			  %s %s
			GROUP BY t.ts
		)
		GROUP BY day, hour
		ORDER BY day, hour
	`, bytesExpr, linkFilter, ipvFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, linkArgs...)
	args = append(args, ipvArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return model.HeatmapData{}, fmt.Errorf("query traffic heatmap: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var present []model.HeatmapCell
	for rows.Next() {
		var c model.HeatmapCell
		if err := rows.Scan(&c.Day, &c.Hour, &c.MeanBps, &c.PeakBps); err != nil {
			return model.HeatmapData{}, err
		}
		present = append(present, c)
	}
	if err := rows.Err(); err != nil {
		return model.HeatmapData{}, err
	}

	return model.HeatmapData{Cells: normalizeHeatmapGrid(present)}, nil
}

// normalizeHeatmapGrid returns a dense 7×24 grid: every (day 1-7, hour 0-23) slot
// is present exactly once, in deterministic (day, hour) order, with slots absent
// from present zero-filled. This keeps the frontend free of gap handling.
func normalizeHeatmapGrid(present []model.HeatmapCell) []model.HeatmapCell {
	type key struct{ day, hour uint8 }
	index := make(map[key]model.HeatmapCell, len(present))
	for _, c := range present {
		index[key{c.Day, c.Hour}] = c
	}

	cells := make([]model.HeatmapCell, 0, 7*24)
	for day := uint8(1); day <= 7; day++ {
		for hour := uint8(0); hour < 24; hour++ {
			if c, ok := index[key{day, hour}]; ok {
				cells = append(cells, c)
			} else {
				cells = append(cells, model.HeatmapCell{Day: day, Hour: hour})
			}
		}
	}
	return cells
}
