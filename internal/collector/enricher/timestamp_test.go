package enricher

import (
	"net"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// TestEnrichClampsBogusTimestamps guards flows_raw's partition key and TTL
// column: a router-supplied timestamp far in the future would create a
// partition that never expires, so retention could never reclaim it.
func TestEnrichClampsBogusTimestamps(t *testing.T) {
	e := New()
	now := time.Now()

	cases := []struct {
		name      string
		ts        time.Time
		wantClamp bool
	}{
		{"far future", now.Add(400 * 24 * time.Hour), true},
		{"slightly future beyond skew", now.Add(30 * time.Minute), true},
		{"far past", now.Add(-90 * 24 * time.Hour), true},
		{"zero value", time.Time{}, true},
		{"normal now", now, false},
		{"recent past within window", now.Add(-2 * time.Hour), false},
		{"small clock skew tolerated", now.Add(1 * time.Minute), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &model.FlowRecord{RouterIP: net.ParseIP("192.0.2.1"), Timestamp: tc.ts}
			before := e.ClampedTimestamps()
			e.Enrich(f)
			clamped := e.ClampedTimestamps() > before

			if clamped != tc.wantClamp {
				t.Fatalf("clamped=%v, want %v (ts=%v)", clamped, tc.wantClamp, tc.ts)
			}
			if tc.wantClamp {
				if d := time.Since(f.Timestamp); d < 0 || d > time.Minute {
					t.Errorf("clamped timestamp should be ~now, got %v", f.Timestamp)
				}
			} else if !f.Timestamp.Equal(tc.ts) {
				t.Errorf("legitimate timestamp must be preserved: got %v want %v", f.Timestamp, tc.ts)
			}
		})
	}
}
