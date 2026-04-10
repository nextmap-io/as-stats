package bgp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// RemoteBlocker implements Blocker by making HTTP calls to the API server.
// This is used by the collector's alert engine to trigger blocks via the
// API when the real BGP daemon is managed by the API process.
//
// No authentication is applied — the collector and API are assumed to be
// on the same host or trusted network.
type RemoteBlocker struct {
	apiURL string       // base URL, e.g. "http://localhost:8080"
	client *http.Client
}

// NewRemote creates a RemoteBlocker that talks to the API server at the
// given base URL. The URL should NOT have a trailing slash.
func NewRemote(apiURL string) *RemoteBlocker {
	apiURL = strings.TrimRight(apiURL, "/")
	return &RemoteBlocker{
		apiURL: apiURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Announce sends a POST to the API server to create a BGP blackhole block.
func (rb *RemoteBlocker) Announce(ctx context.Context, target net.IP, duration time.Duration, reason string) error {
	body := map[string]any{
		"ip":               target.String(),
		"duration_minutes": int(duration.Minutes()),
		"reason":           reason,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("bgp remote: marshal announce body: %w", err)
	}

	url := rb.apiURL + "/api/v1/bgp/blocks"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("bgp remote: create announce request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rb.client.Do(req)
	if err != nil {
		return fmt.Errorf("bgp remote: POST %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("bgp remote: POST %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	log.Printf("bgp[remote]: ANNOUNCE %s (duration=%s, reason=%q) -> %d", target, duration, reason, resp.StatusCode)
	return nil
}

// Withdraw sends a DELETE to the API server to remove a BGP blackhole block.
func (rb *RemoteBlocker) Withdraw(ctx context.Context, target net.IP) error {
	ip := target.String()
	url := rb.apiURL + "/api/v1/bgp/blocks/" + ip

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("bgp remote: create withdraw request: %w", err)
	}

	resp, err := rb.client.Do(req)
	if err != nil {
		return fmt.Errorf("bgp remote: DELETE %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("bgp remote: DELETE %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	log.Printf("bgp[remote]: WITHDRAW %s -> %d", ip, resp.StatusCode)
	return nil
}

// List fetches the list of active BGP blackhole blocks from the API server.
func (rb *RemoteBlocker) List(ctx context.Context) ([]Announcement, error) {
	url := rb.apiURL + "/api/v1/bgp/blocks"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bgp remote: create list request: %w", err)
	}

	resp, err := rb.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bgp remote: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("bgp remote: GET %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	// The API returns {"data": [...]}, so we need to unwrap.
	var envelope struct {
		Data []struct {
			IP        string     `json:"ip"`
			Reason    string     `json:"reason"`
			BlockedAt time.Time  `json:"blocked_at"`
			ExpiresAt *time.Time `json:"expires_at,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("bgp remote: decode list response: %w", err)
	}

	out := make([]Announcement, 0, len(envelope.Data))
	for _, b := range envelope.Data {
		a := Announcement{
			Target:    net.ParseIP(b.IP),
			StartedAt: b.BlockedAt,
			Reason:    b.Reason,
		}
		if b.ExpiresAt != nil {
			a.ExpiresAt = *b.ExpiresAt
		}
		out = append(out, a)
	}

	return out, nil
}

// Status fetches the BGP session status from the API server.
func (rb *RemoteBlocker) Status(ctx context.Context) (*SessionStatus, error) {
	url := rb.apiURL + "/api/v1/bgp/status"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("bgp remote: create status request: %w", err)
	}

	resp, err := rb.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bgp remote: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("bgp remote: GET %s returned %d: %s", url, resp.StatusCode, string(respBody))
	}

	// The API returns {"data": {...}}, so we unwrap.
	var envelope struct {
		Data SessionStatus `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("bgp remote: decode status response: %w", err)
	}

	return &envelope.Data, nil
}
