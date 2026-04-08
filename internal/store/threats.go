package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// LiveThreats returns the top destinations from traffic_by_dst_1min over the
// given window, exposing the metrics used by the alert engine (bps, pps, syn
// pps, unique source IPs). Only IPs inside localPrefixes are considered, so
// the page never shows destinations we don't own.
//
// This is a single aggregating query — much cheaper than running each alert
// rule's query separately. The result is unranked from the rule perspective:
// the handler computes status/percentages by comparing rows to the active
// rule set.
//
// windowSeconds is clamped to [60, 3600]. Rows are ordered by bytes desc and
// truncated to `limit` rows (default 50, max 200).
func (s *ClickHouseStore) LiveThreats(ctx context.Context, windowSeconds uint32, limit int, localPrefixes []string) ([]model.LiveThreat, error) {
	if windowSeconds < 60 {
		windowSeconds = 60
	}
	if windowSeconds > 3600 {
		windowSeconds = 3600
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", windowSeconds),
		clickhouse.Named("limit", limit),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "lt_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip)                          AS target,
			toUInt64(sum(bytes) * 8 / @window)        AS bps,
			toUInt64(sum(packets) / @window)          AS pps,
			toUInt64(sum(syn_count) / @window)        AS syn_pps,
			toUInt64(uniqMerge(unique_src_ips))       AS unique_src_ips
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		ORDER BY bps DESC
		LIMIT @limit
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("live threats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.LiveThreat
	for rows.Next() {
		var t model.LiveThreat
		var target string
		if err := rows.Scan(&target, &t.BPS, &t.PPS, &t.SynPPS, &t.UniqueSourceIPs); err != nil {
			return nil, err
		}
		t.TargetIP = cleanIPv4Mapped(target)
		results = append(results, t)
	}
	return results, nil
}
