package bgp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// Default command templates use the gobgp CLI. Operators can override via
// BGP_ANNOUNCE_CMD / BGP_WITHDRAW_CMD / BGP_STATUS_CMD env vars to support
// BIRD, FRRouting, ExaBGP, or any other BGP daemon.
const (
	DefaultAnnounceCmd = "gobgp global rib add {ip}/{prefix_len} community {community} nexthop {next_hop} -a {family}"
	DefaultWithdrawCmd = "gobgp global rib del {ip}/{prefix_len} -a {family}"
	DefaultStatusCmd   = "gobgp neighbor {peer_address} --json"
)

// cmdTimeout is the maximum time we wait for a shell command to complete.
const cmdTimeout = 10 * time.Second

// autoWithdrawRetryDelay controls retries after a daemon or store failure.
const autoWithdrawRetryDelay = 30 * time.Second

// BlockStore is the minimal store interface needed by ScriptBlocker to
// reload active blocks on startup.
type BlockStore interface {
	ListActiveBlocks(ctx context.Context) ([]model.BGPBlock, error)
	WithdrawBlock(ctx context.Context, ip, unblockedBy, reason string) error
}

// Config holds the ScriptBlocker configuration. Command templates use
// placeholder tokens: {ip}, {prefix_len}, {community}, {next_hop},
// {peer_address}.
type Config struct {
	AnnounceCmd string // shell command template for route announcement
	WithdrawCmd string // shell command template for route withdrawal
	StatusCmd   string // shell command template for session status query
	Community   string // e.g. "65535:666"
	NextHop     string // next-hop IP for announced routes
	PeerAddress string // BGP peer address (for status queries)
	PeerAS      uint32
	LocalAS     uint32
}

// activeRoute tracks one announced blackhole route in memory.
type activeRoute struct {
	block  model.BGPBlock
	cancel context.CancelFunc // cancels the auto-withdraw timer (nil if no timer)
}

// ScriptBlocker implements Blocker by shelling out to CLI tools.
// This approach works with any BGP daemon (GoBGP, BIRD, FRRouting, ExaBGP)
// without pulling heavy Go library dependencies into the binary.
type ScriptBlocker struct {
	mu     sync.RWMutex
	routes map[string]*activeRoute // keyed by IP string (e.g. "192.0.2.1")
	store  BlockStore
	cfg    Config

	// wg tracks background goroutines (auto-withdraw timers) so Stop()
	// can wait for them to finish.
	wg sync.WaitGroup
	// stopCh is closed by Stop() to signal all background goroutines.
	stopCh chan struct{}
}

// NewScript creates a ScriptBlocker and re-announces any active blocks
// found in the store so that routes survive a process restart.
func NewScript(cfg Config, store BlockStore) (*ScriptBlocker, error) {
	sb := &ScriptBlocker{
		routes: make(map[string]*activeRoute),
		store:  store,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}

	if strings.TrimSpace(sb.cfg.AnnounceCmd) == "" {
		sb.cfg.AnnounceCmd = DefaultAnnounceCmd
	}
	if strings.TrimSpace(sb.cfg.WithdrawCmd) == "" {
		sb.cfg.WithdrawCmd = DefaultWithdrawCmd
	}

	// Re-inject active blocks from the database.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	blocks, err := store.ListActiveBlocks(ctx)
	if err != nil {
		log.Printf("bgp[script]: WARNING: could not load active blocks from store: %v", err)
		// Non-fatal — we can still function, just won't have the routes
		// from a previous run until the operator re-announces them.
		return sb, nil
	}

	now := time.Now().UTC()
	for i := range blocks {
		b := blocks[i]
		parsedIP := net.ParseIP(b.IP)
		if parsedIP == nil {
			log.Printf("bgp[script]: WARNING: ignoring active block with invalid IP %q", b.IP)
			continue
		}
		ip := parsedIP.String()
		b.IP = ip
		// Never trust a persisted prefix length: a historical bug stored IPv6
		// host routes as /32. Blackholes are always single-host routes.
		b.PrefixLen = HostPrefixLen(parsedIP)

		if b.ExpiresAt != nil && !b.ExpiresAt.After(now) {
			log.Printf("bgp[script]: active block %s/%d expired while stopped; withdrawing without re-announcing", ip, b.PrefixLen)
			if execErr := sb.execCmd(ctx, sb.buildWithdrawCmd(ip, b.PrefixLen)); execErr != nil {
				log.Printf("bgp[script]: WARNING: cleanup withdrawal for expired %s failed: %v", ip, execErr)
			}
			if storeErr := store.WithdrawBlock(ctx, ip, "system", "block expired while BGP service was stopped"); storeErr != nil {
				log.Printf("bgp[script]: WARNING: could not persist expiration for %s: %v", ip, storeErr)
			}
			continue
		}

		log.Printf("bgp[script]: re-announcing route for %s/%d from store (reason=%q)", ip, b.PrefixLen, b.Reason)

		cmd := sb.buildAnnounceCmd(ip, b.PrefixLen)
		if execErr := sb.execCmd(ctx, cmd); execErr != nil {
			log.Printf("bgp[script]: WARNING: failed to re-announce %s: %v", ip, execErr)
			continue
		}

		ar := &activeRoute{block: b}
		var timerCtx context.Context
		if b.ExpiresAt != nil {
			timerCtx, ar.cancel = context.WithCancel(context.Background())
		}
		// Publish the route before starting its timer. Otherwise a nearly due
		// timer can fire first and leave the route active forever.
		sb.routes[ip] = ar
		if timerCtx != nil {
			sb.startWithdrawTimer(ip, time.Until(*b.ExpiresAt), timerCtx)
		}
	}

	if len(sb.routes) > 0 {
		log.Printf("bgp[script]: re-announced %d active route(s) from store", len(sb.routes))
	}

	return sb, nil
}

