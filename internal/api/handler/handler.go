package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/nextmap-io/as-stats/internal/store"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	Store         *store.ClickHouseStore
	LocalIPFilter string // SQL filter for local IPs (empty = no filter)
	LocalAS       uint32 // Local AS to exclude from top results
}

// New creates a new Handler.
func New(s *store.ClickHouseStore) *Handler {
	return &Handler{Store: s}
}

// Response is the standard JSON response envelope.
type Response struct {
	Data   any              `json:"data"`
	Meta   *ResponseMeta    `json:"meta,omitempty"`
	Error  string           `json:"error,omitempty"`
}

// ResponseMeta contains metadata about the response.
type ResponseMeta struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	TotalBytes uint64    `json:"total_bytes,omitempty"`
	Limit      int       `json:"limit,omitempty"`
	Offset     int       `json:"offset,omitempty"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

// writeError writes a JSON error response.
// For 5xx errors, a generic message is returned to avoid leaking internals.
func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		log.Printf("internal error: %s", msg)
		writeJSON(w, status, Response{Error: "internal server error"})
		return
	}
	writeJSON(w, status, Response{Error: msg})
}

// parseQueryParams extracts common query parameters from the request.
func parseQueryParams(r *http.Request) store.QueryParams {
	p := store.DefaultQueryParams()

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			p.From = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			p.To = t
		}
	}

	// Presets: 1h, 6h, 24h, 7d, 30d
	if preset := r.URL.Query().Get("period"); preset != "" {
		now := time.Now().UTC()
		p.To = now
		switch preset {
		case "1h":
			p.From = now.Add(-1 * time.Hour)
		case "6h":
			p.From = now.Add(-6 * time.Hour)
		case "24h":
			p.From = now.Add(-24 * time.Hour)
		case "7d":
			p.From = now.Add(-7 * 24 * time.Hour)
		case "30d":
			p.From = now.Add(-30 * 24 * time.Hour)
		}
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			p.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 100000 {
			p.Offset = n
		}
	}

	if v := r.URL.Query().Get("direction"); v == "in" || v == "out" {
		p.Direction = v
	}

	if v := r.URL.Query().Get("ip_version"); v == "4" || v == "6" {
		n, _ := strconv.Atoi(v)
		p.IPVersion = uint8(n)
	}

	if v := r.URL.Query().Get("link"); v != "" {
		p.LinkTags = []string{v}
	}
	if vals := r.URL.Query()["links"]; len(vals) > 0 {
		if len(vals) > 50 {
			vals = vals[:50]
		}
		p.LinkTags = vals
	}

	return p
}
