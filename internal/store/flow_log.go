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

// flowLogReadTable is what every flow-log READ queries: a Merge table
// (migration 000017) over flows_log and flows_log_legacy, the pre-3.4 layout
// kept until it ages out. Writes go to flows_log only, through flows_log_mv.
const flowLogReadTable = "flows_log_all"

// flowLogTimeFilter returns the time predicate every flow-log query must carry,
// qualified with the table alias, and its bound parameters (@from, @to,
// @to_incl).
//
// flows_log is sorted by (toStartOfFifteenMinutes(ts), dst_ip, ts, …).
// ClickHouse 24.3 only partially derives a key range on that first column from
// `ts >= @from AND ts < @to` — a 15-minute window read ~80 % of the day on
// production — so the bucket range is stated explicitly as well. Without it a
// dst_ip lookup cannot use the second key column either. @to is exclusive, so
// the last bucket is the one holding the last second before it.
func flowLogTimeFilter(alias string, from, to time.Time) (string, []any) {
	toIncl := to.Truncate(time.Second)
	if toIncl.Equal(to) {
		toIncl = toIncl.Add(-time.Second)
	}
	cond := fmt.Sprintf(
		"%[1]s.ts >= @from AND %[1]s.ts < @to AND toStartOfFifteenMinutes(%[1]s.ts) BETWEEN toStartOfFifteenMinutes(@from) AND toStartOfFifteenMinutes(@to_incl)",
		alias)
	return cond, []any{
		clickhouse.Named("from", from),
		clickhouse.Named("to", to),
		clickhouse.Named("to_incl", toIncl),
	}
}

