package store

import (
	"testing"
	"time"
)

func TestPriorWindow(t *testing.T) {
	cases := []struct {
		name         string
		from, to     time.Time
		wantFrom2    time.Time
		wantTo2      time.Time
		wantDuration time.Duration
	}{
		{
			name:         "24h window",
			from:         time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
			to:           time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			wantFrom2:    time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
			wantTo2:      time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC),
			wantDuration: 24 * time.Hour,
		},
		{
			name:         "90m window",
			from:         time.Date(2026, 6, 30, 10, 30, 0, 0, time.UTC),
			to:           time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
			wantFrom2:    time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC),
			wantTo2:      time.Date(2026, 6, 30, 10, 30, 0, 0, time.UTC),
			wantDuration: 90 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from2, to2 := priorWindow(tc.from, tc.to)
			if !from2.Equal(tc.wantFrom2) {
				t.Errorf("from2 = %v, want %v", from2, tc.wantFrom2)
			}
			if !to2.Equal(tc.wantTo2) {
				t.Errorf("to2 = %v, want %v", to2, tc.wantTo2)
			}
			// The prior window must abut the current window and have equal length.
			if !to2.Equal(tc.from) {
				t.Errorf("prior window must end where current begins: to2=%v from=%v", to2, tc.from)
			}
			if got := to2.Sub(from2); got != tc.wantDuration {
				t.Errorf("prior duration = %v, want %v", got, tc.wantDuration)
			}
			if cur := tc.to.Sub(tc.from); cur != to2.Sub(from2) {
				t.Errorf("prior duration %v != current duration %v", to2.Sub(from2), cur)
			}
		})
	}
}

func TestSupportedMoverDimension(t *testing.T) {
	valid := []string{"as", "prefix", "port", "country"}
	for _, d := range valid {
		if !SupportedMoverDimension(d) {
			t.Errorf("expected %q to be a valid mover dimension", d)
		}
	}

	invalid := []string{
		"", "ip", "AS", "As", " as", "as ",
		"as; DROP TABLE traffic_by_as",
		"traffic_by_as", "1=1", "*",
	}
	for _, d := range invalid {
		if SupportedMoverDimension(d) {
			t.Errorf("expected %q to be rejected as a mover dimension", d)
		}
	}
}

func TestSupportedTalkerDimension(t *testing.T) {
	valid := []string{"as", "ip", "prefix"}
	for _, d := range valid {
		if !SupportedTalkerDimension(d) {
			t.Errorf("expected %q to be a valid talker dimension", d)
		}
	}

	invalid := []string{
		"", "port", "country", "IP", " ip",
		"ip OR 1=1", "prefix--", "as;--",
	}
	for _, d := range invalid {
		if SupportedTalkerDimension(d) {
			t.Errorf("expected %q to be rejected as a talker dimension", d)
		}
	}
}

func TestClampChangesLimit(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 50},
		{-5, 50},
		{10, 10},
		{1000, 1000},
		{5000, 1000},
	}
	for _, c := range cases {
		if got := clampChangesLimit(c.in); got != c.want {
			t.Errorf("clampChangesLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestQuantilesToPercentiles(t *testing.T) {
	got := quantilesToPercentiles([]float64{100, 950, 1990})
	if got.P50 != 100 || got.P95 != 950 || got.P99 != 1990 {
		t.Errorf("unexpected percentiles: %+v", got)
	}
	// Short / empty slices must not panic and default to zero.
	if z := quantilesToPercentiles(nil); z.P50 != 0 || z.P95 != 0 || z.P99 != 0 {
		t.Errorf("expected zero percentiles for nil input, got %+v", z)
	}
	// Negative quantiles (shouldn't happen, but guard) clamp to zero.
	if n := quantilesToPercentiles([]float64{-1, -2, -3}); n.P50 != 0 || n.P95 != 0 || n.P99 != 0 {
		t.Errorf("expected clamped zero for negatives, got %+v", n)
	}
}