// Announce advertises a blackhole route for the given target IP.
// If the target is already announced, the call is a no-op (idempotent).
func (sb *ScriptBlocker) Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error {
	ip := target.String()

	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Idempotent: skip if already active.
	if _, exists := sb.routes[ip]; exists {
		log.Printf("bgp[script]: %s already announced, skipping", ip)
		return nil
	}

	prefixLen := HostPrefixLen(target)
	cmd := sb.buildAnnounceCmd(ip, prefixLen)

	log.Printf("bgp[script]: ANNOUNCE %s/%d — executing: %s", ip, prefixLen, cmd)
	if err := sb.execCmd(ctx, cmd); err != nil {
		return fmt.Errorf("bgp announce %s: %w", ip, err)
	}

	now := time.Now().UTC()
	block := model.BGPBlock{
		IP:        ip,
		PrefixLen: prefixLen,
		Community: sb.cfg.Community,
		NextHop:   sb.cfg.NextHop,
		Reason:    reason,
		Status:    "active",
		BlockedAt: now,
	}
	if duration > 0 {
		exp := now.Add(duration)
		block.ExpiresAt = &exp
		block.DurationSeconds = uint32(duration.Seconds())
	}

	ar := &activeRoute{block: block}
	var timerCtx context.Context
	if duration > 0 {
		timerCtx, ar.cancel = context.WithCancel(context.Background())
	}
	sb.routes[ip] = ar
	if timerCtx != nil {
		sb.startWithdrawTimer(ip, duration, timerCtx)
	}

	log.Printf("bgp[script]: ANNOUNCE %s/%d succeeded (duration=%s, reason=%q)", ip, prefixLen, duration, reason)
	return nil
}

// Withdraw removes a blackhole route for the given target IP.
func (sb *ScriptBlocker) Withdraw(ctx context.Context, target net.IP) error {
	_, err := sb.withdraw(ctx, target, true)
	return err
}

// withdraw removes a route and reports whether it was present. cancelTimer is
// false when the timer itself performs the withdrawal.
func (sb *ScriptBlocker) withdraw(ctx context.Context, target net.IP, cancelTimer bool) (bool, error) {
	ip := target.String()

	sb.mu.Lock()
	defer sb.mu.Unlock()

	ar, exists := sb.routes[ip]
	if !exists {
		log.Printf("bgp[script]: %s not in active routes, nothing to withdraw", ip)
		return false, nil
	}

	prefixLen := ar.block.PrefixLen
	cmd := sb.buildWithdrawCmd(ip, prefixLen)

	log.Printf("bgp[script]: WITHDRAW %s/%d — executing: %s", ip, prefixLen, cmd)
	if err := sb.execCmd(ctx, cmd); err != nil {
		return false, fmt.Errorf("bgp withdraw %s: %w", ip, err)
	}

	// Do not cancel a pending timer until the command succeeds: cancellation
	// after a daemon error would turn a temporary failure into a permanent route.
	if cancelTimer && ar.cancel != nil {
		ar.cancel()
	}

	delete(sb.routes, ip)
	log.Printf("bgp[script]: WITHDRAW %s/%d succeeded", ip, prefixLen)
	return true, nil
}