// SearchFlowLog executes a forensic query against the flow log (flowLogReadTable).
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

	// All column references in WHERE are qualified with the table alias `f`
	// because the SELECT clause aliases `min(ts) AS ts` and `toString(src_ip)
	// AS src_ip` etc. — without the alias ClickHouse tries to resolve `ts`
	// in the WHERE as the aggregated `min(ts)` and fails with
	// "Aggregate function min(ts) AS ts is found in WHERE in query".
	timeCond, timeArgs := flowLogTimeFilter("f", p.From, p.To)
	where = append(where, timeCond)
	args = append(args, timeArgs...)

	// src_ip / dst_ip are IPv6 columns holding IPv4 as ::ffff:a.b.c.d, so a raw
	// IPv4 CIDR never matches (CLAUDE.md bug family #1). cidrBounds maps it to
	// the IPv6-mapped range exactly like the alert queries do, and the typed
	// BETWEEN avoids stringifying every row.
	if p.SrcIP != "" {
		if strings.Contains(p.SrcIP, "/") {
			lo, hi, ok := cidrBounds(p.SrcIP)
			if !ok {
				return nil, fmt.Errorf("invalid src_ip filter %q", p.SrcIP)
			}
			where = append(where, "f.src_ip BETWEEN toIPv6(@src_lo) AND toIPv6(@src_hi)")
			args = append(args, clickhouse.Named("src_lo", lo), clickhouse.Named("src_hi", hi))
		} else {
			where = append(where, "f.src_ip = toIPv6(@src_ip)")
			args = append(args, clickhouse.Named("src_ip", p.SrcIP))
		}
	}
	if p.DstIP != "" {
		if strings.Contains(p.DstIP, "/") {
			lo, hi, ok := cidrBounds(p.DstIP)
			if !ok {
				return nil, fmt.Errorf("invalid dst_ip filter %q", p.DstIP)
			}
			where = append(where, "f.dst_ip BETWEEN toIPv6(@dst_lo) AND toIPv6(@dst_hi)")
			args = append(args, clickhouse.Named("dst_lo", lo), clickhouse.Named("dst_hi", hi))
		} else {
			where = append(where, "f.dst_ip = toIPv6(@dst_ip)")
			args = append(args, clickhouse.Named("dst_ip", p.DstIP))
		}
	}
	if p.SrcAS > 0 {
		where = append(where, "f.src_as = @src_as")
		args = append(args, clickhouse.Named("src_as", p.SrcAS))
	}
	if p.DstAS > 0 {
		where = append(where, "f.dst_as = @dst_as")
		args = append(args, clickhouse.Named("dst_as", p.DstAS))
	}
	if p.Protocol > 0 {
		where = append(where, "f.protocol = @protocol")
		args = append(args, clickhouse.Named("protocol", p.Protocol))
	}
	if p.SrcPort > 0 {
		where = append(where, "f.src_port = @src_port")
		args = append(args, clickhouse.Named("src_port", p.SrcPort))
	}
	if p.DstPort > 0 {
		where = append(where, "f.dst_port = @dst_port")
		args = append(args, clickhouse.Named("dst_port", p.DstPort))
	}
	if p.Port > 0 {
		where = append(where, "(f.src_port = @port OR f.dst_port = @port)")
		args = append(args, clickhouse.Named("port", p.Port))
	}
	if p.LinkTag != "" {
		where = append(where, "f.link_tag = @link_tag")
		args = append(args, clickhouse.Named("link_tag", p.LinkTag))
	}
	if p.IPVersion > 0 {
		where = append(where, "f.ip_version = @ip_version")
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
			min(f.ts) AS ts,
			f.link_tag AS link_tag,
			toString(f.src_ip) AS src_ip,
			toString(f.dst_ip) AS dst_ip,
			f.src_as AS src_as,
			f.dst_as AS dst_as,
			f.protocol AS protocol,
			f.src_port AS src_port,
			f.dst_port AS dst_port,
			any(f.tcp_flags) AS tcp_flags,
			f.ip_version AS ip_version,
			sum(f.bytes) AS total_bytes,
			sum(f.packets) AS total_packets,
			sum(f.flow_count) AS total_flows
		FROM %s f
		WHERE %s
		GROUP BY f.link_tag, f.src_ip, f.dst_ip, f.src_as, f.dst_as, f.protocol, f.src_port, f.dst_port, f.ip_version
		%s
		ORDER BY %s
		LIMIT @limit OFFSET @offset
	`, flowLogReadTable, strings.Join(where, " AND "), having, order)

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

// FlowLogTimeSeries returns per-bucket traffic for a specific flow tuple.
// Used to drill down from a search result into a time-series view.
func (s *ClickHouseStore) FlowLogTimeSeries(ctx context.Context, p model.FlowSearchFilters) ([]model.TrafficPoint, error) {
	// Table alias `f` + qualified columns: the SELECT exposes `ts` and the
	// sums under their column names (CLAUDE.md bug #2).
	timeCond, args := flowLogTimeFilter("f", p.From, p.To)
	where := []string{timeCond}

	if p.SrcIP != "" {
		where = append(where, "f.src_ip = toIPv6(@src_ip)")
		args = append(args, clickhouse.Named("src_ip", p.SrcIP))
	}
	if p.DstIP != "" {
		where = append(where, "f.dst_ip = toIPv6(@dst_ip)")
		args = append(args, clickhouse.Named("dst_ip", p.DstIP))
	}
	if p.Protocol > 0 {
		where = append(where, "f.protocol = @protocol")
		args = append(args, clickhouse.Named("protocol", p.Protocol))
	}
	if p.DstPort > 0 {
		where = append(where, "f.dst_port = @dst_port")
		args = append(args, clickhouse.Named("dst_port", p.DstPort))
	}
	if p.Port > 0 {
		where = append(where, "(f.src_port = @port OR f.dst_port = @port)")
		args = append(args, clickhouse.Named("port", p.Port))
	}

	query := fmt.Sprintf(`
		SELECT
			f.ts AS ts,
			sum(f.bytes) AS bytes,
			sum(f.packets) AS packets
		FROM %s f
		WHERE %s
		GROUP BY f.ts
		ORDER BY ts
	`, flowLogReadTable, strings.Join(where, " AND "))

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
