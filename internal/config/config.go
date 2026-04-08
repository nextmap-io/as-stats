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

	// Alert engine
	FeatureAlerts       bool          // enables the alert evaluator goroutine
	AlertEvalInterval   time.Duration // default 30s
	AlertStaleThreshold time.Duration // alerts are auto-resolved after this gap
}

// APIConfig holds API server settings.
type APIConfig struct {
	ClickHouse   ClickHouseConfig
	ListenAddr   string
	CORSOrigins  []string
	LocalAS      uint32
	AuthEnabled  bool
	OIDCIssuer   string
	OIDCClientID string
	OIDCSecret   string
	OIDCRedirect string
	OIDCScopes   []string

	// Feature flags — control UI/API exposure of optional features
	FeatureFlowSearch bool // /flows/search, detailed forensic log
	FeaturePortStats  bool // /top/protocol, /top/port aggregates
	FeatureAlerts     bool // alert engine + /alerts dashboard
}

// CollectorConfig additions for detailed logging + alert engine.
// (fields added inline on CollectorConfig above via a separate block to keep diffs small)

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

	featureAlerts, _ := strconv.ParseBool(envOr("FEATURE_ALERTS", "false"))
	alertEval, err := time.ParseDuration(envOr("ALERT_EVAL_INTERVAL", "30s"))
	if err != nil {
		return nil, fmt.Errorf("invalid ALERT_EVAL_INTERVAL: %w", err)
	}
	alertStale, err := time.ParseDuration(envOr("ALERT_STALE_THRESHOLD", "5m"))
	if err != nil {
		return nil, fmt.Errorf("invalid ALERT_STALE_THRESHOLD: %w", err)
	}

	return &CollectorConfig{
		ClickHouse:          loadClickHouse(),
		ListenNetFlow:       envOr("COLLECTOR_LISTEN_NETFLOW", ":2055"),
		ListenSFlow:         envOr("COLLECTOR_LISTEN_SFLOW", ":6343"),
		BatchSize:           batchSize,
		FlushInterval:       flushInterval,
		Workers:             workers,
		LocalAS:             uint32(localAS),
		FeatureAlerts:       featureAlerts,
		AlertEvalInterval:   alertEval,
		AlertStaleThreshold: alertStale,
	}, nil
}

// LoadAPI loads API server configuration from environment variables.
func LoadAPI() (*APIConfig, error) {
	origins := strings.Split(envOr("API_CORS_ORIGINS", "http://localhost:5173"), ",")
	scopes := strings.Split(envOr("OIDC_SCOPES", "openid profile email"), " ")
	authEnabled, _ := strconv.ParseBool(envOr("AUTH_ENABLED", "false"))
	apiLocalAS, _ := strconv.ParseUint(envOr("LOCAL_AS", "0"), 10, 32)

	featureFlowSearch, _ := strconv.ParseBool(envOr("FEATURE_FLOW_SEARCH", "false"))
	featurePortStats, _ := strconv.ParseBool(envOr("FEATURE_PORT_STATS", "false"))
	featureAlerts, _ := strconv.ParseBool(envOr("FEATURE_ALERTS", "false"))

	cfg := &APIConfig{
		ClickHouse:        loadClickHouse(),
		ListenAddr:        envOr("API_LISTEN_ADDR", ":8080"),
		CORSOrigins:       origins,
		LocalAS:           uint32(apiLocalAS),
		AuthEnabled:       authEnabled,
		OIDCIssuer:        envOr("OIDC_ISSUER_URL", ""),
		OIDCClientID:      envOr("OIDC_CLIENT_ID", ""),
		OIDCSecret:        envOr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirect:      envOr("OIDC_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		OIDCScopes:        scopes,
		FeatureFlowSearch: featureFlowSearch,
		FeaturePortStats:  featurePortStats,
		FeatureAlerts:     featureAlerts,
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
