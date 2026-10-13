package writer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// mockStore records batch writes for testing. It mimics a real driver by
// refusing to write with an already-cancelled context.
type mockStore struct {
	mu      sync.Mutex
	batches [][]*model.FlowRecord
}

func (m *mockStore) WriteBatch(ctx context.Context, flows []*model.FlowRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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

	w := New(store, ch, 5, 10*time.Second, time.Second) // batch size 5, long flush interval

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

	w := New(store, ch, 100, 100*time.Millisecond, time.Second) // large batch, short timer

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

	w := New(store, ch, 100, 10*time.Second, time.Second)

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

	// Cancel -> should drain and flush. The final write must not reuse the
	// cancelled context, or the tail of every clean restart is lost.
	cancel()
	<-done

	if store.totalFlows() != 2 {
		t.Errorf("expected 2 flows flushed on shutdown, got %d", store.totalFlows())
	}
}

func TestBatchWriterFlushOnClosedInputAfterCancel(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 100, 10*time.Second, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	ch <- &model.FlowRecord{Bytes: 100}
	time.Sleep(10 * time.Millisecond)

	// The pipeline closes the channel during shutdown, i.e. after ctx is done.
	cancel()
	close(ch)
	<-done

	if store.totalFlows() != 1 {
		t.Errorf("expected 1 flow flushed when the input closed, got %d", store.totalFlows())
	}
}

func TestBatchWriterMetrics(t *testing.T) {
	store := &mockStore{}
	ch := make(chan *model.FlowRecord, 100)

	w := New(store, ch, 3, 10*time.Second, time.Second)

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

// gatedStore blocks every WriteBatch until release is closed (or the write's
// context expires), so tests can hold an INSERT in flight.
type gatedStore struct {
	mockStore
	release chan struct{}
	started chan struct{}
}

func (g *gatedStore) WriteBatch(ctx context.Context, flows []*model.FlowRecord) error {
	select {
	case g.started <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.mockStore.WriteBatch(ctx, flows)
}

// A stalled ClickHouse must not hold a batch forever: the write is abandoned
// after flushTimeout, counted as an error, and not retried.
func TestBatchWriterFlushTimeoutDropsBatch(t *testing.T) {
	store := &gatedStore{release: make(chan struct{}), started: make(chan struct{}, 1)}
	ch := make(chan *model.FlowRecord, 100)
	w := New(store, ch, 3, time.Hour, 50*time.Millisecond)

	done := make(chan struct{})
	go func() {
		w.Run(context.Background())
		close(done)
	}()

	for i := 0; i < 3; i++ {
		ch <- &model.FlowRecord{Bytes: 1}
	}
	time.Sleep(200 * time.Millisecond)
	if got := w.Metrics.WriteErrors.Load(); got != 1 {
		t.Fatalf("WriteErrors = %d, want 1 after the flush timeout", got)
	}

	// The writer recovers for the next batch.
	close(store.release)
	for i := 0; i < 3; i++ {
		ch <- &model.FlowRecord{Bytes: 1}
	}
	close(ch)
	<-done
	if got := store.totalFlows(); got != 3 {
		t.Errorf("flows written = %d, want 3 (failed batch dropped, not retried)", got)
	}
}

// While one INSERT is in flight the writer keeps consuming its input into a
// second buffer, but no further: memory stays bounded at two batches.
func TestBatchWriterDoubleBuffers(t *testing.T) {
	store := &gatedStore{release: make(chan struct{}), started: make(chan struct{}, 1)}
	ch := make(chan *model.FlowRecord) // unbuffered: every send is a consumption
	w := New(store, ch, 5, time.Hour, 10*time.Second)

	done := make(chan struct{})
	go func() {
		w.Run(context.Background())
		close(done)
	}()

	send := func(n int) int {
		for i := 0; i < n; i++ {
			select {
			case ch <- &model.FlowRecord{Bytes: 1}:
			case <-time.After(200 * time.Millisecond):
				return i
			}
		}
		return n
	}

	if got := send(5); got != 5 {
		t.Fatalf("first batch: accepted %d of 5", got)
	}
	<-store.started // first INSERT is now blocked in flight

	if got := send(5); got != 5 {
		t.Fatalf("second batch accumulated %d of 5 while the first INSERT was in flight", got)
	}
	// Both buffers are full: the next flow must not be accepted.
	if got := send(1); got != 0 {
		t.Fatalf("writer accepted a flow beyond two batches; memory is unbounded")
	}

	close(store.release)
	if got := send(1); got != 1 {
		t.Fatal("writer did not resume after the INSERT completed")
	}
	close(ch)
	<-done
	if got := store.totalFlows(); got != 11 {
		t.Errorf("flows written = %d, want 11", got)
	}
}
