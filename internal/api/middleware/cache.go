package middleware

import (
	"bytes"
	"net/http"
	"sync"
	"time"
)

type cacheEntry struct {
	body        []byte
	contentType string
	status      int
	expiresAt   time.Time
}

type responseRecorder struct {
	http.ResponseWriter
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Cache returns a middleware that caches successful GET responses in memory.
func Cache(ttl time.Duration) func(http.Handler) http.Handler {
	var mu sync.RWMutex
	entries := make(map[string]*cacheEntry)

	// Cleanup goroutine
	go func() {
		for {
			time.Sleep(30 * time.Second)
			now := time.Now()
			mu.Lock()
			for k, v := range entries {
				if now.After(v.expiresAt) {
					delete(entries, k)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			// Bypass cache
			if r.Header.Get("Cache-Control") == "no-cache" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.URL.RequestURI()

			// Check cache
			mu.RLock()
			entry, ok := entries[key]
			mu.RUnlock()

			if ok && time.Now().Before(entry.expiresAt) {
				w.Header().Set("Content-Type", entry.contentType)
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(entry.status)
				_, _ = w.Write(entry.body)
				return
			}

			// Record response
			rec := &responseRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)

			// Only cache 200 responses
			if rec.status == 200 {
				mu.Lock()
				entries[key] = &cacheEntry{
					body:        rec.body.Bytes(),
					contentType: w.Header().Get("Content-Type"),
					status:      rec.status,
					expiresAt:   time.Now().Add(ttl),
				}
				mu.Unlock()
			}
		})
	}
}
