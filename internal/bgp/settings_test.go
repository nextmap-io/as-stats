package bgp

import (
	"strings"
	"testing"
	"time"
)

func baseRaw() RawSettings {
	return RawSettings{
		PeerAddress: "192.0.2.2", NextHop: "192.0.2.254", PeerAS: 64500, LocalAS: 64501,
		Community: "65535:666", ExtraCommunities: "65535:65281",
		MinPrefixLenV4: 32, MinPrefixLenV6: 128, MaxActive: 50, AutoPerHour: 10,
	}
}

func TestParseSettingsDefaults(t *testing.T) {
	s, err := ParseSettings(baseRaw())
	if err != nil {
		t.Fatal(err)
	}
	if s.Command.Announce.String() != DefaultAnnounceCmd || s.Command.Status.String() != DefaultStatusCmd || s.Command.Routes.String() != DefaultRoutesCmd {
		t.Errorf("gobgp defaults not applied: %+v", s.Command)
	}
	if got := strings.Join(s.Command.Communities, ","); got != "65535:666,65535:65281" {
		t.Errorf("communities = %s", got)
	}
	if s.Limits.DefaultDuration != time.Hour || s.Limits.MaxDuration != 24*time.Hour || s.Command.Timeout != 10*time.Second || s.ReconcileInterval != time.Minute {
		t.Errorf("limits / timeouts = %+v %s %s", s.Limits, s.Command.Timeout, s.ReconcileInterval)
	}
	// The peer, the next hop and the router id are protected automatically.
	var prot []string
	for _, p := range s.Protected {
		prot = append(prot, p.String())
	}
	if got := strings.Join(prot, " "); got != "192.0.2.2/32 192.0.2.254/32" {
		t.Errorf("protected = %s", got)
	}
}

func TestParseSettingsCustomDaemonGetsNoGobgpDefaults(t *testing.T) {
	r := baseRaw()
	r.AnnounceCmd = "/usr/local/bin/rtbh announce {prefix}"
	r.WithdrawCmd = "/usr/local/bin/rtbh withdraw {prefix}"
	s, err := ParseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Command.Status.IsZero() || !s.Command.Routes.IsZero() {
		t.Errorf("a BIRD/FRR setup inherited gobgp status/routes commands: %q %q", s.Command.Status, s.Command.Routes)
	}
	// Explicitly empty disables the default even with gobgp.
	r = baseRaw()
	r.RoutesCmdSet = true
	s, err = ParseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Command.Routes.IsZero() || s.Command.Status.IsZero() {
		t.Errorf("BGP_ROUTES_CMD= did not disable only the routes command")
	}
}

func TestParseSettingsRejects(t *testing.T) {
	cases := map[string]func(*RawSettings){
		"requires BGP_PEER_ADDRESS":  func(r *RawSettings) { r.PeerAddress = "" },
		"requires BGP_LOCAL_AS":      func(r *RawSettings) { r.LocalAS = 0 },
		"must be an IPv4 address":    func(r *RawSettings) { r.NextHop = "2001:db8::1" },
		"BGP_NEXT_HOP_V6 must be an": func(r *RawSettings) { r.NextHopV6 = "192.0.2.1" },
		"not an IP address":          func(r *RawSettings) { r.PeerAddress = "router1" },
		"invalid BGP community":      func(r *RawSettings) { r.Community = "65535:666; reboot" },
		"out of range":               func(r *RawSettings) { r.ExtraCommunities = "65536:1" },
		"shell operator":             func(r *RawSettings) { r.AnnounceCmd = "gobgp global rib add {prefix} | tee /tmp/x" },
		"must contain {prefix}":      func(r *RawSettings) { r.WithdrawCmd = "gobgp global rib del all" },
		"unknown placeholder":        func(r *RawSettings) { r.StatusCmd = "vtysh -c 'show bgp neighbor {peer}'" },
		"too broad":                  func(r *RawSettings) { r.AllowedPrefixes = "0.0.0.0/0" },
		"invalid prefix":             func(r *RawSettings) { r.ProtectedPrefixes = "nope/24" },
		"BGP_MIN_PREFIX_LEN_V4":      func(r *RawSettings) { r.MinPrefixLenV4 = 16 },
		"BGP_MIN_PREFIX_LEN_V6":      func(r *RawSettings) { r.MinPrefixLenV6 = 32 },
		"BGP_MAX_DURATION":           func(r *RawSettings) { r.MaxDuration = 30 * 24 * time.Hour },
		"BGP_DEFAULT_DURATION":       func(r *RawSettings) { r.DefaultDuration = 48 * time.Hour },
		"BGP_COMMAND_TIMEOUT":        func(r *RawSettings) { r.CommandTimeout = 5 * time.Minute },
		"BGP_RECONCILE_INTERVAL":     func(r *RawSettings) { r.ReconcileInterval = time.Second },
		"BGP_AUTO_BLOCK_MAX":         func(r *RawSettings) { r.AutoPerHour = -1 },
	}
	for want, mutate := range cases {
		r := baseRaw()
		mutate(&r)
		if _, err := ParseSettings(r); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
}

func TestSettingsOptionsAllowList(t *testing.T) {
	s, err := ParseSettings(baseRaw())
	if err != nil {
		t.Fatal(err)
	}
	o := s.Options(nil, nil, []string{"198.51.100.0/24"}, 64501)
	if len(o.Policy.Allowed) != 1 || !strings.Contains(o.Policy.AllowedSource, "LOCAL_AS 64501") {
		t.Errorf("LOCAL_AS allow-list: %+v", o.Policy)
	}
	r := baseRaw()
	r.AllowedPrefixes = "203.0.113.0/24"
	s, _ = ParseSettings(r)
	o = s.Options(nil, nil, []string{"198.51.100.0/24"}, 64501)
	if len(o.Policy.Allowed) != 1 || o.Policy.Allowed[0].String() != "203.0.113.0/24" || o.Policy.AllowedSource != "BGP_ALLOWED_PREFIXES" {
		t.Errorf("explicit allow-list must replace LOCAL_AS: %+v", o.Policy)
	}
	o = s.Options(nil, nil, nil, 0)
	if o.Policy.AllowedSource != "BGP_ALLOWED_PREFIXES" {
		t.Errorf("source = %s", o.Policy.AllowedSource)
	}
	s, _ = ParseSettings(baseRaw())
	if o = s.Options(nil, nil, nil, 0); len(o.Policy.Allowed) != 0 || o.Policy.AllowedSource != "none" {
		t.Errorf("no allow-list: %+v", o.Policy)
	}
}
