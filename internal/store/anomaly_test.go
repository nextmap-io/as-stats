package store

import (
	"testing"
)

// anomEq is a tight-tolerance float comparison for the anomaly helper tests.
// (store already defines almostEqual(a, b, tol) in capacity_test.go.)
func anomEq(a, b float64) bool { return almostEqual(a, b, 1e-6) }

func TestMedian(t *testing.T) {
	tests := []struct {
		name string
		in   []float64
		want float64
	}{
		{"empty", nil, 0},
		{"single", []float64{5}, 5},
		{"odd", []float64{3, 1, 2}, 2},
		{"even", []float64{4, 1, 3, 2}, 2.5},
		{"unsorted-not-mutated", []float64{9, 1, 5}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := median(tt.in); !anomEq(got, tt.want) {
				t.Fatalf("median(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}

	// median must not mutate its input.
	in := []float64{9, 1, 5}
	_ = median(in)
	if in[0] != 9 || in[1] != 1 || in[2] != 5 {
		t.Fatalf("median mutated its input: %v", in)
	}
}

func TestMedianMADBaseline(t *testing.T) {
	// Zero-spread samples: MAD = 0 so baseline == median regardless of k.
	if got := medianMADBaseline([]float64{10, 10, 10, 10}, 2.5); !anomEq(got, 10) {
		t.Fatalf("zero-MAD baseline = %v, want 10", got)
	}

	// Known spread: samples 1..5, median = 3, abs deviations {2,1,0,1,2},
	// MAD = 1, so baseline = 3 + k*1.4826*1.
	k := 2.5
	want := 3 + k*madScale*1
	if got := medianMADBaseline([]float64{1, 2, 3, 4, 5}, k); !anomEq(got, want) {
		t.Fatalf("baseline = %v, want %v", got, want)
	}

	// MAD scaling: doubling all spreads doubles the (baseline - median) term.
	base1 := medianMADBaseline([]float64{9, 10, 11}, 3) // median 10, MAD 1
	base2 := medianMADBaseline([]float64{8, 10, 12}, 3) // median 10, MAD 2
	if !anomEq((base2 - 10), 2*(base1-10)) {
		t.Fatalf("MAD scaling wrong: base1=%v base2=%v", base1, base2)
	}

	// Empty input → 0.
	if got := medianMADBaseline(nil, 2.5); got != 0 {
		t.Fatalf("empty baseline = %v, want 0", got)
	}
}

func TestAnomalyDecision(t *testing.T) {
	steady := []float64{100, 100, 100, 100, 100, 100} // baseline == 100 (MAD 0)

	tests := []struct {
		name         string
		current      float64
		samples      []float64
		k            float64
		wantFire     bool
		wantBaseline float64
	}{
		{
			name:         "clear spike fires",
			current:      300,
			samples:      steady,
			k:            2.5,
			wantFire:     true,
			wantBaseline: 100,
		},
		{
			name:     "insufficient history skips",
			current:  10000,
			samples:  []float64{100, 100, 100}, // len 3 < anomalyMinSamples (4)
			k:        2.5,
			wantFire: false,
		},
		{
			name:         "below baseline does not fire",
			current:      50,
			samples:      steady,
			k:            2.5,
			wantFire:     false,
			wantBaseline: 100,
		},
		{
			name:         "tiny excursion below min-ratio does not fire",
			current:      110, // ratio 1.1 < anomalyMinRatio (1.20)
			samples:      steady,
			k:            2.5,
			wantFire:     false,
			wantBaseline: 100,
		},
		{
			name:         "just above min-ratio fires",
			current:      121, // ratio 1.21 >= 1.20
			samples:      steady,
			k:            2.5,
			wantFire:     true,
			wantBaseline: 100,
		},
		{
			name:     "zero baseline (no history) does not fire",
			current:  5,
			samples:  []float64{0, 0, 0, 0, 0},
			k:        2.5,
			wantFire: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fire, baseline, ratio := anomalyDecision(tt.current, tt.samples, tt.k)
			if fire != tt.wantFire {
				t.Fatalf("fire = %v, want %v (baseline=%v ratio=%v)", fire, tt.wantFire, baseline, ratio)
			}
			if tt.wantBaseline != 0 && !anomEq(baseline, tt.wantBaseline) {
				t.Fatalf("baseline = %v, want %v", baseline, tt.wantBaseline)
			}
			if fire && !(tt.current > baseline) {
				t.Fatalf("fired but current %v not above baseline %v", tt.current, baseline)
			}
		})
	}
}
