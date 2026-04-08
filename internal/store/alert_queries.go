package store

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// AlertViolation describes an evaluated rule violation: one target + metric value.
type AlertViolation struct {
	TargetIP    net.IP
	TargetAS    uint32
	Protocol    uint8
	MetricValue float64
	TopSources  []string // for amplification/port scan context
	UniqueCount uint64
}

// EvalVolumeInbound queries traffic_by_dst_1min for destinations exceeding a bps/pps threshold.
// Window is the evaluation window in seconds (e.g. 60).
// localPrefixes is the list of CIDRs to filter (only alert on our own IPs).
func (s *ClickHouseStore) EvalVolumeInbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	// We express the threshold as bits/sec and packets/sec; the window
	// aggregation gives us totals over `window` seconds, so divide.
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_bps", thresholdBps),
		clickhouse.Named("th_pps", thresholdPps),
	}

	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "vin_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	having := []string{}
	if thresholdBps > 0 {
		having = append(having, "(sum(bytes) * 8 / @window) > @th_bps")
	}
	if thresholdPps > 0 {
		having = append(having, "(sum(packets) / @window) > @th_pps")
	}
	if len(having) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			any(protocol) AS protocol,
			sum(bytes) * 8 / @window AS bps,
			sum(packets) / @window AS pps
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING %s
		ORDER BY bps DESC
		LIMIT 100
	`, strings.Join(where, " AND "), strings.Join(having, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval volume_in: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var proto uint8
		var bps, pps float64
		if err := rows.Scan(&target, &proto, &bps, &pps); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: bps,
		})
	}
	return results, nil
}

// EvalSynFlood finds destinations receiving too many TCP SYN packets.
func (s *ClickHouseStore) EvalSynFlood(ctx context.Context, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND", "protocol = 6"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_pps", thresholdPps),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "syn_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			sum(syn_count) / @window AS syn_pps
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING syn_pps > @th_pps
		ORDER BY syn_pps DESC
		LIMIT 100
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval syn_flood: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var val float64
		if err := rows.Scan(&target, &val); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    6,
			MetricValue: val,
		})
	}
	return results, nil
}

// EvalAmplification finds destinations with too many unique sources (reflection attacks).
//
// `minBps` is a sanity floor — destinations whose sustained inbound rate falls
// below this value over the window are excluded even if their unique source
// count is above threshold. This filters out scanners that touch one of our
// IPs from many sources at trivial volume, which would otherwise generate
// constant amplification false positives. Pass 0 to disable the floor.
func (s *ClickHouseStore) EvalAmplification(ctx context.Context, thresholdCount, minBps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_count", thresholdCount),
		clickhouse.Named("min_bps", minBps),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "amp_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	having := []string{"src_count > @th_count"}
	if minBps > 0 {
		having = append(having, "(sum(bytes) * 8 / @window) >= @min_bps")
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			any(protocol) AS protocol,
			uniqMerge(unique_src_ips) AS src_count,
			toUInt64(sum(bytes) * 8 / @window) AS bps
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING %s
		ORDER BY src_count DESC
		LIMIT 100
	`, strings.Join(where, " AND "), strings.Join(having, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval amplification: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var proto uint8
		var cnt, bps uint64
		if err := rows.Scan(&target, &proto, &cnt, &bps); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: float64(cnt),
			UniqueCount: cnt,
		})
	}
	return results, nil
}

// EvalProtocolFlood finds destinations exceeding a packets-per-second threshold
// for one specific protocol number (1=ICMP, 17=UDP, 6=TCP, ...). It is the
// generic engine behind icmp_flood / udp_flood rule types — high pps on a
// single L4 protocol is the most reliable abuse signal we can compute from
// the hot table without per-port aggregates.
func (s *ClickHouseStore) EvalProtocolFlood(ctx context.Context, protocol uint8, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{
		"ts >= now() - INTERVAL @window SECOND",
		"protocol = @proto",
	}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_pps", thresholdPps),
		clickhouse.Named("proto", protocol),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "pflood_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			toUInt64(sum(packets) / @window) AS pps
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING pps > @th_pps
		ORDER BY pps DESC
		LIMIT 100
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval protocol flood (proto=%d): %w", protocol, err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var val uint64
		if err := rows.Scan(&target, &val); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    protocol,
			MetricValue: float64(val),
		})
	}
	return results, nil
}

// EvalConnectionFlood finds destinations receiving a high *number of distinct
// flows* (i.e. connection-rate abuse) regardless of bytes/packets. This catches
// patterns volume_in misses: Slowloris, half-open scans, churning short
// connections that never reach SYN flood territory but still exhaust state.
func (s *ClickHouseStore) EvalConnectionFlood(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_count", thresholdCount),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "cflood_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			any(protocol) AS protocol,
			sum(flow_count) AS flows
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING flows > @th_count
		ORDER BY flows DESC
		LIMIT 100
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval connection flood: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var proto uint8
		var cnt uint64
		if err := rows.Scan(&target, &proto, &cnt); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: float64(cnt),
			UniqueCount: cnt,
		})
	}
	return results, nil
}

