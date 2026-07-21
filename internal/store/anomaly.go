package store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/services"
)

// Anomaly detection tuning constants.
//
// We compare the current hour's throughput on a link against a robust baseline
// built from the SAME hour-of-week over the recent past (e.g. Monday 14:00 vs.
// the last several Mondays at 14:00). Same-hour-of-week comparison captures the
// strong weekly seasonality of network traffic (business hours, weekends) that a
// naive rolling average would smear out.
const (
	// anomalyLookbackWeeks is how far back we gather same-hour-of-week samples.
	anomalyLookbackWeeks = 8
	// anomalyMinSamples is the minimum number of historical samples required to
	// trust a baseline. With fewer than this we skip the link entirely rather
	// than fire on noise (insufficient-history guard).
	anomalyMinSamples = 4
	// madScale converts the median absolute deviation into a robust estimate of
	// the standard deviation for normally-distributed data (1 / Φ⁻¹(0.75)).
	// baseline = median + k * (madScale * MAD) makes k comparable to "k sigmas".
	madScale = 1.4826
	// anomalyMinRatio is a floor guard: even a statistically-significant excess
	// must be at least this multiple of the baseline to fire. Prevents alerts on
	// tiny absolute excursions when the historical spread (MAD) is near zero.
	anomalyMinRatio = 1.20
)

// medianSorted returns the median of an already-sorted slice. Callers that hold
// an unsorted slice should use median().
func medianSorted(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// median returns the median of xs without mutating the input.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	return medianSorted(cp)
}

// medianMADBaseline computes a robust upper baseline for the samples:
//
//	baseline = median + k * (madScale * MAD)
//
// where MAD is the median absolute deviation from the sample median. The MAD is
// scaled by madScale (1.4826) so it estimates the standard deviation for
// normal data, making k behave like a number of standard deviations. This is
// far more outlier-resistant than mean + k*stddev (a single past spike does not
// inflate the baseline and mask future spikes). Pure — unit tested.
func medianMADBaseline(samples []float64, k float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	med := median(samples)
	devs := make([]float64, len(samples))
	for i, v := range samples {
		devs[i] = math.Abs(v - med)
	}
	mad := median(devs)
	return med + k*madScale*mad
}

// anomalyDecision decides whether the current value is anomalously high versus a
// baseline built from the historical samples. It returns whether to fire, the
// computed baseline, and the deviation ratio (current / baseline).
//
// Guards (any → do not fire):
//   - fewer than anomalyMinSamples historical samples (insufficient history),
//   - current not strictly above the baseline,
//   - current below anomalyMinRatio * baseline (tiny absolute excursion), or
//   - a non-positive baseline (no meaningful history to compare against).
//
// Pure — unit tested.
func anomalyDecision(current float64, samples []float64, k float64) (fire bool, baseline, ratio float64) {
	if len(samples) < anomalyMinSamples {
		return false, 0, 0
	}
	baseline = medianMADBaseline(samples, k)
	if baseline <= 0 {
		return false, baseline, 0
	}
	ratio = current / baseline
	if current > baseline && ratio >= anomalyMinRatio {
		return true, baseline, ratio
	}
	return false, baseline, ratio
}

// EvalAnomaly detects links whose most-recent complete hour of throughput is
// statistically anomalous versus the same hour-of-week over the last
// anomalyLookbackWeeks weeks. It reads traffic_by_link_hourly, which is the only
// feasible target: links accumulate weeks of hourly rollup, whereas the per-dst
// hot tables retain only 7 days.
//
// k is the sensitivity (number of scaled-MAD "sigmas" above the median). The
// engine derives it from AlertRule.ThresholdCount as ThresholdCount/10 (so a
// stored 25 → k=2.5), keeping the whole feature on the existing rule schema with
// no migration.
//
// linkFilter, when non-empty, restricts evaluation to that single link tag
// (the rule's optional link filter). Empty = evaluate every link.
//
// Only the single worst anomalous link is returned (largest deviation ratio),
// mirroring disk_usage / link_capacity: TargetIP is nil and the link tag is
// carried in TargetLabel. The baseline / current / deviation / samples_count
// are attached in Extra for the engine to record in details.
func (s *ClickHouseStore) EvalAnomaly(ctx context.Context, k float64, linkFilter string) ([]AlertViolation, error) {
	links, err := s.anomalyCandidateLinks(ctx, linkFilter)
	if err != nil {
		return nil, err
	}

	var worst *AlertViolation
	var worstRatio float64

	for _, tag := range links {
		current, samples, err := s.anomalyLinkSamples(ctx, tag)
		if err != nil {
			return nil, err
		}
		fire, baseline, ratio := anomalyDecision(current, samples, k)
		if !fire {
			continue
		}
		if worst == nil || ratio > worstRatio {
			worstRatio = ratio
			v := AlertViolation{
				TargetLabel: tag,
				MetricValue: current,
				Extra: map[string]any{
					"baseline":      baseline,
					"current":       current,
					"deviation":     ratio,
					"samples_count": len(samples),
					"sensitivity_k": k,
				},
			}
			worst = &v
		}
	}

	if worst == nil {
		return nil, nil
	}
	return []AlertViolation{*worst}, nil
}

