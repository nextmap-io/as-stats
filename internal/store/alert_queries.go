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
	// TargetLabel identifies a non-IP target (e.g. a link tag for link_capacity).
	// When set on a violation whose TargetIP is nil, the engine uses it for the
	// alert's target identity and records it in the alert details.
	TargetLabel string
	// Extra carries rule-type-specific key/values that the engine merges into
	// the alert's details.Extra map (e.g. anomaly baseline/current/deviation and
	// the top-contributor explanation). Nil for rule types that don't use it.
	Extra map[string]any
	// BPS / PPS are the target's rates over the window, set by the volume
	// rules: the engine compares them with the host's usual level when the
	// rule has a baseline_factor (MetricValue is always bps for those rules).
	BPS float64
	PPS float64
}

// The hot tables hold one row per (minute, key). The minute containing now()
// is still being written — the collector flushes in batches — so a window of
// `ts >= now() - INTERVAL window SECOND` included a partial bucket and then
// divided by the full window, under-reading every rate (and the partial
// share grows as the collector flushes less often). Rules therefore
// evaluate COMPLETE minutes only:
//
//	ts >= toStartOfMinute(now()) - INTERVAL window SECOND
//	AND ts < toStartOfMinute(now())
//
// with the window rounded down to whole minutes (at least one) so the
// divisor matches exactly the buckets read. The alert rules bind the end of
// that range from the store's clock (evalMinutes / @eval_end) instead of the
// server's now(), so the replay test (alert_replay_integration_test.go) can
// evaluate any past minute with the production SQL; Live Threats keeps
// completeMinutes.

// alertWindow rounds an evaluation window down to whole minutes, minimum 60 s.
// It is the value bound to @window, so the time filter and every
// "/ @window" rate conversion agree.
func alertWindow(window uint32) uint32 {
	return max(60, window-window%60)
}

// completeMinutes is the complete-minute time filter on col (a caller
// controlled column reference) for the @window parameter.
func completeMinutes(col string) string {
	return fmt.Sprintf("%[1]s >= toStartOfMinute(now()) - INTERVAL @window SECOND AND %[1]s < toStartOfMinute(now())", col)
}

// evalMinutes is completeMinutes ending at @eval_end (see evalEndArg) rather
// than at the server's current minute.
func evalMinutes(col string) string {
	return fmt.Sprintf("%[1]s >= toDateTime(@eval_end, 'UTC') - INTERVAL @window SECOND AND %[1]s < toDateTime(@eval_end, 'UTC')", col)
}

// SetClock replaces the clock the alert queries and the alert lifecycle use
// (default time.Now). Only the alert replay test sets it, before any query.
func (s *ClickHouseStore) SetClock(now func() time.Time) { s.clock = now }

func (s *ClickHouseStore) now() time.Time {
	if s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

// evalEndArg binds @eval_end: the start of the current, still filling
// minute by the store clock — the end of the last complete minute.
func (s *ClickHouseStore) evalEndArg() any {
	return clickhouse.Named("eval_end", uint32(s.now().Truncate(time.Minute).Unix()))
}

// EvalVolumeInbound queries traffic_by_dst_1min for destinations exceeding a bps/pps threshold.
// Window is the evaluation window in seconds (e.g. 60).
// localPrefixes is the list of CIDRs to filter (only alert on our own IPs).
func (s *ClickHouseStore) EvalVolumeInbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	// We express the threshold as bits/sec and packets/sec; the window
	// aggregation gives us totals over `window` seconds, so divide.
	where := []string{evalMinutes("ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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
			BPS:         bps,
			PPS:         pps,
		})
	}
	return results, nil
}

