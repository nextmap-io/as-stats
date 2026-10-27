package bgp

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RawSettings are the BGP_* environment variables as read by
// config.LoadAPI. ParseSettings validates them; any error stops the API at
// startup rather than leaving a half-working blackhole path.
type RawSettings struct {
	AnnounceCmd, WithdrawCmd, StatusCmd, RoutesCmd string
	// StatusCmdSet / RoutesCmdSet distinguish "unset" (default when the
	// announce command is the default) from "set to empty" (disabled).
	StatusCmdSet, RoutesCmdSet bool
	Community                  string // BGP_COMMUNITY (65535:666)
	ExtraCommunities           string // BGP_EXTRA_COMMUNITIES (65535:65281 = NO_EXPORT)
	NextHop, NextHopV6         string
	PeerAddress, RouterID      string
	PeerAS, LocalAS            uint32
	DryRun                     bool
	AllowedPrefixes            string
	ProtectedPrefixes          string
	MinPrefixLenV4             int
	MinPrefixLenV6             int
	MaxActive, AutoPerHour     int
	DefaultDuration            time.Duration
	MaxDuration                time.Duration
	CommandTimeout             time.Duration
	ReconcileInterval          time.Duration
}

// Settings is the validated configuration.
type Settings struct {
	Command           CommandConfig
	DryRun            bool
	Allowed           []netip.Prefix // explicit BGP_ALLOWED_PREFIXES (nil = LOCAL_AS prefixes)
	Protected         []netip.Prefix
	MinLenV4          int
	MinLenV6          int
	Limits            Limits
	ReconcileInterval time.Duration
	// Tick is the expiry / retry cadence (15s; not an env var, tests only).
	Tick time.Duration
}

var communityRe = regexp.MustCompile(`^(\d{1,10}):(\d{1,10})(?::(\d{1,10}))?$`)

// wellKnownCommunities are the names gobgp (and most CLIs) accept.
var wellKnownCommunities = map[string]bool{"no-export": true, "no-advertise": true, "no-export-subconfed": true, "blackhole": true}

