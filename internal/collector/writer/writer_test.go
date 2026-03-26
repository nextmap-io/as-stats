package writer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// mockStore records batch writes for testing.
type mockStore struct {
	mu      sync.Mutex
	batches [][]*model.FlowRecord
}

func (m *mockStore) WriteBatch(_ context.Context, flows []*model.FlowRecord) error {
	cp := make([]*model.FlowRecord, len(flows))
	copy(cp, flows)
	m.mu.Lock()
	m.batches = append(m.batches, cp)
	m.mu.Unlock()
	return nil
}

func (m *mockStore) Close() error { return nil }

func (m *mockStore) totalFlows() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.batches {
		n += len(b)
	}
	return n
}

func (m *mockStore) batchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.batches)
}

func TestBatchWriterFlushOnSize(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 5, 10*time.Second) // batch size 5, long flush interval

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	// Send exactly 5 flows -> should trigger a batch write
	for i := 0; i < 5; i++ {
		ch <- &model.FlowRecord{Bytes: uint64(i + 1)}
	}

	// Wait a bit for the batch to be written
	time.Sleep(50 * time.Millisecond)

	if store.totalFlows() != 5 {
		t.Errorf("expected 5 flows written, got %d", store.totalFlows())
	}
	if store.batchCount() != 1 {
		t.Errorf("expected 1 batch, got %d", store.batchCount())
	}

	cancel()
}

func TestBatchWriterFlushOnTimer(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 100, 100*time.Millisecond) // large batch, short timer

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	// Send 3 flows (less than batch size)
	for i := 0; i < 3; i++ {
		ch <- &model.FlowRecord{Bytes: uint64(i + 1)}
	}

	// Wait for timer flush
	time.Sleep(250 * time.Millisecond)

	if store.totalFlows() != 3 {
		t.Errorf("expected 3 flows written on timer, got %d", store.totalFlows())
	}

	cancel()
}

func TestBatchWriterFlushOnShutdown(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 100, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Send 2 flows
	ch <- &model.FlowRecord{Bytes: 100}
	ch <- &model.FlowRecord{Bytes: 200}
	time.Sleep(10 * time.Millisecond)

	// Cancel -> should drain and flush
	cancel()
	<-done

	if store.totalFlows() != 2 {
		t.Errorf("expected 2 flows flushed on shutdown, got %d", store.totalFlows())
	}
}

func TestBatchWriterMetrics(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 3, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	for i := 0; i < 6; i++ {
		ch <- &model.FlowRecord{Bytes: 1}
	}

	time.Sleep(50 * time.Millisecond)

	if w.Metrics.FlowsReceived.Load() != 6 {
		t.Errorf("expected 6 flows received, got %d", w.Metrics.FlowsReceived.Load())
	}
	if w.Metrics.FlowsWritten.Load() != 6 {
		t.Errorf("expected 6 flows written, got %d", w.Metrics.FlowsWritten.Load())
	}
	if w.Metrics.BatchesWritten.Load() != 2 {
		t.Errorf("expected 2 batches, got %d", w.Metrics.BatchesWritten.Load())
	}

	cancel()
}