// List returns currently active blackhole announcements.
func (sb *ScriptBlocker) List(ctx context.Context) ([]Announcement, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	out := make([]Announcement, 0, len(sb.routes))
	for _, ar := range sb.routes {
		a := Announcement{
			Target:    net.ParseIP(ar.block.IP),
			StartedAt: ar.block.BlockedAt,
			Reason:    ar.block.Reason,
		}
		if ar.block.ExpiresAt != nil {
			a.ExpiresAt = *ar.block.ExpiresAt
		}
		out = append(out, a)
	}
	return out, nil
}

// Status returns the current BGP session status. If a StatusCmd is
// configured, it executes the command and attempts to parse the output.
// Otherwise it returns a basic status derived from in-memory state.
func (sb *ScriptBlocker) Status(ctx context.Context) (*SessionStatus, error) {
	sb.mu.RLock()
	routeCount := len(sb.routes)
	sb.mu.RUnlock()

	st := &SessionStatus{
		Enabled:         true,
		PeerAddress:     sb.cfg.PeerAddress,
		PeerAS:          sb.cfg.PeerAS,
		LocalAS:         sb.cfg.LocalAS,
		RoutesAnnounced: routeCount,
	}

	if sb.cfg.StatusCmd == "" {
		// No status command configured — return what we know from memory.
		st.State = "unknown"
		return st, nil
	}

	cmd := sb.buildStatusCmd()
	out, err := sb.execCmdOutput(ctx, cmd)
	if err != nil {
		log.Printf("bgp[script]: status command failed: %v (output: %s)", err, out)
		st.State = "error"
		return st, nil // non-fatal: return partial status
	}

	// Best-effort JSON parsing (gobgp neighbor --json returns a JSON array).
	sb.parseStatusOutput(out, st)
	return st, nil
}

