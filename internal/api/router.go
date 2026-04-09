package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/nextmap-io/as-stats/internal/api/handler"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/store"
)

// NewRouter creates the API router with all endpoints.
func NewRouter(s *store.ClickHouseStore, cfg *config.APIConfig, localIPFilter string, localPrefixes []string) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
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
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
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
	h.BGPBlocker = bgp.NewNoop() // phase 1: noop blocker
	sessions := middleware.NewSessionStore()

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
			r.Use(middleware.AuthMiddleware(cfg, sessions))
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
			r.Get("/links", h.Links)
			r.Get("/links/traffic", h.LinksTraffic)

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
		}

		// Alerts (gated by FEATURE_ALERTS)
		if cfg.FeatureAlerts {
			r.Get("/alerts", h.ListAlerts)
			r.Get("/alerts/summary", h.AlertsSummary)
			// Live threats — pre-trigger view of top destinations vs. rules
			r.Get("/threats/live", h.LiveThreats)
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

		// AS detail
		r.Get("/as/{asn}", h.ASDetail)
		r.Get("/as/{asn}/peers", h.ASPeers)
		r.Get("/as/{asn}/ips", h.ASTopIPs)

		// IP detail
		r.Get("/ip/{ip}", h.IPDetail)

		// Link detail (not cached — specific to one link)
		r.Get("/link/{tag}", h.LinkDetail)

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

			// Reads (no CSRF)
			if cfg.FeatureAlerts {
				r.Get("/rules", h.ListRules)
				r.Get("/webhooks", h.ListWebhooks)
				r.Get("/audit", h.ListAuditLog)
			}

			// Writes (CSRF required)
			r.Group(func(r chi.Router) {
				r.Use(middleware.CSRF())
				r.Post("/links", h.LinkCreate)
				r.Delete("/links/{tag}", h.LinkDelete)

				if cfg.FeatureAlerts {
					r.Post("/rules", h.CreateRule)
					r.Put("/rules/{id}", h.UpdateRule)
					r.Delete("/rules/{id}", h.DeleteRule)
					r.Post("/webhooks", h.CreateWebhook)
					r.Put("/webhooks/{id}", h.UpdateWebhook)
					r.Delete("/webhooks/{id}", h.DeleteWebhook)
				}
			})
		})
	})

	return r
}
