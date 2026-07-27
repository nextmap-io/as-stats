package middleware

import (
	"net"
	"net/http"
	"strings"
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
			ip := realIP(r)

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

// trustedProxyNets are the peers whose forwarded headers we believe: loopback
// and RFC1918/ULA, i.e. the reverse proxy that fronts this service in every
// shipped topology. A request arriving directly from the internet is keyed on
// its real transport address no matter what headers it sets.
var trustedProxyNets = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"fc00::/7", "169.254.0.0/16", "fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isTrustedProxy(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// realIP resolves the client address used as the rate-limit bucket key.
//
// The forwarded headers are only honoured when the immediate peer is a trusted
// proxy, and we take the RIGHT-most entry rather than the left-most. The shipped
// nginx uses $proxy_add_x_forwarded_for, which *appends* to whatever the client
// sent: trusting the left-most entry let any client pick its own bucket, both
// bypassing the limit and growing the visitors map with arbitrary keys.
func realIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	if !isTrustedProxy(net.ParseIP(host)) {
		return host
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Right-most entry is the one appended by our own trusted proxy.
		for i := len(parts) - 1; i >= 0; i-- {
			if c := strings.TrimSpace(parts[i]); c != "" {
				return c
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	return host
}