// anomalyCandidateLinks returns the set of link tags to evaluate. When
// linkFilter is set it is returned verbatim (single link); otherwise the
// distinct links seen in traffic_by_link_hourly over the lookback window.
func (s *ClickHouseStore) anomalyCandidateLinks(ctx context.Context, linkFilter string) ([]string, error) {
	if linkFilter != "" {
		return []string{linkFilter}, nil
	}
	query := `
		SELECT DISTINCT f.link_tag AS link_tag
		FROM traffic_by_link_hourly f
		WHERE f.ts >= now() - INTERVAL @weeks WEEK
		  AND f.link_tag != ''
	`
	rows, err := s.conn.Query(ctx, query, clickhouse.Named("weeks", anomalyLookbackWeeks))
	if err != nil {
		return nil, fmt.Errorf("anomaly candidate links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// anomalyLinkSamples returns, in ONE query for the link, the current (most
// recent complete hour) throughput in bps and the historical same-hour-of-week
// samples in bps over the lookback window. sum()-wrapped and table-aliased +
// qualified (f.ts) to respect SummingMergeTree merge semantics and avoid the
// ClickHouse alias-shadowing trap.
//
// "Current" is the last COMPLETE hour bucket (toStartOfHour(now()) - 1h): the
// in-progress hour is partial and would understate throughput. Historical
// samples match the current bucket's day-of-week AND hour-of-day, excluding the
// current bucket itself.
func (s *ClickHouseStore) anomalyLinkSamples(ctx context.Context, tag string) (current float64, samples []float64, err error) {
	query := `
		WITH toStartOfHour(now()) - INTERVAL 1 HOUR AS cur_bucket
		SELECT
			(sum(f.bytes_in) + sum(f.bytes_out)) * 8 / 3600 AS bps,
			f.ts = cur_bucket AS is_current
		FROM traffic_by_link_hourly f
		WHERE f.link_tag = @tag
		  AND f.ts >= cur_bucket - INTERVAL @weeks WEEK
		  AND toDayOfWeek(f.ts) = toDayOfWeek(cur_bucket)
		  AND toHour(f.ts) = toHour(cur_bucket)
		GROUP BY f.ts
	`
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("tag", tag),
		clickhouse.Named("weeks", anomalyLookbackWeeks),
	)
	if err != nil {
		return 0, nil, fmt.Errorf("anomaly link samples: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var bps float64
		var isCurrent bool
		if err := rows.Scan(&bps, &isCurrent); err != nil {
			return 0, nil, err
		}
		if isCurrent {
			current = bps
			continue
		}
		samples = append(samples, bps)
	}
	return current, samples, nil
}

// AnomalyExplain decomposes a link's traffic over [from, to] into its top
// contributing source ASes, source IPs, and destination ports by bytes. It
// answers "why is this link's throughput anomalous" (Module E explainability).
//
// It reads flows_raw (bounded by link_tag + the window), weighting counters by
// sampling_rate. Source IPs are emitted in canonical form (IPv4-mapped IPv6
// stripped back to dotted-quad via cleanIPv4Mapped). No user IP/CIDR is
// interpolated, so no IPv6-mapped probe is needed here; the link tag is a
// bound parameter.
func (s *ClickHouseStore) AnomalyExplain(ctx context.Context, target string, from, to time.Time) (model.AnomalyExplanation, error) {
	const topN = 10
	out := model.AnomalyExplanation{
		Target:     target,
		From:       from,
		To:         to,
		TopASes:    []model.AnomalyContributorAS{},
		TopSources: []model.AnomalyContributorIP{},
		TopPorts:   []model.AnomalyContributorPort{},
	}

	// Total bytes over the window for percentage computation.
	var totalBytes uint64
	if err := s.conn.QueryRow(ctx, `
		SELECT toUInt64(sum(f.bytes * f.sampling_rate))
		FROM flows_raw f
		WHERE f.link_tag = @tag
		  AND f.timestamp >= @from
		  AND f.timestamp < @to
	`,
		clickhouse.Named("tag", target),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
	).Scan(&totalBytes); err != nil {
		return out, fmt.Errorf("anomaly explain total: %w", err)
	}
	pct := func(b uint64) float64 {
		if totalBytes == 0 {
			return 0
		}
		return float64(b) / float64(totalBytes) * 100
	}

	// Top source ASes.
	asRows, err := s.conn.Query(ctx, `
		SELECT
			f.src_as AS asn,
			any(an.as_name) AS as_name,
			toUInt64(sum(f.bytes * f.sampling_rate)) AS bytes,
			toUInt64(sum(f.packets * f.sampling_rate)) AS packets
		FROM flows_raw f
		LEFT JOIN as_names an ON f.src_as = an.as_number
		WHERE f.link_tag = @tag
		  AND f.timestamp >= @from
		  AND f.timestamp < @to
		  AND f.src_as > 0
		GROUP BY f.src_as
		ORDER BY bytes DESC
		LIMIT @limit
	`,
		clickhouse.Named("tag", target),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("limit", topN),
	)
	if err != nil {
		return out, fmt.Errorf("anomaly explain top ases: %w", err)
	}
	for asRows.Next() {
		var c model.AnomalyContributorAS
		if err := asRows.Scan(&c.ASNumber, &c.ASName, &c.Bytes, &c.Packets); err != nil {
			_ = asRows.Close()
			return out, err
		}
		c.Percent = pct(c.Bytes)
		out.TopASes = append(out.TopASes, c)
	}
	if err := asRows.Close(); err != nil {
		return out, err
	}

	// Top source IPs.
	ipRows, err := s.conn.Query(ctx, `
		SELECT
			toString(f.src_ip) AS ip,
			toUInt64(sum(f.bytes * f.sampling_rate)) AS bytes,
			toUInt64(sum(f.packets * f.sampling_rate)) AS packets
		FROM flows_raw f
		WHERE f.link_tag = @tag
		  AND f.timestamp >= @from
		  AND f.timestamp < @to
		GROUP BY f.src_ip
		ORDER BY bytes DESC
		LIMIT @limit
	`,
		clickhouse.Named("tag", target),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("limit", topN),
	)
	if err != nil {
		return out, fmt.Errorf("anomaly explain top sources: %w", err)
	}
	for ipRows.Next() {
		var c model.AnomalyContributorIP
		if err := ipRows.Scan(&c.IP, &c.Bytes, &c.Packets); err != nil {
			_ = ipRows.Close()
			return out, err
		}
		c.IP = cleanIPv4Mapped(c.IP)
		c.Percent = pct(c.Bytes)
		out.TopSources = append(out.TopSources, c)
	}
	if err := ipRows.Close(); err != nil {
		return out, err
	}

	// Top destination (protocol, port) tuples.
	portRows, err := s.conn.Query(ctx, `
		SELECT
			f.protocol AS protocol,
			f.dst_port AS port,
			toUInt64(sum(f.bytes * f.sampling_rate)) AS bytes,
			toUInt64(sum(f.packets * f.sampling_rate)) AS packets
		FROM flows_raw f
		WHERE f.link_tag = @tag
		  AND f.timestamp >= @from
		  AND f.timestamp < @to
		GROUP BY f.protocol, f.dst_port
		ORDER BY bytes DESC
		LIMIT @limit
	`,
		clickhouse.Named("tag", target),
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("limit", topN),
	)
	if err != nil {
		return out, fmt.Errorf("anomaly explain top ports: %w", err)
	}
	for portRows.Next() {
		var c model.AnomalyContributorPort
		if err := portRows.Scan(&c.Protocol, &c.Port, &c.Bytes, &c.Packets); err != nil {
			_ = portRows.Close()
			return out, err
		}
		c.ProtocolName = services.ProtocolName(c.Protocol)
		c.Service = services.ServiceName(c.Protocol, c.Port)
		c.Percent = pct(c.Bytes)
		out.TopPorts = append(out.TopPorts, c)
	}
	if err := portRows.Close(); err != nil {
		return out, err
	}

	return out, nil
}
