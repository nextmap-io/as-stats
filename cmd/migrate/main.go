package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/migrate"
	schema "github.com/nextmap-io/as-stats/migrations"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	database := envOr("CLICKHOUSE_DATABASE", "asstats")
	autoBaseline, err := envBool("MIGRATE_AUTO_BASELINE", true)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	timeout, err := time.ParseDuration(envOr("MIGRATE_TIMEOUT", "30m"))
	if err != nil || timeout <= 0 {
		log.Fatalf("invalid MIGRATE_TIMEOUT %q", envOr("MIGRATE_TIMEOUT", "30m"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{envOr("CLICKHOUSE_ADDR", "localhost:9000")},
		Auth: clickhouse.Auth{
			Database: database,
			Username: envOr("CLICKHOUSE_USER", "asstats"),
			Password: envOr("CLICKHOUSE_PASSWORD", "asstats"),
		},
		DialTimeout:  10 * time.Second,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		log.Fatalf("open ClickHouse: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Printf("close ClickHouse: %v", err)
		}
	}()
	if err := conn.Ping(ctx); err != nil {
		log.Fatalf("ping ClickHouse: %v", err)
	}

	migrations, err := migrate.Load(schema.Files, ".")
	if err != nil {
		log.Fatalf("load migrations: %v", err)
	}
	store, err := migrate.NewClickHouseStore(conn, database)
	if err != nil {
		log.Fatalf("initialize migration store: %v", err)
	}
	hostname, _ := os.Hostname()
	owner := fmt.Sprintf("%s:%d", hostname, os.Getpid())
	runner := migrate.Runner{Store: store, Migrations: migrations, Owner: owner, AutoBaseline: autoBaseline}
	log.Printf("migrating ClickHouse database %s (%d embedded migrations)", database, len(migrations))
	if err := runner.Run(ctx); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Printf("schema is current")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q", key, raw)
	}
	return value, nil
}
