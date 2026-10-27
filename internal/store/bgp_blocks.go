package store

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// bgp_blocks is a plain MergeTree written by one process only: the API's
// bgp.Service, which serialises every change. A block is INSERTed once and
// its status changes with a synchronous ALTER … UPDATE keyed by id (the table
// holds a few rows a day). There is no ReplacingMergeTree version to collapse,
// so reads need no FINAL — but legacy versions could insert the same IP twice
// (API + engine), so the service groups active rows by prefix and every
// status change names all their ids.

const bgpBlockColumns = `id, toString(ip), prefix_len, community, next_hop,
	reason, description, status,
	blocked_by, blocked_at, unblocked_by, unblocked_at, unblock_reason,
	alert_id, rule_name, metric_value, metric_type, threshold, top_sources,
	duration_seconds, expires_at, dry_run`

const zeroUUID = "00000000-0000-0000-0000-000000000000"

// InsertBlock persists a new BGP block record.
func (s *ClickHouseStore) InsertBlock(ctx context.Context, b model.BGPBlock) error {
	ip, prefixLen, err := normalizeBlockTarget(b.IP, b.PrefixLen)
	if err != nil {
		return err
	}
	expiresAt := time.Unix(0, 0).UTC()
	if b.ExpiresAt != nil {
		expiresAt = b.ExpiresAt.UTC()
	}
	alertID := b.AlertID
	if alertID == "" {
		alertID = zeroUUID
	}
	topSources := b.TopSources
	if topSources == nil {
		topSources = []string{}
	}
	var dry uint8
	if b.DryRun {
		dry = 1
	}
	return s.conn.Exec(ctx, `
		INSERT INTO bgp_blocks (
			id, ip, prefix_len, community, next_hop,
			reason, description, status,
			blocked_by, blocked_at,
			alert_id, rule_name, metric_value, metric_type, threshold, top_sources,
			duration_seconds, expires_at, dry_run
		) VALUES (
			toUUID(@id), toIPv6(@ip), @prefix_len, @community, @next_hop,
			@reason, @description, @status,
			@blocked_by, @blocked_at,
			toUUIDOrZero(@alert_id), @rule_name, @metric_value, @metric_type, @threshold, @top_sources,
			@duration_seconds, @expires_at, @dry_run
		)`,
		clickhouse.Named("id", b.ID),
		clickhouse.Named("ip", ip),
		clickhouse.Named("prefix_len", prefixLen),
		clickhouse.Named("community", b.Community),
		clickhouse.Named("next_hop", b.NextHop),
		clickhouse.Named("reason", b.Reason),
		clickhouse.Named("description", b.Description),
		clickhouse.Named("status", b.Status),
		clickhouse.Named("blocked_by", b.BlockedBy),
		clickhouse.Named("blocked_at", b.BlockedAt.UTC()),
		clickhouse.Named("alert_id", alertID),
		clickhouse.Named("rule_name", b.RuleName),
		clickhouse.Named("metric_value", b.MetricValue),
		clickhouse.Named("metric_type", b.MetricType),
		clickhouse.Named("threshold", b.Threshold),
		clickhouse.Named("top_sources", topSources),
		clickhouse.Named("duration_seconds", b.DurationSeconds),
		clickhouse.Named("expires_at", expiresAt),
		clickhouse.Named("dry_run", dry),
	)
}

// SetBlockStatus moves the given active rows to status ('withdrawn' or
// 'failed'). It is a synchronous mutation (mutations_sync = 1): when it
// returns, a restart cannot read the rows as active any more. Rows already
// out of 'active' are left alone.
func (s *ClickHouseStore) SetBlockStatus(ctx context.Context, ids []string, status, by, reason string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	if status != "withdrawn" && status != "failed" {
		return fmt.Errorf("invalid block status %q", status)
	}
	return s.conn.Exec(ctx, `
		ALTER TABLE bgp_blocks UPDATE
			status = @status,
			unblocked_by = @by,
			unblocked_at = @at,
			unblock_reason = @reason
		WHERE has(@ids, toString(id)) AND status = 'active'
		SETTINGS mutations_sync = 1`,
		clickhouse.Named("status", status),
		clickhouse.Named("by", by),
		clickhouse.Named("at", at.UTC()),
		clickhouse.Named("reason", reason),
		clickhouse.Named("ids", ids),
	)
}

