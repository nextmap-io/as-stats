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

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	h := handler.New(s)

	// Health check (no auth, no rate limit)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.RateLimit(100))

		// Overview
		r.Get("/overview", h.Overview)

		// Top endpoints
		r.Get("/top/as", h.TopAS)
		r.Get("/top/ip", h.TopIP)
		r.Get("/top/prefix", h.TopPrefix)

		// AS detail
		r.Get("/as/{asn}", h.ASDetail)
		r.Get("/as/{asn}/peers", h.ASPeers)

		// IP detail
		r.Get("/ip/{ip}", h.IPDetail)

		// Links
		r.Get("/links", h.Links)
		r.Get("/link/{tag}", h.LinkDetail)

		// Search
		r.Get("/search", h.Search)

		// Admin (link management)
		r.Route("/admin", func(r chi.Router) {
			r.Get("/links", h.LinksAdmin)
			r.Post("/links", h.LinkCreate)
			r.Delete("/links/{tag}", h.LinkDelete)
		})
	})

	return r
}
