package metrics

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// AccessGuard wraps next with the access control used for /metrics: when
// allowedCIDRs is non-empty the client address must fall in one of them, and
// when user and pass are both set the request must carry matching basic auth.
// clientIP resolves the caller's address; pass middleware.ClientIP so the
// forwarded-header trust rules are identical to the /metrics endpoint's.
//
// It mirrors handler.MetricsHandler, which hard-wires the Prometheus handler,
// so that diagnostics served next to /metrics (pprof) sit behind exactly the
// same gate. Enabled reports whether any guard is configured.
func AccessGuard(next http.Handler, allowedCIDRs []string, user, pass string, clientIP func(*http.Request) string) (h http.Handler, enabled bool) {
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
	needsBasic := user != "" && pass != ""

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if needsIPCheck {
			ip := net.ParseIP(clientIP(r))
			allowed := false
			for _, n := range nets {
				if ip != nil && n.Contains(ip) {
					allowed = true
					break
				}
			}
			if !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if needsBasic {
			u, p, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	}), needsIPCheck || needsBasic
}
