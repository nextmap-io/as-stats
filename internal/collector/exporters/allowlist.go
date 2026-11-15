// Package exporters holds the exporter allow-list of the flow listeners
// (COLLECTOR_ALLOWED_EXPORTERS).
package exporters

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// AllowList is the set of source addresses the NetFlow / sFlow listeners
// accept datagrams from. A nil *AllowList accepts every source, which is the
// behaviour when COLLECTOR_ALLOWED_EXPORTERS is unset.
type AllowList struct {
	prefixes []netip.Prefix
}

// Parse reads a comma-separated list of IPs and CIDRs ("192.0.2.1,
// 198.51.100.0/24, 2001:db8::/48"). Bare IPs are single-host prefixes. IPv4
// entries also match the IPv4-mapped form a dual-stack socket reports. An
// empty string returns nil (accept every source).
func Parse(s string) (*AllowList, error) {
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(entry, "/") {
			parsed, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid exporter %q: want an IP or a CIDR", entry)
			}
			p = parsed
		} else {
			addr, err := netip.ParseAddr(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid exporter %q: want an IP or a CIDR", entry)
			}
			// A zone ("fe80::1%eth0") never matches a received address;
			// refuse it rather than silently dropping that exporter.
			if addr.Zone() != "" {
				return nil, fmt.Errorf("invalid exporter %q: zones are not supported", entry)
			}
			p = netip.PrefixFrom(addr, addr.BitLen())
		}
		if p.Addr().Is4In6() {
			p = netip.PrefixFrom(p.Addr().Unmap(), max(p.Bits()-96, 0))
		}
		prefixes = append(prefixes, p.Masked())
	}
	if len(prefixes) == 0 {
		return nil, nil
	}
	return &AllowList{prefixes: prefixes}, nil
}

// Allows reports whether a datagram from ip may be decoded.
func (a *AllowList) Allows(ip net.IP) bool {
	if a == nil {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, p := range a.prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// String lists the prefixes, for the startup log.
func (a *AllowList) String() string {
	if a == nil {
		return "any"
	}
	parts := make([]string, len(a.prefixes))
	for i, p := range a.prefixes {
		parts[i] = p.String()
	}
	return strings.Join(parts, ", ")
}
