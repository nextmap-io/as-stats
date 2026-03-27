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

// TopAS returns the top ASes by traffic volume.
func (s *ClickHouseStore) TopAS(ctx context.Context, p QueryParams) ([]model.ASTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)
	excludeAS := ""
	var excludeArgs []any
	if p.ExcludeAS > 0 {
		excludeAS = "AND t.as_number != @exclude_as"
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
func (s *ClickHouseStore) TopIP(ctx context.Context, p QueryParams) ([]model.IPTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

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
		%s %s
		GROUP BY ip, t.as_number
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
func (s *ClickHouseStore) TopPrefix(ctx context.Context, p QueryParams) ([]model.PrefixTraffic, uint64, error) {
	dirFilter, dirArgs := buildDirectionFilter(p.Direction)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			t.prefix,
			t.as_number,
			any(an.as_name) AS as_name,
			sum(t.bytes) AS total_bytes,
			sum(t.packets) AS total_packets,
			sum(t.flow_count) AS total_flows
		FROM traffic_by_prefix t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to
		%s %s
		GROUP BY t.prefix, t.as_number
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

	table := pickASTable(p.From, p.To)

	query := fmt.Sprintf(`
		SELECT
			toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
			sumIf(bytes, direction = 'in') AS bytes_in,
			sumIf(bytes, direction = 'out') AS bytes_out,
			sumIf(packets, direction = 'in') AS packets_in,
			sumIf(packets, direction = 'out') AS packets_out
		FROM %s
		WHERE as_number = @asn
		  AND ts >= @from AND ts < @to
		  %s
		GROUP BY period
		ORDER BY period
	`, int(step.Seconds()), table, linkFilter)

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
	table := pickASTable(p.From, p.To)

	ipvFilter := ""
	var ipvArgs []any
	if p.IPVersion == 4 || p.IPVersion == 6 {
		ipvFilter = "AND t.ip_version = @ipv"
		ipvArgs = append(ipvArgs, clickhouse.Named("ipv", p.IPVersion))
	}

	query := fmt.Sprintf(`
		SELECT
			toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
			t.link_tag,
			sumIf(t.bytes, t.direction = 'in') AS bytes_in,
			sumIf(t.bytes, t.direction = 'out') AS bytes_out,
			sumIf(t.packets, t.direction = 'in') AS packets_in,
			sumIf(t.packets, t.direction = 'out') AS packets_out
		FROM %s t
		WHERE t.as_number = @asn
		  AND t.ts >= @from AND t.ts < @to
		  AND t.link_tag != ''
		  %s
		GROUP BY period, t.link_tag
		ORDER BY period, t.link_tag
	`, int(step.Seconds()), table, ipvFilter)

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

	query := fmt.Sprintf(`
		SELECT
			sumIf(bytes, direction = 'in' AND ip_version = 4) AS v4_in,
			sumIf(bytes, direction = 'out' AND ip_version = 4) AS v4_out,
			sumIf(bytes, direction = 'in' AND ip_version = 6) AS v6_in,
			sumIf(bytes, direction = 'out' AND ip_version = 6) AS v6_out
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
		WHERE toString(t.ip_address) = @ip
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

// IPTimeSeries returns traffic time series for a specific IP.
func (s *ClickHouseStore) IPTimeSeries(ctx context.Context, ip string, p QueryParams) ([]model.TrafficPoint, error) {
	step := autoStep(p.From, p.To)
	linkFilter, linkArgs := buildLinkFilter(p.LinkTags)

	query := fmt.Sprintf(`
		SELECT
			toStartOfInterval(ts, INTERVAL %d SECOND) AS period,
			sumIf(bytes, direction = 'in') AS bytes_in,
			sumIf(bytes, direction = 'out') AS bytes_out,
			sumIf(packets, direction = 'in') AS packets_in,
			sumIf(packets, direction = 'out') AS packets_out
		FROM traffic_by_ip
		WHERE toString(ip_address) = @ip
		  AND ts >= @from AND ts < @to
		  %s
		GROUP BY period
		ORDER BY period
	`, int(step.Seconds()), linkFilter)

	args := append([]any{
		clickhouse.Named("ip", ip),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	}, linkArgs...)

	return s.queryTimeSeries(ctx, query, args)
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
	table := pickLinkTable(p.From, p.To)

	query := fmt.Sprintf(`
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

	return s.queryTimeSeries(ctx, query, []any{
		clickhouse.Named("tag", tag),
		clickhouse.Named("from", p.From),
		clickhouse.Named("to", p.To),
	})
}

// LinkTopAS returns the top ASes on a specific link.
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

	// Total traffic (use traffic_by_as — works even without configured links)
	err := s.conn.QueryRow(ctx, `
		SELECT
			sumIf(bytes, direction = 'in') AS total_in,
			sumIf(bytes, direction = 'out') AS total_out,
			sum(flow_count) AS total_flows
		FROM traffic_by_as
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
		SELECT tag, toString(router_ip), snmp_index, description, capacity_mbps
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
		if err := rows.Scan(&l.Tag, &routerIPStr, &l.SNMPIndex, &l.Description, &l.CapacityMbps); err != nil {
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
		INSERT INTO links (tag, router_ip, snmp_index, description, capacity_mbps)
		VALUES (@tag, @router_ip, @snmp_index, @description, @capacity_mbps)
	`,
		clickhouse.Named("tag", link.Tag),
		clickhouse.Named("router_ip", ipToIPv6(link.RouterIP)),
		clickhouse.Named("snmp_index", link.SNMPIndex),
		clickhouse.Named("description", link.Description),
		clickhouse.Named("capacity_mbps", link.CapacityMbps),
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
	case dur <= 6*time.Hour:
		return 5 * time.Minute
	case dur <= 2*24*time.Hour:
		return 15 * time.Minute
	case dur <= 7*24*time.Hour:
		return 1 * time.Hour
	default:
		return 24 * time.Hour
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
	topQuery := fmt.Sprintf(`
		SELECT as_number, any(an.as_name) AS as_name, sum(t.bytes) AS total_bytes
		FROM %s t
		LEFT JOIN as_names an ON t.as_number = an.as_number
		WHERE t.ts >= @from AND t.ts < @to %s %s
		GROUP BY as_number
		ORDER BY total_bytes DESC
		LIMIT @limit
	`, table, excludeAS, ipvFilter)

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
	tsQuery := fmt.Sprintf(`
		SELECT
			t.as_number,
			toStartOfInterval(t.ts, INTERVAL %d SECOND) AS period,
			t.link_tag,
			sumIf(t.bytes, t.direction = 'in') AS bytes_in,
			sumIf(t.bytes, t.direction = 'out') AS bytes_out,
			sumIf(t.packets, t.direction = 'in') AS packets_in,
			sumIf(t.packets, t.direction = 'out') AS packets_out
		FROM %s t
		WHERE t.ts >= @from AND t.ts < @to
		  AND t.as_number IN (@asns)
		  %s
		GROUP BY t.as_number, period, t.link_tag
		ORDER BY t.as_number, period, t.link_tag
	`, int(step.Seconds()), table, ipvFilter)

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

	// Assemble results
	for asn, detail := range asMap {
		for lk, ls := range linkSeries {
			if lk.asn == asn {
				detail.Series = append(detail.Series, *ls)
			}
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

// pickLinkTable selects the best source table for link queries based on time range.
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

	table := pickLinkTable(p.From, p.To)

	query := fmt.Sprintf(`
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