// TopSourcesForTarget returns the top N source IPs hammering a destination IP
// during the last `window` seconds, ordered by bytes. Used to enrich alerts
// with attacker context immediately after a violation is detected.
//
// This queries flows_raw directly, not the hot tables, because the hot tables
// only keep aggregates (HyperLogLog sketches) and cannot enumerate individual
// sources. The query is bounded by time and LIMIT, so it is cheap as long as
// it only runs on actual violations.
//
// `targetIP` must be a parsed IP (any form) — it is converted to the
// IPv6-mapped string ClickHouse stores in the `dst_ip` column.
func (s *ClickHouseStore) TopSourcesForTarget(ctx context.Context, targetIP net.IP, window uint32, limit int) ([]string, error) {
	if targetIP == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	// Normalize: ClickHouse stores IPv4 inside an IPv6 column as ::ffff:1.2.3.4
	probe := targetIP.String()
	if v4 := targetIP.To4(); v4 != nil {
		probe = "::ffff:" + v4.String()
	}

	query := `
		SELECT toString(src_ip) AS src
		FROM flows_raw
		WHERE timestamp >= now() - INTERVAL @window SECOND
		  AND toString(dst_ip) = @target
		GROUP BY src_ip
		ORDER BY sum(bytes * sampling_rate) DESC
		LIMIT @limit
	`
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("window", window),
		clickhouse.Named("target", probe),
		clickhouse.Named("limit", limit),
	)
	if err != nil {
		return nil, fmt.Errorf("top sources: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		sources = append(sources, cleanIPv4Mapped(s))
	}
	return sources, nil
}

// EvalPortScan finds sources hitting too many unique destination ports.
// This detects outbound scans from compromised hosts in our network.
func (s *ClickHouseStore) EvalPortScan(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_count", thresholdCount),
	}
	if len(localPrefixes) > 0 {
		// For port scan, we look at SOURCES in our network (internal bots)
		clause, cidrArgs := buildCIDRFilter("src_ip", "ps_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(src_ip) AS target,
			any(protocol) AS protocol,
			uniqMerge(unique_dst_ports) AS port_count
		FROM traffic_by_src_1min
		WHERE %s
		GROUP BY src_ip
		HAVING port_count > @th_count
		ORDER BY port_count DESC
		LIMIT 100
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval port_scan: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var proto uint8
		var cnt uint64
		if err := rows.Scan(&target, &proto, &cnt); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: float64(cnt),
			UniqueCount: cnt,
		})
	}
	return results, nil
}

// EvalVolumeOutbound finds internal sources emitting too much traffic (bots).
func (s *ClickHouseStore) EvalVolumeOutbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_bps", thresholdBps),
		clickhouse.Named("th_pps", thresholdPps),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("src_ip", "vout_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	having := []string{}
	if thresholdBps > 0 {
		having = append(having, "(sum(bytes) * 8 / @window) > @th_bps")
	}
	if thresholdPps > 0 {
		having = append(having, "(sum(packets) / @window) > @th_pps")
	}
	if len(having) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT
			toString(src_ip) AS target,
			any(protocol) AS protocol,
			sum(bytes) * 8 / @window AS bps
		FROM traffic_by_src_1min
		WHERE %s
		GROUP BY src_ip
		HAVING %s
		ORDER BY bps DESC
		LIMIT 100
	`, strings.Join(where, " AND "), strings.Join(having, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval volume_out: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var proto uint8
		var val float64
		if err := rows.Scan(&target, &proto, &val); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: val,
		})
	}
	return results, nil
}

// buildCIDRFilter returns a parameterized SQL fragment matching any of the
// given CIDRs against the named column. The named-arg prefix lets multiple
// filters coexist in the same query without name collisions.
//
// Each prefix is validated as a real CIDR (or single IP) — invalid entries
// are skipped with a warning. The CIDRs themselves are passed as ClickHouse
// named parameters, never templated into the SQL string.
//
// IPv4 CIDRs are translated to their IPv6-mapped form (`::ffff:x.x.x.x/(n+96)`)
// because the table columns are `IPv6` and `toString()` always emits the
// mapped representation for IPv4 addresses. ClickHouse's
// `isIPAddressInRange("::ffff:1.2.3.4", "1.2.3.0/24")` returns 0 even though
// the address is logically in range — silently dropping every IPv4 row from
// the alert evaluations.
func buildCIDRFilter(column, prefix string, prefixes []string) (string, []any) {
	if len(prefixes) == 0 {
		return "1=1", nil
	}
	parts := make([]string, 0, len(prefixes))
	args := make([]any, 0, len(prefixes))
	idx := 0
	for _, p := range prefixes {
		normalized, ok := normalizeCIDRForIPv6Column(p)
		if !ok {
			continue // skip invalid entries silently — these come from RIPE or admin config
		}
		paramName := fmt.Sprintf("%scidr%d", prefix, idx)
		parts = append(parts, fmt.Sprintf("isIPAddressInRange(toString(%s), @%s)", column, paramName))
		args = append(args, clickhouse.Named(paramName, normalized))
		idx++
	}
	if len(parts) == 0 {
		return "1=1", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// normalizeCIDRForIPv6Column accepts a CIDR ("1.2.3.0/24"), a bare IP
// ("1.2.3.4"), or any IPv6 form, and returns a CIDR that compares correctly
// against an IPv6 column whose IPv4 values are stored as IPv4-mapped IPv6.
//
// Rules:
//   - "1.2.3.0/24"   -> "::ffff:1.2.3.0/120"   (24 + 96 host bits)
//   - "1.2.3.4"      -> "::ffff:1.2.3.4/128"
//   - "2001:db8::/32" -> "2001:db8::/32"        (unchanged)
//   - "2001:db8::1"  -> "2001:db8::1/128"
//   - garbage        -> "", false
func normalizeCIDRForIPv6Column(s string) (string, bool) {
	if ip, ipnet, err := net.ParseCIDR(s); err == nil {
		ones, _ := ipnet.Mask.Size()
		if v4 := ip.To4(); v4 != nil {
			return fmt.Sprintf("::ffff:%s/%d", v4.String(), ones+96), true
		}
		return s, true
	}
	if ip := net.ParseIP(s); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return fmt.Sprintf("::ffff:%s/128", v4.String()), true
		}
		return s + "/128", true
	}
	return "", false
}

// time type alias to avoid unused import warning on files that only use these
var _ = time.Time{}
