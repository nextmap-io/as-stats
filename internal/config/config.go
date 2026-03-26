package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ClickHouseConfig holds ClickHouse connection settings.
type ClickHouseConfig struct {
	Addr     string
	Database string
	User     string
	Password string
}

// CollectorConfig holds flow collector settings.
type CollectorConfig struct {
	ClickHouse    ClickHouseConfig
	ListenNetFlow string
	ListenSFlow   string
	BatchSize     int
	FlushInterval time.Duration
	Workers       int
}

// APIConfig holds API server settings.
type APIConfig struct {
	ClickHouse   ClickHouseConfig
	ListenAddr   string
	CORSOrigins  []string
	AuthEnabled  bool
	OIDCIssuer   string
	OIDCClientID string
	OIDCSecret   string
	OIDCRedirect string
	OIDCScopes   []string
	RedisURL     string
}

func loadClickHouse() ClickHouseConfig {
	return ClickHouseConfig{
		Addr:     envOr("CLICKHOUSE_ADDR", "localhost:9000"),
		Database: envOr("CLICKHOUSE_DATABASE", "asstats"),
		User:     envOr("CLICKHOUSE_USER", "asstats"),
		Password: envOr("CLICKHOUSE_PASSWORD", "asstats"),
	}
}

// LoadCollector loads collector configuration from environment variables.
func LoadCollector() (*CollectorConfig, error) {
	batchSize, err := strconv.Atoi(envOr("COLLECTOR_BATCH_SIZE", "10000"))
	if err != nil {
		return nil, fmt.Errorf("invalid COLLECTOR_BATCH_SIZE: %w", err)
	}
	flushInterval, err := time.ParseDuration(envOr("COLLECTOR_FLUSH_INTERVAL", "5s"))
	if err != nil {
		return nil, fmt.Errorf("invalid COLLECTOR_FLUSH_INTERVAL: %w", err)
	}
	workers, err := strconv.Atoi(envOr("COLLECTOR_WORKERS", "4"))
	if err != nil {
		return nil, fmt.Errorf("invalid COLLECTOR_WORKERS: %w", err)
	}

	return &CollectorConfig{
		ClickHouse:    loadClickHouse(),
		ListenNetFlow: envOr("COLLECTOR_LISTEN_NETFLOW", ":2055"),
		ListenSFlow:   envOr("COLLECTOR_LISTEN_SFLOW", ":6343"),
		BatchSize:     batchSize,
		FlushInterval: flushInterval,
		Workers:       workers,
	}, nil
}

// LoadAPI loads API server configuration from environment variables.
func LoadAPI() (*APIConfig, error) {
	origins := strings.Split(envOr("API_CORS_ORIGINS", "http://localhost:5173"), ",")
	scopes := strings.Split(envOr("OIDC_SCOPES", "openid profile email"), " ")
	authEnabled, _ := strconv.ParseBool(envOr("AUTH_ENABLED", "false"))

	return &APIConfig{
		ClickHouse:   loadClickHouse(),
		ListenAddr:   envOr("API_LISTEN_ADDR", ":8080"),
		CORSOrigins:  origins,
		AuthEnabled:  authEnabled,
		OIDCIssuer:   envOr("OIDC_ISSUER_URL", ""),
		OIDCClientID: envOr("OIDC_CLIENT_ID", ""),
		OIDCSecret:   envOr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirect: envOr("OIDC_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		OIDCScopes:   scopes,
		RedisURL:     envOr("REDIS_URL", ""),
	}, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
