// Package bgp provides an interface for BGP blackhole actions.
//
// Implementations:
//   - NoopBlocker: logs only, no real BGP (default when BGP_ENABLED=false).
//   - ScriptBlocker: executes shell commands (gobgp CLI, BIRD, FRRouting, etc.).
//   - RemoteBlocker: HTTP client that proxies calls to the API server (used by the collector).
//
// The Blocker interface lets the alert handler trigger a block without
// knowing the underlying implementation.
package bgp

import (
	"context"
	"log"
	"net"
	"time"
)

// Blocker triggers and withdraws BGP blackhole announcements.
type Blocker interface {
	// Announce tells upstream providers to drop traffic to the target IP
	// by advertising a /32 (or /128 for IPv6) with the RFC 7999 blackhole
	// community (65535:666).
	//
	// The announcement is automatically withdrawn after `duration`.
	// Announcing the same IP twice is idempotent (no-op if already active).
	Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error

	// Withdraw immediately withdraws a blackhole announcement.
	Withdraw(ctx context.Context, target net.IP) error

	// List returns currently active blackhole announcements.
	List(ctx context.Context) ([]Announcement, error)

	// Status returns the current BGP session status.
	Status(ctx context.Context) (*SessionStatus, error)
}

// Announcement represents an active BGP blackhole.
type Announcement struct {
	Target    net.IP
	StartedAt time.Time
	ExpiresAt time.Time
	Reason    string
}

// SessionStatus describes the state of the BGP session / daemon.
type SessionStatus struct {
	Enabled         bool   `json:"enabled"`
	PeerAddress     string `json:"peer_address,omitempty"`
	PeerAS          uint32 `json:"peer_as,omitempty"`
	LocalAS         uint32 `json:"local_as,omitempty"`
	State           string `json:"state"`            // "established", "idle", "disabled", etc.
	Uptime          int64  `json:"uptime"`           // seconds
	RoutesAnnounced int    `json:"routes_announced"`
}

// NoopBlocker logs but does nothing. Default when no real BGP backend
// is configured. Used to validate the full code path without touching
// production routers.
type NoopBlocker struct{}

// NewNoop returns a no-op Blocker.
func NewNoop() *NoopBlocker {
	return &NoopBlocker{}
}

func (b *NoopBlocker) Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error {
	log.Printf("bgp[noop]: ANNOUNCE blackhole for %s (duration=%s, reason=%q)", target, duration, reason)
	return nil
}

func (b *NoopBlocker) Withdraw(ctx context.Context, target net.IP) error {
	log.Printf("bgp[noop]: WITHDRAW blackhole for %s", target)
	return nil
}

func (b *NoopBlocker) List(ctx context.Context) ([]Announcement, error) {
	return nil, nil
}

func (b *NoopBlocker) Status(ctx context.Context) (*SessionStatus, error) {
	return &SessionStatus{Enabled: false, State: "disabled"}, nil
}
