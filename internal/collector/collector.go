package collector

import (
	"context"
	"log"

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
	cfg       *config.CollectorConfig
	store     store.FlowWriter
	enricher  *enricher.Enricher
	writer    *writer.BatchWriter
	nfListen  *netflow.Listener
	sfListen  *sflow.Listener
	flows     chan *model.FlowRecord
	enriched  chan *model.FlowRecord
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

// Run starts all collector components. It blocks until the context is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	// Start the enrichment pipeline
	go c.runEnricher(ctx)

	// Start the batch writer
	go c.writer.Run(ctx)

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

	log.Println("Stopping collector...")
	if err := c.nfListen.Close(); err != nil {
		log.Printf("netflow listener close error: %v", err)
	}
	if err := c.sfListen.Close(); err != nil {
		log.Printf("sflow listener close error: %v", err)
	}
	close(c.flows)

	return nil
}

// runEnricher reads from the raw flows channel, enriches each flow,
// and sends it to the enriched channel for the batch writer.
func (c *Collector) runEnricher(ctx context.Context) {
	defer close(c.enriched)

	for flow := range c.flows {
		c.enricher.Enrich(flow)

		select {
		case c.enriched <- flow:
		case <-ctx.Done():
			return
		}
	}
}