func parseCommunities(s string) ([]string, error) {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if wellKnownCommunities[strings.ToLower(c)] {
			out = append(out, strings.ToLower(c))
			continue
		}
		m := communityRe.FindStringSubmatch(c)
		if m == nil {
			return nil, fmt.Errorf("invalid BGP community %q: want ASN:VALUE, a large community A:B:C, or no-export / no-advertise / blackhole", c)
		}
		limit := uint64(65535)
		if m[3] != "" {
			limit = 1<<32 - 1
		}
		for _, part := range m[1:] {
			if part == "" {
				continue
			}
			if n, err := strconv.ParseUint(part, 10, 64); err != nil || n > limit {
				return nil, fmt.Errorf("invalid BGP community %q: value out of range", c)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func parseIP(name, s string, want4 bool) (string, error) {
	if s == "" {
		return "", nil
	}
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || a.Zone() != "" {
		return "", fmt.Errorf("%s=%q is not an IP address", name, s)
	}
	a = a.Unmap()
	if want4 && !a.Is4() {
		return "", fmt.Errorf("%s must be an IPv4 address (use BGP_NEXT_HOP_V6 for IPv6)", name)
	}
	if !want4 && name == "BGP_NEXT_HOP_V6" && !a.Is6() {
		return "", fmt.Errorf("%s must be an IPv6 address", name)
	}
	return a.String(), nil
}

// ParseSettings validates the BGP configuration.
func ParseSettings(r RawSettings) (Settings, error) {
	var s Settings
	var err error
	if r.PeerAddress == "" || r.NextHop == "" {
		return s, fmt.Errorf("BGP_ENABLED=true requires BGP_PEER_ADDRESS and BGP_NEXT_HOP")
	}
	if r.LocalAS == 0 || r.PeerAS == 0 {
		return s, fmt.Errorf("BGP_ENABLED=true requires BGP_LOCAL_AS and BGP_PEER_AS")
	}
	cc := CommandConfig{PeerAS: r.PeerAS, LocalAS: r.LocalAS, Timeout: r.CommandTimeout}
	if cc.NextHopV4, err = parseIP("BGP_NEXT_HOP", r.NextHop, true); err != nil {
		return s, err
	}
	if cc.NextHopV6, err = parseIP("BGP_NEXT_HOP_V6", r.NextHopV6, false); err != nil {
		return s, err
	}
	if cc.PeerAddress, err = parseIP("BGP_PEER_ADDRESS", r.PeerAddress, false); err != nil {
		return s, err
	}
	routerID, err := parseIP("BGP_ROUTER_ID", r.RouterID, false)
	if err != nil {
		return s, err
	}
	community := r.Community
	if community == "" {
		community = "65535:666"
	}
	if cc.Communities, err = parseCommunities(community + "," + r.ExtraCommunities); err != nil {
		return s, err
	}

	announce := r.AnnounceCmd
	if strings.TrimSpace(announce) == "" {
		announce = DefaultAnnounceCmd
	}
	withdraw := r.WithdrawCmd
	if strings.TrimSpace(withdraw) == "" {
		withdraw = DefaultWithdrawCmd
	}
	defaultDaemon := announce == DefaultAnnounceCmd
	status, routes := r.StatusCmd, r.RoutesCmd
	if status == "" && !r.StatusCmdSet && defaultDaemon {
		status = DefaultStatusCmd
	}
	if routes == "" && !r.RoutesCmdSet && defaultDaemon {
		routes = DefaultRoutesCmd
	}
	if cc.Announce, err = ParseTemplate(announce); err != nil {
		return s, fmt.Errorf("BGP_ANNOUNCE_CMD: %w", err)
	}
	if !cc.Announce.UsesPlaceholder("ip") && !cc.Announce.UsesPlaceholder("prefix") {
		return s, fmt.Errorf("BGP_ANNOUNCE_CMD must contain {prefix} or {ip}")
	}
	if cc.Withdraw, err = ParseTemplate(withdraw); err != nil {
		return s, fmt.Errorf("BGP_WITHDRAW_CMD: %w", err)
	}
	if !cc.Withdraw.UsesPlaceholder("ip") && !cc.Withdraw.UsesPlaceholder("prefix") {
		return s, fmt.Errorf("BGP_WITHDRAW_CMD must contain {prefix} or {ip}")
	}
	if strings.TrimSpace(status) != "" {
		if cc.Status, err = ParseTemplate(status); err != nil {
			return s, fmt.Errorf("BGP_STATUS_CMD: %w", err)
		}
	}
	if strings.TrimSpace(routes) != "" {
		if cc.Routes, err = ParseTemplate(routes); err != nil {
			return s, fmt.Errorf("BGP_ROUTES_CMD: %w", err)
		}
	}
	s.Command = cc
	s.DryRun = r.DryRun

	if s.Allowed, err = ParsePrefixList(r.AllowedPrefixes); err != nil {
		return s, fmt.Errorf("BGP_ALLOWED_PREFIXES: %w", err)
	}
	for _, a := range s.Allowed {
		if (a.Addr().Is4() && a.Bits() < 8) || (a.Addr().Is6() && a.Bits() < 16) {
			return s, fmt.Errorf("BGP_ALLOWED_PREFIXES: %s is too broad to be your own address space", a)
		}
	}
	if s.Protected, err = ParsePrefixList(r.ProtectedPrefixes); err != nil {
		return s, fmt.Errorf("BGP_PROTECTED_PREFIXES: %w", err)
	}
	for _, ip := range []string{cc.PeerAddress, cc.NextHopV4, cc.NextHopV6, routerID} {
		if ip != "" {
			s.Protected = append(s.Protected, HostPrefix(netip.MustParseAddr(ip)))
		}
	}

	s.MinLenV4, s.MinLenV6 = r.MinPrefixLenV4, r.MinPrefixLenV6
	if s.MinLenV4 == 0 {
		s.MinLenV4 = 32
	}
	if s.MinLenV6 == 0 {
		s.MinLenV6 = 128
	}
	if s.MinLenV4 < MinPrefixLenFloorV4 || s.MinLenV4 > 32 {
		return s, fmt.Errorf("BGP_MIN_PREFIX_LEN_V4 must be between %d and 32", MinPrefixLenFloorV4)
	}
	if s.MinLenV6 < MinPrefixLenFloorV6 || s.MinLenV6 > 128 {
		return s, fmt.Errorf("BGP_MIN_PREFIX_LEN_V6 must be between %d and 128", MinPrefixLenFloorV6)
	}

	s.Limits = Limits{MaxActive: r.MaxActive, AutoPerHour: r.AutoPerHour, DefaultDuration: r.DefaultDuration, MaxDuration: r.MaxDuration}
	if s.Limits.MaxActive <= 0 {
		s.Limits.MaxActive = 50
	}
	if s.Limits.AutoPerHour < 0 {
		return s, fmt.Errorf("BGP_AUTO_BLOCK_MAX_PER_HOUR must be >= 0")
	}
	if s.Limits.MaxDuration <= 0 {
		s.Limits.MaxDuration = 24 * time.Hour
	}
	if s.Limits.DefaultDuration <= 0 {
		s.Limits.DefaultDuration = time.Hour
	}
	if s.Limits.MaxDuration > 7*24*time.Hour {
		return s, fmt.Errorf("BGP_MAX_DURATION must be at most 168h: a blackhole is an emergency measure")
	}
	if s.Limits.DefaultDuration < time.Minute || s.Limits.DefaultDuration > s.Limits.MaxDuration {
		return s, fmt.Errorf("BGP_DEFAULT_DURATION must be between 1m and BGP_MAX_DURATION")
	}
	if s.Command.Timeout <= 0 {
		s.Command.Timeout = 10 * time.Second
	}
	if s.Command.Timeout > time.Minute {
		return s, fmt.Errorf("BGP_COMMAND_TIMEOUT must be at most 1m")
	}
	s.ReconcileInterval = r.ReconcileInterval
	if s.ReconcileInterval <= 0 {
		s.ReconcileInterval = time.Minute
	}
	if s.ReconcileInterval < 10*time.Second {
		return s, fmt.Errorf("BGP_RECONCILE_INTERVAL must be at least 10s")
	}
	return s, nil
}

// Options assembles the Service options. localPrefixes (from LOCAL_AS) are
// the allow-list unless BGP_ALLOWED_PREFIXES was set.
func (s Settings) Options(store Store, audit AuditRecorder, localPrefixes []string, localAS uint32) Options {
	pol := Policy{Protected: s.Protected, MinLenV4: s.MinLenV4, MinLenV6: s.MinLenV6}
	switch {
	case len(s.Allowed) > 0:
		pol.Allowed, pol.AllowedSource = s.Allowed, "BGP_ALLOWED_PREFIXES"
	case len(localPrefixes) > 0:
		ps, _ := ParsePrefixList(strings.Join(localPrefixes, ","))
		pol.Allowed, pol.AllowedSource = ps, fmt.Sprintf("LOCAL_AS %d prefixes (RIPEstat)", localAS)
	default:
		pol.AllowedSource = "none"
	}
	return Options{
		Live:              NewCommandDaemon(s.Command),
		DryRun:            s.DryRun,
		Policy:            pol,
		Limits:            s.Limits,
		Store:             store,
		Audit:             audit,
		ReconcileInterval: s.ReconcileInterval,
		Tick:              s.Tick,
		Communities:       s.Command.Communities,
		NextHopV4:         s.Command.NextHopV4,
		NextHopV6:         s.Command.NextHopV6,
		PeerAddress:       s.Command.PeerAddress,
		PeerAS:            s.Command.PeerAS,
		LocalAS:           s.Command.LocalAS,
	}
}
