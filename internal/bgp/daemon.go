package bgp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUnsupported is returned by Daemon.Status / Routes when no command is
// configured for it.
var ErrUnsupported = errors.New("not configured")

// Daemon is the BGP speaker as seen by the Service: announce / withdraw one
// blackhole route, report the session, list what it currently originates.
type Daemon interface {
	Announce(ctx context.Context, p netip.Prefix) error
	Withdraw(ctx context.Context, p netip.Prefix) error
	// Status returns the session state, or ErrUnsupported.
	Status(ctx context.Context) (SessionState, error)
	// Routes returns the blackhole routes the daemon originates, or
	// ErrUnsupported. Used to reconcile bgp_blocks with the real RIB.
	Routes(ctx context.Context) ([]netip.Prefix, error)
}

// SessionState is what the status command told us.
type SessionState struct {
	State  string // "established", "idle", "active", "connect", … or "unknown"
	Uptime int64  // seconds, 0 if unknown
}

// Default command templates (gobgp CLI). The status and routes defaults only
// apply when the announce template is the default too: a BIRD/FRR setup with
// only BGP_ANNOUNCE_CMD overridden must not suddenly run gobgp.
const (
	DefaultAnnounceCmd = "gobgp global rib add {prefix} community {communities} nexthop {next_hop} -a {family}"
	DefaultWithdrawCmd = "gobgp global rib del {prefix} -a {family}"
	DefaultStatusCmd   = "gobgp neighbor {peer_address} --json"
	DefaultRoutesCmd   = "gobgp global rib -a {family}"
)

// CommandConfig configures a CommandDaemon.
type CommandConfig struct {
	Announce, Withdraw Template
	Status, Routes     Template // zero = unsupported
	Communities        []string // first = BLACKHOLE (65535:666 by default)
	NextHopV4          string
	NextHopV6          string // empty = IPv6 blackholes refused
	PeerAddress        string
	PeerAS, LocalAS    uint32
	Timeout            time.Duration
}

// CommandDaemon drives a BGP daemon through its CLI (gobgp, birdc, vtysh,
// exabgpcli or a wrapper script).
type CommandDaemon struct {
	cfg CommandConfig
}

// NewCommandDaemon returns a daemon that runs the configured templates.
func NewCommandDaemon(cfg CommandConfig) *CommandDaemon {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &CommandDaemon{cfg: cfg}
}

// NextHop returns the next hop announced for a prefix's family.
func (d *CommandDaemon) NextHop(p netip.Prefix) string {
	if p.Addr().Is4() {
		return d.cfg.NextHopV4
	}
	return d.cfg.NextHopV6
}

func (d *CommandDaemon) vars(action string, p netip.Prefix) map[string]string {
	v := map[string]string{
		"action":       action,
		"community":    "",
		"communities":  strings.Join(d.cfg.Communities, ","),
		"peer_address": d.cfg.PeerAddress,
		"peer_as":      strconv.FormatUint(uint64(d.cfg.PeerAS), 10),
		"local_as":     strconv.FormatUint(uint64(d.cfg.LocalAS), 10),
	}
	if len(d.cfg.Communities) > 0 {
		v["community"] = d.cfg.Communities[0]
	}
	if p.IsValid() {
		v["ip"] = p.Addr().String()
		v["prefix_len"] = strconv.Itoa(p.Bits())
		v["prefix"] = p.String()
		v["family"] = family(p)
		v["next_hop"] = d.NextHop(p)
	}
	return v
}

// env exposes the same values to wrapper scripts as ASSTATS_BGP_* variables.
func envFor(v map[string]string) []string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, "ASSTATS_BGP_"+strings.ToUpper(k)+"="+v[k])
	}
	return out
}

func (d *CommandDaemon) run(ctx context.Context, t Template, action string, p netip.Prefix) (string, error) {
	v := d.vars(action, p)
	argv := t.Expand(v)
	start := time.Now()
	out, err := runCommand(ctx, d.cfg.Timeout, argv, envFor(v))
	observeCommand(action, err, time.Since(start))
	if err != nil {
		return out, err
	}
	if action == "announce" || action == "withdraw" {
		log.Printf("bgp: %s %s: %s", action, p, strings.Join(argv, " "))
	}
	return out, nil
}

func (d *CommandDaemon) checkNextHop(p netip.Prefix) error {
	if d.NextHop(p) == "" && d.cfg.Announce.UsesPlaceholder("next_hop") {
		return &PolicyError{fmt.Sprintf("no next hop for %s: set BGP_NEXT_HOP_V6 to blackhole IPv6 addresses", family(p))}
	}
	return nil
}

// Announce runs the announce template.
func (d *CommandDaemon) Announce(ctx context.Context, p netip.Prefix) error {
	if err := d.checkNextHop(p); err != nil {
		return err
	}
	_, err := d.run(ctx, d.cfg.Announce, "announce", p)
	return err
}

// Withdraw runs the withdraw template.
func (d *CommandDaemon) Withdraw(ctx context.Context, p netip.Prefix) error {
	_, err := d.run(ctx, d.cfg.Withdraw, "withdraw", p)
	return err
}

// Status runs the status template and parses its output.
func (d *CommandDaemon) Status(ctx context.Context) (SessionState, error) {
	if d.cfg.Status.IsZero() {
		return SessionState{State: "unknown"}, ErrUnsupported
	}
	out, err := d.run(ctx, d.cfg.Status, "status", netip.Prefix{})
	if err != nil {
		return SessionState{State: "error"}, err
	}
	return ParseStatusOutput(out), nil
}

