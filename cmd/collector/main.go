package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nextmap-io/as-stats/internal/alerts"
	"github.com/nextmap-io/as-stats/internal/api/handler"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/collector"
	"github.com/nextmap-io/as-stats/internal/config"
	_ "github.com/nextmap-io/as-stats/internal/metrics" // register metrics
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

	// Apply configurable TTL to flows_log if the table exists.
	// Idempotent — safe to call on every startup.
	if err := chStore.SetFlowLogRetention(ctx, cfg.FlowLogRetentionDays); err != nil {
		log.Printf("warning: could not set flows_log retention to %d days: %v",
			cfg.FlowLogRetentionDays, err)
	} else if cfg.FlowLogRetentionDays != 180 {
		log.Printf("flows_log retention set to %d days", cfg.FlowLogRetentionDays)
	}

	c := collector.New(cfg, chStore)

	// Load local AS prefixes from RIPEstat
	var localPrefixStrs []string
	if cfg.LocalAS > 0 {
		log.Printf("LOCAL_AS=%d — fetching announced prefixes from RIPEstat", cfg.LocalAS)
		if prefixes, err := ripestat.FetchASPrefixes(cfg.LocalAS); err != nil {
			log.Printf("warning: could not fetch prefixes for AS%d: %v", cfg.LocalAS, err)
		} else {
			c.Enricher().SetLocalAS(cfg.LocalAS, prefixes)
			for _, p := range prefixes {
				localPrefixStrs = append(localPrefixStrs, p.String())
			}
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

	// Start Prometheus /metrics HTTP server for the collector
	if cfg.PrometheusAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", handler.MetricsHandler(
			cfg.PrometheusAllowCIDR,
			cfg.PrometheusUser,
			cfg.PrometheusPass,
		))
		go func() {
			log.Printf("Prometheus /metrics listening on %s", cfg.PrometheusAddr)
			srv := &http.Server{Addr: cfg.PrometheusAddr, Handler: mux}
			go func() {
				<-ctx.Done()
				_ = srv.Close()
			}()
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("prometheus server error: %v", err)
			}
		}()
	}

	// Start alert engine if enabled
	if cfg.FeatureAlerts {
		log.Printf("FEATURE_ALERTS=true — seeding default rules and starting engine")
		if err := alerts.EnsureDefaultRules(ctx, chStore); err != nil {
			log.Printf("warning: could not seed default alert rules: %v", err)
		}
		engine := alerts.New(
			chStore,
			alerts.NewWebhookNotifier(),
			localPrefixStrs,
			cfg.AlertEvalInterval,
			cfg.AlertStaleThreshold,
		)
		// Connect the BGP blocker if BGP_API_URL is configured
		if cfg.BGPAPIURL != "" {
			log.Printf("BGP auto-block via RemoteBlocker → %s", cfg.BGPAPIURL)
			engine.SetBlocker(bgp.NewRemote(cfg.BGPAPIURL), chStore)
		}
		go engine.Run(ctx)
	}

	if err := c.Run(ctx); err != nil {
		log.Fatalf("collector error: %v", err)
	}

	log.Println("Collector stopped")
}
