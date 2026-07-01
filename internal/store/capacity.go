package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	// loadCurveMaxPoints caps how many samples the load-duration curve returns
	// to the frontend. A step chart of a few hundred points is visually
	// indistinguishable from the full series but far cheaper to ship/render.
	loadCurveMaxPoints = 500
	// loadCurveHistBins is the number of throughput histogram buckets.
	loadCurveHistBins = 20
	// forecastLookbackDays is how far back the capacity forecast fits its trend.
	forecastLookbackDays = 90
)

// LinksCapacity returns per-link utilization and a saturation forecast over the
// [from,to] window. For each link in the `links` table it computes:
//   - CurrentBps: throughput in the most recent bucket (in+out)
//   - P95Bps:     p95 of per-bucket (in+out) throughput across the window
//   - UtilizationPct: P95Bps / (capacity_mbps*1e6) * 100 (nil if capacity unset)
//   - ForecastDays80/95/100: linear-regression projection over ~90 days of the
//     daily p95 series (computed in Go, see linearRegression/forecastDaysToLevel)
//
// All aggregate columns are sum()-wrapped to tolerate not-yet-merged
// SummingMergeTree rows.
func (s *ClickHouseStore) LinksCapacity(ctx context.Context, from, to time.Time) ([]model.LinkCapacity, error) {
	// 1. Configured links (tag → description, capacity).
	type linkMeta struct {
		description string
		capacity    uint32
	}
	metaRows, err := s.conn.Query(ctx, `
		SELECT tag, description, capacity_mbps FROM links FINAL ORDER BY tag
	`)
	if err != nil {
		return nil, fmt.Errorf("capacity: list links: %w", err)
	}
	defer func() { _ = metaRows.Close() }()

	meta := make(map[string]linkMeta)
	var order []string
	for metaRows.Next() {
		var tag, desc string
		var capMbps uint32
		if err := metaRows.Scan(&tag, &desc, &capMbps); err != nil {
			return nil, err
		}
		meta[tag] = linkMeta{description: desc, capacity: capMbps}
		order = append(order, tag)
	}
	if len(order) == 0 {
		return []model.LinkCapacity{}, nil
	}

	secs := int(autoStep(from, to).Seconds())
	table := pickLinkTable(from, to)

	// 2. p95 of per-bucket (in+out) throughput per link over the window.
	p95Map, err := s.linkBucketQuantileByTag(ctx, table, secs, from, to)
	if err != nil {
		return nil, err
	}

	// 3. Current throughput = the last bucket in the window.
	curMap, err := s.linkCurrentBpsByTag(ctx, table, secs, from, to)
	if err != nil {
		return nil, err
	}

	// 4. Daily p95 series (last ~90d) for the linear-regression forecast.
	dailyMap, err := s.linkDailySeriesByTag(ctx, to)
	if err != nil {
		return nil, err
	}

	results := make([]model.LinkCapacity, 0, len(order))
	for _, tag := range order {
		m := meta[tag]
		lc := model.LinkCapacity{
			Tag:          tag,
			Description:  m.description,
			CapacityMbps: m.capacity,
			CurrentBps:   uint64(curMap[tag]),
			P95Bps:       uint64(p95Map[tag]),
		}
		if m.capacity > 0 {
			capBps := float64(m.capacity) * 1e6
			util := float64(lc.P95Bps) / capBps * 100
			lc.UtilizationPct = &util

			if series := dailyMap[tag]; len(series) >= 2 {
				xs := make([]float64, len(series))
				for i := range series {
					xs[i] = float64(i)
				}
				slope, intercept := linearRegression(xs, series)
				lc.TrendBpsPerDay = slope
				lastX := float64(len(series) - 1)
				lc.ForecastDays80 = forecastDaysToLevel(slope, intercept, lastX, 0.80*capBps)
				lc.ForecastDays95 = forecastDaysToLevel(slope, intercept, lastX, 0.95*capBps)
				lc.ForecastDays100 = forecastDaysToLevel(slope, intercept, lastX, 1.00*capBps)
			}
		}
		results = append(results, lc)
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].P95Bps > results[j].P95Bps })
	return results, nil
}

