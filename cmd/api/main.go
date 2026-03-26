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

	router := api.NewRouter(chStore, cfg)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
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
