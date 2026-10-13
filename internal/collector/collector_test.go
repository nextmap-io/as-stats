package collector

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
)

// slowStore mimics a production INSERT (~365 ms p50) and, like the real
// driver, refuses to write with an already-cancelled context.
type slowStore struct {
	mu      sync.Mutex
	written int
	delay   time.Duration
}

func (s *slowStore) WriteBatch(ctx context.Context, f []*model.FlowRecord) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.written += len(f)
	s.mu.Unlock()
	return nil
}

func (s *slowStore) Close() error { return nil }

func (s *slowStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

func testConfig() *config.CollectorConfig {
	return &config.CollectorConfig{
		ListenNetFlow:   "127.0.0.1:0",
		ListenSFlow:     "127.0.0.1:0",
		BatchSize:       10000,
		FlushInterval:   time.Minute,
		Workers:         2,
		ShutdownTimeout: 10 * time.Second,
	}
}

// TestRunFlushesEverythingOnShutdown is the regression for Run returning while
// the final flush was still in flight: main() exits right after Run, so every
// flow buffered at SIGTERM was lost (0 of 500 written).
func TestRunFlushesEverythingOnShutdown(t *testing.T) {
	st := &slowStore{delay: 300 * time.Millisecond}
	c := New(testConfig(), st)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		if err := c.Run(ctx); err != nil {
			t.Errorf("Run: %v", err)
		}
		close(done)
	}()

	const n = 500
	for i := 0; i < n; i++ {
		c.flows <- &model.FlowRecord{Timestamp: time.Now(), RouterIP: net.IP{192, 0, 2, 1}}
	}
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if got := st.count(); got != n {
		t.Fatalf("flows persisted when Run returned: %d / %d", got, n)
	}
}

// TestRunShutdownIsBounded checks that a wedged store cannot hold the process
// past ShutdownTimeout.
func TestRunShutdownIsBounded(t *testing.T) {
	st := &slowStore{delay: time.Hour}
	cfg := testConfig()
	cfg.ShutdownTimeout = 300 * time.Millisecond
	c := New(cfg, st)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = c.Run(ctx)
		close(done)
	}()

	c.flows <- &model.FlowRecord{Timestamp: time.Now(), RouterIP: net.IP{192, 0, 2, 1}}
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run blocked past the shutdown timeout")
	}
}
