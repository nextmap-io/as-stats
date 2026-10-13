package store

import (
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

func TestBuildLiveRouters(t *testing.T) {
	now := time.Date(2026, 10, 13, 19, 47, 0, 0, time.UTC)
	seen := []liveRouterRow{
		{ip: "192.0.2.1", lastSeen: now.Add(-10 * time.Second), ageSecs: 10, flows: 100},
		{ip: "2001:db8::4", lastSeen: now.Add(-300 * time.Second), ageSecs: 300, flows: 5000},
		{ip: "192.0.2.9", lastSeen: now.Add(-119 * time.Second), ageSecs: 119, flows: 1},
		{ip: "192.0.2.8", lastSeen: now.Add(-120 * time.Second), ageSecs: 120, flows: 1},
	}
	// 192.0.2.1 is both seen and configured: it must appear once.
	expected := []string{"192.0.2.1", "198.51.100.7", "198.51.100.3", "198.51.100.7"}

	got := buildLiveRouters(seen, expected)
	want := []struct{ ip, state string }{
		{"2001:db8::4", model.RouterStateStale},
		{"192.0.2.1", model.RouterStateOK},
		{"192.0.2.8", model.RouterStateStale}, // 120 s is the boundary: stale
		{"192.0.2.9", model.RouterStateOK},
		{"198.51.100.3", model.RouterStateMissing},
		{"198.51.100.7", model.RouterStateMissing},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routers, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].RouterIP != w.ip || got[i].State != w.state {
			t.Errorf("router %d = %s/%s, want %s/%s", i, got[i].RouterIP, got[i].State, w.ip, w.state)
		}
	}
	if got[1].LastSeen != "2026-10-13T19:46:50Z" {
		t.Errorf("last_seen = %q, want RFC3339 UTC", got[1].LastSeen)
	}
	if got[4].LastSeen != "" || got[4].Flows10m != 0 {
		t.Errorf("missing router should have empty last_seen and 0 flows: %+v", got[4])
	}
}