// EvalSynFlood finds destinations receiving too many TCP SYN packets.
func (s *ClickHouseStore) EvalSynFlood(ctx context.Context, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{evalMinutes("ts"), "protocol = 6"}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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
	where := []string{evalMinutes("ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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
		evalMinutes("ts"),
		"protocol = @proto",
	}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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
	where := []string{evalMinutes("ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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

	query := fmt.Sprintf(`
		SELECT toString(f.src_ip) AS src
		FROM flows_raw AS f
		WHERE %s
		  AND f.dst_ip = toIPv6(@target)
		GROUP BY f.src_ip
		ORDER BY sum(f.bytes * f.sampling_rate) DESC
		LIMIT @limit
	`, evalMinutes("f.timestamp"))
	rows, err := s.conn.Query(ctx, query,
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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

// Port scan detection (rule type port_scan). A local SOURCE is a scanner
// when it sends connection probes to many distinct destination ports.
//
// The 3.6 rule counted every distinct destination port in the hot table
// (traffic_by_src_1min.unique_dst_ports). A server answering clients sends
// to their ephemeral ports, so every busy server "scanned" thousands of
// ports a minute: 1,938 alerts on 33 servers in 10 days of production, none
// a scan (CLAUDE.md bug #23). The hot table has no flags and no source port,
// so the rule now runs in two steps:
//
//  1. PortScanCandidates — the old hot-table query, a cheap superset: a
//     host's probe ports are a subset of all its destination ports, so the
//     candidates are the hosts above the threshold (lowered by
//     portScanHLLSlack for the HLL error), at most portScanMaxCandidates.
//  2. flows_raw over the same complete minutes, candidates only: the
//     distinct destination ports of PROBE flows (portScanProbeSQL).
//
// Step 2 reads one window of flows_raw and only runs when step 1 returned a
// candidate.
const (
	portScanMaxCandidates = 200
	portScanHLLSlack      = 0.9 // uniq() is approximate (~1-2 %); never miss a candidate for it
)

// portScanProbeSQL matches the flows of a local source that look like
// connection probes rather than replies (f = flows_raw alias):
//
//   - TCP with SYN and without ACK: a connection attempt not acknowledged in
//     this record — a SYN scan probe (nmap -sS, masscan, zmap) or an
//     unanswered connect(). A reply from a local server always carries ACK
//     (SYN-ACK, then ACK), so busy servers never match.
//   - TCP without any flags (exporters that do not report them) and UDP:
//     from an unprivileged local port (>= 1024) to a well-known remote port
//     (< 1024). A local service answers from its own port to the client's
//     ephemeral port, which this excludes. Without TCP flags the rule
//     therefore only sees probes to ports below 1024.
const portScanProbeSQL = `((f.protocol = 6 AND bitAnd(f.tcp_flags, 2) != 0 AND bitAnd(f.tcp_flags, 16) = 0)
	OR (f.protocol IN (6, 17) AND f.tcp_flags = 0 AND f.src_port >= 1024 AND f.dst_port < 1024))`

// PortScanCandidates returns the local sources whose distinct destination
// ports (all flows, hot table) exceed thresholdCount over the window — the
// 3.6 port_scan rule, now the first step of EvalPortScan.
func (s *ClickHouseStore) PortScanCandidates(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{evalMinutes("h.ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
		clickhouse.Named("th_count", thresholdCount),
		clickhouse.Named("max_cands", portScanMaxCandidates),
	}
	if len(localPrefixes) > 0 {
		// For port scan, we look at SOURCES in our network (internal bots)
		clause, cidrArgs := buildCIDRFilter("h.src_ip", "ps_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	query := fmt.Sprintf(`
		SELECT
			toString(h.src_ip) AS target,
			any(h.protocol) AS protocol,
			uniqMerge(h.unique_dst_ports) AS port_count
		FROM traffic_by_src_1min AS h
		WHERE %s
		GROUP BY h.src_ip
		HAVING port_count > @th_count
		ORDER BY port_count DESC
		LIMIT @max_cands
	`, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval port_scan candidates: %w", err)
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
	return results, rows.Err()
}

// EvalPortScan finds local sources sending connection probes to more than
// thresholdCount distinct destination ports over the window (see the
// comment above portScanMaxCandidates). The violation's metric is the probe
// port count; details carry the probed hosts and the all-flows port count.
func (s *ClickHouseStore) EvalPortScan(ctx context.Context, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	cands, err := s.PortScanCandidates(ctx, uint64(float64(thresholdCount)*portScanHLLSlack), window, localPrefixes)
	if err != nil || len(cands) == 0 {
		return nil, err
	}
	probes := make([]string, 0, len(cands))
	allPorts := make(map[string]uint64, len(cands))
	for _, c := range cands {
		probes = append(probes, ipv6Literal(ipToIPv6(c.TargetIP)))
		allPorts[c.TargetIP.String()] = c.UniqueCount
	}

	query := fmt.Sprintf(`
		SELECT
			toString(f.src_ip)             AS target,
			uniqExactIf(f.dst_port, %[1]s) AS probe_ports,
			uniqExactIf(f.dst_ip, %[1]s)   AS probe_hosts,
			countIf(%[1]s)                 AS probe_flows,
			count()                        AS flows
		FROM flows_raw AS f
		WHERE %[2]s AND %[3]s
		GROUP BY f.src_ip
		HAVING probe_ports > @th_count
		ORDER BY probe_ports DESC
	`, portScanProbeSQL, evalMinutes("f.timestamp"), ipsInList("f.src_ip"))
	rows, err := s.conn.Query(ctx, query,
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
		clickhouse.Named("th_count", thresholdCount),
		clickhouse.Named("ips", probes),
	)
	if err != nil {
		return nil, fmt.Errorf("eval port_scan probes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var ports, hosts, probeFlows, flows uint64
		if err := rows.Scan(&target, &ports, &hosts, &probeFlows, &flows); err != nil {
			return nil, err
		}
		ip := net.ParseIP(cleanIPv4Mapped(target))
		results = append(results, AlertViolation{
			TargetIP:    ip,
			MetricValue: float64(ports),
			UniqueCount: ports,
			Extra: map[string]any{
				"probe_hosts":   hosts,
				"probe_flows":   probeFlows,
				"flows":         flows,
				"all_dst_ports": allPorts[ip.String()],
			},
		})
	}
	return results, rows.Err()
}

// EvalVolumeOutbound finds internal sources emitting too much traffic (bots).
func (s *ClickHouseStore) EvalVolumeOutbound(ctx context.Context, thresholdBps, thresholdPps uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{evalMinutes("ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
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
			sum(bytes) * 8 / @window AS bps,
			sum(packets) / @window AS pps
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
		var bps, pps float64
		if err := rows.Scan(&target, &proto, &bps, &pps); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    proto,
			MetricValue: bps,
			BPS:         bps,
			PPS:         pps,
		})
	}
	return results, nil
}

// buildCIDRFilter returns a parameterized SQL fragment matching any of the
// given CIDRs against the named IPv6 column. The named-arg prefix lets
// multiple filters coexist in the same query without name collisions.
//
// Each prefix is validated as a real CIDR (or single IP) — invalid entries
// are skipped. The bounds are passed as ClickHouse named parameters, never
// templated into the SQL string.
//
// Each CIDR becomes a typed range predicate on the raw column:
//
//	col BETWEEN toIPv6(@p_lo0) AND toIPv6(@p_hi0)
//
// with IPv4 CIDRs mapped onto their ::ffff:a.b.c.d range, exactly as
// normalizeCIDRForIPv6Column does. That mapping is load-bearing (CLAUDE.md
// bug #1): the columns store IPv4 as IPv4-mapped IPv6, and an unmapped IPv4
// bound would silently drop every IPv4 row from every alert evaluation. The
// predicate used to be isIPAddressInRange(toString(col), cidr), which
// stringifies every row: 532 ms vs 107 ms for 12 prefixes on 24.3, with zero
// rows of difference (pinned by TestIntegrationCIDRRangeMatchesIsIPAddressInRange).
func buildCIDRFilter(column, prefix string, prefixes []string) (string, []any) {
	if len(prefixes) == 0 {
		return "1=1", nil
	}
	parts := make([]string, 0, len(prefixes))
	args := make([]any, 0, 2*len(prefixes))
	idx := 0
	for _, p := range prefixes {
		lo, hi, ok := cidrBounds(p)
		if !ok {
			continue // skip invalid entries silently — these come from RIPE or admin config
		}
		loName := fmt.Sprintf("%slo%d", prefix, idx)
		hiName := fmt.Sprintf("%shi%d", prefix, idx)
		parts = append(parts, fmt.Sprintf("(%s BETWEEN toIPv6(@%s) AND toIPv6(@%s))", column, loName, hiName))
		args = append(args, clickhouse.Named(loName, lo), clickhouse.Named(hiName, hi))
		idx++
	}
	if len(parts) == 0 {
		return "1=1", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}

// CIDRRangeSQL returns a literal (parameter-free) SQL predicate matching any of
// the CIDRs against an IPv6 column, with the same typed-range semantics as
// buildCIDRFilter. It exists for filters built once at startup and reused on
// every request (the LOCAL_AS prefixes from RIPEstat). Inlining is safe: the
// bounds are re-rendered from parsed addresses (hex digits, ':' and '.' only),
// never echoed from the input; invalid entries are skipped. Returns "1=1" when
// nothing valid remains. col must be a caller-controlled column reference.
func CIDRRangeSQL(col string, cidrs []string) string {
	parts := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		lo, hi, ok := cidrBounds(c)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("(%s BETWEEN toIPv6('%s') AND toIPv6('%s'))", col, lo, hi))
	}
	if len(parts) == 0 {
		return "1=1"
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// privateRanges are the non-routable ranges excluded from "external" Top IPs:
// RFC 1918, CGNAT (RFC 6598), IPv4 link-local, IPv6 link-local and ULA.
var privateRanges = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16",
	"fe80::/10", "fc00::/7",
}

// PrivateRangeSQL matches the private / non-routable ranges against an IPv6
// column (typed range predicates, IPv4 mapped — see CIDRRangeSQL).
func PrivateRangeSQL(col string) string { return CIDRRangeSQL(col, privateRanges) }

// cidrBounds returns the first and last address of a CIDR or bare IP as
// strings that toIPv6() parses into the column's representation: IPv4
// addresses and CIDRs become ::ffff:a.b.c.d (see normalizeCIDRForIPv6Column).
// Host bits set in the input ("192.0.2.7/24") are masked off, like
// isIPAddressInRange does.
func cidrBounds(s string) (lo, hi string, ok bool) {
	normalized, ok := normalizeCIDRForIPv6Column(s)
	if !ok {
		return "", "", false
	}
	_, ipnet, err := net.ParseCIDR(normalized)
	if err != nil || len(ipnet.IP) != net.IPv6len || len(ipnet.Mask) != net.IPv6len {
		return "", "", false
	}
	first := make(net.IP, net.IPv6len)
	last := make(net.IP, net.IPv6len)
	for i := range first {
		first[i] = ipnet.IP[i] & ipnet.Mask[i]
		last[i] = first[i] | ^ipnet.Mask[i]
	}
	return ipv6Literal(first), ipv6Literal(last), true
}

// ipv6Literal renders a 16-byte address for toIPv6(). Go prints addresses in
// ::ffff:0:0/96 as a bare dotted quad, which toIPv6() would also map, but the
// explicit ::ffff: form keeps the intent obvious in logged queries.
func ipv6Literal(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return "::ffff:" + v4.String()
	}
	return ip.String()
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
//   - "::ffff:1.2.3.0/120" -> unchanged (already in the column's form)
//   - garbage        -> "", false
func normalizeCIDRForIPv6Column(s string) (string, bool) {
	if ip, ipnet, err := net.ParseCIDR(s); err == nil {
		ones, _ := ipnet.Mask.Size()
		// The mask length, not ip.To4(), says whether this is an IPv4 CIDR:
		// "::ffff:1.2.3.0/120" also has a To4() form but is already mapped.
		if len(ipnet.Mask) == net.IPv4len {
			return fmt.Sprintf("::ffff:%s/%d", ip.To4().String(), ones+96), true
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

// EvalSubnetFlood detects carpet-bombing attacks by aggregating traffic to a
// configurable subnet level before applying thresholds. A carpet bomb
// distributes the attack across an entire /24 so no single IP breaches the
// per-host threshold, but the aggregate is massive.
//
// `aggPrefix` is the IPv4 prefix length (e.g. 24 for /24). Internally the
// query adds 96 to convert to the IPv6-mapped prefix that ClickHouse stores.
// IPv6 carpet bombing is out of scope for now (very rare in practice).
//
// Returns one AlertViolation per subnet that exceeds the threshold.
func (s *ClickHouseStore) EvalSubnetFlood(ctx context.Context, thresholdBps, thresholdPps uint64, aggPrefix int, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	if aggPrefix < 8 || aggPrefix > 32 {
		return nil, fmt.Errorf("subnet prefix length must be 8..32, got %d", aggPrefix)
	}

	// +96 for the IPv4-mapped IPv6 representation
	v6Prefix := aggPrefix + 96

	where := []string{evalMinutes("t.ts")}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
		clickhouse.Named("agg_prefix", uint8(v6Prefix)),
		clickhouse.Named("th_bps", thresholdBps),
		clickhouse.Named("th_pps", thresholdPps),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("t.dst_ip", "sf_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	having := []string{}
	if thresholdBps > 0 {
		having = append(having, "bps > @th_bps")
	}
	if thresholdPps > 0 {
		having = append(having, "pps > @th_pps")
	}
	if len(having) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT
			toString(IPv6CIDRToRange(t.dst_ip, toUInt8(@agg_prefix)).1) AS subnet,
			toUInt64(sum(t.bytes) * 8 / @window) AS bps,
			toUInt64(sum(t.packets) / @window) AS pps
		FROM traffic_by_dst_1min t
		WHERE %s
		GROUP BY subnet
		HAVING %s
		ORDER BY bps DESC
		LIMIT 100
	`, strings.Join(where, " AND "), strings.Join(having, " OR "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval subnet_flood: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var bps, pps uint64
		if err := rows.Scan(&target, &bps, &pps); err != nil {
			return nil, err
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			MetricValue: float64(bps),
		})
	}
	return results, nil
}

// EvalSMTPAbuse detects internal hosts sending suspicious volumes of SMTP
// traffic — a classic sign of a compromised machine being used as a spam relay.
//
// The query hits flows_raw (not the hot tables) because we need the dst_port
// dimension. This is acceptable: the query is bounded by the window, filtered
// on protocol=6 and dst_port IN (25, 465, 587), and runs at most once per
// eval cycle (every 30s). On a typical network this scans a tiny fraction
// of flows_raw.
//
// The threshold can be pps-based (ThresholdPps) or flow-count based
// (ThresholdCount for distinct connections). Both are supported.
func (s *ClickHouseStore) EvalSMTPAbuse(ctx context.Context, thresholdPps, thresholdCount uint64, window uint32, localPrefixes []string) ([]AlertViolation, error) {
	where := []string{
		evalMinutes("timestamp"),
		"protocol = 6",
		"dst_port IN (25, 465, 587)",
	}
	args := []any{
		s.evalEndArg(), clickhouse.Named("window", alertWindow(window)),
		clickhouse.Named("th_pps", thresholdPps),
		clickhouse.Named("th_count", thresholdCount),
	}
	// Filter to internal source IPs only — we want to detect OUR hosts
	// that are compromised, not external SMTP traffic passing through.
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("src_ip", "smtp_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	having := []string{}
	if thresholdPps > 0 {
		having = append(having, "pps > @th_pps")
	}
	if thresholdCount > 0 {
		having = append(having, "flows > @th_count")
	}
	if len(having) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT
			toString(src_ip) AS target,
			toUInt64(sum(packets * sampling_rate) / @window) AS pps,
			count() AS flows
		FROM flows_raw
		WHERE %s
		GROUP BY src_ip
		HAVING %s
		ORDER BY pps DESC
		LIMIT 100
	`, strings.Join(where, " AND "), strings.Join(having, " OR "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("eval smtp_abuse: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []AlertViolation
	for rows.Next() {
		var target string
		var pps, flows uint64
		if err := rows.Scan(&target, &pps, &flows); err != nil {
			return nil, err
		}
		metricVal := float64(pps)
		if thresholdPps == 0 {
			metricVal = float64(flows)
		}
		results = append(results, AlertViolation{
			TargetIP:    net.ParseIP(cleanIPv4Mapped(target)),
			Protocol:    6,
			MetricValue: metricVal,
			UniqueCount: flows,
		})
	}
	return results, nil
}

// EvalDiskUsage returns a violation when any ClickHouse disk's used percentage
// exceeds thresholdPct. Disk usage has no IP target, so the violation carries a
// nil TargetIP (persisted as the zero IPv6 address). Only the single most
// utilised disk is reported, so multiple disks cannot collide on the same
// zero-IP alert key during dedup.
//
// The percentage threshold is carried in the rule's ThresholdCount field
// (repurposed as a unitless 0..100 value) because the alert schema has no
// dedicated percent column; the engine maps metric_type="percent" back to
// ThresholdCount when recording the alert.
func (s *ClickHouseStore) EvalDiskUsage(ctx context.Context, thresholdPct uint64) ([]AlertViolation, error) {
	disks, err := s.diskStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("eval disk_usage: %w", err)
	}
	// Every disk counts — with a hot/cold storage policy that includes the
	// cold HDD — and the fullest one decides.
	worst, ok := fullestDisk(disks)
	if !ok || worst.UsedPercent <= float64(thresholdPct) {
		return nil, nil
	}
	return []AlertViolation{{MetricValue: worst.UsedPercent}}, nil
}

// time type alias to avoid unused import warning on files that only use these
var _ = time.Time{}
