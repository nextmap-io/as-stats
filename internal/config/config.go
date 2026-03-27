package config

import (
	"fmt"
	"log"
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
	LocalAS       uint32
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
	OIDCScopes []string
}

func loadClickHouse() ClickHouseConfig {
	cfg := ClickHouseConfig{
		Addr:     envOr("CLICKHOUSE_ADDR", "localhost:9000"),
		Database: envOr("CLICKHOUSE_DATABASE", "asstats"),
		User:     envOr("CLICKHOUSE_USER", ""),
		Password: envOr("CLICKHOUSE_PASSWORD", ""),
	}

	if cfg.User == "" || cfg.Password == "" {
		log.Println("WARNING: CLICKHOUSE_USER and CLICKHOUSE_PASSWORD not set, using defaults (not safe for production)")
		if cfg.User == "" {
			cfg.User = "asstats"
		}
		if cfg.Password == "" {
			cfg.Password = "asstats"
		}
	}

	return cfg
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

	localAS, _ := strconv.ParseUint(envOr("LOCAL_AS", "0"), 10, 32)

	return &CollectorConfig{
		ClickHouse:    loadClickHouse(),
		ListenNetFlow: envOr("COLLECTOR_LISTEN_NETFLOW", ":2055"),
		ListenSFlow:   envOr("COLLECTOR_LISTEN_SFLOW", ":6343"),
		BatchSize:     batchSize,
		FlushInterval: flushInterval,
		Workers:       workers,
		LocalAS:       uint32(localAS),
	}, nil
}

// LoadAPI loads API server configuration from environment variables.
func LoadAPI() (*APIConfig, error) {
	origins := strings.Split(envOr("API_CORS_ORIGINS", "http://localhost:5173"), ",")
	scopes := strings.Split(envOr("OIDC_SCOPES", "openid profile email"), " ")
	authEnabled, _ := strconv.ParseBool(envOr("AUTH_ENABLED", "false"))

	cfg := &APIConfig{
		ClickHouse:   loadClickHouse(),
		ListenAddr:   envOr("API_LISTEN_ADDR", ":8080"),
		CORSOrigins:  origins,
		AuthEnabled:  authEnabled,
		OIDCIssuer:   envOr("OIDC_ISSUER_URL", ""),
		OIDCClientID: envOr("OIDC_CLIENT_ID", ""),
		OIDCSecret:   envOr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirect: envOr("OIDC_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		OIDCScopes: scopes,
	}

	// Validate OIDC config when auth is enabled
	if cfg.AuthEnabled {
		if cfg.OIDCIssuer == "" || cfg.OIDCClientID == "" {
			return nil, fmt.Errorf("AUTH_ENABLED=true requires OIDC_ISSUER_URL and OIDC_CLIENT_ID")
		}
	}

	// Warn about wildcard CORS with credentials
	for _, origin := range cfg.CORSOrigins {
		if strings.TrimSpace(origin) == "*" {
			log.Println("WARNING: CORS origin '*' is not safe with AllowCredentials=true")
		}
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
