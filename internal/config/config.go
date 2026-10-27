package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/bgp"
)

// ClickHouseConfig holds ClickHouse connection settings.
type ClickHouseConfig struct {
	Addr     string
	Database string
	User     string
	Password string
}

// SMTPConfig holds outbound email settings for scheduled reports (Module D).
// Shared by the collector (which sends on schedule) and the API server (which
// exposes the "send test report now" endpoint).
type SMTPConfig struct {
	Host     string // SMTP server host (required when FEATURE_REPORTS=true)
	Port     int    // SMTP server port (default 587)
	From     string // envelope + header From address (required when FEATURE_REPORTS=true)
	User     string // SMTP AUTH username (empty = no auth)
	Password string // SMTP AUTH password
	STARTTLS bool   // upgrade the connection with STARTTLS (default true)
}

// loadSMTP loads SMTP settings from the environment. When featureReports is
// true, SMTP_HOST and SMTP_FROM are mandatory.
func loadSMTP(featureReports bool) (SMTPConfig, error) {
	port, err := strconv.Atoi(envOr("SMTP_PORT", "587"))
	if err != nil || port < 1 || port > 65535 {
		return SMTPConfig{}, fmt.Errorf("invalid SMTP_PORT: %q", envOr("SMTP_PORT", "587"))
	}
	starttls, _ := strconv.ParseBool(envOr("SMTP_STARTTLS", "true"))
	cfg := SMTPConfig{
		Host:     envOr("SMTP_HOST", ""),
		Port:     port,
		From:     envOr("SMTP_FROM", ""),
		User:     envOr("SMTP_USER", ""),
		Password: envOr("SMTP_PASS", ""),
		STARTTLS: starttls,
	}
	if featureReports && (cfg.Host == "" || cfg.From == "") {
		return SMTPConfig{}, fmt.Errorf("FEATURE_REPORTS=true requires SMTP_HOST and SMTP_FROM")
	}
	return cfg, nil
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

	// ShutdownTimeout bounds the drain on SIGTERM (decoders, enricher, final
	// flush). Keep it below the container's stop grace period, or the runtime
	// SIGKILLs the process mid-flush.
	ShutdownTimeout time.Duration

	// FlushTimeout bounds one batch INSERT. A batch that fails or times out is
	// dropped (and counted), never retried — see writer.write.
	FlushTimeout time.Duration

	// SamplingOverride maps router IP → 1-in-N sampling rate, applied only to
	// flows whose sampling rate is known neither from the record itself nor
	// from the exporter's options data (COLLECTOR_SAMPLING_OVERRIDE).
	SamplingOverride map[string]uint32

	// Flow log retention (only applied when the table exists; default 180 days)
	FlowLogRetentionDays int

	// Retention management
	RetentionReconcileInterval time.Duration // how often to apply desired TTLs (default 15m)
	ConfigPurgeDays            int           // age threshold for purging soft-deleted config rows (default 30)

	// Alert engine
	FeatureAlerts       bool          // enables the alert evaluator goroutine
	AlertEvalInterval   time.Duration // default 30s
	AlertStaleThreshold time.Duration // alerts are auto-resolved after this gap

	// Scheduled reports (Module D)
	FeatureReports bool       // enables the report scheduler goroutine
	SMTP           SMTPConfig // outbound email settings

	// BGP: the collector's alert engine asks the API to blackhole the target
	// of an auto_block rule. BGP_API_URL is the API's base URL (e.g.
	// "http://api:8080"), BGP_API_TOKEN the secret shared with the API. Both
	// are needed; without them auto_block rules only alert.
	BGPAPIURL   string
	BGPAPIToken string

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
	FeatureReports    bool // scheduled reports admin + /admin/reports CRUD

	// SMTP settings for the "send test report now" endpoint.
	SMTP SMTPConfig

	// Prometheus: /metrics access control for the API server (served on
	// the same port as the API itself).
	PrometheusAllowCIDR []string // PROMETHEUS_ALLOW_CIDR, comma-separated
	PrometheusUser      string   // PROMETHEUS_USER (basic auth)
	PrometheusPass      string   // PROMETHEUS_PASS (basic auth)

	// BGP blackholing (docs/bgp-blackhole.md). BGP holds the validated
	// BGP_* settings when BGPEnabled; BGPAPIToken authenticates the
	// collector's auto-block requests on /internal/bgp/blocks.
	BGPEnabled  bool
	BGP         bgp.Settings
	BGPAPIToken string
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
	// Flushing is meant to be time-driven: every INSERT into flows_raw fans out
	// to ~20 materialised views and creates ~21 parts, so the part count — and
	// the merge I/O that follows — scales with the number of INSERTs, not with
	// rows. Measured on production traffic, 30s instead of 5s cut new parts by
	// 83% and merge I/O by 74%. 15s keeps most of that gain while the 1-min hot
	// tables used by the alert engine stay fresh. The batch size is a ceiling
	// for bursts, sized well above interval × normal flow rate.
	batchSize, err := strconv.Atoi(envOr("COLLECTOR_BATCH_SIZE", "50000"))
	if err != nil || batchSize <= 0 {
		return nil, fmt.Errorf("invalid COLLECTOR_BATCH_SIZE: %q", envOr("COLLECTOR_BATCH_SIZE", "50000"))
	}
	flushInterval, err := time.ParseDuration(envOr("COLLECTOR_FLUSH_INTERVAL", "15s"))
	if err != nil || flushInterval <= 0 {
		return nil, fmt.Errorf("invalid COLLECTOR_FLUSH_INTERVAL: %q", envOr("COLLECTOR_FLUSH_INTERVAL", "15s"))
	}
	workers, err := strconv.Atoi(envOr("COLLECTOR_WORKERS", "4"))
	if err != nil {
		return nil, fmt.Errorf("invalid COLLECTOR_WORKERS: %w", err)
	}

	shutdownTimeout, err := time.ParseDuration(envOr("COLLECTOR_SHUTDOWN_TIMEOUT", "40s"))
	if err != nil || shutdownTimeout <= 0 {
		return nil, fmt.Errorf("invalid COLLECTOR_SHUTDOWN_TIMEOUT: %q", envOr("COLLECTOR_SHUTDOWN_TIMEOUT", "40s"))
	}

	flushTimeout, err := time.ParseDuration(envOr("COLLECTOR_FLUSH_TIMEOUT", "30s"))
	if err != nil || flushTimeout <= 0 {
		return nil, fmt.Errorf("invalid COLLECTOR_FLUSH_TIMEOUT: %q", envOr("COLLECTOR_FLUSH_TIMEOUT", "30s"))
	}

	samplingOverride, err := parseSamplingOverride(envOr("COLLECTOR_SAMPLING_OVERRIDE", ""))
	if err != nil {
		return nil, err
	}

	localAS, _ := strconv.ParseUint(envOr("LOCAL_AS", "0"), 10, 32)

	flowLogRetention, err := strconv.Atoi(envOr("FLOW_LOG_RETENTION_DAYS", "180"))
	if err != nil || flowLogRetention < 1 {
		return nil, fmt.Errorf("invalid FLOW_LOG_RETENTION_DAYS: %w", err)
	}

	reconcileInterval, err := time.ParseDuration(envOr("RETENTION_RECONCILE_INTERVAL", "15m"))
	if err != nil || reconcileInterval <= 0 {
		return nil, fmt.Errorf("invalid RETENTION_RECONCILE_INTERVAL: %w", err)
	}
	configPurgeDays, err := strconv.Atoi(envOr("CONFIG_PURGE_DAYS", "30"))
	if err != nil || configPurgeDays < 1 {
		return nil, fmt.Errorf("invalid CONFIG_PURGE_DAYS: %w", err)
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

	featureReports, _ := strconv.ParseBool(envOr("FEATURE_REPORTS", "false"))
	smtp, err := loadSMTP(featureReports)
	if err != nil {
		return nil, err
	}

	return &CollectorConfig{
		ClickHouse:                 loadClickHouse(),
		ListenNetFlow:              envOr("COLLECTOR_LISTEN_NETFLOW", ":2055"),
		ListenSFlow:                envOr("COLLECTOR_LISTEN_SFLOW", ":6343"),
		BatchSize:                  batchSize,
		FlushInterval:              flushInterval,
		Workers:                    workers,
		ShutdownTimeout:            shutdownTimeout,
		FlushTimeout:               flushTimeout,
		SamplingOverride:           samplingOverride,
		LocalAS:                    uint32(localAS),
		FlowLogRetentionDays:       flowLogRetention,
		RetentionReconcileInterval: reconcileInterval,
		ConfigPurgeDays:            configPurgeDays,
		FeatureAlerts:              featureAlerts,
		AlertEvalInterval:          alertEval,
		AlertStaleThreshold:        alertStale,
		FeatureReports:             featureReports,
		SMTP:                       smtp,
		BGPAPIURL:                  envOr("BGP_API_URL", ""),
		BGPAPIToken:                os.Getenv("BGP_API_TOKEN"),
		PrometheusAddr:             envOr("COLLECTOR_PROMETHEUS_ADDR", ":9090"),
		PrometheusAllowCIDR:        splitCSV(envOr("PROMETHEUS_ALLOW_CIDR", "")),
		PrometheusUser:             envOr("PROMETHEUS_USER", ""),
		PrometheusPass:             envOr("PROMETHEUS_PASS", ""),
	}, nil
}

// LoadAPI loads API server configuration from environment variables.
func LoadAPI() (*APIConfig, error) {
	origins := strings.Split(envOr("API_CORS_ORIGINS", "http://localhost:5173"), ",")
	scopes := strings.Split(envOr("OIDC_SCOPES", "openid profile email"), " ")
	authEnabled, err := envBool("AUTH_ENABLED", true)
	if err != nil {
		return nil, err
	}
	allowUnauthenticated, err := envBool("ALLOW_UNAUTHENTICATED", false)
	if err != nil {
		return nil, err
	}
	if !authEnabled && !allowUnauthenticated {
		return nil, fmt.Errorf("AUTH_ENABLED=false requires ALLOW_UNAUTHENTICATED=true")
	}
	apiLocalAS, _ := strconv.ParseUint(envOr("LOCAL_AS", "0"), 10, 32)

	featureFlowSearch, _ := strconv.ParseBool(envOr("FEATURE_FLOW_SEARCH", "false"))
	featurePortStats, _ := strconv.ParseBool(envOr("FEATURE_PORT_STATS", "false"))
	featureAlerts, _ := strconv.ParseBool(envOr("FEATURE_ALERTS", "false"))
	featureReports, _ := strconv.ParseBool(envOr("FEATURE_REPORTS", "false"))
	smtp, err := loadSMTP(featureReports)
	if err != nil {
		return nil, err
	}

	bgpEnabled, err := envBool("BGP_ENABLED", false)
	if err != nil {
		return nil, err
	}
	var bgpSettings bgp.Settings
	if bgpEnabled {
		if bgpSettings, err = loadBGP(); err != nil {
			return nil, err
		}
	}
	bgpToken := os.Getenv("BGP_API_TOKEN")
	if bgpToken != "" && len(bgpToken) < bgp.MinAPITokenLen {
		return nil, fmt.Errorf("BGP_API_TOKEN must be at least %d characters (e.g. openssl rand -hex 32)", bgp.MinAPITokenLen)
	}

	cfg := &APIConfig{
		ClickHouse:          loadClickHouse(),
		ListenAddr:          envOr("API_LISTEN_ADDR", ":8080"),
		CORSOrigins:         origins,
		LocalAS:             uint32(apiLocalAS),
		AuthEnabled:         authEnabled,
		OIDCIssuer:          envOr("OIDC_ISSUER_URL", ""),
		OIDCClientID:        envOr("OIDC_CLIENT_ID", ""),
		OIDCSecret:          envOr("OIDC_CLIENT_SECRET", ""),
		OIDCRedirect:        envOr("OIDC_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		OIDCScopes:          scopes,
		FeatureFlowSearch:   featureFlowSearch,
		FeaturePortStats:    featurePortStats,
		FeatureAlerts:       featureAlerts,
		FeatureReports:      featureReports,
		SMTP:                smtp,
		PrometheusAllowCIDR: splitCSV(envOr("PROMETHEUS_ALLOW_CIDR", "")),
		PrometheusUser:      envOr("PROMETHEUS_USER", ""),
		PrometheusPass:      envOr("PROMETHEUS_PASS", ""),
		BGPEnabled:          bgpEnabled,
		BGP:                 bgpSettings,
		BGPAPIToken:         bgpToken,
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

// loadBGP reads and validates the BGP_* variables (BGP_ENABLED=true).
func loadBGP() (bgp.Settings, error) {
	u32 := func(key string) (uint32, error) {
		v, err := strconv.ParseUint(envOr(key, "0"), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %q", key, os.Getenv(key))
		}
		return uint32(v), nil
	}
	num := func(key string, def int) (int, error) {
		raw := os.Getenv(key)
		if raw == "" {
			return def, nil
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %q", key, raw)
		}
		return v, nil
	}
	dur := func(key string) (time.Duration, error) {
		raw := os.Getenv(key)
		if raw == "" {
			return 0, nil
		}
		v, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %q", key, raw)
		}
		return v, nil
	}
	var r bgp.RawSettings
	var err error
	if r.LocalAS, err = u32("BGP_LOCAL_AS"); err != nil {
		return bgp.Settings{}, err
	}
	if r.PeerAS, err = u32("BGP_PEER_AS"); err != nil {
		return bgp.Settings{}, err
	}
	if r.DryRun, err = envBool("BGP_DRY_RUN", false); err != nil {
		return bgp.Settings{}, err
	}
	ints := []struct {
		key string
		def int
		dst *int
	}{
		{"BGP_MIN_PREFIX_LEN_V4", 32, &r.MinPrefixLenV4},
		{"BGP_MIN_PREFIX_LEN_V6", 128, &r.MinPrefixLenV6},
		{"BGP_MAX_ACTIVE_BLOCKS", 50, &r.MaxActive},
		{"BGP_AUTO_BLOCK_MAX_PER_HOUR", 10, &r.AutoPerHour},
	}
	for _, i := range ints {
		if *i.dst, err = num(i.key, i.def); err != nil {
			return bgp.Settings{}, err
		}
	}
	durs := []struct {
		key string
		dst *time.Duration
	}{
		{"BGP_DEFAULT_DURATION", &r.DefaultDuration},
		{"BGP_MAX_DURATION", &r.MaxDuration},
		{"BGP_COMMAND_TIMEOUT", &r.CommandTimeout},
		{"BGP_RECONCILE_INTERVAL", &r.ReconcileInterval},
	}
	for _, d := range durs {
		if *d.dst, err = dur(d.key); err != nil {
			return bgp.Settings{}, err
		}
	}
	_, r.StatusCmdSet = os.LookupEnv("BGP_STATUS_CMD")
	_, r.RoutesCmdSet = os.LookupEnv("BGP_ROUTES_CMD")
	extra, extraSet := os.LookupEnv("BGP_EXTRA_COMMUNITIES")
	if !extraSet {
		extra = "65535:65281" // NO_EXPORT, RFC 7999 section 3.2
	}
	r.AnnounceCmd = os.Getenv("BGP_ANNOUNCE_CMD")
	r.WithdrawCmd = os.Getenv("BGP_WITHDRAW_CMD")
	r.StatusCmd = os.Getenv("BGP_STATUS_CMD")
	r.RoutesCmd = os.Getenv("BGP_ROUTES_CMD")
	r.Community = envOr("BGP_COMMUNITY", "65535:666")
	r.ExtraCommunities = extra
	r.NextHop = os.Getenv("BGP_NEXT_HOP")
	r.NextHopV6 = os.Getenv("BGP_NEXT_HOP_V6")
	r.PeerAddress = os.Getenv("BGP_PEER_ADDRESS")
	r.RouterID = os.Getenv("BGP_ROUTER_ID")
	r.AllowedPrefixes = os.Getenv("BGP_ALLOWED_PREFIXES")
	r.ProtectedPrefixes = os.Getenv("BGP_PROTECTED_PREFIXES")
	return bgp.ParseSettings(r)
}

// parseSamplingOverride parses "ip=rate,ip=rate" (e.g.
// "2001:db8::1=1000,192.0.2.1=512"). A wrong rate silently scales a router's
// traffic, so any malformed entry fails startup instead of being skipped.
func parseSamplingOverride(s string) (map[string]uint32, error) {
	out := make(map[string]uint32)
	for _, entry := range splitCSV(s) {
		ipStr, rateStr, ok := strings.Cut(entry, "=")
		ip := net.ParseIP(strings.TrimSpace(ipStr))
		rate, err := strconv.ParseUint(strings.TrimSpace(rateStr), 10, 32)
		if !ok || ip == nil || err != nil || rate == 0 {
			return nil, fmt.Errorf("invalid COLLECTOR_SAMPLING_OVERRIDE entry %q: want <router-ip>=<rate>, rate >= 1", entry)
		}
		out[ip.String()] = uint32(rate)
	}
	return out, nil
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

// envBool parses an environment boolean without silently accepting typos.
// Security-sensitive switches must fail closed when their value is invalid.
func envBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %q", key, raw)
	}
	return v, nil
}
