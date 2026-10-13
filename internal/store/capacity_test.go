package store

import (
	"math"
	"testing"
	"time"
)

func TestCompleteBuckets(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 13, h, m, s, 0, time.UTC) }
	step := 5 * time.Minute

	// Unaligned window ending "now": the bucket containing now (12:00–12:05)
	// is still filling and must not be the "current" one.
	start, end, last := completeBuckets(at(11, 2, 30), at(12, 3, 10), at(12, 3, 10), step)
	if !start.Equal(at(11, 5, 0)) || !end.Equal(at(12, 0, 0)) || !last.Equal(at(11, 55, 0)) {
		t.Fatalf("got start=%s end=%s last=%s", start, end, last)
	}

	// `to` in the future is capped at now.
	_, end, last = completeBuckets(at(10, 0, 0), at(13, 0, 0), at(12, 3, 10), step)
	if !end.Equal(at(12, 0, 0)) || !last.Equal(at(11, 55, 0)) {
		t.Fatalf("future to: end=%s last=%s", end, last)
	}

	// Aligned bounds are kept as-is.
	start, end, _ = completeBuckets(at(11, 0, 0), at(12, 0, 0), at(13, 0, 0), step)
	if !start.Equal(at(11, 0, 0)) || !end.Equal(at(12, 0, 0)) {
		t.Fatalf("aligned: start=%s end=%s", start, end)
	}

	// A window narrower than a bucket degrades to the last complete bucket.
	start, end, last = completeBuckets(at(12, 1, 0), at(12, 3, 0), at(12, 3, 0), step)
	if !start.Equal(at(11, 55, 0)) || !end.Equal(at(12, 0, 0)) || !last.Equal(at(11, 55, 0)) {
		t.Fatalf("narrow: start=%s end=%s last=%s", start, end, last)
	}

	// Daily step aligns to UTC midnight, like toStartOfInterval(…, 86400).
	_, end, _ = completeBuckets(at(0, 0, 0).Add(-72*time.Hour), at(15, 0, 0), at(15, 0, 0), 24*time.Hour)
	if !end.Equal(at(0, 0, 0)) {
		t.Fatalf("daily end = %s, want midnight", end)
	}
}

func almostEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestLinearRegression(t *testing.T) {
	// y = 100 + 100*x for x = 0..9
	xs := make([]float64, 10)
	ys := make([]float64, 10)
	for i := 0; i < 10; i++ {
		xs[i] = float64(i)
		ys[i] = 100 + 100*float64(i)
	}
	slope, intercept := linearRegression(xs, ys)
	if !almostEqual(slope, 100, 1e-6) {
		t.Fatalf("slope = %v, want 100", slope)
	}
	if !almostEqual(intercept, 100, 1e-6) {
		t.Fatalf("intercept = %v, want 100", intercept)
	}

	// Flat series → slope 0, intercept = mean.
	flat := []float64{5, 5, 5, 5}
	fx := []float64{0, 1, 2, 3}
	s2, i2 := linearRegression(fx, flat)
	if !almostEqual(s2, 0, 1e-9) || !almostEqual(i2, 5, 1e-9) {
		t.Fatalf("flat regression = (%v,%v), want (0,5)", s2, i2)
	}

	// Empty / mismatched → (0,0).
	if s, i := linearRegression(nil, nil); s != 0 || i != 0 {
		t.Fatalf("empty regression = (%v,%v), want (0,0)", s, i)
	}
}

func TestForecastDaysToLevel(t *testing.T) {
	// Trend y = 100 + 100*x, last sample at x=9 (value 1000).
	slope, intercept, lastX := 100.0, 100.0, 9.0

	// Level 1500: crossX = (1500-100)/100 = 14 → 14-9 = 5 days.
	if d := forecastDaysToLevel(slope, intercept, lastX, 1500); d == nil {
		t.Fatal("expected non-nil forecast for level 1500")
	} else if !almostEqual(*d, 5, 1e-6) {
		t.Fatalf("days to 1500 = %v, want 5", *d)
	}

	// Level equal to current projected value (1000) → 0 (already reached).
	if d := forecastDaysToLevel(slope, intercept, lastX, 1000); d == nil || *d != 0 {
		t.Fatalf("days to 1000 = %v, want 0", d)
	}

	// Level below current (900) → 0 (already exceeded).
	if d := forecastDaysToLevel(slope, intercept, lastX, 900); d == nil || *d != 0 {
		t.Fatalf("days to 900 = %v, want 0", d)
	}

	// Flat/declining trend → nil (never crosses upward).
	if d := forecastDaysToLevel(0, 500, lastX, 1000); d != nil {
		t.Fatalf("flat trend forecast = %v, want nil", *d)
	}
	if d := forecastDaysToLevel(-10, 500, lastX, 1000); d != nil {
		t.Fatalf("declining trend forecast = %v, want nil", *d)
	}
}

func TestDownsampleDesc(t *testing.T) {
	// Small unsorted input → returned sorted descending, unchanged length.
	small := []float64{3, 1, 2}
	got := downsampleDesc(small, 500)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Fatalf("not sorted descending: %v", got)
	}

	// Large input → capped at max, endpoints preserved.
	n := 1000
	big := make([]float64, n)
	for i := 0; i < n; i++ {
		big[i] = float64(i) // ascending; will be reversed
	}
	ds := downsampleDesc(big, 500)
	if len(ds) != 500 {
		t.Fatalf("downsampled len = %d, want 500", len(ds))
	}
	if ds[0] != 999 {
		t.Fatalf("first point = %v, want 999 (max)", ds[0])
	}
	if ds[len(ds)-1] != 0 {
		t.Fatalf("last point = %v, want 0 (min)", ds[len(ds)-1])
	}
	// Strictly non-increasing.
	for i := 1; i < len(ds); i++ {
		if ds[i] > ds[i-1] {
			t.Fatalf("not non-increasing at %d: %v > %v", i, ds[i], ds[i-1])
		}
	}

	// max == 1 → single (max) element.
	if one := downsampleDesc(big, 1); len(one) != 1 || one[0] != 999 {
		t.Fatalf("max=1 got %v, want [999]", one)
	}
}

func TestBuildHistogram(t *testing.T) {
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = float64(i) // 0..99
	}
	h := buildHistogram(vals, 20)
	if len(h) != 20 {
		t.Fatalf("bins = %d, want 20", len(h))
	}
	var total uint64
	for _, b := range h {
		total += b.Count
		if b.UpperBps < b.LowerBps {
			t.Fatalf("bin bounds inverted: %+v", b)
		}
	}
	if total != uint64(len(vals)) {
		t.Fatalf("histogram total = %d, want %d", total, len(vals))
	}

	// All-equal values → everything in the first bin.
	eq := []float64{7, 7, 7, 7, 7}
	he := buildHistogram(eq, 20)
	if len(he) != 20 || he[0].Count != 5 {
		t.Fatalf("equal-values histogram wrong: %+v", he[0])
	}

	// Empty input → nil.
	if buildHistogram(nil, 20) != nil {
		t.Fatal("expected nil histogram for empty input")
	}
}
