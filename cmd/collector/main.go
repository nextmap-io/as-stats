package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nextmap-io/as-stats/internal/config"
)

func main() {
	cfg, err := config.LoadCollector()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("AS-Stats collector starting")
	log.Printf("  NetFlow listen: %s", cfg.ListenNetFlow)
	log.Printf("  sFlow listen:   %s", cfg.ListenSFlow)
	log.Printf("  ClickHouse:     %s/%s", cfg.ClickHouse.Addr, cfg.ClickHouse.Database)
	log.Printf("  Batch size:     %d", cfg.BatchSize)
	log.Printf("  Flush interval: %s", cfg.FlushInterval)
	log.Printf("  Workers:        %d", cfg.Workers)

	// TODO: Initialize ClickHouse store
	// TODO: Initialize enricher (link mapping, AS names)
	// TODO: Initialize batch writer
	// TODO: Start UDP listeners (NetFlow, sFlow)

	<-ctx.Done()
	log.Printf("Shutting down collector...")
}
