package store

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// priorWindow returns the immediately-prior window of equal duration for
// [from,to]: [from-(to-from), from]. Used by Movers/Talkers to compare the
// current window against the same-length window that ended where it began.
func priorWindow(from, to time.Time) (from2, to2 time.Time) {
	d := to.Sub(from)
	return from.Add(-d), from
}

// SupportedMoverDimension reports whether dim is a whitelisted movers dimension.
// The dimension is NEVER interpolated into SQL — it only selects a fixed,
// hardcoded query branch. "port" additionally requires FEATURE_PORT_STATS,
// which the handler enforces (this only validates the dimension name itself).
func SupportedMoverDimension(dim string) bool {
	switch dim {
	case "as", "prefix", "port", "country":
		return true
	default:
		return false
	}
}

// SupportedTalkerDimension reports whether dim is a whitelisted talkers
// dimension. Same non-interpolation guarantee as SupportedMoverDimension.
func SupportedTalkerDimension(dim string) bool {
	switch dim {
	case "as", "ip", "prefix":
		return true
	default:
		return false
	}
}

// clampChangesLimit bounds the row limit for movers/talkers queries.
func clampChangesLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

// Movers compares total bytes per entity in the current window [from,to] against
// the immediately-prior equal-length window, returning the largest absolute
// movers (gainers and losers mixed) ranked by |delta|, bounded by limit.
//
// dim is whitelisted {as, prefix, port, country} and only ever selects a fixed
// query branch — it is never concatenated into SQL. Both windows are covered by
// a single scan over [from2, to] using sumIf on the (aliased, qualified) ts
// column, so aggregation and the time filter cannot collide (no alias
// shadowing). All aggregates are sum()-wrapped to tolerate unmerged rows.
func (s *ClickHouseStore) Movers(ctx context.Context, dim string, from, to time.Time, limit int) ([]model.Mover, error) {
	if !SupportedMoverDimension(dim) {
		return nil, fmt.Errorf("unsupported movers dimension")
	}
	limit = clampChangesLimit(limit)
	from2, _ := priorWindow(from, to)

	var query string
	switch dim {
	case "as":
		table := pickASTable(from2, to)
		// Private-use ASNs collapse into the single PrivateASGroup entity, same
		// as Top AS — a mover list is only useful if its entities match the
		// entities the rest of the UI shows.
		// Names joined after aggregation, from the deduplicated view (names.go).
		asExpr := groupedAS("t.as_number")
		query = fmt.Sprintf(`
			SELECT toString(a.as_num) AS k, %[2]s AS lbl, a.cur, a.prev
			FROM (
				SELECT
					%[1]s AS as_num,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM %[3]s t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY as_num
				HAVING cur > 0 OR prev > 0
				ORDER BY abs(toInt64(cur) - toInt64(prev)) DESC
				LIMIT @limit
			) a
			LEFT JOIN %[4]s an ON a.as_num = an.as_number
			ORDER BY abs(toInt64(a.cur) - toInt64(a.prev)) DESC, a.as_num
		`, asExpr, groupedASName("a.as_num", "an.as_name"), table, asNamesLatest)
	case "country":
		table := pickASTable(from2, to)
		// Country needs the join before the final GROUP BY, so aggregate per
		// ASN first (cheap) and join the deduplicated names onto that.
		query = fmt.Sprintf(`
			SELECT
				if(an.country = '', 'Unknown', an.country) AS k,
				'' AS lbl,
				sum(a.cur) AS cur,
				sum(a.prev) AS prev
			FROM (
				SELECT t.as_number AS as_number,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM %s t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY t.as_number
			) a
			LEFT JOIN %s an ON a.as_number = an.as_number
			GROUP BY k
			HAVING cur > 0 OR prev > 0
			ORDER BY abs(toInt64(cur) - toInt64(prev)) DESC, k
			LIMIT @limit
		`, table, asNamesLatest)
	case "prefix":
		// Only traffic_by_prefix exists (5-min, 30d TTL). Grouping is on the
		// stored prefix string; no IP filtering is applied, so no CIDR
		// normalisation is required here (the column is already the canonical
		// IPv6-mapped-aware textual prefix written by the collector).
		query = `
			SELECT a.k, an.as_name AS lbl, a.cur, a.prev
			FROM (
				SELECT
					t.prefix AS k,
					any(t.as_number) AS as_number,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM traffic_by_prefix t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY t.prefix
				HAVING cur > 0 OR prev > 0
				ORDER BY abs(toInt64(cur) - toInt64(prev)) DESC
				LIMIT @limit
			) a
			LEFT JOIN ` + asNamesLatest + ` an ON a.as_number = an.as_number
			ORDER BY abs(toInt64(a.cur) - toInt64(a.prev)) DESC, a.k
		`
	case "port":
		// Service ports, every flow once (migration 000018), all directions —
		// including the '' rows of flows on no configured link. Never the
		// pre-3.5 traffic_by_port_legacy: it counted each flow twice.
		query = `
			SELECT
				concat(toString(t.protocol), '/', toString(t.port)) AS k,
				'' AS lbl,
				sumIf(t.bytes, t.ts >= @from) AS cur,
				sumIf(t.bytes, t.ts < @from) AS prev
			FROM ` + portStatsTable + ` t
			WHERE t.ts >= @from2 AND t.ts < @to AND t.port > 0
			GROUP BY t.protocol, t.port
			HAVING cur > 0 OR prev > 0
			ORDER BY abs(toInt64(cur) - toInt64(prev)) DESC
			LIMIT @limit
		`
	}

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("from2", from2),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("limit", limit),
	)
	if err != nil {
		return nil, fmt.Errorf("movers (%s): %w", dim, err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Mover
	for rows.Next() {
		var k, lbl string
		var cur, prev uint64
		if err := rows.Scan(&k, &lbl, &cur, &prev); err != nil {
			return nil, err
		}
		if dim == "ip" || dim == "prefix" {
			k = cleanIPv4Mapped(k)
		}
		m := model.Mover{
			Dimension: dim,
			Key:       k,
			Label:     lbl,
			Current:   cur,
			Previous:  prev,
			Delta:     int64(cur) - int64(prev),
		}
		if prev > 0 {
			m.Pct = float64(m.Delta) / float64(prev) * 100
		}
		results = append(results, m)
	}
	return results, nil
}

// Talkers returns entities that APPEARED (no prior-window traffic, traffic now)
// or DISAPPEARED (prior-window traffic, none now) between the current window
// [from,to] and the immediately-prior equal-length window, ranked by the
// non-zero volume and bounded by limit.
//
// dim is whitelisted {as, ip, prefix} and only ever selects a fixed query
// branch. IPv4-mapped IPv6 keys are cleaned to their dotted-quad form on scan.
// Aggregates are sum()-wrapped and the ts column is aliased/qualified.
func (s *ClickHouseStore) Talkers(ctx context.Context, dim string, from, to time.Time, limit int) ([]model.TalkerChange, error) {
	if !SupportedTalkerDimension(dim) {
		return nil, fmt.Errorf("unsupported talkers dimension")
	}
	limit = clampChangesLimit(limit)
	from2, _ := priorWindow(from, to)

	var query string
	switch dim {
	case "as":
		table := pickASTable(from2, to)
		// Grouped like Top AS / Movers: the private group only counts as "new"
		// or "gone" when *all* of its member ASNs appear or disappear, which is
		// the answer an operator actually wants for a downstream aggregate.
		asExpr := groupedAS("t.as_number")
		query = fmt.Sprintf(`
			SELECT toString(a.as_num) AS k, %[2]s AS lbl, a.cur, a.prev
			FROM (
				SELECT
					%[1]s AS as_num,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM %[3]s t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY as_num
				HAVING (cur = 0 AND prev > 0) OR (prev = 0 AND cur > 0)
				ORDER BY greatest(cur, prev) DESC
				LIMIT @limit
			) a
			LEFT JOIN %[4]s an ON a.as_num = an.as_number
			ORDER BY greatest(a.cur, a.prev) DESC, a.as_num
		`, asExpr, groupedASName("a.as_num", "an.as_name"), table, asNamesLatest)
	case "ip":
		query = `
			SELECT toString(a.ip_addr) AS k, '' AS lbl, a.cur, a.prev
			FROM (
				SELECT
					t.ip_address AS ip_addr,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM traffic_by_ip t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY t.ip_address
				HAVING (cur = 0 AND prev > 0) OR (prev = 0 AND cur > 0)
				ORDER BY greatest(cur, prev) DESC
				LIMIT @limit
			) a
			ORDER BY greatest(a.cur, a.prev) DESC, a.ip_addr
		`
	case "prefix":
		query = `
			SELECT a.k, an.as_name AS lbl, a.cur, a.prev
			FROM (
				SELECT
					t.prefix AS k,
					any(t.as_number) AS as_number,
					sumIf(t.bytes, t.ts >= @from) AS cur,
					sumIf(t.bytes, t.ts < @from) AS prev
				FROM traffic_by_prefix t
				WHERE t.ts >= @from2 AND t.ts < @to
				GROUP BY t.prefix
				HAVING (cur = 0 AND prev > 0) OR (prev = 0 AND cur > 0)
				ORDER BY greatest(cur, prev) DESC
				LIMIT @limit
			) a
			LEFT JOIN ` + asNamesLatest + ` an ON a.as_number = an.as_number
			ORDER BY greatest(a.cur, a.prev) DESC, a.k
		`
	}

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("from2", from2),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("limit", limit),
	)
	if err != nil {
		return nil, fmt.Errorf("talkers (%s): %w", dim, err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.TalkerChange
	for rows.Next() {
		var k, lbl string
		var cur, prev uint64
		if err := rows.Scan(&k, &lbl, &cur, &prev); err != nil {
			return nil, err
		}
		if dim == "ip" || dim == "prefix" {
			k = cleanIPv4Mapped(k)
		}
		tc := model.TalkerChange{
			Dimension: dim,
			Key:       k,
			Label:     lbl,
		}
		if cur > 0 {
			tc.Status = "new"
			tc.Bytes = cur
		} else {
			tc.Status = "gone"
			tc.Bytes = prev
		}
		results = append(results, tc)
	}
	return results, nil
}

// LinkPercentiles returns the p50/p95/p99 of per-bucket in/out throughput
// (bytes per bucket) for a single link over [from,to], in one pass via
// quantiles(0.5,0.95,0.99). Buckets follow linkSeriesPlan — flows_raw for
// short windows, never finer than the source table — so the values divide by
// LinkSeriesStepSeconds to give bps. It is the link detail page's only
// percentile query (the former LinkP95 computed the same p95 a second time).
func (s *ClickHouseStore) LinkPercentiles(ctx context.Context, tag string, p QueryParams) (in, out model.Percentiles, err error) {
	plan := linkSeriesPlan(p.From, p.To)

	var query string
	if plan.Raw() {
		query = fmt.Sprintf(`
			SELECT quantiles(0.5, 0.95, 0.99)(bi), quantiles(0.5, 0.95, 0.99)(bo) FROM (
				SELECT toStartOfInterval(f.timestamp, INTERVAL %d SECOND) AS period,
					sumIf(f.bytes * f.sampling_rate, f.direction = 'in') AS bi,
					sumIf(f.bytes * f.sampling_rate, f.direction = 'out') AS bo
				FROM flows_raw f WHERE f.timestamp >= @from AND f.timestamp < @to AND f.link_tag = @tag
				GROUP BY period
			)`, plan.Secs())
	} else {
		query = fmt.Sprintf(`
			SELECT quantiles(0.5, 0.95, 0.99)(bi), quantiles(0.5, 0.95, 0.99)(bo) FROM (
				SELECT toStartOfInterval(f.ts, INTERVAL %d SECOND) AS period,
					sum(f.bytes_in) AS bi, sum(f.bytes_out) AS bo
				FROM %s f WHERE f.ts >= @from AND f.ts < @to AND f.link_tag = @tag
				GROUP BY period
			)`, plan.Secs(), plan.Table)
	}

	var inQ, outQ []float64
	err = s.conn.QueryRow(ctx, query,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("tag", tag),
	).Scan(&inQ, &outQ)
	if err != nil {
		err = fmt.Errorf("link percentiles: %w", err)
		return
	}
	in = quantilesToPercentiles(inQ)
	out = quantilesToPercentiles(outQ)
	return
}

// quantilesToPercentiles maps a [p50,p95,p99] float slice from ClickHouse's
// quantiles() into a Percentiles struct, guarding against short slices.
func quantilesToPercentiles(q []float64) model.Percentiles {
	var p model.Percentiles
	if len(q) > 0 {
		p.P50 = uint64(math.Max(q[0], 0))
	}
	if len(q) > 1 {
		p.P95 = uint64(math.Max(q[1], 0))
	}
	if len(q) > 2 {
		p.P99 = uint64(math.Max(q[2], 0))
	}
	return p
}
