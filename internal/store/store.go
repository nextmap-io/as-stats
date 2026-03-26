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
	ASPeers(ctx context.Context, asn uint32, p QueryParams) ([]model.ASTraffic, error)
	ASTopIPs(ctx context.Context, asn uint32, p QueryParams) ([]model.IPTraffic, error)

	IPTimeSeries(ctx context.Context, ip string, p QueryParams) ([]model.TrafficPoint, error)
	IPTopAS(ctx context.Context, ip string, p QueryParams) ([]model.ASTraffic, error)

	LinkList(ctx context.Context, p QueryParams) ([]model.LinkTraffic, error)
	LinkTimeSeries(ctx context.Context, tag string, p QueryParams) ([]model.TrafficPoint, error)
	LinkTopAS(ctx context.Context, tag string, p QueryParams) ([]model.ASTraffic, uint64, error)

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
	From      time.Time
	To        time.Time
	LinkTags  []string
	Direction string // "in", "out", or "" for both
	Limit     int
	Offset    int
}

// DefaultQueryParams returns sensible defaults (last 24h, limit 20).
func DefaultQueryParams() QueryParams {
	return QueryParams{
		From:  time.Now().UTC().Add(-24 * time.Hour),
		To:    time.Now().UTC(),
		Limit: 20,
	}
}
