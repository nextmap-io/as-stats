package main

import (
	"context"
	"fmt"
	"log"
	"net"
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
	"github.com/nextmap-io/as-stats/internal/reports"
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

	// Seed DB-backed retention policies (idempotent — only on first startup).
	// flows_log is seeded with FLOW_LOG_RETENTION_DAYS; everything else with the
	// migration defaults. The reconciler below applies any divergence.
	if err := chStore.EnsureRetentionPolicies(ctx, cfg.FlowLogRetentionDays); err != nil {
		log.Printf("warning: could not seed retention policies: %v", err)
	}

	// Retention reconciler: applies desired TTLs from retention_policies to the
	// live tables on a fixed interval, running once immediately at startup.
	go func() {
		if err := chStore.ReconcileRetention(ctx); err != nil {
			log.Printf("warning: retention reconcile failed: %v", err)
		}
		ticker := time.NewTicker(cfg.RetentionReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := chStore.ReconcileRetention(ctx); err != nil {
					log.Printf("warning: retention reconcile failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Config-table soft-delete purge: physically removes tombstoned rows from
	// the ReplacingMergeTree config tables once a day.
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := chStore.PurgeSoftDeleted(ctx, cfg.ConfigPurgeDays); err != nil {
					log.Printf("warning: config purge failed: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	c := collector.New(cfg, chStore)

	// Load local AS prefixes from RIPEstat.
	//
	// This is not best-effort for the alert engine: every IP-scoped rule skips
	// its CIDR filter when the prefix list is empty (buildCIDRFilter returns
	// "1=1"), so starting the engine without prefixes makes every rule evaluate
	// all traffic the MVs see — including pure transit — and alert on other
	// people's networks. A single transient RIPEstat failure at boot would do
	// that silently, so retry here and, if that fails, keep retrying in the
	// background and only start the engine once the scope is known.
	var localPrefixStrs []string
	if cfg.LocalAS > 0 {
		log.Printf("LOCAL_AS=%d — fetching announced prefixes from RIPEstat", cfg.LocalAS)
		if prefixes, err := fetchPrefixesWithRetry(ctx, cfg.LocalAS, 3); err != nil {
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

		startEngine := func(prefixes []string) {
			engine := alerts.New(
				chStore,
				alerts.NewWebhookNotifier(),
				prefixes,
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

		switch {
		case cfg.LocalAS == 0:
			// No local AS configured: the operator has opted out of scoping.
			startEngine(nil)
		case len(localPrefixStrs) > 0:
			startEngine(localPrefixStrs)
		default:
			// Scope unknown. Do not evaluate unscoped — keep retrying instead.
			log.Printf("alert engine held back: LOCAL_AS=%d but no prefixes known; retrying in background", cfg.LocalAS)
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Minute):
					}
					prefixes, err := fetchPrefixesWithRetry(ctx, cfg.LocalAS, 3)
					if err != nil {
						log.Printf("alert engine still held back: %v", err)
						continue
					}
					c.Enricher().SetLocalAS(cfg.LocalAS, prefixes)
					strs := make([]string, 0, len(prefixes))
					for _, p := range prefixes {
						strs = append(strs, p.String())
					}
					log.Printf("alert engine starting: resolved %d prefixes for AS%d", len(strs), cfg.LocalAS)
					startEngine(strs)
					return
				}
			}()
		}
	}

	// Start scheduled-report goroutine if enabled. Renders HTML+CSV summaries and
	// delivers them via SMTP on the schedule stored in report_schedules.
	if cfg.FeatureReports {
		log.Printf("FEATURE_REPORTS=true — starting report scheduler (SMTP %s:%d)", cfg.SMTP.Host, cfg.SMTP.Port)
		gen, err := reports.NewGenerator(chStore)
		if err != nil {
			log.Printf("warning: could not init report generator: %v", err)
		} else {
			svc := reports.NewService(chStore, gen, reports.NewSender(cfg.SMTP))
			go svc.Run(ctx)
		}
	}

	if err := c.Run(ctx); err != nil {
		log.Fatalf("collector error: %v", err)
	}

	log.Println("Collector stopped")
}

// fetchPrefixesWithRetry fetches the announced prefixes for asn, retrying a few
// times with linear backoff. RIPEstat is an external dependency and a single
// failure at boot must not silently leave the alert engine unscoped.
func fetchPrefixesWithRetry(ctx context.Context, asn uint32, attempts int) ([]net.IPNet, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(i) * 5 * time.Second):
			}
		}
		prefixes, err := ripestat.FetchASPrefixes(asn)
		if err == nil && len(prefixes) > 0 {
			return prefixes, nil
		}
		if err == nil {
			lastErr = fmt.Errorf("RIPEstat returned no prefixes for AS%d", asn)
		} else {
			lastErr = err
		}
		log.Printf("ripestat: attempt %d/%d for AS%d failed: %v", i+1, attempts, asn, lastErr)
	}
	return nil, lastErr
}