// Stop cancels all pending auto-withdraw timers and waits for background
// goroutines to finish.
func (sb *ScriptBlocker) Stop() {
	close(sb.stopCh)
	sb.wg.Wait()
	log.Printf("bgp[script]: stopped, %d route(s) remain announced (will persist until daemon restart)", len(sb.routes))
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildAnnounceCmd substitutes template placeholders in the announce command.
func (sb *ScriptBlocker) buildAnnounceCmd(ip string, prefixLen uint8) string {
	return sb.expandTemplate(sb.cfg.AnnounceCmd, ip, prefixLen)
}

// buildWithdrawCmd substitutes template placeholders in the withdraw command.
func (sb *ScriptBlocker) buildWithdrawCmd(ip string, prefixLen uint8) string {
	return sb.expandTemplate(sb.cfg.WithdrawCmd, ip, prefixLen)
}

// buildStatusCmd substitutes template placeholders in the status command.
func (sb *ScriptBlocker) buildStatusCmd() string {
	return sb.expandTemplate(sb.cfg.StatusCmd, "", 0)
}

// expandTemplate replaces {ip}, {prefix_len}, {family}, {community}, {next_hop},
// {peer_address} in the given template string.
func (sb *ScriptBlocker) expandTemplate(tmpl string, ip string, prefixLen uint8) string {
	family := addressFamily(net.ParseIP(ip))
	r := strings.NewReplacer(
		"{ip}", ip,
		"{prefix_len}", fmt.Sprintf("%d", prefixLen),
		"{family}", family,
		"{community}", sb.cfg.Community,
		"{next_hop}", sb.cfg.NextHop,
		"{peer_address}", sb.cfg.PeerAddress,
	)
	return r.Replace(tmpl)
}

// execCmd runs a shell command and returns an error if it fails.
func (sb *ScriptBlocker) execCmd(ctx context.Context, cmdStr string) error {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %q failed: %w (output: %s)", cmdStr, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// execCmdOutput runs a shell command and returns its combined output.
func (sb *ScriptBlocker) execCmdOutput(ctx context.Context, cmdStr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// startWithdrawTimer auto-withdraws a route and persists its expiration. Both
// daemon and store failures are retried until success or shutdown.
func (sb *ScriptBlocker) startWithdrawTimer(ip string, after time.Duration, timerCtx context.Context) {
	sb.wg.Add(1)
	go func() {
		defer sb.wg.Done()

		delay := after
		for {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				log.Printf("bgp[script]: auto-withdraw timer fired for %s", ip)
				wCtx, wCancel := context.WithTimeout(context.Background(), cmdTimeout)
				withdrawn, err := sb.withdraw(wCtx, net.ParseIP(ip), false)
				wCancel()
				if err != nil {
					log.Printf("bgp[script]: auto-withdraw %s failed; retrying in %s: %v", ip, autoWithdrawRetryDelay, err)
					delay = autoWithdrawRetryDelay
					continue
				}
				if !withdrawn {
					return
				}

				for {
					pCtx, pCancel := context.WithTimeout(context.Background(), cmdTimeout)
					err = sb.store.WithdrawBlock(pCtx, ip, "system", "block duration expired")
					pCancel()
					if err == nil {
						return
					}
					log.Printf("bgp[script]: persist expiration for %s failed; retrying in %s: %v", ip, autoWithdrawRetryDelay, err)
					persistTimer := time.NewTimer(autoWithdrawRetryDelay)
					select {
					case <-persistTimer.C:
					case <-sb.stopCh:
						persistTimer.Stop()
						return
					}
				}
			case <-timerCtx.Done():
				timer.Stop()
				return
			case <-sb.stopCh:
				timer.Stop()
				return
			}
		}
	}()
}

// parseStatusOutput attempts to parse gobgp-style JSON output into the
// SessionStatus struct. Falls back gracefully if the format is unexpected.
func (sb *ScriptBlocker) parseStatusOutput(raw string, st *SessionStatus) {
	// gobgp neighbor <addr> --json returns an array like:
	// [{"state":{"peer-as":65001,"neighbor-address":"10.0.0.1",
	//   "session-state":"established","admin-state":"up",
	//   "messages":{...},"queues":{...}},
	//   "timers":{"state":{"uptime":"2024-01-01T00:00:00Z",...}}}]
	//
	// We do best-effort extraction. If parsing fails entirely, we leave
	// the state as "unknown" and return.

	raw = strings.TrimSpace(raw)
	if raw == "" {
		st.State = "unknown"
		return
	}

	// Try parsing as a JSON array (gobgp format).
	var neighbors []map[string]any
	if err := json.Unmarshal([]byte(raw), &neighbors); err == nil && len(neighbors) > 0 {
		sb.parseGoBGPNeighbor(neighbors[0], st)
		return
	}

	// Try parsing as a single JSON object.
	var single map[string]any
	if err := json.Unmarshal([]byte(raw), &single); err == nil {
		sb.parseGoBGPNeighbor(single, st)
		return
	}

	// Plain text fallback — look for common state keywords.
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "established"):
		st.State = "established"
	case strings.Contains(lower, "idle"):
		st.State = "idle"
	case strings.Contains(lower, "active"):
		st.State = "active"
	case strings.Contains(lower, "opensent"):
		st.State = "opensent"
	case strings.Contains(lower, "openconfirm"):
		st.State = "openconfirm"
	case strings.Contains(lower, "connect"):
		st.State = "connect"
	default:
		st.State = "unknown"
	}
}

// parseGoBGPNeighbor extracts fields from a gobgp neighbor JSON object.
func (sb *ScriptBlocker) parseGoBGPNeighbor(obj map[string]any, st *SessionStatus) {
	// Navigate the nested structure: state.session-state
	if stateObj, ok := obj["state"].(map[string]any); ok {
		if ss, ok := stateObj["session-state"].(string); ok {
			st.State = strings.ToLower(ss)
		}
		if pa, ok := stateObj["neighbor-address"].(string); ok && st.PeerAddress == "" {
			st.PeerAddress = pa
		}
		if peerAS, ok := stateObj["peer-as"].(float64); ok && st.PeerAS == 0 {
			st.PeerAS = uint32(peerAS)
		}
	}

	// Timers: try to extract uptime.
	if timersObj, ok := obj["timers"].(map[string]any); ok {
		if timerState, ok := timersObj["state"].(map[string]any); ok {
			if uptimeStr, ok := timerState["uptime"].(string); ok {
				if t, err := time.Parse(time.RFC3339, uptimeStr); err == nil {
					st.Uptime = int64(time.Since(t).Seconds())
				}
			}
			// Some versions use a numeric uptime in seconds.
			if uptimeNum, ok := timerState["uptime"].(float64); ok {
				st.Uptime = int64(uptimeNum)
			}
		}
	}

	// If state is still empty, mark as unknown.
	if st.State == "" {
		st.State = "unknown"
	}
}

func addressFamily(ip net.IP) string {
	if ip != nil && ip.To4() == nil {
		return "ipv6"
	}
	return "ipv4"
}
