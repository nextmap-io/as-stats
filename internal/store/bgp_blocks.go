package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// InsertBlock persists a new BGP block record.
func (s *ClickHouseStore) InsertBlock(ctx context.Context, b model.BGPBlock) error {
	expiresAt := time.Time{}
	if b.ExpiresAt != nil {
		expiresAt = *b.ExpiresAt
	}
	alertID := b.AlertID
	if alertID == "" {
		alertID = "00000000-0000-0000-0000-000000000000"
	}
	return s.conn.Exec(ctx, `
		INSERT INTO bgp_blocks (
			id, ip, prefix_len, community, next_hop,
			reason, description, status,
			blocked_by, blocked_at,
			alert_id, rule_name, metric_value, metric_type, threshold, top_sources,
			duration_seconds, expires_at
		) VALUES (
			@id, toIPv6(@ip), @prefix_len, @community, @next_hop,
			@reason, @description, @status,
			@blocked_by, @blocked_at,
			@alert_id, @rule_name, @metric_value, @metric_type, @threshold, @top_sources,
			@duration_seconds, @expires_at
		)`,
		clickhouse.Named("id", b.ID),
		clickhouse.Named("ip", b.IP),
		clickhouse.Named("prefix_len", b.PrefixLen),
		clickhouse.Named("community", b.Community),
		clickhouse.Named("next_hop", b.NextHop),
		clickhouse.Named("reason", b.Reason),
		clickhouse.Named("description", b.Description),
		clickhouse.Named("status", b.Status),
		clickhouse.Named("blocked_by", b.BlockedBy),
		clickhouse.Named("blocked_at", b.BlockedAt),
		clickhouse.Named("alert_id", alertID),
		clickhouse.Named("rule_name", b.RuleName),
		clickhouse.Named("metric_value", b.MetricValue),
		clickhouse.Named("metric_type", b.MetricType),
		clickhouse.Named("threshold", b.Threshold),
		clickhouse.Named("top_sources", b.TopSources),
		clickhouse.Named("duration_seconds", b.DurationSeconds),
		clickhouse.Named("expires_at", expiresAt),
	)
}

// WithdrawBlock marks an active block as withdrawn via ALTER TABLE UPDATE.
func (s *ClickHouseStore) WithdrawBlock(ctx context.Context, ip, unblockedBy, reason string) error {
	// Normalize IPv4 to mapped form for the WHERE clause
	probe := ip
	if !containsColon(ip) {
		probe = "::ffff:" + ip
	}
	return s.conn.Exec(ctx, `
		ALTER TABLE bgp_blocks UPDATE
			status = 'withdrawn',
			unblocked_by = @user,
			unblocked_at = @now,
			unblock_reason = @reason
		WHERE toString(ip) = @ip AND status = 'active'`,
		clickhouse.Named("user", unblockedBy),
		clickhouse.Named("now", time.Now().UTC()),
		clickhouse.Named("reason", reason),
		clickhouse.Named("ip", probe),
	)
}

// ListActiveBlocks returns all blocks with status='active'.
func (s *ClickHouseStore) ListActiveBlocks(ctx context.Context) ([]model.BGPBlock, error) {
	return s.queryBlocks(ctx, `
		SELECT id, toString(ip), prefix_len, community, next_hop,
			reason, description, status,
			blocked_by, blocked_at, unblocked_by, unblocked_at, unblock_reason,
			alert_id, rule_name, metric_value, metric_type, threshold, top_sources,
			duration_seconds, expires_at
		FROM bgp_blocks
		WHERE status = 'active'
		ORDER BY blocked_at DESC
	`)
}

// ListBlockHistory returns recent block records (active + withdrawn).
func (s *ClickHouseStore) ListBlockHistory(ctx context.Context, limit int) ([]model.BGPBlock, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	return s.queryBlocks(ctx, fmt.Sprintf(`
		SELECT id, toString(ip), prefix_len, community, next_hop,
			reason, description, status,
			blocked_by, blocked_at, unblocked_by, unblocked_at, unblock_reason,
			alert_id, rule_name, metric_value, metric_type, threshold, top_sources,
			duration_seconds, expires_at
		FROM bgp_blocks
		ORDER BY blocked_at DESC
		LIMIT %d
	`, limit))
}

// FindActiveBlock returns the block ID if an active block exists for the IP.
func (s *ClickHouseStore) FindActiveBlock(ctx context.Context, ip string) (string, error) {
	probe := ip
	if !containsColon(ip) {
		probe = "::ffff:" + ip
	}
	var id string
	err := s.conn.QueryRow(ctx, `
		SELECT id FROM bgp_blocks
		WHERE toString(ip) = @ip AND status = 'active'
		LIMIT 1
	`, clickhouse.Named("ip", probe)).Scan(&id)
	if err != nil {
		return "", nil // not found is not an error
	}
	return id, nil
}

func (s *ClickHouseStore) queryBlocks(ctx context.Context, query string) ([]model.BGPBlock, error) {
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query bgp_blocks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.BGPBlock
	for rows.Next() {
		var b model.BGPBlock
		var unblockedAt, expiresAt time.Time
		var alertID string
		if err := rows.Scan(
			&b.ID, &b.IP, &b.PrefixLen, &b.Community, &b.NextHop,
			&b.Reason, &b.Description, &b.Status,
			&b.BlockedBy, &b.BlockedAt, &b.UnblockedBy, &unblockedAt, &b.UnblockReason,
			&alertID, &b.RuleName, &b.MetricValue, &b.MetricType, &b.Threshold, &b.TopSources,
			&b.DurationSeconds, &expiresAt,
		); err != nil {
			return nil, err
		}
		b.IP = cleanIPv4Mapped(b.IP)
		if !unblockedAt.IsZero() && unblockedAt.Year() > 1970 {
			b.UnblockedAt = &unblockedAt
		}
		if !expiresAt.IsZero() && expiresAt.Year() > 1970 {
			b.ExpiresAt = &expiresAt
		}
		if alertID != "00000000-0000-0000-0000-000000000000" {
			b.AlertID = alertID
		}
		results = append(results, b)
	}
	return results, nil
}

func containsColon(s string) bool {
	for _, c := range s {
		if c == ':' {
			return true
		}
	}
	return false
}
