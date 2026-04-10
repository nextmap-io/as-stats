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

	// Flow log retention (only applied when the table exists; default 180 days)
	FlowLogRetentionDays int

	// Alert engine
	FeatureAlerts       bool          // enables the alert evaluator goroutine
	AlertEvalInterval   time.Duration // default 30s
	AlertStaleThreshold time.Duration // alerts are auto-resolved after this gap

	// BGP: the collector's alert engine can trigger blocks via the API server.
	// Set BGP_API_URL to the API server's base URL (e.g. "http://localhost:8080")
	// to enable RemoteBlocker. When empty, the collector uses NoopBlocker.
	BGPAPIURL string

	// Prometheus metrics endpoint served on a separate HTTP listener.
	// Defaults to ":9090". Empty string disables the endpoint.
	PrometheusAddr      string
	PrometheusAllowCIDR []string // PROMETHEUS_ALLOW_CIDR, comma-separated
	PrometheusUser      string   // PROMETHEUS_USER (basic auth)
	PrometheusPass      string   // PROMETHEUS_PASS (basic auth)
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

	// Prometheus: /metrics access control for the API server (served on
	// the same port as the API itself).
	PrometheusAllowCIDR []string // PROMETHEUS_ALLOW_CIDR, comma-separated
	PrometheusUser      string   // PROMETHEUS_USER (basic auth)
	PrometheusPass      string   // PROMETHEUS_PASS (basic auth)

	// BGP blackhole client — announces /32 routes via shell commands
	// (gobgp CLI, BIRD, FRRouting, ExaBGP, etc.).
	BGPEnabled     bool
	BGPRouterID    string // e.g. "192.0.2.1"
	BGPLocalAS     uint32
	BGPPeerAddress string // e.g. "10.0.0.1"
	BGPPeerAS      uint32
	BGPCommunity   string // e.g. "65535:666" (RFC 7999 BLACKHOLE)
	BGPNextHop     string // next-hop for announced routes

	// Custom shell command templates. Placeholders: {ip}, {prefix_len},
	// {community}, {next_hop}, {peer_address}. When empty and BGP_ENABLED=true,
	// defaults to gobgp CLI commands.
	BGPAnnounceCmd string // BGP_ANNOUNCE_CMD
	BGPWithdrawCmd string // BGP_WITHDRAW_CMD
	BGPStatusCmd   string // BGP_STATUS_CMD
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

	flowLogRetention, err := strconv.Atoi(envOr("FLOW_LOG_RETENTION_DAYS", "180"))
	if err != nil || flowLogRetention < 1 {
		return nil, fmt.Errorf("invalid FLOW_LOG_RETENTION_DAYS: %w", err)
	}

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
		ClickHouse:           loadClickHouse(),
		ListenNetFlow:        envOr("COLLECTOR_LISTEN_NETFLOW", ":2055"),
		ListenSFlow:          envOr("COLLECTOR_LISTEN_SFLOW", ":6343"),
		BatchSize:            batchSize,
		FlushInterval:        flushInterval,
		Workers:              workers,
		LocalAS:              uint32(localAS),
		FlowLogRetentionDays: flowLogRetention,
		FeatureAlerts:        featureAlerts,
		AlertEvalInterval:    alertEval,
		AlertStaleThreshold:  alertStale,
		BGPAPIURL:            envOr("BGP_API_URL", ""),
		PrometheusAddr:       envOr("COLLECTOR_PROMETHEUS_ADDR", ":9090"),
		PrometheusAllowCIDR:  splitCSV(envOr("PROMETHEUS_ALLOW_CIDR", "")),
		PrometheusUser:       envOr("PROMETHEUS_USER", ""),
		PrometheusPass:       envOr("PROMETHEUS_PASS", ""),
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

	bgpEnabled, _ := strconv.ParseBool(envOr("BGP_ENABLED", "false"))
	bgpLocalAS, _ := strconv.ParseUint(envOr("BGP_LOCAL_AS", "0"), 10, 32)
	bgpPeerAS, _ := strconv.ParseUint(envOr("BGP_PEER_AS", "0"), 10, 32)

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
		FeatureFlowSearch:   featureFlowSearch,
		FeaturePortStats:    featurePortStats,
		FeatureAlerts:       featureAlerts,
		PrometheusAllowCIDR: splitCSV(envOr("PROMETHEUS_ALLOW_CIDR", "")),
		PrometheusUser:      envOr("PROMETHEUS_USER", ""),
		PrometheusPass:      envOr("PROMETHEUS_PASS", ""),
		BGPEnabled:          bgpEnabled,
		BGPRouterID:         envOr("BGP_ROUTER_ID", ""),
		BGPLocalAS:          uint32(bgpLocalAS),
		BGPPeerAddress:      envOr("BGP_PEER_ADDRESS", ""),
		BGPPeerAS:           uint32(bgpPeerAS),
		BGPCommunity:        envOr("BGP_COMMUNITY", "65535:666"),
		BGPNextHop:          envOr("BGP_NEXT_HOP", ""),
		BGPAnnounceCmd:      envOr("BGP_ANNOUNCE_CMD", ""),
		BGPWithdrawCmd:      envOr("BGP_WITHDRAW_CMD", ""),
		BGPStatusCmd:        envOr("BGP_STATUS_CMD", ""),
	}

	// Validate OIDC config when auth is enabled
	if cfg.AuthEnabled {
		if cfg.OIDCIssuer == "" || cfg.OIDCClientID == "" {
			return nil, fmt.Errorf("AUTH_ENABLED=true requires OIDC_ISSUER_URL and OIDC_CLIENT_ID")
		}
	}

	// Validate BGP config when enabled
	if cfg.BGPEnabled {
		if cfg.BGPRouterID == "" || cfg.BGPPeerAddress == "" || cfg.BGPNextHop == "" {
			return nil, fmt.Errorf("BGP_ENABLED=true requires BGP_ROUTER_ID, BGP_PEER_ADDRESS, and BGP_NEXT_HOP")
		}
		if cfg.BGPLocalAS == 0 || cfg.BGPPeerAS == 0 {
			return nil, fmt.Errorf("BGP_ENABLED=true requires BGP_LOCAL_AS and BGP_PEER_AS")
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

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
