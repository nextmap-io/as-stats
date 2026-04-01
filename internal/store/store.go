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

// FlowReader reads aggregated traffic data from storage.
type FlowReader interface {
	TopAS(ctx context.Context, p QueryParams) ([]model.ASTraffic, uint64, error)
	TopIP(ctx context.Context, p QueryParams) ([]model.IPTraffic, uint64, error)
	TopPrefix(ctx context.Context, p QueryParams) ([]model.PrefixTraffic, uint64, error)

	ASTimeSeries(ctx context.Context, asn uint32, p QueryParams) ([]model.TrafficPoint, error)
	ASLinkSeries(ctx context.Context, asn uint32, p QueryParams) ([]model.LinkTimeSeries, error)
	ASTotals(ctx context.Context, asn uint32, p QueryParams) (v4In, v4Out, v6In, v6Out uint64, err error)
	ASPeers(ctx context.Context, asn uint32, p QueryParams) ([]model.ASTraffic, error)
	ASTopIPs(ctx context.Context, asn uint32, p QueryParams) ([]model.IPTraffic, error)
	ASRemoteIPs(ctx context.Context, asn uint32, p QueryParams) ([]model.IPTraffic, error)

	IPTimeSeries(ctx context.Context, ip string, p QueryParams) ([]model.TrafficPoint, error)
	IPTopAS(ctx context.Context, ip string, p QueryParams) ([]model.ASTraffic, error)
	IPPeerIPs(ctx context.Context, ip string, p QueryParams) ([]model.IPTraffic, error)
	IPInfo(ctx context.Context, ip string) (asn uint32, asName string, prefix string, err error)

	LinkASTimeSeries(ctx context.Context, tag string, p QueryParams) ([]model.ASTrafficDetail, error)
	LinkList(ctx context.Context, p QueryParams) ([]model.LinkTraffic, error)
	LinkTimeSeries(ctx context.Context, tag string, p QueryParams) ([]model.TrafficPoint, error)
	LinkTopAS(ctx context.Context, tag string, p QueryParams) ([]model.ASTraffic, uint64, error)
	LinksTrafficSeries(ctx context.Context, p QueryParams) ([]model.LinkTimeSeries, error)
	LinksP95(ctx context.Context, p QueryParams) (inP95, outP95 uint64, err error)
	LinkP95(ctx context.Context, tag string, p QueryParams) (inP95, outP95 uint64, err error)
	TopASTrafficSeries(ctx context.Context, p QueryParams) ([]model.ASTrafficDetail, error)

	Overview(ctx context.Context, p QueryParams) (*model.Overview, error)

	SearchAS(ctx context.Context, query string, limit int) ([]model.ASInfo, error)

	Close() error
}

// LinkStore manages known link configuration.
type LinkStore interface {
	ListLinks(ctx context.Context) ([]model.Link, error)
	UpsertLink(ctx context.Context, link model.Link) error
	DeleteLink(ctx context.Context, tag string) error
}

// ASNameStore manages AS name lookups.
type ASNameStore interface {
	GetASName(ctx context.Context, asn uint32) (string, error)
	BulkUpsertASNames(ctx context.Context, names []model.ASInfo) error
}

// QueryParams holds common query parameters for all read operations.
type QueryParams struct {
	From           time.Time
	To             time.Time
	LinkTags       []string
	Direction      string // "in", "out", or "" for both
	IPVersion      uint8  // 0=all, 4=IPv4, 6=IPv6
	LocalIPFilter  string // SQL filter for local IPs (from ripestat.PrefixesToSQL)
	ExcludeAS      uint32   // AS to exclude from top results (local AS)
	LocalPrefixes  []string // Local CIDR prefixes for prefix scope filtering
	PrefixScope    string   // "internal" or "external"
	Limit          int
	Offset         int
}

// DefaultQueryParams returns sensible defaults (last 24h, limit 20).
func DefaultQueryParams() QueryParams {
	return QueryParams{
		From:  time.Now().UTC().Add(-24 * time.Hour),
		To:    time.Now().UTC(),
		Limit: 20,
	}
}
