package store

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// TopAS returns the top ASes by traffic volume.
func (s *ClickHouseStore) TopAS(ctx context.Context, p QueryParams) ([]model.ASTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)
	excludeAS := ""
	var excludeArgs []any
	if p.ExcludeAS > 0 {
		excludeAS = "AND as_number != @exclude_as"
		excludeArgs = append(excludeArgs, clickhouse.Named("exclude_as", p.ExcludeAS))
	}

	query := fmt.Sprintf(`
		SELECT
			as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_as t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to
		%s %s %s
		GROUP BY as_number
		ORDER BY total_bytes DESC
		LIMIT @limit OFFSET @offset
	`, dirFilter, linkFilter, excludeAS)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
		clickhouse.Named("offset", p.Offset),
	}, dirArgs...)
	args = append(args, linkArgs...)
	args = append(args, excludeArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query top AS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ASTraffic
	for rows.Next() {
		var r model.ASTraffic
		if err := rows.Scan(&r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}

	// Get total bytes for percentage calculation
	totalQuery := fmt.Sprintf(`
		SELECT sum(bytes) FROM traffic_by_as
		WHERE ts >= @from AND ts < @to %s %s %s
	`, dirFilter, linkFilter, excludeAS)
	totalArgs := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, dirArgs...)
	totalArgs = append(totalArgs, linkArgs...)
	totalArgs = append(totalArgs, excludeArgs...)

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

// TopIP returns the top IPs by traffic volume.
// If LocalIPFilter is set, only returns IPs matching that filter (internal).
// If ExternalIPFilter is set (LocalIPFilter prefixed with NOT), returns external IPs.
func (s *ClickHouseStore) TopIP(ctx context.Context, p QueryParams) ([]model.IPTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	ipFilter := ""
	if p.LocalIPFilter != "" {
		ipFilter = "AND " + p.LocalIPFilter
	}

	query := fmt.Sprintf(`
		SELECT
			toString(t.ip_address) AS ip,
			t.as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_ip t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to
		%s %s %s
		GROUP BY ip, t.as_number
		ORDER BY total_bytes DESC
		LIMIT @limit OFFSET @offset
	`, dirFilter, linkFilter, ipFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
		clickhouse.Named("offset", p.Offset),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query top IP: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.IPTraffic
	for rows.Next() {
		var r model.IPTraffic
		if err := rows.Scan(&r.IP, &r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, 0, err
		}
		r.IP = cleanIPv4Mapped(r.IP)
		results = append(results, r)
	}

	return results, 0, nil
}

// TopPrefix returns the top prefixes by traffic volume.
// scope=internal groups sub-prefixes under their parent announced prefix.
// scope=external excludes internal prefixes.
func (s *ClickHouseStore) TopPrefix(ctx context.Context, p QueryParams) ([]model.PrefixTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	// Build prefix scope filter and grouping
	prefixFilter := ""
	prefixExpr := "t.prefix"

	if len(p.LocalPrefixes) > 0 {
		// Build isIPAddressInRange conditions on the IP part of the prefix
		var conditions []string
		var caseWhen []string
		for _, cidr := range p.LocalPrefixes {
			cond := fmt.Sprintf("isIPAddressInRange(splitByChar('/', t.prefix)[1], '%s')", cidr)
			conditions = append(conditions, cond)
			caseWhen = append(caseWhen, fmt.Sprintf("WHEN %s THEN '%s'", cond, cidr))
		}
		internalCond := "(" + strings.Join(conditions, " OR ") + ")"

		switch p.PrefixScope {
		case "internal":
			prefixFilter = "AND " + internalCond
			prefixExpr = "CASE " + strings.Join(caseWhen, " ") + " ELSE t.prefix END"
		case "external":
			prefixFilter = "AND NOT " + internalCond
		}
	}

	query := fmt.Sprintf(`
		SELECT
			%s AS grouped_prefix,
			t.as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_prefix t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to
		%s %s %s
		GROUP BY grouped_prefix, t.as_number
		ORDER BY total_bytes DESC
		LIMIT @limit OFFSET @offset
	`, prefixExpr, dirFilter, linkFilter, prefixFilter)

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
		clickhouse.Named("offset", p.Offset),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query top prefix: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.PrefixTraffic
	for rows.Next() {
		var r model.PrefixTraffic
		if err := rows.Scan(&r.Prefix, &r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}

	return results, 0, nil
}

// ASTimeSeries returns traffic time series for a specific AS.
// Picks the best source table based on the time range.
func (s *ClickHouseStore) ASTimeSeries(ctx context.Context, asn uint32, p QueryParams) ([]model.TrafficPoint, error) {
	step := autoStep(p.From, p.To)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	var query string
	if useRawTable(p.From, p.To) {
		// On flows_raw: src_as=@asn → AS sends to us (download=bytes_in)
		//               dst_as=@asn → we send to AS (upload=bytes_out)
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
				sumIf(bytes * sampling_rate, src_as = @asn) AS bytes_in,
				sumIf(bytes * sampling_rate, dst_as = @asn) AS bytes_out,
				sumIf(packets * sampling_rate, src_as = @asn) AS packets_in,
				sumIf(packets * sampling_rate, dst_as = @asn) AS packets_out
			FROM flows_raw
			WHERE (src_as = @asn OR dst_as = @asn)
			  AND timestamp >= @from AND timestamp < @to
			  %s
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()), linkFilter)
	} else {
		table := pickASTable(p.From, p.To)
		// Swap in/out: AS MV 'in'=upload-to-AS, 'out'=download-from-AS
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
				sumIf(bytes, direction = 'out') AS bytes_in,
				sumIf(bytes, direction = 'in') AS bytes_out,
				sumIf(packets, direction = 'out') AS packets_in,
				sumIf(packets, direction = 'in') AS packets_out
			FROM %s
			WHERE as_number = @asn
			  AND ts >= @from AND ts < @to
			  %s
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()), table, linkFilter)
	}

	args := append([]any{
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, linkArgs...)

	return s.queryTimeSeries(ctx, query, args)
}

