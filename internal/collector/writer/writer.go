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
	FlowsReceived  atomic.Uint64
	FlowsWritten   atomic.Uint64
	BatchesWritten  atomic.Uint64
	WriteErrors     atomic.Uint64
	LastBatchSizeMs atomic.Int64
}

// BatchWriter buffers flow records and writes them to ClickHouse in batches.
type BatchWriter struct {
	store     store.FlowWriter
	batchSize int
	flushInt  time.Duration
	input     <-chan *model.FlowRecord
	Metrics   Metrics
}

// New creates a new BatchWriter.
func New(s store.FlowWriter, input <-chan *model.FlowRecord, batchSize int, flushInterval time.Duration) *BatchWriter {
	return &BatchWriter{
		store:     s,
		batchSize: batchSize,
		flushInt:  flushInterval,
		input:     input,
	}
}

// shutdownFlushTimeout bounds the final batch write during shutdown. Generous
// enough for a full batch on a loaded ClickHouse, short enough that a wedged
// server cannot stall the process exit.
const shutdownFlushTimeout = 10 * time.Second

// Run starts the batch writer loop. It blocks until the context is cancelled.
func (w *BatchWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(w.flushInt)
	defer ticker.Stop()

	buf := make([]*model.FlowRecord, 0, w.batchSize)

	flush := func(ctx context.Context) {
		if len(buf) == 0 {
			return
		}

		start := time.Now()
		err := w.store.WriteBatch(ctx, buf)
		elapsed := time.Since(start)

		if err != nil {
			w.Metrics.WriteErrors.Add(1)
			// Surface the failure to Prometheus. The batch is dropped (there is
			// no retry queue), so an operator must be able to see both facts —
			// otherwise a total ingestion stall looks exactly like idle traffic.
			metrics.BatchWriteErrors.Inc()
			metrics.FlowsDropped.Add(float64(len(buf)))
			log.Printf("batch write error (%d flows dropped): %v", len(buf), err)
		} else {
			count := uint64(len(buf))
			w.Metrics.FlowsWritten.Add(count)
			w.Metrics.BatchesWritten.Add(1)
			w.Metrics.LastBatchSizeMs.Store(elapsed.Milliseconds())
			log.Printf("batch written: %d flows in %s", count, elapsed)
			// Prometheus metrics
			metrics.FlowsWritten.Add(float64(count))
			metrics.BatchWriteDuration.Observe(elapsed.Seconds())
			metrics.BatchSize.Observe(float64(count))
		}

		buf = buf[:0]
	}

	// finalFlush writes the tail of the buffer on the way out. Run's ctx comes
	// from signal.NotifyContext and is already cancelled once we reach any of
	// the shutdown paths, so reusing it would fail the write and drop the last
	// batch on every clean restart.
	finalFlush := func() {
		fctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
		defer cancel()
		flush(fctx)
	}

	for {
		select {
		case flow, ok := <-w.input:
			if !ok {
				finalFlush()
				return
			}
			w.Metrics.FlowsReceived.Add(1)
			buf = append(buf, flow)
			if len(buf) >= w.batchSize {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			// Drain remaining flows from channel
			for {
				select {
				case flow, ok := <-w.input:
					if !ok {
						finalFlush()
						return
					}
					// Counted here too, otherwise the drained tail shows up in
					// FlowsWritten without ever appearing in FlowsReceived.
					w.Metrics.FlowsReceived.Add(1)
					buf = append(buf, flow)
				default:
					finalFlush()
					return
				}
			}
		}
	}
}
