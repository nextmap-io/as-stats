package middleware

import (
	"net/http"
	"sync"
	"time"
)

type visitor struct {
	tokens   float64
	lastSeen time.Time
}

const (
	// maxVisitors bounds the per-IP bucket map. Entries are ~40 bytes, so this
	// caps it in the low megabytes even under a deliberate key-churn attempt.
	maxVisitors = 50_000
	// overflowBucket is the shared key used once maxVisitors is reached.
	overflowBucket = "\x00overflow"
)

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
			ip := ClientIP(r)

			mu.Lock()
			v, exists := visitors[ip]
			if !exists {
				// Hard cap so the map cannot be grown without bound (a spoofing
				// client behind a trusted proxy, or simply a very large NAT
				// fan-out). Past the cap, new keys share a single overflow
				// bucket rather than allocating: degraded fairness beats OOM.
				if len(visitors) >= maxVisitors {
					ip = overflowBucket
					v, exists = visitors[ip]
				}
				if !exists {
					v = &visitor{tokens: requestsPerSecond}
					visitors[ip] = v
				}
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
