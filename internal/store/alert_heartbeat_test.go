package store

import (
	"testing"
	"time"
)

// TestHeartbeatDueThrottles verifies the per-alert throttle collapses a stream
// of cycle-rate heartbeats into one mutation per window, and that the window is
// short enough relative to the auto-resolve threshold that a continuously
// firing alert can never be seen as stale.
func TestHeartbeatDueThrottles(t *testing.T) {
	const (
		staleAfter   = 5 * time.Minute
		evalInterval = 30 * time.Second
	)
	s := &ClickHouseStore{} // nil conn — heartbeatDue must not touch it
	s.setHeartbeatWindow(staleAfter)

	now := time.Now().UTC()
	writes := 0
	var lastWrite time.Time
	for i := 0; i < 20; i++ { // 10 minutes of cycles
		ts := now.Add(time.Duration(i) * evalInterval)
		if s.heartbeatDue("alert-1", ts) {
			writes++
			lastWrite = ts
		}
		// Persisted freshness must always stay inside the stale threshold.
		if age := ts.Sub(lastWrite); age >= staleAfter {
			t.Fatalf("cycle %d: persisted last_seen_at is %s old, threshold is %s", i, age, staleAfter)
		}
	}
	if writes >= 20 {
		t.Fatalf("no throttling happened: %d writes for 20 cycles", writes)
	}
	if writes < 2 {
		t.Fatalf("throttled too aggressively: %d writes over 10 minutes", writes)
	}
}

// TestHeartbeatDueWithoutWindow keeps the safe default: until the engine tells
// the store what "stale" means, every heartbeat is written through.
func TestHeartbeatDueWithoutWindow(t *testing.T) {
	s := &ClickHouseStore{}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if !s.heartbeatDue("alert-1", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("heartbeat %d was throttled with no window configured", i)
		}
	}
}

// TestHeartbeatDueShortThresholdDisablesThrottle guards against flapping: with a
// stale threshold measured in seconds there is no room to skip writes.
func TestHeartbeatDueShortThresholdDisablesThrottle(t *testing.T) {
	s := &ClickHouseStore{}
	s.setHeartbeatWindow(20 * time.Second) // /3 → under alertHeartbeatMinWindow
	now := time.Now().UTC()
	if !s.heartbeatDue("alert-1", now) || !s.heartbeatDue("alert-1", now.Add(time.Second)) {
		t.Fatal("throttling must be disabled for very short stale thresholds")
	}
}

// TestHeartbeatMapIsPruned guards the bounded-map rule: alert IDs come from
// per-target detections, so a map keyed by them must not grow forever.
func TestHeartbeatMapIsPruned(t *testing.T) {
	s := &ClickHouseStore{}
	s.setHeartbeatWindow(5 * time.Minute)

	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		s.heartbeatDue(string(rune('a'+i%26))+time.Duration(i).String(), now)
	}
	// Far enough in the future that every entry above is idle.
	s.heartbeatDue("fresh", now.Add(2*alertHeartbeatMaxIdle))
	if got := len(s.hbWritten); got != 1 {
		t.Fatalf("stale heartbeat entries were not pruned: %d left", got)
	}
}

// TestForgetHeartbeatAllowsRetry verifies a failed mutation does not throttle
// away the retry on the next cycle.
func TestForgetHeartbeatAllowsRetry(t *testing.T) {
	s := &ClickHouseStore{}
	s.setHeartbeatWindow(5 * time.Minute)

	now := time.Now().UTC()
	if !s.heartbeatDue("alert-1", now) {
		t.Fatal("first heartbeat should be written")
	}
	s.forgetHeartbeat("alert-1")
	if !s.heartbeatDue("alert-1", now.Add(time.Second)) {
		t.Fatal("heartbeat should be retried after a failed write")
	}
}
