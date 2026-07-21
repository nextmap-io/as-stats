package store

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// convDimension describes how one conversation grouping dimension (F3) maps to
// fixed SQL column expressions. Every field is a hardcoded fragment — the
// operator only ever picks a whitelisted KEY (src_dst_ip / src_dst_as /
// dst_port_proto), never supplies raw SQL. All column refs are qualified with
// the table alias `t` so the ts/timestamp filter is never shadowed by an alias
// and so buildLinkFilter (which references `t.link_tag`) composes cleanly.
type convDimension struct {
	// aExpr / bExpr produce the two endpoint labels. For the symmetric IP/AS
	// dimensions they use least()/greatest() so A↔B is canonical and a pair is
	// deduped regardless of who was the flow source.
	aExpr string
	bExpr string
	// fwdExpr is the predicate identifying the A→B ("forward") direction, i.e.
	// the row whose source is the canonically-lower endpoint. For dst_port_proto
	// there is no reverse, so everything counts as forward.
	fwdExpr string
	// cleanIP strips the ::ffff: prefix from the endpoint labels (IP dimension).
	cleanIP bool
}

// convDimensions is the whitelist of conversation grouping dimensions. It is the
// ONLY place a dimension string becomes SQL.
var convDimensions = map[string]convDimension{
	"src_dst_ip": {
		aExpr:   "toString(least(t.src_ip, t.dst_ip))",
		bExpr:   "toString(greatest(t.src_ip, t.dst_ip))",
		fwdExpr: "t.src_ip <= t.dst_ip",
		cleanIP: true,
	},
	"src_dst_as": {
		aExpr:   "toString(least(t.src_as, t.dst_as))",
		bExpr:   "toString(greatest(t.src_as, t.dst_as))",
		fwdExpr: "t.src_as <= t.dst_as",
	},
	"dst_port_proto": {
		aExpr:   "toString(t.protocol)",
		bExpr:   "toString(t.dst_port)",
		fwdExpr: "1 = 1",
	},
}

// convDimensionFor resolves a (possibly untrusted) dimension name to its fixed
// column mapping. The second return is false for anything not whitelisted;
// callers must reject unknown dimensions rather than defaulting silently.
func convDimensionFor(dim string) (convDimension, bool) {
	d, ok := convDimensions[dim]
	return d, ok
}

// Conversations returns bidirectional top-talker rows for the chosen dimension
// (F3). A→B and B→A are folded into a single row via a canonical endpoint
// ordering. It reads flows_log when the flow-search feature is enabled
// (useFlowLog=true) and flows_raw otherwise; flows_raw counts are scaled by
// sampling_rate. Results are bounded by p.Limit and ordered by total bytes.
func (s *ClickHouseStore) Conversations(ctx context.Context, p QueryParams, dim string, useFlowLog bool) ([]model.Conversation, error) {
	d, ok := convDimensionFor(dim)
	if !ok {
		return nil, fmt.Errorf("invalid conversation dimension: %q", dim)
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	// Source table differs in ts column, byte scaling and flow counting.
	var table, tsCol, byteExpr, pktExpr, flowExpr string
	if useFlowLog {
		table, tsCol = "flows_log", "ts"
		byteExpr, pktExpr = "t.bytes", "t.packets"
		flowExpr = "sum(t.flow_count)"
	} else {
		table, tsCol = "flows_raw", "timestamp"
		byteExpr, pktExpr = "t.bytes * t.sampling_rate", "t.packets * t.sampling_rate"
		flowExpr = "count()"
	}

	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			%s AS endpoint_a,
			%s AS endpoint_b,
			sum(%s) AS total_bytes,
			sum(%s) AS total_packets,
			sumIf(%s, %s) AS fwd_bytes,
			sumIf(%s, %s) AS fwd_packets,
			sumIf(%s, NOT (%s)) AS rev_bytes,
			sumIf(%s, NOT (%s)) AS rev_packets,
			%s AS total_flows
		FROM %s t
		WHERE t.%s >= @from AND t.%s < @to
		%s
		GROUP BY endpoint_a, endpoint_b
		ORDER BY total_bytes DESC
		LIMIT @limit
	`,
		d.aExpr, d.bExpr,
		byteExpr, pktExpr,
		byteExpr, d.fwdExpr,
		pktExpr, d.fwdExpr,
		byteExpr, d.fwdExpr,
		pktExpr, d.fwdExpr,
		flowExpr,
		table,
		tsCol, tsCol,
		linkFilter,
	)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", limit),
	}, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Conversation
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(
			&c.EndpointA, &c.EndpointB,
			&c.TotalBytes, &c.TotalPackets,
			&c.ForwardBytes, &c.ForwardPackets,
			&c.ReverseBytes, &c.ReversePackets,
			&c.Flows,
		); err != nil {
			return nil, err
		}
		if d.cleanIP {
			c.EndpointA = cleanIPv4Mapped(c.EndpointA)
			c.EndpointB = cleanIPv4Mapped(c.EndpointB)
		}
		results = append(results, c)
	}
	return results, nil
}
