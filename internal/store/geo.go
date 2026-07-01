package store

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// TopCountry aggregates AS traffic to the country level by joining the AS
// traffic table to as_names.country (AS-level geo — no per-IP lookup). ASes
// whose country is not populated are grouped under "Unknown", so the query
// degrades gracefully when the as_names.country column is empty.
//
// Alias-shadowing rule: the aggregate is grouped by the derived country label
// while the range filter stays on the qualified source column t.ts, so no
// aggregate alias is referenced in WHERE.
func (s *ClickHouseStore) TopCountry(ctx context.Context, p QueryParams) ([]model.CountryTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			if(an.country = '', 'Unknown', an.country) AS country_code,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_as t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to
		%s %s
		GROUP BY country_code
		ORDER BY total_bytes DESC
		LIMIT @limit OFFSET @offset
	`, dirFilter, linkFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
		clickhouse.Named("offset", p.Offset),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query top country: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.CountryTraffic
	for rows.Next() {
		var r model.CountryTraffic
		if err := rows.Scan(&r.Country, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Total bytes over the same window for percentage calculation.
	totalQuery := fmt.Sprintf(`
		SELECT sum(bytes) FROM traffic_by_as
		WHERE ts >= @from AND ts < @to %s %s
	`, dirFilter, linkFilter)
	totalArgs := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, dirArgs...)
	totalArgs = append(totalArgs, linkArgs...)

	var totalBytes uint64
	if err := s.conn.QueryRow(ctx, totalQuery, totalArgs...).Scan(&totalBytes); err != nil {
		totalBytes = 0
	}
	for i := range results {
		if totalBytes > 0 {
			results[i].Percent = float64(results[i].Bytes) / float64(totalBytes) * 100
		}
	}

	return results, totalBytes, nil
}
