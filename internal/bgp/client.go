package bgp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/metrics"
)

// InternalBlockPath is the API route the collector's alert engine posts
// auto-blocks to. It lives outside /api/v1: no session, no CSRF cookie —
// only the shared BGP_API_TOKEN, compared in constant time. The bundled
// nginx only proxies /api/ and /auth/, so it is not reachable through the
// frontend.
const InternalBlockPath = "/internal/bgp/blocks"

// MinAPITokenLen is the shortest BGP_API_TOKEN accepted (e.g. the output
// of `openssl rand -hex 32` is 64).
const MinAPITokenLen = 32

// AutoBlockRequest is the body of POST /internal/bgp/blocks.
type AutoBlockRequest struct {
	IP              string   `json:"ip"`
	DurationMinutes int      `json:"duration_minutes"`
	Description     string   `json:"description"`
	AlertID         string   `json:"alert_id,omitempty"`
	RuleName        string   `json:"rule_name,omitempty"`
	MetricValue     float64  `json:"metric_value,omitempty"`
	MetricType      string   `json:"metric_type,omitempty"`
	Threshold       float64  `json:"threshold,omitempty"`
	TopSources      []string `json:"top_sources,omitempty"`
}

// ErrRefused is returned by Client.AutoBlock when the API answered but would
// not block (rails, policy, BGP off): retrying will not help.
var ErrRefused = errors.New("refused by the API")

// Client is the collector side of auto-blocking: the alert engine hands it a
// violation and it asks the API, which owns the BGP daemon and every rail.
type Client struct {
	apiURL string
	token  string
	http   *http.Client
}

// NewClient returns a client for the API at apiURL (e.g.
// "http://api:8080") authenticated with token (BGP_API_TOKEN).
func NewClient(apiURL, token string) (*Client, error) {
	if len(token) < MinAPITokenLen {
		return nil, fmt.Errorf("BGP_API_TOKEN must be at least %d characters", MinAPITokenLen)
	}
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if !strings.HasPrefix(apiURL, "http://") && !strings.HasPrefix(apiURL, "https://") {
		return nil, fmt.Errorf("BGP_API_URL must start with http:// or https://, got %q", apiURL)
	}
	return &Client{
		apiURL: apiURL,
		token:  token,
		http: &http.Client{
			Timeout: 70 * time.Second, // the API may run a 10 s command plus ClickHouse writes
			// Never follow a redirect with the bearer token to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// AutoBlock asks the API to blackhole req.IP. created is false when the
// target was already blocked.
func (c *Client) AutoBlock(ctx context.Context, req AutoBlockRequest) (created bool, err error) {
	body, err := json.Marshal(req)
	if err != nil {
		return false, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+InternalBlockPath, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(hreq)
	if err != nil {
		metrics.BGPAutoBlockRequests.WithLabelValues("error").Inc()
		return false, fmt.Errorf("bgp auto-block: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusCreated:
		metrics.BGPAutoBlockRequests.WithLabelValues("created").Inc()
		return true, nil
	case resp.StatusCode == http.StatusOK:
		metrics.BGPAutoBlockRequests.WithLabelValues("existing").Inc()
		return false, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		metrics.BGPAutoBlockRequests.WithLabelValues("refused").Inc()
		return false, fmt.Errorf("%w (%d): %s", ErrRefused, resp.StatusCode, strings.TrimSpace(string(msg)))
	default:
		metrics.BGPAutoBlockRequests.WithLabelValues("error").Inc()
		return false, fmt.Errorf("bgp auto-block: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
}
