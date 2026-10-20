package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// Port statistics (traffic_by_port, rebuilt by migration 000018).
//
// One row per flow: direction is flows_raw.direction for flows credited to a
// configured link ('in' = entered our network on that link, 'out' = left it)
// and empty otherwise; port is the service port (ServicePort). The pre-3.5
// table, whose rows counted every flow twice with a direction that only said
// which port column had been copied, is kept as traffic_by_port_legacy until
// it ages out and is never read: its numbers do not mean the same thing.
const portStatsTable = "traffic_by_port"

// ServicePort is the Go mirror of the port expression of traffic_by_port_mv
// (migration 000018) — the port that identifies the service of a flow:
//   - ICMP / ICMPv6 (1 / 58) carry type/code, not ports: 0;
//   - both ports set: the lower one, since the client side of a connection
//     uses an ephemeral (high) port — 51000→443 and 443→51000 both give 443;
//   - one port 0: the other one; both 0 (GRE, ESP, …): 0.
func ServicePort(protocol uint8, srcPort, dstPort uint16) uint16 {
	switch {
	case protocol == 1 || protocol == 58:
		return 0
	case srcPort == 0:
		return dstPort
	case dstPort == 0:
		return srcPort
	default:
		return min(srcPort, dstPort)
	}
}

// portDirectionFilter restricts a traffic_by_port read to one direction, or to
// the link-attributed rows ('in' and 'out') when none is asked for: rows stored
// with an empty direction belong to no configured link and are listed under neither
// Inbound nor Outbound (as in the link totals).
func portDirectionFilter(dir string) string {
	switch dir {
	case "in":
		return "AND t.direction = 'in'"
	case "out":
		return "AND t.direction = 'out'"
	default:
		return "AND t.direction IN ('in', 'out')"
	}
}

// TopProtocols returns the traffic per (protocol, direction). Percent is the
// row's share of its own direction, so the Inbound and Outbound lists each add
// up to 100 %.
func (s *ClickHouseStore) TopProtocols(ctx context.Context, p QueryParams) ([]model.ProtocolTraffic, error) {
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			t.protocol AS protocol,
			t.direction AS direction,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM %s t
		WHERE t.ts >= @from AND t.ts < @to
		%s %s
		GROUP BY t.protocol, t.direction
		ORDER BY total_bytes DESC, protocol, direction
	`, portStatsTable, portDirectionFilter(p.Direction), linkFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("top protocols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ProtocolTraffic
	totals := map[string]uint64{}
	for rows.Next() {
		var r model.ProtocolTraffic
		if err := rows.Scan(&r.Protocol, &r.Direction, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		totals[r.Direction] += r.Bytes
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range results {
		if t := totals[results[i].Direction]; t > 0 {
			results[i].Percent = float64(results[i].Bytes) / float64(t) * 100
		}
	}
	return results, nil
}

// TopPorts returns the top (protocol, service port) tuples by traffic volume,
// ranked and limited per direction. Portless rows (port 0) are left out except
// ICMP / ICMPv6, which are stored with port 0 by design. Percent is the share
// of the direction's total under the same filters (protocol included), not of
// the returned page.
func (s *ClickHouseStore) TopPorts(ctx context.Context, p QueryParams, protocol uint8) ([]model.PortTraffic, error) {
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	var protoArgs []any
	protoFilter := ""
	if protocol > 0 {
		protoFilter = "AND t.protocol = @protocol"
		protoArgs = append(protoArgs, clickhouse.Named("protocol", protocol))
	}

	// The window total runs over the grouped rows before LIMIT … BY, so it is
	// the whole direction's traffic.
	query := fmt.Sprintf(`
		SELECT protocol, port, direction, total_bytes, total_packets, total_flows, dir_bytes
		FROM (
			SELECT
				t.protocol AS protocol,
				t.port AS port,
				t.direction AS direction,
				sum(t.bytes) AS total_bytes,
				sum(t.packets) AS total_packets,
				sum(t.flow_count) AS total_flows,
				sum(total_bytes) OVER (PARTITION BY direction) AS dir_bytes
			FROM %s t
			WHERE t.ts >= @from AND t.ts < @to
			  AND (t.port > 0 OR t.protocol IN (1, 58))
			%s %s %s
			GROUP BY t.protocol, t.port, t.direction
		)
		ORDER BY total_bytes DESC, protocol, port, direction
		LIMIT @offset, @limit BY direction
	`, portStatsTable, portDirectionFilter(p.Direction), linkFilter, protoFilter)

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", limit),
		clickhouse.Named("offset", p.Offset),
	}, linkArgs...)
	args = append(args, protoArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("top ports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.PortTraffic
	for rows.Next() {
		var r model.PortTraffic
		var dirBytes uint64
		if err := rows.Scan(&r.Protocol, &r.Port, &r.Direction, &r.Bytes, &r.Packets, &r.Flows, &dirBytes); err != nil {
			return nil, err
		}
		if dirBytes > 0 {
			r.Percent = float64(r.Bytes) / float64(dirBytes) * 100
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// PortHistorySince returns when the current port statistics begin if they
// restarted at the 3.5 upgrade (migration 000018), i.e. while
// traffic_by_port_legacy still holds data: the oldest part of traffic_by_port,
// or now() if it is still empty. It returns the zero time on a fresh install
// (nothing restarted) and once the legacy table has aged out. Reads
// system.parts only.
func (s *ClickHouseStore) PortHistorySince(ctx context.Context) (time.Time, error) {
	var legacyParts uint64
	var since time.Time
	err := s.conn.QueryRow(ctx, `
		SELECT
			(SELECT count() FROM system.parts
			 WHERE database = currentDatabase() AND table = 'traffic_by_port_legacy' AND active) AS legacy_parts,
			(SELECT if(count() = 0, now(), min(min_time)) FROM system.parts
			 WHERE database = currentDatabase() AND table = 'traffic_by_port' AND active) AS since
	`).Scan(&legacyParts, &since)
	if err != nil {
		return time.Time{}, fmt.Errorf("port history start: %w", err)
	}
	if legacyParts == 0 {
		return time.Time{}, nil
	}
	return since.UTC(), nil
}
