package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nextmap-io/as-stats/internal/api"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/ripestat"
	"github.com/nextmap-io/as-stats/internal/store"
)

func main() {
	cfg, err := config.LoadAPI()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("AS-Stats API server starting")
	log.Printf("  Listen:      %s", cfg.ListenAddr)
	log.Printf("  ClickHouse:  %s/%s", cfg.ClickHouse.Addr, cfg.ClickHouse.Database)
	log.Printf("  Auth:        %v", cfg.AuthEnabled)

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

	// Load local IP filter for Top IPs queries
	var localIPFilter string
	var localPrefixStrs []string
	if cfg.LocalAS > 0 {
		log.Printf("LOCAL_AS=%d — fetching prefixes for IP filtering", cfg.LocalAS)
		if prefixes, err := ripestat.FetchASPrefixes(cfg.LocalAS); err != nil {
			log.Printf("warning: could not fetch prefixes for AS%d: %v", cfg.LocalAS, err)
		} else {
			localIPFilter = ripestat.PrefixesToSQL("ip_address", prefixes)
			for _, p := range prefixes {
				localPrefixStrs = append(localPrefixStrs, p.String())
			}
			log.Printf("Loaded %d local prefixes for IP filtering", len(prefixes))
		}
	}

	router := api.NewRouter(chStore, cfg, localIPFilter, localPrefixStrs)

	srv := &http.Server{
		Addr:           cfg.ListenAddr,
		Handler:        router,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	go func() {
		log.Printf("API server listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("Shutting down API server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
