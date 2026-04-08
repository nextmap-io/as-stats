package handler

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateWebhookURL rejects URLs that could be used for SSRF.
// We block:
//   - non-http(s) schemes
//   - loopback addresses (127.0.0.0/8, ::1)
//   - link-local (169.254.0.0/16, fe80::/10)
//   - RFC1918 private ranges (10/8, 172.16/12, 192.168/16)
//   - CGNAT (100.64.0.0/10)
//   - unique local IPv6 (fc00::/7)
//   - IPv4 multicast and IPv6 multicast
//   - the unspecified addresses (0.0.0.0, ::)
//
// Hostnames are allowed without DNS resolution (DNS resolution at request time
// is the responsibility of the caller; we cannot prevent rebinding here, but we
// at least reject literal IPs in the configured URL).
func validateWebhookURL(raw string) error {
	if raw == "" {
		return errors.New("URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https schemes are allowed (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("URL must include a host")
	}

	host := u.Hostname()

	// If the host is an IP literal, validate it against blocklists.
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return errors.New("loopback addresses are not allowed")
		}
		if ip.IsUnspecified() {
			return errors.New("unspecified addresses are not allowed")
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("link-local addresses are not allowed")
		}
		if ip.IsMulticast() {
			return errors.New("multicast addresses are not allowed")
		}
		if ip.IsPrivate() {
			return errors.New("private IP ranges are not allowed")
		}
		// CGNAT 100.64.0.0/10
		if v4 := ip.To4(); v4 != nil {
			if v4[0] == 100 && (v4[1]&0xC0) == 0x40 {
				return errors.New("CGNAT addresses are not allowed")
			}
		}
	}

	// Reject obvious local hostnames
	lower := strings.ToLower(host)
	switch lower {
	case "localhost", "localhost.localdomain", "ip6-localhost", "ip6-loopback":
		return errors.New("localhost is not allowed")
	}
	if strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") {
		return errors.New("local hostnames are not allowed")
	}

	return nil
}