// Routes runs the routes template (once per family when it uses {family})
// and extracts every prefix from its output.
func (d *CommandDaemon) Routes(ctx context.Context) ([]netip.Prefix, error) {
	if d.cfg.Routes.IsZero() {
		return nil, ErrUnsupported
	}
	probes := []netip.Prefix{{}}
	if d.cfg.Routes.UsesPlaceholder("family") {
		probes = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/32")}
		if d.cfg.NextHopV6 != "" {
			probes = append(probes, netip.MustParsePrefix("2001:db8::/128"))
		}
	}
	var all []netip.Prefix
	for _, probe := range probes {
		v := d.vars("routes", netip.Prefix{})
		if probe.IsValid() {
			v["family"] = family(probe)
		}
		argv := d.cfg.Routes.Expand(v)
		start := time.Now()
		out, err := runCommand(ctx, d.cfg.Timeout, argv, envFor(v))
		observeCommand("routes", err, time.Since(start))
		if err != nil {
			return nil, err
		}
		all = append(all, ParseRoutesOutput(out)...)
	}
	return all, nil
}

var prefixTokenRe = regexp.MustCompile(`[0-9A-Fa-f:.]+/[0-9]{1,3}`)

// ParseRoutesOutput extracts the prefixes from a routes listing. It works on
// gobgp / birdc / vtysh text and JSON alike, since all of them print routes
// as address/length.
func ParseRoutesOutput(out string) []netip.Prefix {
	seen := map[netip.Prefix]bool{}
	var res []netip.Prefix
	for _, tok := range prefixTokenRe.FindAllString(out, -1) {
		p, err := netip.ParsePrefix(tok)
		if err != nil {
			continue
		}
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		if p.Masked() != p || seen[p] {
			continue
		}
		seen[p] = true
		res = append(res, p)
	}
	return res
}

var bgpStateRe = regexp.MustCompile(`(?i)bgp\s*state\W+(established|idle|active|connect|opensent|openconfirm)`)

// ParseStatusOutput understands gobgp `neighbor <addr> --json`, FRR
// `show bgp neighbor <addr> json`, BIRD `show protocols all <name>` and, as a
// last resort, any text naming a BGP FSM state.
func ParseStatusOutput(raw string) SessionState {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SessionState{State: "unknown"}
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		return parseJSONNeighbor(arr[0])
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		if _, ok := obj["state"]; ok {
			return parseJSONNeighbor(obj)
		}
		// FRR: {"192.0.2.2": {"bgpState": "Established", "bgpTimerUpMsec": …}}
		for _, v := range obj {
			if n, ok := v.(map[string]any); ok {
				if s, ok := n["bgpState"].(string); ok {
					st := SessionState{State: strings.ToLower(s)}
					if ms, ok := n["bgpTimerUpMsec"].(float64); ok {
						st.Uptime = int64(ms / 1000)
					}
					return st
				}
			}
		}
		return SessionState{State: "unknown"}
	}
	if m := bgpStateRe.FindStringSubmatch(raw); m != nil {
		return SessionState{State: strings.ToLower(m[1])}
	}
	lower := strings.ToLower(raw)
	for _, s := range []string{"established", "openconfirm", "opensent", "idle", "connect", "active"} {
		if strings.Contains(lower, s) {
			return SessionState{State: s}
		}
	}
	return SessionState{State: "unknown"}
}

func parseJSONNeighbor(obj map[string]any) SessionState {
	st := SessionState{State: "unknown"}
	if s, ok := obj["state"].(map[string]any); ok {
		if ss, ok := s["session-state"].(string); ok && ss != "" {
			st.State = strings.ToLower(ss)
		}
	}
	if timers, ok := obj["timers"].(map[string]any); ok {
		if ts, ok := timers["state"].(map[string]any); ok {
			switch u := ts["uptime"].(type) {
			case string:
				if t, err := time.Parse(time.RFC3339, u); err == nil {
					st.Uptime = int64(time.Since(t).Seconds())
				}
			case float64:
				st.Uptime = int64(u)
			case map[string]any: // gobgp ≥ 3: {"seconds": …}
				if s, ok := u["seconds"].(float64); ok && s > 0 {
					st.Uptime = int64(time.Since(time.Unix(int64(s), 0)).Seconds())
				}
			}
		}
	}
	return st
}

// DryRunDaemon executes nothing. It keeps an in-memory RIB so the rest of the
// machinery (listing, reconciliation, expiry) behaves as with a real daemon.
type DryRunDaemon struct {
	mu  sync.Mutex
	rib map[netip.Prefix]bool
}

// NewDryRunDaemon returns a daemon that only logs.
func NewDryRunDaemon() *DryRunDaemon {
	return &DryRunDaemon{rib: map[netip.Prefix]bool{}}
}

func (d *DryRunDaemon) Announce(_ context.Context, p netip.Prefix) error {
	d.mu.Lock()
	d.rib[p] = true
	d.mu.Unlock()
	log.Printf("bgp[dry-run]: would announce %s", p)
	return nil
}

func (d *DryRunDaemon) Withdraw(_ context.Context, p netip.Prefix) error {
	d.mu.Lock()
	delete(d.rib, p)
	d.mu.Unlock()
	log.Printf("bgp[dry-run]: would withdraw %s", p)
	return nil
}

func (d *DryRunDaemon) Status(context.Context) (SessionState, error) {
	return SessionState{State: "dry-run"}, nil
}

func (d *DryRunDaemon) Routes(context.Context) ([]netip.Prefix, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]netip.Prefix, 0, len(d.rib))
	for p := range d.rib {
		out = append(out, p)
	}
	return out, nil
}