// linkBucketQuantileByTag returns tag → p95 of per-bucket (in+out) bps.
func (s *ClickHouseStore) linkBucketQuantileByTag(ctx context.Context, table string, secs int, from, to time.Time) (map[string]float64, error) {
	query := fmt.Sprintf(`
		SELECT link_tag, quantile(0.95)(bps) AS p95
		FROM (
			SELECT f.link_tag AS link_tag,
				(sum(f.bytes_in) + sum(f.bytes_out)) * 8 / %d AS bps
			FROM %s f
			WHERE f.ts >= @from AND f.ts < @to
			GROUP BY f.link_tag, toStartOfInterval(f.ts, INTERVAL %d SECOND)
		)
		GROUP BY link_tag
	`, secs, table, secs)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: p95 by link: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]float64)
	for rows.Next() {
		var tag string
		var p95 float64
		if err := rows.Scan(&tag, &p95); err != nil {
			return nil, err
		}
		out[tag] = p95
	}
	return out, nil
}

// linkCurrentBpsByTag returns tag → throughput bps in the most recent bucket.
func (s *ClickHouseStore) linkCurrentBpsByTag(ctx context.Context, table string, secs int, from, to time.Time) (map[string]float64, error) {
	curFrom := to.Add(-time.Duration(secs) * time.Second)
	if curFrom.Before(from) {
		curFrom = from
	}
	query := fmt.Sprintf(`
		SELECT f.link_tag AS link_tag,
			(sum(f.bytes_in) + sum(f.bytes_out)) * 8 / %d AS bps
		FROM %s f
		WHERE f.ts >= @cur_from AND f.ts < @to
		GROUP BY f.link_tag
	`, secs, table)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("cur_from", curFrom),
		clickhouse.Named("to", to),
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: current bps by link: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]float64)
	for rows.Next() {
		var tag string
		var bps float64
		if err := rows.Scan(&tag, &bps); err != nil {
			return nil, err
		}
		out[tag] = bps
	}
	return out, nil
}

// linkDailySeriesByTag returns tag → ordered daily throughput bps series over
// the last forecastLookbackDays, from traffic_by_link_daily. Each day has a
// single summed bucket, so the per-day value is that day's average bps.
func (s *ClickHouseStore) linkDailySeriesByTag(ctx context.Context, to time.Time) (map[string][]float64, error) {
	dailyFrom := to.Add(-forecastLookbackDays * 24 * time.Hour)
	query := `
		SELECT f.link_tag AS link_tag,
			toDate(f.ts) AS day,
			(sum(f.bytes_in) + sum(f.bytes_out)) * 8 / 86400 AS bps
		FROM traffic_by_link_daily f
		WHERE f.ts >= @daily_from AND f.ts < @to
		GROUP BY f.link_tag, day
		ORDER BY f.link_tag, day
	`
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("daily_from", dailyFrom),
		clickhouse.Named("to", to),
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: daily series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string][]float64)
	for rows.Next() {
		var tag string
		var day time.Time
		var bps float64
		if err := rows.Scan(&tag, &day, &bps); err != nil {
			return nil, err
		}
		out[tag] = append(out[tag], bps)
	}
	return out, nil
}

// LinkP95Total returns the p95 of per-bucket (in+out) throughput bps for a
// single link over [from,to]. Used to compute utilization on the link detail
// page consistently with the Capacity page.
func (s *ClickHouseStore) LinkP95Total(ctx context.Context, tag string, from, to time.Time) (uint64, error) {
	inner, args := s.linkBucketSubquery(from, to, tag)
	query := fmt.Sprintf(`SELECT quantile(0.95)(bps) FROM (%s) t`, inner)
	var v float64
	if err := s.conn.QueryRow(ctx, query, args...).Scan(&v); err != nil {
		return 0, fmt.Errorf("link p95 total: %w", err)
	}
	return uint64(v), nil
}

// LinkCapacityMbps returns the configured capacity (Mbps) for a link, or 0 if
// the link is unknown or has no capacity set.
func (s *ClickHouseStore) LinkCapacityMbps(ctx context.Context, tag string) (uint32, error) {
	var capMbps uint32
	err := s.conn.QueryRow(ctx, `SELECT capacity_mbps FROM links FINAL WHERE tag = @tag`,
		clickhouse.Named("tag", tag),
	).Scan(&capMbps)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return 0, nil
		}
		return 0, fmt.Errorf("link capacity: %w", err)
	}
	return capMbps, nil
}

