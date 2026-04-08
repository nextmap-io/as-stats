package store

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// SearchFlowLog executes a forensic query against the flows_log table.
// Filters are additive — empty fields are ignored.
// Returns up to p.Limit rows, sorted by bytes (or ts if p.OrderBy == "ts").
func (s *ClickHouseStore) SearchFlowLog(ctx context.Context, p model.FlowSearchFilters) ([]model.FlowLogEntry, error) {
	// Defense in depth: enforce range and limit caps even if the handler
	// doesn't (e.g., if the store is called from another code path).
	if p.From.IsZero() || p.To.IsZero() {
		return nil, fmt.Errorf("from and to are required")
	}
	if p.To.Before(p.From) {
		return nil, fmt.Errorf("to must be after from")
	}
	if p.To.Sub(p.From) > 30*24*time.Hour {
		return nil, fmt.Errorf("time range cannot exceed 30 days")
	}
	// Validate user-supplied IP/CIDR strings before they reach ClickHouse.
	// Even though they're passed as named params, ClickHouse will throw a
	// confusing error if the format is wrong; we want a clear error message.
	if err := parseCIDROrIP(p.SrcIP); err != nil {
		return nil, fmt.Errorf("invalid src_ip: %w", err)
	}
	if err := parseCIDROrIP(p.DstIP); err != nil {
		return nil, fmt.Errorf("invalid dst_ip: %w", err)
	}

	var where []string
	var args []any

	where = append(where, "ts >= @from AND ts < @to")
	args = append(args, clickhouse.Named("from", p.From), clickhouse.Named("to", p.To))

	if p.SrcIP != "" {
		if strings.Contains(p.SrcIP, "/") {
			where = append(where, "isIPAddressInRange(toString(src_ip), @src_cidr)")
			args = append(args, clickhouse.Named("src_cidr", p.SrcIP))
		} else {
			where = append(where, "src_ip = toIPv6(@src_ip)")
			args = append(args, clickhouse.Named("src_ip", p.SrcIP))
		}
	}
	if p.DstIP != "" {
		if strings.Contains(p.DstIP, "/") {
			where = append(where, "isIPAddressInRange(toString(dst_ip), @dst_cidr)")
			args = append(args, clickhouse.Named("dst_cidr", p.DstIP))
		} else {
			where = append(where, "dst_ip = toIPv6(@dst_ip)")
			args = append(args, clickhouse.Named("dst_ip", p.DstIP))
		}
	}
	if p.SrcAS > 0 {
		where = append(where, "src_as = @src_as")
		args = append(args, clickhouse.Named("src_as", p.SrcAS))
	}
	if p.DstAS > 0 {
		where = append(where, "dst_as = @dst_as")
		args = append(args, clickhouse.Named("dst_as", p.DstAS))
	}
	if p.Protocol > 0 {
		where = append(where, "protocol = @protocol")
		args = append(args, clickhouse.Named("protocol", p.Protocol))
	}
	if p.SrcPort > 0 {
		where = append(where, "src_port = @src_port")
		args = append(args, clickhouse.Named("src_port", p.SrcPort))
	}
	if p.DstPort > 0 {
		where = append(where, "dst_port = @dst_port")
		args = append(args, clickhouse.Named("dst_port", p.DstPort))
	}
	if p.LinkTag != "" {
		where = append(where, "link_tag = @link_tag")
		args = append(args, clickhouse.Named("link_tag", p.LinkTag))
	}
	if p.IPVersion > 0 {
		where = append(where, "ip_version = @ip_version")
		args = append(args, clickhouse.Named("ip_version", p.IPVersion))
	}

	// Post-aggregation filters
	havingClauses := []string{}
	if p.MinBytes > 0 {
		havingClauses = append(havingClauses, "total_bytes >= @min_bytes")
		args = append(args, clickhouse.Named("min_bytes", p.MinBytes))
	}

	order := "total_bytes DESC"
	if p.OrderBy == "ts" {
		order = "ts DESC"
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 100000 {
		limit = 100000
	}
	args = append(args, clickhouse.Named("limit", limit), clickhouse.Named("offset", p.Offset))

	having := ""
	if len(havingClauses) > 0 {
		having = "HAVING " + strings.Join(havingClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT
			min(ts) AS ts,
			link_tag,
			toString(src_ip) AS src_ip,
			toString(dst_ip) AS dst_ip,
			src_as,
			dst_as,
			protocol,
			src_port,
			dst_port,
			any(tcp_flags) AS tcp_flags,
			ip_version,
			sum(bytes) AS total_bytes,
			sum(packets) AS total_packets,
			sum(flow_count) AS total_flows
		FROM flows_log
		WHERE %s
		GROUP BY link_tag, src_ip, dst_ip, src_as, dst_as, protocol, src_port, dst_port, ip_version
		%s
		ORDER BY %s
		LIMIT @limit OFFSET @offset
	`, strings.Join(where, " AND "), having, order)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search flow_log: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.FlowLogEntry
	for rows.Next() {
		var e model.FlowLogEntry
		if err := rows.Scan(
			&e.Timestamp, &e.LinkTag, &e.SrcIP, &e.DstIP,
			&e.SrcAS, &e.DstAS, &e.Protocol, &e.SrcPort, &e.DstPort,
			&e.TCPFlags, &e.IPVersion,
			&e.Bytes, &e.Packets, &e.FlowCount,
		); err != nil {
			return nil, err
		}
		e.SrcIP = cleanIPv4Mapped(e.SrcIP)
		e.DstIP = cleanIPv4Mapped(e.DstIP)
		results = append(results, e)
	}
	return results, nil
}

// SetFlowLogRetention applies an ALTER TABLE TTL change to flows_log so the
// retention can be tuned at deploy time without rewriting the migration. The
// change is idempotent: if the current TTL already matches, this is a no-op
// from the user's perspective. Returns nil silently if the table doesn't
// exist (FEATURE_FLOW_SEARCH might be disabled).
func (s *ClickHouseStore) SetFlowLogRetention(ctx context.Context, days int) error {
	if days < 1 {
		return fmt.Errorf("retention must be >= 1 day")
	}
	// Check existence first to avoid noisy errors when the feature is off.
	var exists uint8
	err := s.conn.QueryRow(ctx, `
		SELECT count() FROM system.tables
		WHERE database = currentDatabase() AND name = 'flows_log'
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check flows_log: %w", err)
	}
	if exists == 0 {
		return nil // table not present — nothing to do
	}
	// ALTER TABLE ... MODIFY TTL is idempotent and fast (metadata-only).
	// Note: this does NOT take effect for already-deleted partitions.
	q := fmt.Sprintf("ALTER TABLE flows_log MODIFY TTL ts + INTERVAL %d DAY", days)
	if err := s.conn.Exec(ctx, q); err != nil {
		return fmt.Errorf("apply TTL: %w", err)
	}
	return nil
}

// FlowLogTimeSeries returns per-bucket traffic for a specific flow tuple.
// Used to drill down from a search result into a time-series view.
func (s *ClickHouseStore) FlowLogTimeSeries(ctx context.Context, p model.FlowSearchFilters) ([]model.TrafficPoint, error) {
	var where []string
	var args []any

	where = append(where, "ts >= @from AND ts < @to")
	args = append(args, clickhouse.Named("from", p.From), clickhouse.Named("to", p.To))

	if p.SrcIP != "" {
		where = append(where, "src_ip = toIPv6(@src_ip)")
		args = append(args, clickhouse.Named("src_ip", p.SrcIP))
	}
	if p.DstIP != "" {
		where = append(where, "dst_ip = toIPv6(@dst_ip)")
		args = append(args, clickhouse.Named("dst_ip", p.DstIP))
	}
	if p.Protocol > 0 {
		where = append(where, "protocol = @protocol")
		args = append(args, clickhouse.Named("protocol", p.Protocol))
	}
	if p.DstPort > 0 {
		where = append(where, "dst_port = @dst_port")
		args = append(args, clickhouse.Named("dst_port", p.DstPort))
	}

	query := fmt.Sprintf(`
		SELECT
			ts,
			sum(bytes) AS bytes,
			sum(packets) AS packets
		FROM flows_log
		WHERE %s
		GROUP BY ts
		ORDER BY ts
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("flow_log timeseries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.TrafficPoint
	for rows.Next() {
		var p model.TrafficPoint
		var bytes, packets uint64
		if err := rows.Scan(&p.Timestamp, &bytes, &packets); err != nil {
			return nil, err
		}
		p.BytesIn = bytes
		p.PacketsIn = packets
		results = append(results, p)
	}
	return results, nil
}

// TopProtocols returns the top protocols by traffic volume.
func (s *ClickHouseStore) TopProtocols(ctx context.Context, p QueryParams) ([]model.ProtocolTraffic, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			protocol,
			direction,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_port t
		WHERE t.ts >= @from AND t.ts < @to
		%s %s
		GROUP BY protocol, direction
		ORDER BY total_bytes DESC
	`, dirFilter, linkFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("top protocols: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ProtocolTraffic
	var total uint64
	for rows.Next() {
		var r model.ProtocolTraffic
		if err := rows.Scan(&r.Protocol, &r.Direction, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		total += r.Bytes
		results = append(results, r)
	}
	for i := range results {
		if total > 0 {
			results[i].Percent = float64(results[i].Bytes) / float64(total) * 100
		}
	}
	return results, nil
}

// TopPorts returns the top (protocol, port) tuples by traffic volume.
func (s *ClickHouseStore) TopPorts(ctx context.Context, p QueryParams, protocol uint8) ([]model.PortTraffic, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	protoFilter := ""
	if protocol > 0 {
		protoFilter = "AND t.protocol = @protocol"
		dirArgs = append(dirArgs, clickhouse.Named("protocol", protocol))
	}

	query := fmt.Sprintf(`
		SELECT
			protocol,
			port,
			direction,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_port t
		WHERE t.ts >= @from AND t.ts < @to
		  AND t.port > 0
		%s %s %s
		GROUP BY protocol, port, direction
		ORDER BY total_bytes DESC
		LIMIT @limit OFFSET @offset
	`, dirFilter, linkFilter, protoFilter)

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", limit),
		clickhouse.Named("offset", p.Offset),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("top ports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.PortTraffic
	var total uint64
	for rows.Next() {
		var r model.PortTraffic
		if err := rows.Scan(&r.Protocol, &r.Port, &r.Direction, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		total += r.Bytes
		results = append(results, r)
	}
	for i := range results {
		if total > 0 {
			results[i].Percent = float64(results[i].Bytes) / float64(total) * 100
		}
	}
	return results, nil
}

// parseCIDROrIP validates a single IP or CIDR string. Returns error if invalid.
func parseCIDROrIP(s string) error {
	if s == "" {
		return nil
	}
	if strings.Contains(s, "/") {
		_, _, err := net.ParseCIDR(s)
		return err
	}
	if net.ParseIP(s) == nil {
		return fmt.Errorf("invalid IP: %s", s)
	}
	return nil
}
