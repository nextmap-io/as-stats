package store

import "time"

// A per-bucket value can never be finer than the rows it is built from.
// Bucketing 5-minute rows into 60 s or 120 s slots does not add resolution: it
// leaves most slots empty and piles a whole 5-minute row into the others, so
// every bytes-per-bucket → bps conversion (bytes × 8 / step) over-reads by
// granularity/step — 2.5x for the old 120 s step on 3h–6h windows, 5x for the
// Capacity page's 60 s step. The p95 of those slots inherits the same factor.
//
// Every time-series / percentile query therefore picks its source table first
// and then clamps the bucket width to that table's granularity. Handlers report
// the width actually used as meta.step_seconds so the frontend never has to
// guess it.
const (
	// flows_raw holds individual flows; one minute is the finest bucket we
	// ever expose (flows are exported with ~1 min active timeouts anyway).
	rawGranularity     = time.Minute
	fiveMinGranularity = 5 * time.Minute
	hourlyGranularity  = time.Hour
	dailyGranularity   = 24 * time.Hour
)

// tableGranularity returns the row granularity of a source table. Unknown
// tables are assumed to be 5-minute rollups, which is what every other
// aggregate table in the schema is (traffic_by_as, traffic_by_link,
// traffic_by_ip, traffic_by_ip_as, traffic_by_prefix, traffic_by_port).
func tableGranularity(table string) time.Duration {
	switch table {
	case "flows_raw":
		return rawGranularity
	case "traffic_by_as_hourly", "traffic_by_link_hourly":
		return hourlyGranularity
	case "traffic_by_as_daily", "traffic_by_link_daily":
		return dailyGranularity
	default:
		return fiveMinGranularity
	}
}

// seriesPlan is the (source table, bucket width) pair a time-series or
// percentile query reads. Build it with one of the *Plan helpers below so the
// store query and the step the handler reports can never disagree.
type seriesPlan struct {
	Table string
	Step  time.Duration
}

// Raw reports whether the plan reads flows_raw (per-flow rows that still need
// the `bytes * sampling_rate` correction) rather than a pre-scaled rollup.
func (sp seriesPlan) Raw() bool { return sp.Table == "flows_raw" }

// Secs is the bucket width in whole seconds, as interpolated into
// toStartOfInterval(…, INTERVAL n SECOND) and reported as meta.step_seconds.
func (sp seriesPlan) Secs() int { return int(sp.Step / time.Second) }

// planFor clamps autoStep's display-driven bucket to the table's granularity.
func planFor(table string, from, to time.Time) seriesPlan {
	step := autoStep(from, to)
	if g := tableGranularity(table); step < g {
		step = g
	}
	return seriesPlan{Table: table, Step: step}
}

// linkSeriesPlan: flows_raw for ≤3h (true 1-minute buckets), otherwise the
// range-appropriate traffic_by_link rollup.
func linkSeriesPlan(from, to time.Time) seriesPlan {
	if useRawTable(from, to) {
		return planFor("flows_raw", from, to)
	}
	return planFor(pickLinkTable(from, to), from, to)
}

// asSeriesPlan: flows_raw for ≤3h, otherwise the traffic_by_as rollup.
func asSeriesPlan(from, to time.Time) seriesPlan {
	if useRawTable(from, to) {
		return planFor("flows_raw", from, to)
	}
	return planFor(pickASTable(from, to), from, to)
}

// ipSeriesPlan: flows_raw for ≤6h (IP-filtered raw scans are cheap), otherwise
// traffic_by_ip — which only exists at 5-minute granularity.
func ipSeriesPlan(from, to time.Time) seriesPlan {
	if useRawTableForIP(from, to) {
		return planFor("flows_raw", from, to)
	}
	return planFor("traffic_by_ip", from, to)
}

// capacityPlan never reads flows_raw: the Capacity page aggregates every link
// at once, and the 5-minute rollup is the natural resolution for a
// utilisation figure anyway.
func capacityPlan(from, to time.Time) seriesPlan {
	return planFor(pickLinkTable(from, to), from, to)
}

// LinkSeriesStepSeconds is the bucket width used by every per-link series and
// percentile query (LinkTimeSeries, LinksTrafficSeries, LinkPercentiles,
// LinkLoadCurve) over [from,to].
func LinkSeriesStepSeconds(from, to time.Time) int { return linkSeriesPlan(from, to).Secs() }

// ASSeriesStepSeconds is the bucket width used by ASTimeSeries, ASLinkSeries,
// ASP95, LinkASTimeSeries and TopASTrafficSeries over [from,to].
func ASSeriesStepSeconds(from, to time.Time) int { return asSeriesPlan(from, to).Secs() }

// IPSeriesStepSeconds is the bucket width used by IPTimeSeries over [from,to].
func IPSeriesStepSeconds(from, to time.Time) int { return ipSeriesPlan(from, to).Secs() }

// CapacityStepSeconds is the bucket width LinksCapacity uses over [from,to].
func CapacityStepSeconds(from, to time.Time) int { return capacityPlan(from, to).Secs() }

// HeatmapStepSeconds is the bucket width TrafficHeatmap aggregates (hourly).
const HeatmapStepSeconds = int(hourlyGranularity / time.Second)
