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
// [from,to] window.
//
// A link is full duplex: each direction has the whole capacity to itself, so
// utilisation is the BUSIER direction against capacity — never in+out, which
// used to report a link at 60% in / 60% out as 120% full. For each link in the
// `links` table:
//   - Current{In,Out}Bps: throughput in the last COMPLETE bucket. The bucket
//     that contains `to` is still filling (and the collector flushes in
//     batches), so reading it under-reports; CurrentBps = max(in, out).
//   - Peak{In,Out}Bps: the busiest bucket per direction; PeakUtilizationPct =
//     max(peak_in, peak_out) / capacity.
//   - P95{In,Out}Bps: p95 of per-bucket throughput per direction; P95Bps =
//     max(p95_in, p95_out); UtilizationPct = P95Bps / capacity.
//   - TrendBpsPerDay / ForecastDays80/95/100: linear regression over the last
//     ~90 days of the DAILY p95 of max(in, out) (linkDailyP95ByTag). Fitting
//     daily averages instead — night-time troughs included — under-stated the
//     level a link actually has to carry and pushed forecasts months out.
//
// Buckets come from capacityPlan (never finer than the source table, see
// store/step.go) and only whole buckets inside the window are used. All
// aggregates are sum()-wrapped to tolerate not-yet-merged rows.
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

	plan := capacityPlan(from, to)

	// 2. Per-direction current / peak / p95 over the complete buckets.
	stats, err := s.linkDirectionStats(ctx, plan, from, to, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	// 3. Daily p95 of the busier direction (last ~90d) for the forecast.
	dailyMap, err := s.linkDailyP95ByTag(ctx, to)
	if err != nil {
		return nil, err
	}

	results := make([]model.LinkCapacity, 0, len(order))
	for _, tag := range order {
		m := meta[tag]
		st := stats[tag]
		lc := model.LinkCapacity{
			Tag:           tag,
			Description:   m.description,
			CapacityMbps:  m.capacity,
			CurrentInBps:  uint64(st.curIn),
			CurrentOutBps: uint64(st.curOut),
			PeakInBps:     uint64(st.peakIn),
			PeakOutBps:    uint64(st.peakOut),
			P95InBps:      uint64(st.p95In),
			P95OutBps:     uint64(st.p95Out),
		}
		lc.CurrentBps = max(lc.CurrentInBps, lc.CurrentOutBps)
		lc.P95Bps = max(lc.P95InBps, lc.P95OutBps)
		if m.capacity > 0 {
			capBps := float64(m.capacity) * 1e6
			util := float64(lc.P95Bps) / capBps * 100
			lc.UtilizationPct = &util
			peak := float64(max(lc.PeakInBps, lc.PeakOutBps)) / capBps * 100
			lc.PeakUtilizationPct = &peak

			if series := dailyMap[tag]; len(series.days) >= 2 {
				slope, intercept := linearRegression(series.days, series.p95)
				lc.TrendBpsPerDay = slope
				lastX := series.days[len(series.days)-1]
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

// completeBuckets returns the span of whole step-wide buckets inside
// [from, min(to, now)) and the start of the last one. The bucket containing
// `to` (or `now`) is excluded because it is still filling. A window narrower
// than one bucket degrades to the last complete bucket before it, so a link
// never reports "no data" just because the window was short.
//
// Bucket boundaries are multiples of step since the Unix epoch — the same
// alignment ClickHouse's toStartOfInterval uses — so Go and SQL agree on
// which bucket is "last".
func completeBuckets(from, to, now time.Time, step time.Duration) (start, end, last time.Time) {
	if now.Before(to) {
		to = now
	}
	end = truncEpoch(to, step)
	start = truncEpoch(from, step)
	if start.Before(from) {
		start = start.Add(step)
	}
	if !start.Before(end) {
		start = end.Add(-step)
	}
	return start, end, end.Add(-step)
}

// truncEpoch floors t to a multiple of step counted from the Unix epoch.
func truncEpoch(t time.Time, step time.Duration) time.Time {
	secs := int64(step / time.Second)
	if secs <= 0 {
		return t.UTC()
	}
	u := t.Unix()
	return time.Unix(u-((u%secs)+secs)%secs, 0).UTC()
}

// linkDirStats holds per-direction bps statistics for one link.
type linkDirStats struct {
	curIn, curOut, peakIn, peakOut, p95In, p95Out float64
}

// linkDirectionStats returns tag → per-direction current / peak / p95 bps over
// the complete buckets of [from,to] (see completeBuckets).
func (s *ClickHouseStore) linkDirectionStats(ctx context.Context, plan seriesPlan, from, to, now time.Time) (map[string]linkDirStats, error) {
	start, end, last := completeBuckets(from, to, now, plan.Step)
	secs := plan.Secs()
	query := fmt.Sprintf(`
		SELECT
			link_tag,
			sumIf(bi, b = @last_bucket) AS cur_in,
			sumIf(bo, b = @last_bucket) AS cur_out,
			max(bi) AS peak_in,
			max(bo) AS peak_out,
			quantile(0.95)(bi) AS p95_in,
			quantile(0.95)(bo) AS p95_out
		FROM (
			SELECT f.link_tag AS link_tag,
				toStartOfInterval(f.ts, INTERVAL %[1]d SECOND) AS b,
				sum(f.bytes_in) * 8 / %[1]d AS bi,
				sum(f.bytes_out) * 8 / %[1]d AS bo
			FROM %[2]s f
			WHERE f.ts >= @from AND f.ts < @to
			GROUP BY f.link_tag, b
		)
		GROUP BY link_tag
	`, secs, plan.Table)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("from", start),
		clickhouse.Named("to", end),
		clickhouse.Named("last_bucket", last),
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: per-direction stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]linkDirStats)
	for rows.Next() {
		var tag string
		var st linkDirStats
		if err := rows.Scan(&tag, &st.curIn, &st.curOut, &st.peakIn, &st.peakOut, &st.p95In, &st.p95Out); err != nil {
			return nil, err
		}
		out[tag] = st
	}
	return out, rows.Err()
}

// dailyP95Series is one link's forecast input: x = days since the Unix epoch
// (so gaps in the data keep their real spacing), y = that day's p95 bps.
type dailyP95Series struct {
	days []float64
	p95  []float64
}

// linkDailyP95ByTag returns tag → the daily p95 of per-5-minute max(in, out)
// throughput over the last forecastLookbackDays whole days before `to`, from
// traffic_by_link (5-minute rows; its default retention is exactly the 90-day
// lookback). The day containing `to` is left out: a partial day's p95 is
// biased by which hours it happens to contain.
func (s *ClickHouseStore) linkDailyP95ByTag(ctx context.Context, to time.Time) (map[string]dailyP95Series, error) {
	dayEnd := truncEpoch(to, 24*time.Hour)
	dayStart := dayEnd.Add(-forecastLookbackDays * 24 * time.Hour)
	query := `
		SELECT link_tag, toUInt32(toRelativeDayNum(day)) AS day_num, quantile(0.95)(bps) AS p95
		FROM (
			SELECT f.link_tag AS link_tag,
				toDate(f.ts) AS day,
				greatest(sum(f.bytes_in), sum(f.bytes_out)) * 8 / 300 AS bps
			FROM traffic_by_link f
			WHERE f.ts >= @daily_from AND f.ts < @daily_to
			GROUP BY f.link_tag, f.ts
		)
		GROUP BY link_tag, day
		ORDER BY link_tag, day
	`
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("daily_from", dayStart),
		clickhouse.Named("daily_to", dayEnd),
	)
	if err != nil {
		return nil, fmt.Errorf("capacity: daily p95 series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]dailyP95Series)
	for rows.Next() {
		var tag string
		var day uint32
		var p95 float64
		if err := rows.Scan(&tag, &day, &p95); err != nil {
			return nil, err
		}
		ser := out[tag]
		ser.days = append(ser.days, float64(day))
		ser.p95 = append(ser.p95, p95)
		out[tag] = ser
	}
	return out, rows.Err()
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
// per-bucket throughput of the busier direction sorted descending
// (downsampled), the standard quantiles computed by ClickHouse, and a ~20-bin
// histogram. Like the Capacity page it uses max(in, out) per bucket — the
// curve is read against the link's capacity, and a full-duplex link is only
// as full as its busier direction.
//
// The per-bucket subquery aliases the table (`f`) and qualifies the timestamp
// in WHERE/GROUP BY to avoid ClickHouse alias shadowing.
func (s *ClickHouseStore) LinkLoadCurve(ctx context.Context, tag string, from, to time.Time) (model.LoadCurve, error) {
	inner, args := s.linkBucketSubquery(from, to, tag)
	lc := model.LoadCurve{Tag: tag}

	// Quantiles via ClickHouse over the per-bucket values.
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

// linkBucketSubquery builds the per-bucket busier-direction throughput (bps)
// subquery for a link following linkSeriesPlan — flows_raw for short windows,
// the range-appropriate rollup otherwise, never finer than the source table.
func (s *ClickHouseStore) linkBucketSubquery(from, to time.Time, tag string) (string, []any) {
	plan := linkSeriesPlan(from, to)
	secs := plan.Secs()
	var inner string
	if plan.Raw() {
		inner = fmt.Sprintf(`
			SELECT greatest(sumIf(f.bytes * f.sampling_rate, f.direction = 'in'),
				sumIf(f.bytes * f.sampling_rate, f.direction = 'out')) * 8 / %[1]d AS bps
			FROM flows_raw f
			WHERE f.link_tag = @tag AND f.timestamp >= @from AND f.timestamp < @to
			GROUP BY toStartOfInterval(f.timestamp, INTERVAL %[1]d SECOND)
		`, secs)
	} else {
		inner = fmt.Sprintf(`
			SELECT greatest(sum(f.bytes_in), sum(f.bytes_out)) * 8 / %[1]d AS bps
			FROM %[2]s f
			WHERE f.link_tag = @tag AND f.ts >= @from AND f.ts < @to
			GROUP BY toStartOfInterval(f.ts, INTERVAL %[1]d SECOND)
		`, secs, plan.Table)
	}
	args := []any{
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	}
	return inner, args
}

// EvalLinkCapacity evaluates recent link utilization for the link_capacity
// alert rule with the Capacity page's semantics: per link, the p95 of hourly
// throughput of the BUSIER direction (max(p95_in, p95_out), never in+out —
// links are full duplex) over the last `window` seconds of traffic_by_link_hourly,
// against the configured capacity. Returns the single worst-utilized link
// exceeding thresholdPct. Links with capacity_mbps == 0 are skipped.
//
// Only complete hours are read: the current hour's row is partial, and
// dividing it by 3600 under-reads. The window is therefore widened to at least
// one hour so a short rule window still evaluates the last complete hour.
//
// Like disk_usage, the metric is a percentage carried through the engine as
// metric_type="percent"; the violation has no IP target (the link tag is
// surfaced via AlertViolation.TargetLabel).
func (s *ClickHouseStore) EvalLinkCapacity(ctx context.Context, thresholdPct uint64, window uint32) ([]AlertViolation, error) {
	window = max(window, 3600)
	query := `
		SELECT
			t.link_tag AS link_tag,
			greatest(quantile(0.95)(t.bi), quantile(0.95)(t.bo)) AS p95,
			any(l.capacity_mbps) AS cap
		FROM (
			SELECT f.link_tag AS link_tag,
				sum(f.bytes_in) * 8 / 3600 AS bi,
				sum(f.bytes_out) * 8 / 3600 AS bo
			FROM traffic_by_link_hourly f
			WHERE f.ts >= toStartOfHour(now()) - INTERVAL @window SECOND
			  AND f.ts < toStartOfHour(now())
			GROUP BY f.link_tag, f.ts
		) t
		LEFT JOIN (SELECT tag, capacity_mbps FROM links FINAL) l ON t.link_tag = l.tag
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
