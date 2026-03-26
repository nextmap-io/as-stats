package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/nextmap-io/as-stats/internal/api/handler"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/store"
)

// NewRouter creates the API router with all endpoints.
func NewRouter(s *store.ClickHouseStore, cfg *config.APIConfig) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

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
	sessions := middleware.NewSessionStore()

	// Health check (no auth, no rate limit)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

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

		// Auth info
		authH := handler.NewAuthHandler(cfg, sessions)
		r.Get("/auth/me", authH.Me)

		// Overview
		r.Get("/overview", h.Overview)

		// Top endpoints
		r.Get("/top/as", h.TopAS)
		r.Get("/top/ip", h.TopIP)
		r.Get("/top/prefix", h.TopPrefix)

		// AS detail
		r.Get("/as/{asn}", h.ASDetail)
		r.Get("/as/{asn}/peers", h.ASPeers)
		r.Get("/as/{asn}/ips", h.ASTopIPs)

		// IP detail
		r.Get("/ip/{ip}", h.IPDetail)

		// Links
		r.Get("/links", h.Links)
		r.Get("/link/{tag}", h.LinkDetail)

		// Search
		r.Get("/search", h.Search)

		// Admin (link management, requires admin role when auth is enabled)
		r.Route("/admin", func(r chi.Router) {
			if cfg.AuthEnabled {
				r.Use(middleware.RequireRole("admin"))
			}
			r.Use(middleware.CSRF())
			r.Get("/links", h.LinksAdmin)
			r.Post("/links", h.LinkCreate)
			r.Delete("/links/{tag}", h.LinkDelete)
		})
	})

	return r
}
