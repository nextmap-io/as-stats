package store

import (
	"testing"
	"time"
)

func TestSeriesPlans(t *testing.T) {
	to := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		window time.Duration
		plan   func(from, to time.Time) seriesPlan
		table  string
		step   time.Duration
	}{
		// Link series: raw up to 3h, then the link rollups.
		{"link 1h", time.Hour, linkSeriesPlan, "flows_raw", time.Minute},
		{"link 3h", 3 * time.Hour, linkSeriesPlan, "flows_raw", time.Minute},
		// autoStep says 2 min here, but traffic_by_link only has 5-min rows:
		// a 120 s bucket would read 2.5x too high.
		{"link 6h", 6 * time.Hour, linkSeriesPlan, "traffic_by_link", 5 * time.Minute},
		{"link 24h", 24 * time.Hour, linkSeriesPlan, "traffic_by_link", 5 * time.Minute},
		{"link 7d", 7 * 24 * time.Hour, linkSeriesPlan, "traffic_by_link", 30 * time.Minute},
		{"link 30d", 30 * 24 * time.Hour, linkSeriesPlan, "traffic_by_link", time.Hour},
		{"link 1y", 365 * 24 * time.Hour, linkSeriesPlan, "traffic_by_link_hourly", time.Hour},
		// > 2 years reads the daily rollup: an hourly bucket would be 24x off.
		{"link 3y", 3 * 365 * 24 * time.Hour, linkSeriesPlan, "traffic_by_link_daily", 24 * time.Hour},

		{"as 1h", time.Hour, asSeriesPlan, "flows_raw", time.Minute},
		{"as 6h", 6 * time.Hour, asSeriesPlan, "traffic_by_as", 5 * time.Minute},
		{"as 1y", 365 * 24 * time.Hour, asSeriesPlan, "traffic_by_as_hourly", time.Hour},
		{"as 3y", 3 * 365 * 24 * time.Hour, asSeriesPlan, "traffic_by_as_daily", 24 * time.Hour},

		// IP series stay on flows_raw up to 6h (2-min buckets are real there).
		{"ip 3h", 3 * time.Hour, ipSeriesPlan, "flows_raw", time.Minute},
		{"ip 6h", 6 * time.Hour, ipSeriesPlan, "flows_raw", 2 * time.Minute},
		{"ip 12h", 12 * time.Hour, ipSeriesPlan, "traffic_by_ip", 5 * time.Minute},

		// Capacity never reads flows_raw: 1h used to bucket 5-min rows into
		// 60 s slots and over-read utilisation by up to 5x.
		{"capacity 1h", time.Hour, capacityPlan, "traffic_by_link", 5 * time.Minute},
		{"capacity 24h", 24 * time.Hour, capacityPlan, "traffic_by_link", 5 * time.Minute},
		{"capacity 3y", 3 * 365 * 24 * time.Hour, capacityPlan, "traffic_by_link_daily", 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.plan(to.Add(-c.window), to)
			if got.Table != c.table {
				t.Errorf("table = %q, want %q", got.Table, c.table)
			}
			if got.Step != c.step {
				t.Errorf("step = %s, want %s", got.Step, c.step)
			}
			if got.Secs() != int(c.step/time.Second) {
				t.Errorf("Secs() = %d, want %d", got.Secs(), int(c.step/time.Second))
			}
			if got.Raw() != (c.table == "flows_raw") {
				t.Errorf("Raw() = %v for table %q", got.Raw(), got.Table)
			}
		})
	}
}

// Invariant behind meta.step_seconds: whatever window is requested, the bucket
// is never finer than the rows of the table it reads.
func TestSeriesPlansNeverFinerThanSource(t *testing.T) {
	to := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	plans := map[string]func(from, to time.Time) seriesPlan{
		"link": linkSeriesPlan, "as": asSeriesPlan, "ip": ipSeriesPlan, "capacity": capacityPlan,
	}
	for w := 10 * time.Minute; w <= 6*365*24*time.Hour; w = w * 5 / 4 {
		for name, plan := range plans {
			got := plan(to.Add(-w), to)
			if g := tableGranularity(got.Table); got.Step < g {
				t.Fatalf("%s plan for %s: step %s finer than %s granularity %s", name, w, got.Step, got.Table, g)
			}
		}
	}
}

func TestStepSecondsExported(t *testing.T) {
	to := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	from := to.Add(-6 * time.Hour)
	if got := LinkSeriesStepSeconds(from, to); got != 300 {
		t.Errorf("LinkSeriesStepSeconds(6h) = %d, want 300", got)
	}
	if got := IPSeriesStepSeconds(from, to); got != 120 {
		t.Errorf("IPSeriesStepSeconds(6h) = %d, want 120", got)
	}
	if got := CapacityStepSeconds(to.Add(-time.Hour), to); got != 300 {
		t.Errorf("CapacityStepSeconds(1h) = %d, want 300", got)
	}
	if HeatmapStepSeconds != 3600 {
		t.Errorf("HeatmapStepSeconds = %d, want 3600", HeatmapStepSeconds)
	}
}
