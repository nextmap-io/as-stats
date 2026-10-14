package writer

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Metrics tracks batch writer statistics.
type Metrics struct {
	FlowsReceived   atomic.Uint64
	FlowsWritten    atomic.Uint64
	BatchesWritten  atomic.Uint64
	WriteErrors     atomic.Uint64
	FlowsDropped    atomic.Uint64
	LastBatchSizeMs atomic.Int64
}

// Pending returns the flows the writer has accepted but not yet written or
// dropped: the accumulating batch plus any INSERT in flight.
func (m *Metrics) Pending() uint64 {
	// Outcomes first: a flow is counted received before it is written or
	// dropped, so this order can never underflow.
	done := m.FlowsWritten.Load() + m.FlowsDropped.Load()
	return m.FlowsReceived.Load() - done
}

// DefaultFlushTimeout bounds a single INSERT when the caller passes no
// timeout. clickhouse-go would otherwise wait up to its 300s read timeout on a
// stalled server, holding the whole pipeline behind one batch.
const DefaultFlushTimeout = 30 * time.Second

// BatchWriter buffers flow records and writes them to ClickHouse in batches.
type BatchWriter struct {
	store        store.FlowWriter
	batchSize    int
	flushInt     time.Duration
	flushTimeout time.Duration
	input        <-chan *model.FlowRecord
	Metrics      Metrics
}

// New creates a new BatchWriter. flushTimeout bounds each INSERT; <= 0 means
// DefaultFlushTimeout.
func New(s store.FlowWriter, input <-chan *model.FlowRecord, batchSize int, flushInterval, flushTimeout time.Duration) *BatchWriter {
	if flushTimeout <= 0 {
		flushTimeout = DefaultFlushTimeout
	}
	return &BatchWriter{
		store:        s,
		batchSize:    batchSize,
		flushInt:     flushInterval,
		flushTimeout: flushTimeout,
		input:        input,
	}
}

// Run starts the batch writer loop. It returns once the input channel is
// closed (or ctx is cancelled) and every accumulated flow has been handed to
// ClickHouse, including any INSERT still in flight.
//
// Batches are double-buffered: one INSERT runs in a separate goroutine while
// the next batch accumulates, so a slow INSERT no longer stalls consumption of
// the input channel. At most two batches are held in memory — when a second
// batch is complete before the first INSERT returns, the loop blocks and
// backpressure reaches the channels (then the UDP socket) as before.
func (w *BatchWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(w.flushInt)
	defer ticker.Stop()

	// Unbuffered: a send succeeds only once the inserter is idle.
	pending := make(chan []*model.FlowRecord)
	// spare recycles the slice of the batch just written so steady state
	// alternates between two allocations.
	spare := make(chan []*model.FlowRecord, 1)
	inserterDone := make(chan struct{})
	go func() {
		defer close(inserterDone)
		for batch := range pending {
			w.write(batch)
			clear(batch) // drop the record pointers so they can be collected
			select {
			case spare <- batch[:0]:
			default:
			}
		}
	}()
	defer func() {
		close(pending)
		<-inserterDone
	}()

	buf := make([]*model.FlowRecord, 0, w.batchSize)
	handOff := func() {
		if len(buf) == 0 {
			return
		}
		pending <- buf
		select {
		case buf = <-spare:
		default:
			buf = make([]*model.FlowRecord, 0, w.batchSize)
		}
	}

	for {
		select {
		case flow, ok := <-w.input:
			if !ok {
				handOff()
				return
			}
			w.Metrics.FlowsReceived.Add(1)
			buf = append(buf, flow)
			if len(buf) >= w.batchSize {
				handOff()
			}
		case <-ticker.C:
			handOff()
		case <-ctx.Done():
			// Drain what is already queued, then flush and stop.
			for {
				select {
				case flow, ok := <-w.input:
					if !ok {
						handOff()
						return
					}
					// Counted here too, otherwise the drained tail shows up in
					// FlowsWritten without ever appearing in FlowsReceived.
					w.Metrics.FlowsReceived.Add(1)
					buf = append(buf, flow)
					if len(buf) >= w.batchSize {
						handOff()
					}
				default:
					handOff()
					return
				}
			}
		}
	}
}

// write performs one INSERT, bounded by flushTimeout. It never uses Run's ctx:
// that one is cancelled at shutdown, exactly when the final batch must still
// be written.
//
// A failed batch is dropped, not retried. One INSERT into flows_raw fans out
// to every materialised view, and a failure can leave some of them committed —
// re-sending the batch would double count in those aggregates. The loss is
// counted in asstats_flows_dropped_total instead.
func (w *BatchWriter) write(batch []*model.FlowRecord) {
	ctx, cancel := context.WithTimeout(context.Background(), w.flushTimeout)
	defer cancel()

	start := time.Now()
	err := w.store.WriteBatch(ctx, batch)
	elapsed := time.Since(start)

	if err != nil {
		w.Metrics.WriteErrors.Add(1)
		w.Metrics.FlowsDropped.Add(uint64(len(batch)))
		// Surface the failure to Prometheus. Without it a total ingestion stall
		// looks exactly like idle traffic.
		metrics.BatchWriteErrors.Inc()
		metrics.FlowsDropped.Add(float64(len(batch)))
		log.Printf("batch write error after %s (%d flows dropped): %v", elapsed.Round(time.Millisecond), len(batch), err)
		return
	}

	count := uint64(len(batch))
	w.Metrics.FlowsWritten.Add(count)
	w.Metrics.BatchesWritten.Add(1)
	w.Metrics.LastBatchSizeMs.Store(elapsed.Milliseconds())
	log.Printf("batch written: %d flows in %s", count, elapsed)
	metrics.FlowsWritten.Add(float64(count))
	metrics.BatchWriteDuration.Observe(elapsed.Seconds())
	metrics.BatchSize.Observe(float64(count))
}