// ASLinkSeries returns per-link time series for a specific AS, optionally filtered by ip_version.
func (s *ClickHouseStore) ASLinkSeries(ctx context.Context, asn uint32, p QueryParams) ([]model.LinkTimeSeries, error) {
	step := autoStep(p.From, p.To)

	ipvFilter := ""
	var ipvArgs []any
	if p.IPVersion == 4 || p.IPVersion == 6 {
		ipvFilter = "AND t.ip_version = @ipv"
		ipvArgs = append(ipvArgs, clickhouse.Named("ipv", p.IPVersion))
	}

	var query string
	if useRawTable(p.From, p.To) {
		// flows_raw: src_as=@asn → download (bytes_in), dst_as=@asn → upload (bytes_out)
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(t.timestamp, INTERVAL %d SECOND) AS period,
				t.link_tag,
				sumIf(t.bytes * t.sampling_rate, t.src_as = @asn) AS bytes_in,
				sumIf(t.bytes * t.sampling_rate, t.dst_as = @asn) AS bytes_out,
				sumIf(t.packets * t.sampling_rate, t.src_as = @asn) AS packets_in,
				sumIf(t.packets * t.sampling_rate, t.dst_as = @asn) AS packets_out
			FROM flows_raw t
			WHERE (t.src_as = @asn OR t.dst_as = @asn)
			  AND t.timestamp >= @from AND t.timestamp < @to
			  AND t.link_tag != ''
			  %s
			GROUP BY period, t.link_tag
			ORDER BY period, t.link_tag
		`, int(step.Seconds()), ipvFilter)
	} else {
		table := pickASTable(p.From, p.To)
		// Swap in/out: AS MV 'in'=upload-to-AS, 'out'=download-from-AS
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
				t.link_tag,
				sumIf(t.bytes, t.direction = 'out') AS bytes_in,
				sumIf(t.bytes, t.direction = 'in') AS bytes_out,
				sumIf(t.packets, t.direction = 'out') AS packets_in,
				sumIf(t.packets, t.direction = 'in') AS packets_out
			FROM %s t
			WHERE t.as_number = @asn
			  AND t.ts >= @from AND t.ts < @to
			  AND t.link_tag != ''
			  %s
			GROUP BY period, t.link_tag
			ORDER BY period, t.link_tag
		`, int(step.Seconds()), table, ipvFilter)
	}

	args := append([]any{
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, ipvArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query AS link series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seriesMap := make(map[string]*model.LinkTimeSeries)
	var order []string

	for rows.Next() {
		var ts time.Time
		var tag string
		var bytesIn, bytesOut, packetsIn, packetsOut uint64
		if err := rows.Scan(&ts, &tag, &bytesIn, &bytesOut, &packetsIn, &packetsOut); err != nil {
			return nil, err
		}
		ls, ok := seriesMap[tag]
		if !ok {
			ls = &model.LinkTimeSeries{Tag: tag}
			seriesMap[tag] = ls
			order = append(order, tag)
		}
		ls.Points = append(ls.Points, model.TrafficPoint{
			Timestamp: ts, BytesIn: bytesIn, BytesOut: bytesOut,
			PacketsIn: packetsIn, PacketsOut: packetsOut,
		})
	}

	results := make([]model.LinkTimeSeries, 0, len(order))
	for _, tag := range order {
		results = append(results, *seriesMap[tag])
	}
	return results, nil
}

// ASTotals returns total bytes exchanged per IP version for a specific AS.
func (s *ClickHouseStore) ASTotals(ctx context.Context, asn uint32, p QueryParams) (v4In, v4Out, v6In, v6Out uint64, err error) {
	table := pickASTable(p.From, p.To)

	// Swap: AS MV 'out'=download-from-AS, 'in'=upload-to-AS
	query := fmt.Sprintf(`
		SELECT
			sumIf(bytes, direction = 'out' AND ip_version = 4) AS v4_in,
			sumIf(bytes, direction = 'in' AND ip_version = 4) AS v4_out,
			sumIf(bytes, direction = 'out' AND ip_version = 6) AS v6_in,
			sumIf(bytes, direction = 'in' AND ip_version = 6) AS v6_out
		FROM %s
		WHERE as_number = @asn AND ts >= @from AND ts < @to
	`, table)

	err = s.conn.QueryRow(ctx, query,
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	).Scan(&v4In, &v4Out, &v6In, &v6Out)
	if err != nil {
		err = fmt.Errorf("query AS totals: %w", err)
	}
	return
}

// ASRemoteIPs returns the top remote IPs belonging to a given AS.
// Queries flows_raw directly since aggregation tables don't store remote IPs.
func (s *ClickHouseStore) ASRemoteIPs(ctx context.Context, asn uint32, p QueryParams) ([]model.IPTraffic, error) {
	query := `
		SELECT
			replaceRegexpOne(toString(src_ip), '^::ffff:', '') AS ip,
			sum(bytes * sampling_rate) AS total_bytes,
			sum(packets * sampling_rate) AS total_packets,
			count() AS total_flows
		FROM flows_raw
		WHERE src_as = @asn
		  AND timestamp >= @from AND timestamp < @to
		GROUP BY ip
		ORDER BY total_bytes DESC
		LIMIT @limit
	`

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query AS remote IPs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.IPTraffic
	for rows.Next() {
		var r model.IPTraffic
		if err := rows.Scan(&r.IP, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		r.ASNumber = asn
		results = append(results, r)
	}

	return results, nil
}

// ASPeers returns ASes seen in the same flows as the given AS.
func (s *ClickHouseStore) ASPeers(ctx context.Context, asn uint32, p QueryParams) ([]model.ASTraffic, error) {
	query := `
		SELECT
			dst_as AS as_number,
			any(an.as_name) AS as_name,
			sum(bytes * sampling_rate) AS total_bytes,
			sum(packets * sampling_rate) AS total_packets,
			count() AS total_flows
		FROM flows_raw f
		LEFT JOIN as_names an ON dst_as = an.as_number
		WHERE src_as = @asn
		  AND timestamp >= @from AND timestamp < @to
		  AND dst_as > 0 AND dst_as != @asn
		GROUP BY as_number
		ORDER BY total_bytes DESC
		LIMIT @limit
	`

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query AS peers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ASTraffic
	for rows.Next() {
		var r model.ASTraffic
		if err := rows.Scan(&r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// ASTopIPs returns the top internal IPs communicating with a given AS.
func (s *ClickHouseStore) ASTopIPs(ctx context.Context, asn uint32, p QueryParams) ([]model.IPTraffic, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	localFilter := ""
	if p.LocalIPFilter != "" {
		localFilter = "AND " + p.LocalIPFilter
	}

	query := fmt.Sprintf(`
		SELECT
			toString(ip_address) AS ip,
			sum(bytes) AS total_bytes,
			sum(packets) AS total_packets,
			sum(flow_count) AS total_flows
		FROM traffic_by_ip_as
		WHERE as_number = @asn
		  AND ts >= @from AND ts < @to
		  %s %s %s
		GROUP BY ip
		ORDER BY total_bytes DESC
		LIMIT @limit
	`, dirFilter, linkFilter, localFilter)

	args := append([]any{
		clickhouse.Named("asn", asn),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query AS top IPs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.IPTraffic
	for rows.Next() {
		var r model.IPTraffic
		if err := rows.Scan(&r.IP, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		r.IP = cleanIPv4Mapped(r.IP)
		r.ASNumber = asn
		results = append(results, r)
	}

	return results, nil
}

// IPTopAS returns the top ASes communicating with a given internal IP.
func (s *ClickHouseStore) IPTopAS(ctx context.Context, ip string, p QueryParams) ([]model.ASTraffic, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			t.as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_ip_as t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE (toString(t.ip_address) = @ip OR toString(t.ip_address) = concat('::ffff:', @ip))
		  AND t.ts >= @from AND t.ts < @to
		  %s %s
		GROUP BY t.as_number
		ORDER BY total_bytes DESC
		LIMIT @limit
	`, dirFilter, linkFilter)

	args := append([]any{
		clickhouse.Named("ip", ip),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	}, dirArgs...)
	args = append(args, linkArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query IP top AS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ASTraffic
	for rows.Next() {
		var r model.ASTraffic
		if err := rows.Scan(&r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// IPPeerIPs returns the top IPs communicating with a given IP (from flows_raw).
func (s *ClickHouseStore) IPPeerIPs(ctx context.Context, ip string, p QueryParams) ([]model.IPTraffic, error) {
	query := `
		SELECT
			replaceRegexpOne(toString(peer_ip), '^::ffff:', '') AS ip,
			sum(bytes * sampling_rate) AS total_bytes,
			sum(packets * sampling_rate) AS total_packets,
			count() AS total_flows
		FROM (
			SELECT dst_ip AS peer_ip, bytes, packets, sampling_rate
			FROM flows_raw
			WHERE (toString(src_ip) = @ip OR toString(src_ip) = concat('::ffff:', @ip))
			  AND timestamp >= @from AND timestamp < @to
			UNION ALL
			SELECT src_ip AS peer_ip, bytes, packets, sampling_rate
			FROM flows_raw
			WHERE (toString(dst_ip) = @ip OR toString(dst_ip) = concat('::ffff:', @ip))
			  AND timestamp >= @from AND timestamp < @to
		)
		GROUP BY ip
		ORDER BY total_bytes DESC
		LIMIT @limit
	`

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("ip", ip),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query IP peers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.IPTraffic
	for rows.Next() {
		var r model.IPTraffic
		if err := rows.Scan(&r.IP, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// IPInfo returns the AS number, AS name, and prefix for an IP from recent flows.
func (s *ClickHouseStore) IPInfo(ctx context.Context, ip string) (asn uint32, asName string, prefix string, err error) {
	// Find the most common AS + prefix for this IP in recent flows
	query := `
		SELECT src_as, src_prefix, count() c FROM flows_raw
		WHERE (toString(src_ip) = @ip OR toString(src_ip) = concat('::ffff:', @ip))
		  AND src_as > 0 AND timestamp >= now() - INTERVAL 1 DAY
		GROUP BY src_as, src_prefix ORDER BY c DESC LIMIT 1
	`

	var pfx string
	if err = s.conn.QueryRow(ctx, query, clickhouse.Named("ip", ip)).Scan(&asn, &pfx, new(uint64)); err != nil {
		// Try dst side
		query2 := `
			SELECT dst_as, dst_prefix, count() c FROM flows_raw
			WHERE (toString(dst_ip) = @ip OR toString(dst_ip) = concat('::ffff:', @ip))
			  AND dst_as > 0 AND timestamp >= now() - INTERVAL 1 DAY
			GROUP BY dst_as, dst_prefix ORDER BY c DESC LIMIT 1
		`
		if err = s.conn.QueryRow(ctx, query2, clickhouse.Named("ip", ip)).Scan(&asn, &pfx, new(uint64)); err != nil {
			return 0, "", "", nil // No data, not an error
		}
	}
	prefix = pfx

	// Get AS name
	if asn > 0 {
		asName, _ = s.GetASName(ctx, asn)
	}

	return asn, asName, prefix, nil
}

// IPTimeSeries returns traffic time series for a specific IP.
//
// For short windows (<= useRawTableForIP threshold) the query goes against
// flows_raw with the requested bucket so the user gets true sub-5-min
// granularity (1 min on the 1h/3h views, 2 min on 6h). For longer windows
// it falls back to the pre-aggregated traffic_by_ip table (5-min buckets)
// because scanning many hours of flows_raw filtered by IP becomes expensive.
//
// The bucket size in both branches comes from autoStep() — keeping the
// switchover invisible to callers.
func (s *ClickHouseStore) IPTimeSeries(ctx context.Context, ip string, p QueryParams) ([]model.TrafficPoint, error) {
	step := autoStep(p.From, p.To)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	var query string
	if useRawTableForIP(p.From, p.To) {
		// Note: traffic_by_ip aggregates by direction relative to the local IP
		// (in = received, out = sent). flows_raw has no such field, so we
		// reconstruct it by matching the IP against dst_ip (inbound) or
		// src_ip (outbound). toIPv6() handles both IPv4 and IPv6 input strings.
		// The link filter uses the same alias `t` semantics as buildLinkFilter,
		// so we alias flows_raw as `t` to keep the helper queries valid.
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
				sumIf(bytes * sampling_rate,   dst_ip = toIPv6(@ip)) AS bytes_in,
				sumIf(bytes * sampling_rate,   src_ip = toIPv6(@ip)) AS bytes_out,
				sumIf(packets * sampling_rate, dst_ip = toIPv6(@ip)) AS packets_in,
				sumIf(packets * sampling_rate, src_ip = toIPv6(@ip)) AS packets_out
			FROM flows_raw t
			WHERE (t.src_ip = toIPv6(@ip) OR t.dst_ip = toIPv6(@ip))
			  AND t.timestamp >= @from AND t.timestamp < @to
			  %s
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()), linkFilter)
	} else {
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
				sumIf(bytes, direction = 'in') AS bytes_in,
				sumIf(bytes, direction = 'out') AS bytes_out,
				sumIf(packets, direction = 'in') AS packets_in,
				sumIf(packets, direction = 'out') AS packets_out
			FROM traffic_by_ip t
			WHERE (toString(t.ip_address) = @ip OR toString(t.ip_address) = concat('::ffff:', @ip))
			  AND t.ts >= @from AND t.ts < @to
			  %s
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()), linkFilter)
	}

	args := append([]any{
		clickhouse.Named("ip", ip),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, linkArgs...)

	return s.queryTimeSeries(ctx, query, args)
}

// useRawTableForIP is a per-IP variant of useRawTable: scanning flows_raw
// filtered by a single IP is much cheaper than the unfiltered global queries
// (Dashboard chart, etc.) so we can afford to use it for longer windows.
// Going beyond 6h starts to noticeably slow the IP detail page on busy
// networks because the WHERE filter degenerates to a full timestamp scan.
func useRawTableForIP(from, to time.Time) bool {
	return to.Sub(from) <= 6*time.Hour
}

// LinkList returns all known links with their aggregated traffic.
func (s *ClickHouseStore) LinkList(ctx context.Context, p QueryParams) ([]model.LinkTraffic, error) {
	query := `
		SELECT
			t.link_tag,
			any(l.description) AS description,
			any(l.capacity_mbps) AS capacity_mbps,
			sum(t.bytes_in) AS bytes_in,
			sum(t.bytes_out) AS bytes_out
		FROM traffic_by_link t
		LEFT JOIN links l ON t.link_tag = l.tag
		WHERE t.ts >= @from AND t.ts < @to
		GROUP BY t.link_tag
		ORDER BY (bytes_in + bytes_out) DESC
	`

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	)
	if err != nil {
		return nil, fmt.Errorf("query links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.LinkTraffic
	for rows.Next() {
		var r model.LinkTraffic
		if err := rows.Scan(&r.Tag, &r.Description, &r.CapacityMbps, &r.BytesIn, &r.BytesOut); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// LinkTimeSeries returns traffic time series for a specific link.
func (s *ClickHouseStore) LinkTimeSeries(ctx context.Context, tag string, p QueryParams) ([]model.TrafficPoint, error) {
	step := autoStep(p.From, p.To)

	var query string
	if useRawTable(p.From, p.To) {
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
				sumIf(bytes * sampling_rate, direction = 'in') AS bytes_in,
				sumIf(bytes * sampling_rate, direction = 'out') AS bytes_out,
				sumIf(packets * sampling_rate, direction = 'in') AS packets_in,
				sumIf(packets * sampling_rate, direction = 'out') AS packets_out
			FROM flows_raw
			WHERE link_tag = @tag
			  AND timestamp >= @from AND timestamp < @to
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()))
	} else {
		table := pickLinkTable(p.From, p.To)
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
				sum(bytes_in) AS bytes_in,
				sum(bytes_out) AS bytes_out,
				sum(packets_in) AS packets_in,
				sum(packets_out) AS packets_out
			FROM %s
			WHERE link_tag = @tag
			  AND ts >= @from AND ts < @to
			GROUP BY period
			ORDER BY period
		`, int(step.Seconds()), table)
	}

	return s.queryTimeSeries(ctx, query, []any{
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	})
}

// LinkTopAS returns the top ASes on a specific link.
// LinkASTimeSeries returns the top 10 AS on a link with per-AS time series.
func (s *ClickHouseStore) LinkASTimeSeries(ctx context.Context, tag string, p QueryParams) ([]model.ASTrafficDetail, error) {
	step := autoStep(p.From, p.To)
	table := pickASTable(p.From, p.To)

	// Get top 10 AS on this link
	topQuery := fmt.Sprintf(`
		SELECT as_number, any(an.as_name) AS as_name, sum(t.bytes) AS total_bytes
		FROM %s t LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.link_tag = @tag AND t.ts >= @from AND t.ts < @to
		GROUP BY as_number ORDER BY total_bytes DESC LIMIT 10
	`, table)

	rows, err := s.conn.Query(ctx, topQuery,
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	)
	if err != nil {
		return nil, fmt.Errorf("link AS top: %w", err)
	}

	var asns []uint32
	asMap := make(map[uint32]*model.ASTrafficDetail)
	var order []uint32
	for rows.Next() {
		var asn uint32
		var name string
		var bytes uint64
		if err := rows.Scan(&asn, &name, &bytes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		asns = append(asns, asn)
		order = append(order, asn)
		asMap[asn] = &model.ASTrafficDetail{ASNumber: asn, ASName: name, Bytes: bytes}
	}
	_ = rows.Close()

	if len(asns) == 0 {
		return nil, nil
	}

	// Get time series for these AS on this link
	// Swap in/out: AS MV 'out' = download, 'in' = upload
	tsQuery := fmt.Sprintf(`
		SELECT
			t.as_number,
			toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
			sumIf(t.bytes, t.direction = 'out') AS bytes_in,
			sumIf(t.bytes, t.direction = 'in') AS bytes_out
		FROM %s t
		WHERE t.link_tag = @tag AND t.ts >= @from AND t.ts < @to
		  AND t.as_number IN (@asns)
		GROUP BY t.as_number, period
		ORDER BY t.as_number, period
	`, int(step.Seconds()), table)

	tsRows, err := s.conn.Query(ctx, tsQuery,
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("asns", asns),
	)
	if err != nil {
		return nil, fmt.Errorf("link AS series: %w", err)
	}
	defer func() { _ = tsRows.Close() }()

	for tsRows.Next() {
		var asn uint32
		var ts time.Time
		var bytesIn, bytesOut uint64
		if err := tsRows.Scan(&asn, &ts, &bytesIn, &bytesOut); err != nil {
			return nil, err
		}
		if d, ok := asMap[asn]; ok {
			// Use a single "series" entry per AS (tag = AS name for chart label)
			if len(d.Series) == 0 {
				d.Series = []model.LinkTimeSeries{{Tag: fmt.Sprintf("AS%d", asn), Description: d.ASName}}
			}
			d.Series[0].Points = append(d.Series[0].Points, model.TrafficPoint{
				Timestamp: ts, BytesIn: bytesIn, BytesOut: bytesOut,
			})
		}
	}

	results := make([]model.ASTrafficDetail, 0, len(order))
	for _, asn := range order {
		results = append(results, *asMap[asn])
	}
	return results, nil
}

func (s *ClickHouseStore) LinkTopAS(ctx context.Context, tag string, p QueryParams) ([]model.ASTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)

	query := fmt.Sprintf(`
		SELECT
			as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_as t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.link_tag = @tag
		  AND t.ts >= @from AND t.ts < @to
		  %s
		GROUP BY as_number
		ORDER BY total_bytes DESC
		LIMIT @limit
	`, dirFilter)

	args := append([]any{
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	}, dirArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query link top AS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ASTraffic
	for rows.Next() {
		var r model.ASTraffic
		if err := rows.Scan(&r.ASNumber, &r.ASName, &r.Bytes, &r.Packets, &r.Flows); err != nil {
			return nil, 0, err
		}
		results = append(results, r)
	}

	return results, 0, nil
}

// Overview returns dashboard overview data.
func (s *ClickHouseStore) Overview(ctx context.Context, p QueryParams) (*model.Overview, error) {
	ov := &model.Overview{}

	// Total traffic from traffic_by_link (real in/out from interface direction)
	err := s.conn.QueryRow(ctx, `
		SELECT
			sum(bytes_in) AS total_in,
			sum(bytes_out) AS total_out,
			sum(flow_count) AS total_flows
		FROM traffic_by_link
		WHERE ts >= @from AND ts < @to
	`,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	).Scan(&ov.TotalBytesIn, &ov.TotalBytesOut, &ov.TotalFlows)
	if err != nil {
		return nil, fmt.Errorf("overview totals: %w", err)
	}

	// Active AS count
	_ = s.conn.QueryRow(ctx, `
		SELECT uniq(as_number) FROM traffic_by_as
		WHERE ts >= @from AND ts < @to
	`,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	).Scan(&ov.ActiveASCount)

	// Top AS
	topASParams := p
	topASParams.Limit = 10
	ov.TopAS, _, _ = s.TopAS(ctx, topASParams)
	if ov.TopAS == nil {
		ov.TopAS = []model.ASTraffic{}
	}

	// Top IP
	topIPParams := p
	topIPParams.Limit = 10
	ov.TopIP, _, _ = s.TopIP(ctx, topIPParams)
	if ov.TopIP == nil {
		ov.TopIP = []model.IPTraffic{}
	}

	// Links
	ov.Links, _ = s.LinkList(ctx, p)
	if ov.Links == nil {
		ov.Links = []model.LinkTraffic{}
	}

	return ov, nil
}

// SearchAS searches for ASes by number or name.
func (s *ClickHouseStore) SearchAS(ctx context.Context, query string, limit int) ([]model.ASInfo, error) {
	sqlQuery := `
		SELECT as_number, as_name, country
		FROM as_names
		WHERE toString(as_number) LIKE @q OR lower(as_name) LIKE @q
		ORDER BY as_number
		LIMIT @limit
	`

	rows, err := s.conn.Query(ctx, sqlQuery,
		clickhouse.Named("q", "%"+strings.ToLower(query)+"%"),
		clickhouse.Named("limit", limit),
	)
	if err != nil {
		return nil, fmt.Errorf("search AS: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.ASInfo
	for rows.Next() {
		var r model.ASInfo
		if err := rows.Scan(&r.Number, &r.Name, &r.Country); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	return results, nil
}

// ListLinks returns all configured links.
func (s *ClickHouseStore) ListLinks(ctx context.Context) ([]model.Link, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT tag, toString(router_ip), snmp_index, description, capacity_mbps, color
		FROM links FINAL
		ORDER BY tag
	`)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.Link
	for rows.Next() {
		var l model.Link
		var routerIPStr string
		if err := rows.Scan(&l.Tag, &routerIPStr, &l.SNMPIndex, &l.Description, &l.CapacityMbps, &l.Color); err != nil {
			return nil, err
		}
		l.RouterIP = parseIP(routerIPStr)
		results = append(results, l)
	}

	return results, nil
}

// UpsertLink inserts or replaces a link configuration.
func (s *ClickHouseStore) UpsertLink(ctx context.Context, link model.Link) error {
	return s.conn.Exec(ctx, `
		INSERT INTO links (tag, router_ip, snmp_index, description, capacity_mbps, color)
		VALUES (@tag, @router_ip, @snmp_index, @description, @capacity_mbps, @color)
	`,
		clickhouse.Named("tag", link.Tag),
		clickhouse.Named("router_ip", ipToIPv6(link.RouterIP)),
		clickhouse.Named("snmp_index", link.SNMPIndex),
		clickhouse.Named("description", link.Description),
		clickhouse.Named("capacity_mbps", link.CapacityMbps),
		clickhouse.Named("color", link.Color),
	)
}

// DeleteLink removes a link configuration.
func (s *ClickHouseStore) DeleteLink(ctx context.Context, tag string) error {
	return s.conn.Exec(ctx, `ALTER TABLE links DELETE WHERE tag = @tag`,
		clickhouse.Named("tag", tag),
	)
}

// BulkUpsertASNames inserts or updates AS name records.
func (s *ClickHouseStore) BulkUpsertASNames(ctx context.Context, names []model.ASInfo) error {
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO as_names (as_number, as_name, country)`)
	if err != nil {
		return fmt.Errorf("prepare AS names batch: %w", err)
	}

	for _, n := range names {
		if err := batch.Append(n.Number, n.Name, n.Country); err != nil {
			return err
		}
	}

	return batch.Send()
}

// GetASName returns the name for a given AS number.
func (s *ClickHouseStore) GetASName(ctx context.Context, asn uint32) (string, error) {
	var name string
	err := s.conn.QueryRow(ctx, `SELECT as_name FROM as_names FINAL WHERE as_number = @asn`,
		clickhouse.Named("asn", asn),
	).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

// Helper: query time series data
func (s *ClickHouseStore) queryTimeSeries(ctx context.Context, query string, args []any) ([]model.TrafficPoint, error) {
	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []model.TrafficPoint
	for rows.Next() {
		var p model.TrafficPoint
		if err := rows.Scan(&p.Timestamp, &p.BytesIn, &p.BytesOut, &p.PacketsIn, &p.PacketsOut); err != nil {
			return nil, err
		}
		results = append(results, p)
	}

	return results, nil
}

// autoStep determines the aggregation step based on the time range.
func autoStep(from, to time.Time) time.Duration {
	dur := to.Sub(from)
	switch {
	case dur <= 3*time.Hour:
		return 1 * time.Minute
	case dur <= 6*time.Hour:
		return 2 * time.Minute
	case dur <= 36*time.Hour:
		return 5 * time.Minute
	case dur <= 3*24*time.Hour:
		return 15 * time.Minute
	case dur <= 7*24*time.Hour:
		return 30 * time.Minute
	default:
		return 1 * time.Hour
	}
}

// TopASTrafficSeries returns the top N ASes with per-link time series.
// Optionally filtered by ip_version. Used for the dashboard top AS graphs.
func (s *ClickHouseStore) TopASTrafficSeries(ctx context.Context, p QueryParams) ([]model.ASTrafficDetail, error) {
	table := pickASTable(p.From, p.To)
	step := autoStep(p.From, p.To)

	excludeAS := ""
	var excludeArgs []any
	if p.ExcludeAS > 0 {
		excludeAS = "AND as_number != @exclude_as"
		excludeArgs = append(excludeArgs, clickhouse.Named("exclude_as", p.ExcludeAS))
	}

	ipvFilter := ""
	var ipvArgs []any
	if p.IPVersion == 4 || p.IPVersion == 6 {
		ipvFilter = "AND ip_version = @ipv"
		ipvArgs = append(ipvArgs, clickhouse.Named("ipv", p.IPVersion))
	}

	// Get top ASes by total bytes
	var topQuery string
	if useRawTable(p.From, p.To) {
		// From flows_raw, consider both src_as and dst_as
		topQuery = fmt.Sprintf(`
			SELECT as_number, any(an.as_name) AS as_name, sum(total_bytes) AS total_bytes FROM (
				SELECT src_as AS as_number, sum(bytes * sampling_rate) AS total_bytes
				FROM flows_raw WHERE timestamp >= @from AND timestamp < @to AND src_as > 0 %s %s
				GROUP BY src_as
				UNION ALL
				SELECT dst_as AS as_number, sum(bytes * sampling_rate) AS total_bytes
				FROM flows_raw WHERE timestamp >= @from AND timestamp < @to AND dst_as > 0 %s %s
				GROUP BY dst_as
			) t
			LEFT JOIN as_names an ON t.as_number = an.as_number
			GROUP BY as_number
			ORDER BY total_bytes DESC
			LIMIT @limit
		`, excludeAS, ipvFilter, excludeAS, ipvFilter)
	} else {
		topQuery = fmt.Sprintf(`
			SELECT as_number, any(an.as_name) AS as_name, sum(t.bytes) AS total_bytes
			FROM %s t
			LEFT JOIN as_names an ON t.as_number = an.as_number
			WHERE t.ts >= @from AND t.ts < @to %s %s
			GROUP BY as_number
			ORDER BY total_bytes DESC
			LIMIT @limit
		`, table, excludeAS, ipvFilter)
	}

	topArgs := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("limit", p.Limit),
	}, excludeArgs...)
	topArgs = append(topArgs, ipvArgs...)

	rows, err := s.conn.Query(ctx, topQuery, topArgs...)
	if err != nil {
		return nil, fmt.Errorf("query top AS traffic: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var asns []uint32
	asMap := make(map[uint32]*model.ASTrafficDetail)
	var order []uint32
	for rows.Next() {
		var asn uint32
		var name string
		var bytes uint64
		if err := rows.Scan(&asn, &name, &bytes); err != nil {
			return nil, err
		}
		asns = append(asns, asn)
		order = append(order, asn)
		asMap[asn] = &model.ASTrafficDetail{ASNumber: asn, ASName: name, Bytes: bytes}
	}

	if len(asns) == 0 {
		return nil, nil
	}

	// Get per-link time series for these ASes
	var tsQuery string
	if useRawTable(p.From, p.To) {
		// flows_raw: src_as=ASN → download, dst_as=ASN → upload
		tsQuery = fmt.Sprintf(`
			SELECT
				multiIf(t.src_as IN (@asns), t.src_as, t.dst_as) AS as_num,
				toStartOfInterval(t.timestamp, INTERVAL %d SECOND) AS period,
				t.link_tag,
				sumIf(t.bytes * t.sampling_rate, t.src_as IN (@asns)) AS bytes_in,
				sumIf(t.bytes * t.sampling_rate, t.dst_as IN (@asns)) AS bytes_out,
				sumIf(t.packets * t.sampling_rate, t.src_as IN (@asns)) AS packets_in,
				sumIf(t.packets * t.sampling_rate, t.dst_as IN (@asns)) AS packets_out
			FROM flows_raw t
			WHERE t.timestamp >= @from AND t.timestamp < @to
			  AND (t.src_as IN (@asns) OR t.dst_as IN (@asns))
			  AND t.link_tag != ''
			  %s
			GROUP BY as_num, period, t.link_tag
			ORDER BY as_num, period, t.link_tag
		`, int(step.Seconds()), ipvFilter)
	} else {
		tsQuery = fmt.Sprintf(`
			SELECT
				t.as_number,
				toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
				t.link_tag,
				sumIf(t.bytes, t.direction = 'out') AS bytes_in,
				sumIf(t.bytes, t.direction = 'in') AS bytes_out,
				sumIf(t.packets, t.direction = 'out') AS packets_in,
				sumIf(t.packets, t.direction = 'in') AS packets_out
			FROM %s t
			WHERE t.ts >= @from AND t.ts < @to
			  AND t.as_number IN (@asns)
			  %s
			GROUP BY t.as_number, period, t.link_tag
			ORDER BY t.as_number, period, t.link_tag
		`, int(step.Seconds()), table, ipvFilter)
	}

	tsArgs := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("asns", asns),
	}, ipvArgs...)

	tsRows, err := s.conn.Query(ctx, tsQuery, tsArgs...)
	if err != nil {
		return nil, fmt.Errorf("query AS traffic series: %w", err)
	}
	defer func() { _ = tsRows.Close() }()

	// Pivot into per-AS, per-link series
	type linkKey struct {
		asn uint32
		tag string
	}
	linkSeries := make(map[linkKey]*model.LinkTimeSeries)

	for tsRows.Next() {
		var asn uint32
		var ts time.Time
		var tag string
		var bytesIn, bytesOut, packetsIn, packetsOut uint64
		if err := tsRows.Scan(&asn, &ts, &tag, &bytesIn, &bytesOut, &packetsIn, &packetsOut); err != nil {
			return nil, err
		}

		lk := linkKey{asn: asn, tag: tag}
		ls, ok := linkSeries[lk]
		if !ok {
			ls = &model.LinkTimeSeries{Tag: tag}
			linkSeries[lk] = ls
		}
		ls.Points = append(ls.Points, model.TrafficPoint{
			Timestamp:  ts,
			BytesIn:    bytesIn,
			BytesOut:   bytesOut,
			PacketsIn:  packetsIn,
			PacketsOut: packetsOut,
		})
	}

	// Assemble results and compute p95 from time series
	for asn, detail := range asMap {
		// Aggregate all link series into per-period totals for p95
		periodTotals := make(map[time.Time][2]uint64) // [in, out]
		for lk, ls := range linkSeries {
			if lk.asn == asn {
				detail.Series = append(detail.Series, *ls)
				for _, pt := range ls.Points {
					t := periodTotals[pt.Timestamp]
					t[0] += pt.BytesIn
					t[1] += pt.BytesOut
					periodTotals[pt.Timestamp] = t
				}
			}
		}
		// Calculate p95
		if len(periodTotals) > 0 {
			inVals := make([]uint64, 0, len(periodTotals))
			outVals := make([]uint64, 0, len(periodTotals))
			for _, v := range periodTotals {
				inVals = append(inVals, v[0])
				outVals = append(outVals, v[1])
			}
			detail.P95In = percentile95(inVals)
			detail.P95Out = percentile95(outVals)
		}
	}

	results := make([]model.ASTrafficDetail, 0, len(order))
	for _, asn := range order {
		results = append(results, *asMap[asn])
	}

	return results, nil
}

// pickASTable selects the best source table for AS queries based on time range.
//   <= 90 days: traffic_by_as (5-min granularity)
//   <= 2 years: traffic_by_as_hourly (1-hour granularity)
//   > 2 years:  traffic_by_as_daily (1-day granularity)
func pickASTable(from, to time.Time) string {
	dur := to.Sub(from)
	switch {
	case dur <= 90*24*time.Hour:
		return "traffic_by_as"
	case dur <= 730*24*time.Hour:
		return "traffic_by_as_hourly"
	default:
		return "traffic_by_as_daily"
	}
}

// useRawTable returns true if the query should use flows_raw for finer granularity.
func useRawTable(from, to time.Time) bool {
	return to.Sub(from) <= 3*time.Hour
}

// pickLinkTable selects the best source table for link queries based on time range.
// LinksP95 returns the 95th percentile of total in/out traffic across all links.
func (s *ClickHouseStore) LinksP95(ctx context.Context, p QueryParams) (inP95, outP95 uint64, err error) {
	step := autoStep(p.From, p.To)
	table := pickLinkTable(p.From, p.To)

	var query string
	if useRawTable(p.From, p.To) {
		query = fmt.Sprintf(`
			SELECT
				quantile(0.95)(bucket_in) AS p95_in,
				quantile(0.95)(bucket_out) AS p95_out
			FROM (
				SELECT
					toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
					sumIf(bytes * sampling_rate, direction = 'in') AS bucket_in,
					sumIf(bytes * sampling_rate, direction = 'out') AS bucket_out
				FROM flows_raw
				WHERE timestamp >= @from AND timestamp < @to AND link_tag != ''
				GROUP BY period
			)
		`, int(step.Seconds()))
	} else {
		query = fmt.Sprintf(`
			SELECT
				quantile(0.95)(bucket_in) AS p95_in,
				quantile(0.95)(bucket_out) AS p95_out
			FROM (
				SELECT
					toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
					sum(bytes_in) AS bucket_in,
					sum(bytes_out) AS bucket_out
				FROM %s
				WHERE ts >= @from AND ts < @to
				GROUP BY period
			)
		`, int(step.Seconds()), table)
	}

	var fIn, fOut float64
	err = s.conn.QueryRow(ctx, query,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	).Scan(&fIn, &fOut)
	inP95, outP95 = uint64(fIn), uint64(fOut)
	if err != nil {
		err = fmt.Errorf("query links p95: %w", err)
	}
	return
}

// LinkP95 returns the 95th percentile for a specific link.
func (s *ClickHouseStore) LinkP95(ctx context.Context, tag string, p QueryParams) (inP95, outP95 uint64, err error) {
	step := autoStep(p.From, p.To)

	var query string
	if useRawTable(p.From, p.To) {
		query = fmt.Sprintf(`
			SELECT quantile(0.95)(bi), quantile(0.95)(bo) FROM (
				SELECT toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
					sumIf(bytes * sampling_rate, direction = 'in') AS bi,
					sumIf(bytes * sampling_rate, direction = 'out') AS bo
				FROM flows_raw WHERE timestamp >= @from AND timestamp < @to AND link_tag = @tag
				GROUP BY period
			)`, int(step.Seconds()))
	} else {
		table := pickLinkTable(p.From, p.To)
		query = fmt.Sprintf(`
			SELECT quantile(0.95)(bi), quantile(0.95)(bo) FROM (
				SELECT toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
					sum(bytes_in) AS bi, sum(bytes_out) AS bo
				FROM %s WHERE ts >= @from AND ts < @to AND link_tag = @tag
				GROUP BY period
			)`, int(step.Seconds()), table)
	}

	var fIn, fOut float64
	err = s.conn.QueryRow(ctx, query,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("tag", tag),
	).Scan(&fIn, &fOut)
	inP95, outP95 = uint64(fIn), uint64(fOut)
	return
}

// ASP95 returns the 95th percentile for a specific AS, split by ip_version.
func (s *ClickHouseStore) ASP95(ctx context.Context, asn uint32, p QueryParams) (v4In, v4Out, v6In, v6Out uint64, err error) {
	step := autoStep(p.From, p.To)

	var query string
	if useRawTable(p.From, p.To) {
		query = fmt.Sprintf(`
			SELECT
				quantile(0.95)(v4i), quantile(0.95)(v4o),
				quantile(0.95)(v6i), quantile(0.95)(v6o)
			FROM (
				SELECT toStartOfInterval(timestamp, INTERVAL %d SECOND) AS period,
					sumIf(bytes * sampling_rate, src_as = @asn AND ip_version = 4) AS v4i,
					sumIf(bytes * sampling_rate, dst_as = @asn AND ip_version = 4) AS v4o,
					sumIf(bytes * sampling_rate, src_as = @asn AND ip_version = 6) AS v6i,
					sumIf(bytes * sampling_rate, dst_as = @asn AND ip_version = 6) AS v6o
				FROM flows_raw WHERE timestamp >= @from AND timestamp < @to
					AND (src_as = @asn OR dst_as = @asn)
				GROUP BY period
			)`, int(step.Seconds()))
	} else {
		table := pickASTable(p.From, p.To)
		query = fmt.Sprintf(`
			SELECT
				quantile(0.95)(v4i), quantile(0.95)(v4o),
				quantile(0.95)(v6i), quantile(0.95)(v6o)
			FROM (
				SELECT toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
					sumIf(bytes, direction = 'out' AND ip_version = 4) AS v4i,
					sumIf(bytes, direction = 'in' AND ip_version = 4) AS v4o,
					sumIf(bytes, direction = 'out' AND ip_version = 6) AS v6i,
					sumIf(bytes, direction = 'in' AND ip_version = 6) AS v6o
				FROM %s WHERE ts >= @from AND ts < @to AND as_number = @asn
				GROUP BY period
			)`, int(step.Seconds()), table)
	}

	var f1, f2, f3, f4 float64
	err = s.conn.QueryRow(ctx, query,
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
		clickhouse.Named("asn", asn),
	).Scan(&f1, &f2, &f3, &f4)
	v4In, v4Out, v6In, v6Out = uint64(f1), uint64(f2), uint64(f3), uint64(f4)
	return
}

func pickLinkTable(from, to time.Time) string {
	dur := to.Sub(from)
	switch {
	case dur <= 90*24*time.Hour:
		return "traffic_by_link"
	case dur <= 730*24*time.Hour:
		return "traffic_by_link_hourly"
	default:
		return "traffic_by_link_daily"
	}
}

func percentile95(vals []uint64) uint64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := make([]uint64, len(vals))
	copy(sorted, vals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)-1) * 0.95)
	return sorted[idx]
}

func buildDirectionFilter(dir string) (string, []any) {
	switch dir {
	case "in":
		return "AND t.direction = 'in'", nil
	case "out":
		return "AND t.direction = 'out'", nil
	default:
		return "", nil
	}
}

func buildLinkFilter(tags []string) (string, []any) {
	if len(tags) == 0 {
		return "", nil
	}
	if len(tags) == 1 {
		return "AND t.link_tag = @link", []any{clickhouse.Named("link", tags[0])}
	}
	return "AND t.link_tag IN (@links)", []any{clickhouse.Named("links", tags)}
}

// LinksTrafficSeries returns traffic time series for all links, optionally filtered by IP version.
func (s *ClickHouseStore) LinksTrafficSeries(ctx context.Context, p QueryParams) ([]model.LinkTimeSeries, error) {
	step := autoStep(p.From, p.To)

	ipvFilter := ""
	var ipvArgs []any
	if p.IPVersion == 4 || p.IPVersion == 6 {
		ipvFilter = "AND t.ip_version = @ipv"
		ipvArgs = append(ipvArgs, clickhouse.Named("ipv", p.IPVersion))
	}

	var query string
	if useRawTable(p.From, p.To) {
		// Use flows_raw for per-minute granularity
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(t.timestamp, INTERVAL %d SECOND) AS period,
				t.link_tag,
				any(l.description) AS description,
				sumIf(t.bytes * t.sampling_rate, t.direction = 'in') AS bytes_in,
				sumIf(t.bytes * t.sampling_rate, t.direction = 'out') AS bytes_out,
				sumIf(t.packets * t.sampling_rate, t.direction = 'in') AS packets_in,
				sumIf(t.packets * t.sampling_rate, t.direction = 'out') AS packets_out
			FROM flows_raw t
			LEFT JOIN links l ON t.link_tag = l.tag
			WHERE t.timestamp >= @from AND t.timestamp < @to
			  AND t.link_tag != ''
			  %s
			GROUP BY period, t.link_tag
			ORDER BY period, t.link_tag
		`, int(step.Seconds()), ipvFilter)
	} else {
		table := pickLinkTable(p.From, p.To)
		query = fmt.Sprintf(`
			SELECT
				toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
				t.link_tag,
				any(l.description) AS description,
				sum(t.bytes_in) AS bytes_in,
				sum(t.bytes_out) AS bytes_out,
				sum(t.packets_in) AS packets_in,
				sum(t.packets_out) AS packets_out
			FROM %s t
			LEFT JOIN links l ON t.link_tag = l.tag
			WHERE t.ts >= @from AND t.ts < @to
			  AND t.link_tag != ''
			  %s
			GROUP BY period, t.link_tag
			ORDER BY period, t.link_tag
		`, int(step.Seconds()), table, ipvFilter)
	}

	args := append([]any{
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, ipvArgs...)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query links traffic series: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Pivot rows into per-link series
	seriesMap := make(map[string]*model.LinkTimeSeries)
	var order []string

	for rows.Next() {
		var ts time.Time
		var tag, desc string
		var bytesIn, bytesOut, packetsIn, packetsOut uint64

		if err := rows.Scan(&ts, &tag, &desc, &bytesIn, &bytesOut, &packetsIn, &packetsOut); err != nil {
			return nil, err
		}

		ls, ok := seriesMap[tag]
		if !ok {
			ls = &model.LinkTimeSeries{Tag: tag, Description: desc}
			seriesMap[tag] = ls
			order = append(order, tag)
		}
		ls.Points = append(ls.Points, model.TrafficPoint{
			Timestamp:  ts,
			BytesIn:    bytesIn,
			BytesOut:   bytesOut,
			PacketsIn:  packetsIn,
			PacketsOut: packetsOut,
		})
	}

	results := make([]model.LinkTimeSeries, 0, len(order))
	for _, tag := range order {
		results = append(results, *seriesMap[tag])
	}

	return results, nil
}

func parseIP(s string) net.IP {
	return net.ParseIP(s)
}

// cleanIPv4Mapped strips the ::ffff: prefix from IPv4-mapped IPv6 addresses.
func cleanIPv4Mapped(ip string) string {
	if strings.HasPrefix(ip, "::ffff:") {
		return ip[7:]
	}
	return ip
}
