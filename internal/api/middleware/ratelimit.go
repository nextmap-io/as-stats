package middleware

import (
	"net/http"
	"sync"
	"time"
)

type visitor struct {
	tokens    float64
	lastSeen  time.Time
}

// RateLimit returns a middleware that limits requests per IP.
func RateLimit(requestsPerSecond float64) func(http.Handler) http.Handler {
	var mu sync.Mutex
	visitors := make(map[string]*visitor)

	// Clean up old entries periodically
	go func() {
		for {
			time.Sleep(time.Minute)
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr

			mu.Lock()
			v, exists := visitors[ip]
			if !exists {
				v = &visitor{tokens: requestsPerSecond}
				visitors[ip] = v
			}

			elapsed := time.Since(v.lastSeen).Seconds()
			v.lastSeen = time.Now()
			v.tokens += elapsed * requestsPerSecond
			if v.tokens > requestsPerSecond*10 { // burst capacity: 10x rate
				v.tokens = requestsPerSecond * 10
			}

			if v.tokens < 1 {
				mu.Unlock()
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			v.tokens--
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}
