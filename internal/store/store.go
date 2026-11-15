package store

import (
	"context"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// FlowWriter writes flow records to storage.
type FlowWriter interface {
	WriteBatch(ctx context.Context, flows []*model.FlowRecord) error
	Close() error
}

// QueryParams holds common query parameters for all read operations.
type QueryParams struct {
	From          time.Time
	To            time.Time
	LinkTags      []string
	Direction     string   // "in", "out", or "" for both
	IPVersion     uint8    // 0=all, 4=IPv4, 6=IPv6
	LocalIPFilter string   // SQL filter for local IPs on t.ip_address (from CIDRRangeSQL)
	ExcludeAS     uint32   // AS to exclude from top results (local AS)
	LocalPrefixes []string // Local CIDR prefixes for prefix scope filtering
	PrefixScope   string   // "internal" or "external"
	Metric        string   // Top-N sort metric: "bytes" (default), "packets", "flows" — whitelisted
	Limit         int
	Offset        int
}

// DefaultQueryParams returns sensible defaults (last 24h, limit 20).
func DefaultQueryParams() QueryParams {
	return QueryParams{
		From:  time.Now().UTC().Add(-24 * time.Hour),
		To:    time.Now().UTC(),
		Limit: 20,
	}
}
