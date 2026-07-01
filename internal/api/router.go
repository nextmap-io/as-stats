package api

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/nextmap-io/as-stats/internal/api/handler"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/reports"
	"github.com/nextmap-io/as-stats/internal/store"
)

// NewRouter creates the API router with all endpoints.
func NewRouter(s *store.ClickHouseStore, cfg *config.APIConfig, localIPFilter string, localPrefixes []string) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	// NOTE: chimw.RealIP is intentionally NOT used. It was deprecated in
	// chi v5.3.0 because it rewrites r.RemoteAddr from X-Forwarded-For /
	// X-Real-IP / True-Client-IP unconditionally, which is spoofable
	// (GHSA-3fxj-6jh8-hvhx). The two places that need the client IP
	// (middleware.RateLimit and the /metrics CIDR guard) parse the
	// forwarded headers themselves, so RealIP was redundant here anyway.
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(120 * time.Second))

	// Security headers
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			next.ServeHTTP(w, r)
		})
	})

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	h := handler.New(s)
	h.LocalIPFilter = localIPFilter
	h.LocalPrefixes = localPrefixes
	h.LocalAS = cfg.LocalAS
	h.FeatureFlowSearch = cfg.FeatureFlowSearch
	h.FeaturePortStats = cfg.FeaturePortStats
	h.FeatureAlerts = cfg.FeatureAlerts
	h.FeatureBGP = cfg.BGPEnabled
	h.FeatureReports = cfg.FeatureReports
	h.AuthEnabled = cfg.AuthEnabled

	// Report delivery service for the "send test report now" endpoint. Requires
	// SMTP config; loadSMTP already enforces SMTP_HOST/SMTP_FROM when the feature
	// is on, so a non-nil generator implies a usable sender.
	if cfg.FeatureReports {
		if gen, err := reports.NewGenerator(s); err != nil {
			log.Printf("WARNING: report generator init failed: %v — test-send disabled", err)
		} else {
			h.ReportService = reports.NewService(s, gen, reports.NewSender(cfg.SMTP))
		}
	}

	// BGP blocker: real ScriptBlocker when BGP_ENABLED=true, noop otherwise.
	// The ScriptBlocker is initialized here (not in main.go) because the
	// handler needs the blocker reference and the router owns the lifecycle.
	if cfg.BGPEnabled {
		scriptCfg := bgp.Config{
			AnnounceCmd: cfg.BGPAnnounceCmd,
			WithdrawCmd: cfg.BGPWithdrawCmd,
			StatusCmd:   cfg.BGPStatusCmd,
			Community:   cfg.BGPCommunity,
			NextHop:     cfg.BGPNextHop,
			PeerAddress: cfg.BGPPeerAddress,
			PeerAS:      cfg.BGPPeerAS,
			LocalAS:     cfg.BGPLocalAS,
		}
		blocker, err := bgp.NewScript(scriptCfg, s)
		if err != nil {
			// Non-fatal: log and fall back to noop so the rest of the API works
			log.Printf("WARNING: BGP blocker init failed: %v — falling back to noop", err)
			h.BGPBlocker = bgp.NewNoop()
			h.FeatureBGP = false
		} else {
			h.BGPBlocker = blocker
		}
	} else {
		h.BGPBlocker = bgp.NewNoop()
	}
	sessions := middleware.NewSessionStore()
	// Read-only API token authenticator (Module G). The api_tokens table is
	// always created, so this is wired unconditionally; it is only consulted
	// from AuthMiddleware, which is applied only when AUTH_ENABLED=true.
	tokenAuth := middleware.NewAPITokenAuthenticator(s)

	// Prometheus metrics middleware (must be applied before routes so it
	// captures every request including /healthz and /metrics itself)
	r.Use(middleware.Prometheus())

	// Health check (no auth, no rate limit)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Prometheus metrics endpoint — outside /api/v1, optional IP/basic-auth guard
	r.Handle("/metrics", handler.MetricsHandler(
		cfg.PrometheusAllowCIDR,
		cfg.PrometheusUser,
		cfg.PrometheusPass,
	))

	// Auth endpoints (if OIDC enabled)
	if cfg.AuthEnabled {
		authH := handler.NewAuthHandler(cfg, sessions)
		r.Get("/auth/login", authH.Login)
		r.Get("/auth/callback", authH.Callback)
		r.Post("/auth/logout", authH.Logout)
	}

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.RateLimit(100))

		// Apply auth middleware if enabled
		if cfg.AuthEnabled {
			r.Use(middleware.AuthMiddleware(cfg, sessions, tokenAuth))
		}

		// Audit log middleware: records sensitive actions (only if alerts/audit
		// features are available, since audit_log table is part of that migration)
		if cfg.FeatureAlerts || cfg.FeatureFlowSearch {
			r.Use(middleware.Audit(s, middleware.DefaultAuditActions()))
		}

		// Auth info
		authH := handler.NewAuthHandler(cfg, sessions)
		r.Get("/auth/me", authH.Me)

		// Feature discovery (always available, no cache)
		r.Get("/features", h.Features)

		// Cached read routes (30s TTL)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Cache(30 * time.Second))
			r.Get("/overview", h.Overview)
			r.Get("/top/as", h.TopAS)
			r.Get("/top/as/traffic", h.TopASTraffic)
			r.Get("/top/ip", h.TopIP)
			r.Get("/top/prefix", h.TopPrefix)
			r.Get("/top/country", h.TopCountry)
			r.Get("/links", h.Links)
			r.Get("/links/traffic", h.LinksTraffic)
			r.Get("/links/capacity", h.LinksCapacity)

			// Traffic heatmap (U8) — 7×24 day-of-week × hour-of-day grid.
			r.Get("/traffic/heatmap", h.TrafficHeatmap)

			// Comparison — movers / talkers (Module D). Always available;
			// port dimension is gated inside the handler on FEATURE_PORT_STATS.
			r.Get("/changes/movers", h.Movers)
			r.Get("/changes/talkers", h.Talkers)

			// Port stats (gated by FEATURE_PORT_STATS)
			if cfg.FeaturePortStats {
				r.Get("/top/protocol", h.TopProtocols)
				r.Get("/top/port", h.TopPortsHandler)
			}
		})

		// Flow search (gated by FEATURE_FLOW_SEARCH, not cached)
		if cfg.FeatureFlowSearch {
			r.Get("/flows/search", h.FlowSearch)
			r.Get("/flows/timeseries", h.FlowTimeSeries)
			// Conversations explorer (F3) — bidirectional top talkers.
			r.Get("/conversations", h.Conversations)
		}

		// Alerts (gated by FEATURE_ALERTS)
		if cfg.FeatureAlerts {
			r.Get("/alerts", h.ListAlerts)
			r.Get("/alerts/summary", h.AlertsSummary)
			// Live threats — pre-trigger view of top destinations vs. rules
			r.Get("/threats/live", h.LiveThreats)
			// Anomaly explainability — decompose a link's window into top
			// contributing ASes / source IPs / dst ports (Module E).
			r.Get("/anomaly/explain", h.AnomalyExplain)
			// Alert actions require CSRF + optional admin role
			r.Group(func(r chi.Router) {
				r.Use(middleware.CSRF())
				r.Post("/alerts/{id}/ack", h.AcknowledgeAlert)
				r.Post("/alerts/{id}/resolve", h.ResolveAlert)
				// Block action requires admin role
				r.Group(func(r chi.Router) {
					if cfg.AuthEnabled {
						r.Use(middleware.RequireRole("admin"))
					}
					r.Post("/alerts/{id}/block", h.BlockAlertBGP)
				})
			})
		}

		// BGP blackhole management
		if cfg.BGPEnabled {
			r.Get("/bgp/status", h.BGPStatus)
			r.Get("/bgp/blocks", h.ListBGPBlocks)
			r.Get("/bgp/blocks/history", h.ListBGPBlockHistory)
			r.Group(func(r chi.Router) {
				r.Use(middleware.CSRF())
				if cfg.AuthEnabled {
					r.Use(middleware.RequireRole("admin"))
				}
				r.Post("/bgp/blocks", h.CreateBGPBlock)
				r.Delete("/bgp/blocks/{ip}", h.DeleteBGPBlock)
			})
		}

		// AS detail
		r.Get("/as/{asn}", h.ASDetail)
		r.Get("/as/{asn}/peers", h.ASPeers)
		r.Get("/as/{asn}/ips", h.ASTopIPs)

		// IP detail
		r.Get("/ip/{ip}", h.IPDetail)

		// Link detail (not cached — specific to one link)
		r.Get("/link/{tag}", h.LinkDetail)
		r.Get("/link/{tag}/load-curve", h.LinkLoadCurve)

		// Status
		r.Get("/status", h.Status)

		// DNS
		r.Get("/dns/ptr", h.DNSPtr)

		// Search
		r.Get("/search", h.Search)

		// Read-only admin endpoint accessible to all authenticated users
		// (UI shows link config in dropdowns, etc.). Webhook URLs and audit
		// log are admin-only — see the role-gated group below.
		r.Get("/admin/links", h.LinksAdmin)

		// All other /admin endpoints require admin role when auth is enabled.
		// Both reads and writes are gated to prevent enumeration of webhook
		// URLs, alert rules, and audit log entries by non-admin users.
		r.Route("/admin", func(r chi.Router) {
			if cfg.AuthEnabled {
				r.Use(middleware.RequireRole("admin"))
			}
			// CSRF applied at the admin level: GETs set the cookie,
			// POST/PUT/DELETE validate it. Previously CSRF was only on the
			// writes sub-group, so GETs never set the cookie and the first
			// POST from a fresh page load failed with "missing CSRF cookie".
			r.Use(middleware.CSRF())

			// Reads
			// Note: GET /admin/links is registered OUTSIDE this group
			// (line above) because v1.1.1 opened it to all authenticated
			// users, not just admins.

			// Storage & retention observability (core — always available).
			r.Get("/storage", h.StorageStatus)

			// Read-only API tokens (core — always available).
			r.Get("/tokens", h.ListTokens)

			if cfg.FeatureAlerts {
				r.Get("/rules", h.ListRules)
				r.Get("/webhooks", h.ListWebhooks)
				r.Get("/hostgroups", h.ListHostgroups)
				r.Get("/audit", h.ListAuditLog)
			}

			// Scheduled reports (gated by FEATURE_REPORTS).
			if cfg.FeatureReports {
				r.Get("/reports", h.ListReports)
			}

			// Writes
			r.Post("/links", h.LinkCreate)
			r.Delete("/links/{tag}", h.LinkDelete)

			// Retention policy edit (core — always available).
			r.Put("/retention/{table}", h.SetRetention)

			// API token mint / revoke (core — always available).
			r.Post("/tokens", h.CreateToken)
			r.Delete("/tokens/{id}", h.RevokeToken)

			if cfg.FeatureAlerts {
				r.Post("/rules", h.CreateRule)
				r.Put("/rules/{id}", h.UpdateRule)
				r.Delete("/rules/{id}", h.DeleteRule)
				r.Post("/webhooks", h.CreateWebhook)
				r.Put("/webhooks/{id}", h.UpdateWebhook)
				r.Delete("/webhooks/{id}", h.DeleteWebhook)
				r.Post("/hostgroups", h.CreateHostgroup)
				r.Put("/hostgroups/{id}", h.UpdateHostgroup)
				r.Delete("/hostgroups/{id}", h.DeleteHostgroup)
			}

			if cfg.FeatureReports {
				r.Post("/reports", h.CreateReport)
				r.Put("/reports/{id}", h.UpdateReport)
				r.Delete("/reports/{id}", h.DeleteReport)
				r.Post("/reports/{id}/test", h.TestReport)
			}
		})
	})

	return r
}
