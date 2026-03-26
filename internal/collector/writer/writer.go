package writer

import (
	"context"
	"log"
	"sync/atomic"
	"time"

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

// Run starts the batch writer loop. It blocks until the context is cancelled.
func (w *BatchWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(w.flushInt)
	defer ticker.Stop()

	buf := make([]*model.FlowRecord, 0, w.batchSize)

	flush := func() {
		if len(buf) == 0 {
			return
		}

		start := time.Now()
		err := w.store.WriteBatch(ctx, buf)
		elapsed := time.Since(start)

		if err != nil {
			w.Metrics.WriteErrors.Add(1)
			log.Printf("batch write error (%d flows): %v", len(buf), err)
		} else {
			count := uint64(len(buf))
			w.Metrics.FlowsWritten.Add(count)
			w.Metrics.BatchesWritten.Add(1)
			w.Metrics.LastBatchSizeMs.Store(elapsed.Milliseconds())
			log.Printf("batch written: %d flows in %s", count, elapsed)
		}

		buf = buf[:0]
	}

	for {
		select {
		case flow, ok := <-w.input:
			if !ok {
				flush()
				return
			}
			w.Metrics.FlowsReceived.Add(1)
			buf = append(buf, flow)
			if len(buf) >= w.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			// Drain remaining flows from channel
			for {
				select {
				case flow, ok := <-w.input:
					if !ok {
						flush()
						return
					}
					buf = append(buf, flow)
				default:
					flush()
					return
				}
			}
		}
	}
}
