package store

import (
	"testing"

	"github.com/nextmap-io/as-stats/internal/model"
)

func TestNormalizeHeatmapGrid(t *testing.T) {
	present := []model.HeatmapCell{
		{Day: 1, Hour: 0, MeanBps: 100, PeakBps: 200},
		{Day: 7, Hour: 23, MeanBps: 300, PeakBps: 400},
		{Day: 3, Hour: 12, MeanBps: 50, PeakBps: 75},
	}

	cells := normalizeHeatmapGrid(present)

	// Full dense grid, one cell per (day, hour).
	if len(cells) != 7*24 {
		t.Fatalf("expected %d cells, got %d", 7*24, len(cells))
	}

	// Deterministic ordering: (day, hour) ascending.
	idx := 0
	byKey := make(map[[2]uint8]model.HeatmapCell, len(cells))
	for day := uint8(1); day <= 7; day++ {
		for hour := uint8(0); hour < 24; hour++ {
			c := cells[idx]
			if c.Day != day || c.Hour != hour {
				t.Fatalf("cell %d: expected (%d,%d), got (%d,%d)", idx, day, hour, c.Day, c.Hour)
			}
			byKey[[2]uint8{day, hour}] = c
			idx++
		}
	}

	// Present cells retain their values.
	if got := byKey[[2]uint8{1, 0}]; got.MeanBps != 100 || got.PeakBps != 200 {
		t.Errorf("(1,0): expected mean=100 peak=200, got mean=%v peak=%v", got.MeanBps, got.PeakBps)
	}
	if got := byKey[[2]uint8{7, 23}]; got.MeanBps != 300 || got.PeakBps != 400 {
		t.Errorf("(7,23): expected mean=300 peak=400, got mean=%v peak=%v", got.MeanBps, got.PeakBps)
	}
	if got := byKey[[2]uint8{3, 12}]; got.MeanBps != 50 || got.PeakBps != 75 {
		t.Errorf("(3,12): expected mean=50 peak=75, got mean=%v peak=%v", got.MeanBps, got.PeakBps)
	}

	// Missing slots are zero-filled.
	if got := byKey[[2]uint8{2, 5}]; got.MeanBps != 0 || got.PeakBps != 0 {
		t.Errorf("(2,5): expected zero-filled, got mean=%v peak=%v", got.MeanBps, got.PeakBps)
	}
}

func TestNormalizeHeatmapGridEmpty(t *testing.T) {
	cells := normalizeHeatmapGrid(nil)
	if len(cells) != 7*24 {
		t.Fatalf("expected %d zero-filled cells, got %d", 7*24, len(cells))
	}
	for _, c := range cells {
		if c.MeanBps != 0 || c.PeakBps != 0 {
			t.Fatalf("expected all-zero cells, got mean=%v peak=%v at (%d,%d)", c.MeanBps, c.PeakBps, c.Day, c.Hour)
		}
	}
}