// LinkLoadCurve returns a load-duration curve for a link over [from,to]:
// per-bucket (in+out) throughput sorted descending (downsampled), the standard
// quantiles computed by ClickHouse, and a ~20-bin histogram.
//
// The per-bucket subquery aliases the table (`f`) and qualifies the timestamp
// in WHERE/GROUP BY to avoid ClickHouse alias shadowing.
func (s *ClickHouseStore) LinkLoadCurve(ctx context.Context, tag string, from, to time.Time) (model.LoadCurve, error) {
	inner, args := s.linkBucketSubquery(from, to, tag)
	lc := model.LoadCurve{Tag: tag}

	// Quantiles via ClickHouse over the per-bucket sum.
	qQuery := fmt.Sprintf(`SELECT quantiles(0.5, 0.9, 0.95, 0.99, 1.0)(bps) FROM (%s) t`, inner)
	var q []float64
	if err := s.conn.QueryRow(ctx, qQuery, args...).Scan(&q); err != nil {
		return lc, fmt.Errorf("load curve quantiles: %w", err)
	}
	if len(q) == 5 {
		lc.Quantiles = model.LoadCurveQuantiles{P50: q[0], P90: q[1], P95: q[2], P99: q[3], P100: q[4]}
	}

	// Full per-bucket series (sorted descending) for the curve + histogram.
	vQuery := fmt.Sprintf(`SELECT bps FROM (%s) t ORDER BY bps DESC`, inner)
	rows, err := s.conn.Query(ctx, vQuery, args...)
	if err != nil {
		return lc, fmt.Errorf("load curve series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var vals []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return lc, err
		}
		vals = append(vals, v)
	}

	lc.SampleCount = len(vals)
	lc.Points = downsampleDesc(vals, loadCurveMaxPoints)
	lc.Histogram = buildHistogram(vals, loadCurveHistBins)
	return lc, nil
}

// linkBucketSubquery builds the per-bucket (in+out) throughput subquery for a
// link, choosing flows_raw for short windows (finer granularity) and the
// range-appropriate aggregate table otherwise. Returns the SQL and its args.
func (s *ClickHouseStore) linkBucketSubquery(from, to time.Time, tag string) (string, []any) {
	secs := int(autoStep(from, to).Seconds())
	var inner string
	if useRawTable(from, to) {
		inner = fmt.Sprintf(`
			SELECT (sumIf(f.bytes * f.sampling_rate, f.direction = 'in')
				+ sumIf(f.bytes * f.sampling_rate, f.direction = 'out')) * 8 / %d AS bps
			FROM flows_raw f
			WHERE f.link_tag = @tag AND f.timestamp >= @from AND f.timestamp < @to
			GROUP BY toStartOfInterval(f.timestamp, INTERVAL %d SECOND)
		`, secs, secs)
	} else {
		table := pickLinkTable(from, to)
		inner = fmt.Sprintf(`
			SELECT (sum(f.bytes_in) + sum(f.bytes_out)) * 8 / %d AS bps
			FROM %s f
			WHERE f.link_tag = @tag AND f.ts >= @from AND f.ts < @to
			GROUP BY toStartOfInterval(f.ts, INTERVAL %d SECOND)
		`, secs, table, secs)
	}
	args := []any{
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	}
	return inner, args
}

