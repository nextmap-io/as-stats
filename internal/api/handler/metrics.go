package handler

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns an http.Handler for the /metrics endpoint that
// serves Prometheus metrics with optional access control.
//
// Access is granted when ALL configured guards pass:
//
//   - allowedCIDRs: if non-empty, the client IP must match at least one CIDR.
//     Useful for restricting to your Prometheus server's IP. The address comes
//     from middleware.ClientIP, which only believes forwarded headers when the
//     transport peer is itself a trusted proxy — otherwise a single forged
//     X-Forwarded-For would be enough to walk straight through this guard.
//
//   - basicUser / basicPass: if both non-empty, the request must carry a
//     matching Authorization: Basic header. Useful when /metrics is exposed
//     to the internet behind a reverse proxy.
//
// If both are empty, /metrics is open (common in private networks).
func MetricsHandler(allowedCIDRs []string, basicUser, basicPass string) http.Handler {
	promHandler := promhttp.Handler()

	var nets []*net.IPNet
	for _, cidr := range allowedCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		// Accept bare IPs too (e.g. "10.0.0.5" → "10.0.0.5/32")
		if !strings.Contains(cidr, "/") {
			if strings.Contains(cidr, ":") {
				cidr += "/128"
			} else {
				cidr += "/32"
			}
		}
		if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, ipnet)
		}
	}

	needsIPCheck := len(nets) > 0
	needsBasic := basicUser != "" && basicPass != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ── IP allow-list check ──────────────────────────────
		if needsIPCheck {
			clientIP := middleware.ClientIP(r)
			ip := net.ParseIP(clientIP)
			if ip == nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			allowed := false
			for _, n := range nets {
				if n.Contains(ip) {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}

		// ── Basic auth check ─────────────────────────────────
		if needsBasic {
			user, pass, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(user), []byte(basicUser)) != 1 ||
				subtle.ConstantTimeCompare([]byte(pass), []byte(basicPass)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		promHandler.ServeHTTP(w, r)
	})
}