// ListActiveBlocks returns every row with status = 'active', oldest first.
func (s *ClickHouseStore) ListActiveBlocks(ctx context.Context) ([]model.BGPBlock, error) {
	return s.queryBlocks(ctx, `
		SELECT `+bgpBlockColumns+`
		FROM bgp_blocks AS b
		WHERE b.status = 'active'
		ORDER BY b.blocked_at, b.id`)
}

// ListBlocksSince returns the rows created at or after since (any status),
// used to seed the auto-blocks-per-hour rail after a restart.
func (s *ClickHouseStore) ListBlocksSince(ctx context.Context, since time.Time) ([]model.BGPBlock, error) {
	return s.queryBlocks(ctx, `
		SELECT `+bgpBlockColumns+`
		FROM bgp_blocks AS b
		WHERE b.blocked_at >= @since
		ORDER BY b.blocked_at, b.id`,
		clickhouse.Named("since", since.UTC()))
}

// ListBlockHistory returns the most recent block records (any status).
func (s *ClickHouseStore) ListBlockHistory(ctx context.Context, limit int) ([]model.BGPBlock, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	return s.queryBlocks(ctx, `
		SELECT `+bgpBlockColumns+`
		FROM bgp_blocks AS b
		ORDER BY b.blocked_at DESC, b.id
		LIMIT @limit`,
		clickhouse.Named("limit", limit))
}

func (s *ClickHouseStore) queryBlocks(ctx context.Context, query string, args ...any) ([]model.BGPBlock, error) {
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query bgp_blocks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := []model.BGPBlock{}
	for rows.Next() {
		var b model.BGPBlock
		var unblockedAt, expiresAt time.Time
		var alertID string
		var dry uint8
		if err := rows.Scan(
			&b.ID, &b.IP, &b.PrefixLen, &b.Community, &b.NextHop,
			&b.Reason, &b.Description, &b.Status,
			&b.BlockedBy, &b.BlockedAt, &b.UnblockedBy, &unblockedAt, &b.UnblockReason,
			&alertID, &b.RuleName, &b.MetricValue, &b.MetricType, &b.Threshold, &b.TopSources,
			&b.DurationSeconds, &expiresAt, &dry,
		); err != nil {
			return nil, err
		}
		if ip, pl, err := normalizeBlockTarget(b.IP, b.PrefixLen); err == nil {
			b.IP, b.PrefixLen = ip, pl
		}
		b.BlockedAt = b.BlockedAt.UTC()
		if unblockedAt.Unix() > 0 {
			t := unblockedAt.UTC()
			b.UnblockedAt = &t
		}
		if expiresAt.Unix() > 0 {
			t := expiresAt.UTC()
			b.ExpiresAt = &t
		}
		if alertID != zeroUUID {
			b.AlertID = alertID
		}
		b.DryRun = dry == 1
		results = append(results, b)
	}
	return results, rows.Err()
}

// normalizeBlockTarget canonicalises a stored target: IPv4-mapped addresses
// back to dotted quad, the prefix length in the address's own family. A
// historical bug stored IPv6 host routes as /32; no IPv6 block shorter than
// /48 can exist, so those rows are host routes.
func normalizeBlockTarget(raw string, prefixLen uint8) (string, uint8, error) {
	a, err := netip.ParseAddr(raw)
	if err != nil {
		return "", 0, fmt.Errorf("invalid BGP block IP %q", raw)
	}
	if a.Is4In6() {
		a = a.Unmap()
		if prefixLen > 32 {
			prefixLen -= 96
		}
	}
	switch {
	case a.Is4() && (prefixLen == 0 || prefixLen > 32):
		prefixLen = 32
	case a.Is6() && (prefixLen < 48 || prefixLen > 128):
		prefixLen = 128
	}
	return a.String(), prefixLen, nil
}
