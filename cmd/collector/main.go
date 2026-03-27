package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nextmap-io/as-stats/internal/collector"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/ripestat"
	"github.com/nextmap-io/as-stats/internal/store"
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

	chStore, err := store.NewClickHouseStore(cfg.ClickHouse)
	if err != nil {
		log.Fatalf("failed to connect to ClickHouse: %v", err)
	}
	defer func() {
		if err := chStore.Close(); err != nil {
			log.Printf("clickhouse close error: %v", err)
		}
	}()
	log.Println("Connected to ClickHouse")

	c := collector.New(cfg, chStore)

	// Load local AS prefixes from RIPEstat
	if cfg.LocalAS > 0 {
		log.Printf("LOCAL_AS=%d — fetching announced prefixes from RIPEstat", cfg.LocalAS)
		if prefixes, err := ripestat.FetchASPrefixes(cfg.LocalAS); err != nil {
			log.Printf("warning: could not fetch prefixes for AS%d: %v", cfg.LocalAS, err)
		} else {
			c.Enricher().SetLocalAS(cfg.LocalAS, prefixes)
		}
	}

	// Load link configuration from ClickHouse
	links, err := chStore.ListLinks(ctx)
	if err != nil {
		log.Printf("warning: could not load links: %v", err)
	} else {
		c.Enricher().LoadLinks(links)
	}

	// Periodically reload link config (picks up links added via API)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if links, err := chStore.ListLinks(ctx); err != nil {
					log.Printf("warning: link reload failed: %v", err)
				} else {
					c.Enricher().LoadLinks(links)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := c.Run(ctx); err != nil {
		log.Fatalf("collector error: %v", err)
	}

	log.Println("Collector stopped")
}