// EvalLinkCapacity evaluates recent link utilization for the link_capacity
// alert rule. It computes the p95 of per-bucket (in+out) throughput over the
// last `window` seconds from traffic_by_link_hourly, compares it to each link's
// configured capacity, and returns the single worst-utilized link exceeding
// thresholdPct. Links with capacity_mbps == 0 are skipped.
//
// Like disk_usage, the metric is a percentage carried through the engine as
// metric_type="percent"; the violation has no IP target (the link tag is
// surfaced via AlertViolation.TargetLabel).
func (s *ClickHouseStore) EvalLinkCapacity(ctx context.Context, thresholdPct uint64, window uint32) ([]AlertViolation, error) {
	query := `
		SELECT
			t.link_tag AS link_tag,
			quantile(0.95)(t.bps) AS p95,
			any(l.capacity_mbps) AS cap
		FROM (
			SELECT f.link_tag AS link_tag,
				(sum(f.bytes_in) + sum(f.bytes_out)) * 8 / 3600 AS bps
			FROM traffic_by_link_hourly f
			WHERE f.ts >= now() - INTERVAL @window SECOND
			GROUP BY f.link_tag, toStartOfInterval(f.ts, INTERVAL 3600 SECOND)
		) t
		LEFT JOIN links l ON t.link_tag = l.tag
		GROUP BY t.link_tag
		ORDER BY p95 DESC
	`
	rows, err := s.conn.Query(ctx, query, clickhouse.Named("window", window))
	if err != nil {
		return nil, fmt.Errorf("eval link_capacity: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var worstTag string
	var worstUtil float64
	for rows.Next() {
		var tag string
		var p95 float64
		var capMbps uint32
		if err := rows.Scan(&tag, &p95, &capMbps); err != nil {
			return nil, err
		}
		if capMbps == 0 {
			continue
		}
		util := p95 / (float64(capMbps) * 1e6) * 100
		if util > float64(thresholdPct) && util > worstUtil {
			worstUtil = util
			worstTag = tag
		}
	}
	if worstTag == "" {
		return nil, nil
	}
	return []AlertViolation{{MetricValue: worstUtil, TargetLabel: worstTag}}, nil
}

// linearRegression fits ys against xs by ordinary least squares and returns the
// slope and intercept. Returns (0,0) for empty/mismatched input and (0, mean)
// when all xs are equal (vertical — undefined slope).
func linearRegression(xs, ys []float64) (slope, intercept float64) {
	n := float64(len(xs))
	if len(xs) == 0 || len(xs) != len(ys) {
		return 0, 0
	}
	var sx, sy, sxy, sxx float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxy += xs[i] * ys[i]
		sxx += xs[i] * xs[i]
	}
	denom := n*sxx - sx*sx
	if denom == 0 {
		return 0, sy / n
	}
	slope = (n*sxy - sx*sy) / denom
	intercept = (sy - slope*sx) / n
	return slope, intercept
}

// forecastDaysToLevel projects, from the linear trend (slope/intercept over day
// indices) evaluated at lastX, how many days until the value reaches `level`.
//
//   - nil  when slope <= 0 (flat/declining — the level is never reached upward)
//   - 0    when the current projected value already meets/exceeds the level
//   - >0   estimated days from lastX otherwise
func forecastDaysToLevel(slope, intercept, lastX, level float64) *float64 {
	if slope <= 0 {
		return nil
	}
	current := intercept + slope*lastX
	if current >= level {
		zero := 0.0
		return &zero
	}
	crossX := (level - intercept) / slope
	d := crossX - lastX
	if d < 0 {
		zero := 0.0
		return &zero
	}
	return &d
}

// downsampleDesc sorts vals descending and, if there are more than max points,
// strides evenly through them (always keeping the first and last) to produce at
// most max points while preserving the curve's shape.
func downsampleDesc(vals []float64, max int) []float64 {
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))

	n := len(sorted)
	if max <= 0 || n <= max {
		return sorted
	}
	if max == 1 {
		return []float64{sorted[0]}
	}
	out := make([]float64, 0, max)
	for i := 0; i < max; i++ {
		idx := i * (n - 1) / (max - 1)
		out = append(out, sorted[idx])
	}
	return out
}

// buildHistogram splits vals into `bins` equal-width buckets between the min and
// max value. When every value is identical the first bin holds them all.
func buildHistogram(vals []float64, bins int) []model.HistogramBin {
	if len(vals) == 0 || bins <= 0 {
		return nil
	}
	minV, maxV := vals[0], vals[0]
	for _, v := range vals {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	out := make([]model.HistogramBin, bins)
	width := (maxV - minV) / float64(bins)
	if width <= 0 {
		out[0] = model.HistogramBin{LowerBps: minV, UpperBps: maxV, Count: uint64(len(vals))}
		for i := 1; i < bins; i++ {
			out[i] = model.HistogramBin{LowerBps: maxV, UpperBps: maxV}
		}
		return out
	}
	for i := 0; i < bins; i++ {
		out[i].LowerBps = minV + float64(i)*width
		out[i].UpperBps = minV + float64(i+1)*width
	}
	for _, v := range vals {
		idx := int((v - minV) / width)
		if idx >= bins {
			idx = bins - 1
		}
		if idx < 0 {
			idx = 0
		}
		out[idx].Count++
	}
	return out
}
