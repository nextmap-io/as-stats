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
func (s *ClickHouseStore) EvalAmplification(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{"ts >= now() - INTERVAL @window SECOND"}
	args := []any{
		clickhouse.Named("window", window),
		clickhouse.Named("th_count", thresholdCount),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("dst_ip", "amp_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(dst_ip) AS target,
			any(protocol) AS protocol,
			uniqMerge(unique_src_ips) AS src_count
		FROM traffic_by_dst_1min
		WHERE %s
		GROUP BY dst_ip
		HAVING src_count > @th_count
		ORDER BY src_count DESC
		LIMIT 100
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval amplification: %w", err)
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
