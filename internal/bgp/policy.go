package bgp

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// Hard floors for BGP_MIN_PREFIX_LEN_V4 / _V6: whatever the operator
// configures, as-stats never announces a blackhole shorter than these. A /24
// (IPv4) or /48 (IPv6) is the shortest prefix most RTBH services accept and
// already drops traffic to 256 addresses / a whole site.
const (
	MinPrefixLenFloorV4 = 24
	MinPrefixLenFloorV6 = 48
)

// ErrInvalidTarget is returned for a target that is not an address or
// prefix. It wraps ErrInvalidRequest (HTTP 400).
var ErrInvalidTarget = fmt.Errorf("%w: target", ErrInvalidRequest)

// PolicyError explains why a syntactically valid target may not be blackholed.
// Handlers answer 422 with its message.
type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }

// Policy is the safety net between a block request and the BGP daemon. Every
// announcement — manual, from an alert, or from the alert engine — goes
// through Check, so an operator typo or a runaway rule cannot blackhole the
// default route, a router, or someone else's address space.
type Policy struct {
	// Allowed is the allow-list: a target must sit entirely inside one of
	// these prefixes. Empty means nothing may be blackholed (fail closed).
	Allowed []netip.Prefix
	// AllowedSource says where Allowed came from ("BGP_ALLOWED_PREFIXES",
	// "LOCAL_AS 64500 (RIPEstat)" or "none") for the status page.
	AllowedSource string
	// Protected addresses / prefixes are never blackholed even when inside
	// Allowed: the BGP peer, the next hops, the router id, plus
	// BGP_PROTECTED_PREFIXES (resolvers, router loopbacks, the as-stats host…).
	Protected []netip.Prefix
	// MinLenV4 / MinLenV6 are the shortest prefix lengths accepted (32 / 128
	// = host routes only, the default).
	MinLenV4 int
	MinLenV6 int
}

// ParseTarget turns "192.0.2.1", "192.0.2.0/30", "2001:db8::1" or
// "::ffff:192.0.2.1" into a canonical prefix. A bare address is a host route.
// IPv4-mapped IPv6 is unmapped (prefix length 96+n becomes n). Host bits must
// be zero: "192.0.2.1/30" is rejected rather than silently widened.
func ParseTarget(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, fmt.Errorf("%w: empty", ErrInvalidTarget)
	}
	if !strings.Contains(raw, "/") {
		addr, err := netip.ParseAddr(raw)
		if err != nil || addr.Zone() != "" {
			return netip.Prefix{}, fmt.Errorf("%w: %q is not an IP address", ErrInvalidTarget, raw)
		}
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	p, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%w: %q is not a prefix", ErrInvalidTarget, raw)
	}
	return canonicalPrefix(p, raw)
}

// TargetFromParts builds a prefix from an address and an optional prefix
// length (0 = host route), as used by DELETE /bgp/blocks/{ip}?prefix_len=.
func TargetFromParts(ip string, prefixLen int) (netip.Prefix, error) {
	if prefixLen == 0 {
		return ParseTarget(ip)
	}
	return ParseTarget(ip + "/" + strconv.Itoa(prefixLen))
}

func canonicalPrefix(p netip.Prefix, raw string) (netip.Prefix, error) {
	addr := p.Addr()
	bits := p.Bits()
	if addr.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("%w: %q has a zone", ErrInvalidTarget, raw)
	}
	if addr.Is4In6() {
		if bits < 96 {
			return netip.Prefix{}, fmt.Errorf("%w: %q mixes IPv4-mapped and IPv6 space", ErrInvalidTarget, raw)
		}
		addr = addr.Unmap()
		bits -= 96
	}
	out := netip.PrefixFrom(addr, bits)
	if out.Masked() != out {
		return netip.Prefix{}, fmt.Errorf("%w: %q has host bits set (did you mean %s?)", ErrInvalidTarget, raw, out.Masked())
	}
	return out, nil
}

// reservedPrefixes can never be blackholed: announcing them is meaningless or
// actively harmful (default route, loopback, multicast, link-local, the
// RFC 6666 discard prefix as-stats uses for synthetic alert targets…).
var reservedPrefixes = mustPrefixes(
	"0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fe80::/10", "ff00::/8", "100::/64", "::ffff:0:0/96",
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// overlaps reports whether two prefixes share at least one address.
func overlaps(a, b netip.Prefix) bool {
	return a.Overlaps(b)
}

// within reports whether every address of p is inside outer.
func within(p, outer netip.Prefix) bool {
	return outer.Bits() <= p.Bits() && outer.Contains(p.Addr())
}

// Check validates a canonical target (from ParseTarget) against the policy.
func (pol *Policy) Check(p netip.Prefix) error {
	if !p.IsValid() {
		return fmt.Errorf("%w: empty prefix", ErrInvalidTarget)
	}
	minLen := pol.MinLenV6
	floor := MinPrefixLenFloorV6
	if p.Addr().Is4() {
		minLen, floor = pol.MinLenV4, MinPrefixLenFloorV4
	}
	if minLen < floor {
		minLen = floor
	}
	if max := p.Addr().BitLen(); minLen > max {
		minLen = max
	}
	if p.Bits() < minLen {
		return &PolicyError{fmt.Sprintf("%s is shorter than the allowed /%d (BGP_MIN_PREFIX_LEN_V%s)", p, minLen, family(p)[3:])}
	}
	for _, r := range reservedPrefixes {
		if overlaps(p, r) {
			return &PolicyError{fmt.Sprintf("%s is reserved address space (%s) and cannot be blackholed", p, r)}
		}
	}
	if p.Addr().Is4() && p.Addr() == netip.MustParseAddr("255.255.255.255") {
		return &PolicyError{"the limited broadcast address cannot be blackholed"}
	}
	for _, r := range pol.Protected {
		if overlaps(p, r) {
			return &PolicyError{fmt.Sprintf("%s overlaps protected %s (BGP peer, next hop, router id or BGP_PROTECTED_PREFIXES)", p, r)}
		}
	}
	if len(pol.Allowed) == 0 {
		return &PolicyError{"no allow-list: set BGP_ALLOWED_PREFIXES or LOCAL_AS so as-stats knows which addresses are yours"}
	}
	for _, a := range pol.Allowed {
		if within(p, a) {
			return nil
		}
	}
	return &PolicyError{fmt.Sprintf("%s is outside the allowed prefixes (%s)", p, pol.AllowedSource)}
}

// ParsePrefixList parses a comma-separated list of prefixes or addresses
// (BGP_ALLOWED_PREFIXES, BGP_PROTECTED_PREFIXES). Host bits are masked: an
// allow-list entry "192.0.2.1/24" means 192.0.2.0/24.
func ParsePrefixList(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(item, "/") {
			pp, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("invalid prefix %q", item)
			}
			if pp.Addr().Is4In6() && pp.Bits() >= 96 {
				pp = netip.PrefixFrom(pp.Addr().Unmap(), pp.Bits()-96)
			}
			p = pp.Masked()
		} else {
			a, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q", item)
			}
			a = a.Unmap()
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p)
	}
	return out, nil
}

// HostPrefix returns the host route for an address.
func HostPrefix(a netip.Addr) netip.Prefix {
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen())
}

func family(p netip.Prefix) string {
	if p.Addr().Is4() {
		return "ipv4"
	}
	return "ipv6"
}
