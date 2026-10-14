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

	// Aggregate per ASN first, then join the deduplicated names (names.go)
	// and roll up to the country: joining raw as_names before the GROUP BY
	// inflated every country holding a not-yet-merged renamed AS.
	query := fmt.Sprintf(`
		SELECT
			if(an.country = '', 'Unknown', an.country) AS country_code,
			sum(a.bytes) AS total_bytes,
			sum(a.packets) AS total_packets,
			sum(a.flows) AS total_flows
		FROM (
			SELECT t.as_number AS as_number,
				sum(t.bytes) AS bytes,
				sum(t.packets) AS packets,
				sum(t.flow_count) AS flows
			FROM traffic_by_as t
			WHERE t.ts >= @from AND t.ts < @to
			%s %s
			GROUP BY t.as_number
		) a
		LEFT JOIN %s an ON a.as_number = an.as_number
		GROUP BY country_code
		ORDER BY total_bytes DESC, country_code
		LIMIT @limit OFFSET @offset
	`, dirFilter, linkFilter, asNamesLatest)

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

	// Total bytes over the same window for percentage calculation. Keep the `t`
	// alias: dirFilter/linkFilter reference t.direction / t.link_tag, and an
	// unaliased FROM makes this query error out — silently zeroing every
	// percentage as soon as a direction or link filter is applied.
	totalQuery := fmt.Sprintf(`
		SELECT sum(t.bytes) FROM traffic_by_as t
		WHERE t.ts >= @from AND t.ts < @to %s %s
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
