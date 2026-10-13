package collector

import (
	"context"
	"log"
	"time"

	"github.com/nextmap-io/as-stats/internal/collector/enricher"
	"github.com/nextmap-io/as-stats/internal/collector/netflow"
	"github.com/nextmap-io/as-stats/internal/collector/sflow"
	"github.com/nextmap-io/as-stats/internal/collector/writer"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Collector orchestrates flow collection, enrichment, and storage.
type Collector struct {
	cfg      *config.CollectorConfig
	store    store.FlowWriter
	enricher *enricher.Enricher
	writer   *writer.BatchWriter
	nfListen *netflow.Listener
	sfListen *sflow.Listener
	flows    chan *model.FlowRecord
	enriched chan *model.FlowRecord
}

// New creates a new Collector.
func New(cfg *config.CollectorConfig, s store.FlowWriter) *Collector {
	flowsCh := make(chan *model.FlowRecord, cfg.BatchSize*4)
	enrichedCh := make(chan *model.FlowRecord, cfg.BatchSize*4)

	return &Collector{
		cfg:      cfg,
		store:    s,
		enricher: enricher.New(),
		writer:   writer.New(s, enrichedCh, cfg.BatchSize, cfg.FlushInterval),
		nfListen: netflow.NewListener(cfg.ListenNetFlow, cfg.Workers),
		sfListen: sflow.NewListener(cfg.ListenSFlow, cfg.Workers),
		flows:    flowsCh,
		enriched: enrichedCh,
	}
}

// Enricher returns the enricher for loading link/AS data.
func (c *Collector) Enricher() *enricher.Enricher {
	return c.enricher
}

// WriterMetrics returns the writer metrics.
func (c *Collector) WriterMetrics() *writer.Metrics {
	return &c.writer.Metrics
}

// Run starts all collector components. It blocks until the context is
// cancelled and the pipeline has drained, or the shutdown timeout expired.
func (c *Collector) Run(ctx context.Context) error {
	go c.runEnricher()

	// The writer is deliberately not handed ctx: on shutdown it must keep
	// consuming until the enricher closes its input, otherwise flows that were
	// already decoded are lost on every clean restart. It stops when its input
	// channel closes.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		c.writer.Run(context.Background())
	}()

	// Start UDP listeners
	if c.cfg.ListenNetFlow != "" {
		if err := c.nfListen.Start(ctx, c.flows); err != nil {
			return err
		}
		log.Printf("NetFlow/IPFIX listener started on %s", c.cfg.ListenNetFlow)
	}

	if c.cfg.ListenSFlow != "" {
		if err := c.sfListen.Start(ctx, c.flows); err != nil {
			return err
		}
		log.Printf("sFlow listener started on %s", c.cfg.ListenSFlow)
	}

	<-ctx.Done()
	c.shutdown(writerDone)
	return nil
}

// defaultShutdownTimeout applies when the config leaves ShutdownTimeout unset.
const defaultShutdownTimeout = 30 * time.Second

// shutdown drains the pipeline stage by stage: the listeners stop reading, the
// decoders finish the packets already queued, closing c.flows lets the
// enricher drain and close c.enriched, and the writer flushes what it holds.
// Each stage stops only once its input is closed, so no decoded flow is
// dropped. The whole cascade is bounded so a wedged ClickHouse cannot hold the
// process past the container's stop grace period.
func (c *Collector) shutdown(writerDone <-chan struct{}) {
	timeout := c.cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	log.Println("Stopping collector: draining pipeline")
	if err := c.nfListen.Close(); err != nil {
		log.Printf("netflow listener close error: %v", err)
	}
	if err := c.sfListen.Close(); err != nil {
		log.Printf("sflow listener close error: %v", err)
	}

	// The decoder goroutines are the only senders on c.flows, so the channel
	// must not be closed until they have all stopped — otherwise a decoder
	// mid-send panics with "send on closed channel" and kills the process on
	// the way out. If they do not settle in time, leave the channel open and
	// give up: a leaked channel at exit is strictly better than a panic.
	decodersDone := make(chan struct{})
	go func() {
		defer close(decodersDone)
		c.nfListen.Wait()
		c.sfListen.Wait()
	}()
	select {
	case <-decodersDone:
		close(c.flows)
	case <-deadline.C:
		log.Printf("warning: decoders still running after %s; about %d queued flows not written",
			timeout, len(c.flows)+len(c.enriched))
		return
	}

	select {
	case <-writerDone:
		log.Println("Collector pipeline drained")
	case <-deadline.C:
		log.Printf("warning: writer did not finish within %s; about %d queued flows not written",
			timeout, len(c.flows)+len(c.enriched))
	}
}

// runEnricher reads from the raw flows channel, enriches each flow, and sends
// it to the enriched channel for the batch writer. It stops only once c.flows
// is closed, then closes c.enriched so the writer flushes and exits.
func (c *Collector) runEnricher() {
	defer close(c.enriched)

	for flow := range c.flows {
		c.enricher.Enrich(flow)
		c.enriched <- flow
	}
}
