package middleware

import (
	"net"
	"net/http"
	"strings"
)

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

// ClientIP resolves the address of the client that actually reached us. It is
// the single source of truth for every access-control or accounting decision
// that keys on the caller (rate-limit bucket, audit trail, /metrics allow-list).
//
// The forwarded headers are only honoured when the immediate peer is a trusted
// proxy, and we take the RIGHT-most entry rather than the left-most. The shipped
// nginx uses $proxy_add_x_forwarded_for, which *appends* to whatever the client
// sent: trusting the left-most entry let any client pick its own identity, both
// bypassing the rate limit and forging its way past IP allow-lists.
func ClientIP(r *http.Request) string {
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
