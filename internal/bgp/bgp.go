// Package bgp provides an interface for BGP blackhole actions.
//
// Phase 1: noop implementation (logs only, no real BGP).
// Phase 2 (future): ExaBGP or GoBGP backend that actually announces
// RFC 7999 BGP blackhole routes to upstream providers.
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
	Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error

	// Withdraw immediately withdraws a blackhole announcement.
	Withdraw(ctx context.Context, target net.IP) error

	// List returns currently active blackhole announcements.
	List(ctx context.Context) ([]Announcement, error)
}

// Announcement represents an active BGP blackhole.
type Announcement struct {
	Target    net.IP
	StartedAt time.Time
	ExpiresAt time.Time
	Reason    string
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
