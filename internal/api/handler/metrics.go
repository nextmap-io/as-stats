package handler

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns an http.Handler for the /metrics endpoint that
// serves Prometheus metrics with optional access control.
//
// Access is granted when ALL configured guards pass:
//
//   - allowedCIDRs: if non-empty, the client IP (from X-Forwarded-For or
//     RemoteAddr) must match at least one CIDR. Useful for restricting to
//     your Prometheus server's IP.
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
			clientIP := clientAddr(r)
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

// clientAddr extracts the client IP from X-Forwarded-For (first entry)
// or falls back to RemoteAddr.
func clientAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
