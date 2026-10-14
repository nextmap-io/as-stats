package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// Live Threats queries (GET /threats/live). The handler runs, in order:
//
//  1. ThreatCandidates — one pass over the window of the hot 1-minute table:
//     every local host's window totals, ranked four ways (bytes, packets,
//     peers, SYN), keeping the union of the tops.
//  2. ThreatBaselines — the candidates' per-minute history at the same time
//     of day over the previous 7 days (p50 / p95 of bytes and packets).
//     ThreatPeerBaselines adds the usual peer count, only for the few
//     inbound hosts with ≥ 1000 peers (it reads the heavy HLL column).
//  3. ThreatPorts — a flows_raw port breakdown for at most
//     ThreatPortLookupMax hosts that already look abnormal.
//  4. ThreatSeries — the last 60 complete minutes of the rows returned.
//
// Every query after the first is pinned to the anchor minute the first one
// returns, so they agree on "now" even when they straddle a minute
// boundary. Classification is pure Go in the handler.

// Candidate selection: the union of these per-window tops.
const (
	threatTopBytes   = 50
	threatTopPackets = 50
	threatTopPeers   = 25
	threatTopSyn     = 25

	// ThreatPortLookupMax caps the hosts sent to the flows_raw port lookup.
	ThreatPortLookupMax = 25
	// ThreatPortLookupMaxWindow caps the flows_raw lookup window (seconds).
	ThreatPortLookupMaxWindow = 900
	// ThreatSeriesMinutes is the length of the per-minute series.
	ThreatSeriesMinutes = 60
	// threatBaselineDays × threatBaselineMinutes = the history sampled.
	threatBaselineDays    = 7
	threatBaselineMinutes = 60
)

// threatSQL holds the whitelisted identifiers for one direction. Inbound
// looks at local destinations, outbound at local sources.
type threatSQL struct {
	table    string // hot 1-minute table
	ipCol    string // local-host column (same name in the hot table and flows_raw)
	peersCol string // HLL of the remote IPs
	portsCol string // HLL of the remote ports
	synExpr  string // SYN packets (inbound only)
	portCol  string // flows_raw remote port: source (reflector) port in, destination port out
}

func threatSQLFor(direction string) (threatSQL, error) {
	switch direction {
	case "in":
		return threatSQL{
			table: "traffic_by_dst_1min", ipCol: "dst_ip",
			peersCol: "unique_src_ips", portsCol: "unique_src_ports",
			synExpr: "sum(h.syn_count)", portCol: "src_port",
		}, nil
	case "out":
		return threatSQL{
			table: "traffic_by_src_1min", ipCol: "src_ip",
			peersCol: "unique_dst_ips", portsCol: "unique_dst_ports",
			synExpr: "toUInt64(0)", portCol: "dst_port",
		}, nil
	}
	return threatSQL{}, fmt.Errorf("live threats: invalid direction %q", direction)
}

// ThreatCandidate is one local host's totals over the window. Counters are
// raw sums over the window (bytes, packets, flows), not rates.
type ThreatCandidate struct {
	IP    string // display form (IPv4 without ::ffff:)
	Probe string // the column's own textual form, used for IN lists

	Bytes       uint64
	Packets     uint64
	Flows       uint64
	SynPackets  uint64
	TCPPackets  uint64
	UDPPackets  uint64
	ICMPPackets uint64 // ICMP + ICMPv6
	UDPBytes    uint64

	Peers          uint64 // unique remote IPs over the window
	PeerPorts      uint64 // unique remote ports over the window
	MaxMinutePeers uint64 // highest per-minute unique remote IPs in the window
}

// ThreatHistory is a host's per-minute history at the same time of day over
// the previous days. Values are per minute (bytes, packets), not rates.
type ThreatHistory struct {
	Samples    uint64
	BytesP50   uint64
	BytesP95   uint64
	PacketsP50 uint64
	PacketsP95 uint64
}

// ThreatPortStat is one (protocol, remote port) of a host in the lookup.
type ThreatPortStat struct {
	Protocol uint8
	Port     uint16
	Bytes    uint64
	Flows    uint64
}

// ThreatPortLookup is a host's flows_raw breakdown by remote port. Ports
// holds the top entries by bytes (plus TCP/25 for outbound when present).
type ThreatPortLookup struct {
	WindowSeconds uint32
	TotalBytes    uint64
	TotalFlows    uint64
	Ports         []ThreatPortStat
}

// ThreatCandidates returns the anchor minute (start of the current, still
// filling minute — the window ends just before it) and the candidate hosts:
// the union of the window's top 50 by bytes, top 50 by packets, top 25 by
// peers and (inbound) top 25 by SYN, local prefixes only. window is rounded
// down to whole complete minutes (alertWindow).
func (s *ClickHouseStore) ThreatCandidates(ctx context.Context, direction string, window uint32, localPrefixes []string) (time.Time, []ThreatCandidate, error) {
	q, err := threatSQLFor(direction)
	if err != nil {
		return time.Time{}, nil, err
	}
	where := []string{completeMinutes("h.ts")}
	args := []any{
		clickhouse.Named("window", alertWindow(window)),
		clickhouse.Named("top_b", threatTopBytes),
		clickhouse.Named("top_p", threatTopPackets),
		clickhouse.Named("top_peers", threatTopPeers),
		clickhouse.Named("top_syn", threatTopSyn),
	}
	if len(localPrefixes) > 0 {
		clause, cidrArgs := buildCIDRFilter("h."+q.ipCol, "lt_", localPrefixes)
		where = append(where, clause)
		args = append(args, cidrArgs...)
	}

	// Inner: one row per (host, minute) so the per-minute peer count is
	// available; the HLL states are carried up (uniqMergeState) to merge
	// into the window-wide unique counts. Middle: per-host totals. Outer:
	// rank four ways in one pass and keep the union of the tops. Only the
	// surviving rows are stringified.
	query := fmt.Sprintf(`
		SELECT
			toString(c.ip) AS ip_s,
			c.b, c.p, c.fl, c.syn, c.tcp_p, c.udp_p, c.icmp_p, c.udp_b,
			c.peers, c.peer_ports, c.max_min_peers,
			toUnixTimestamp(toStartOfMinute(now())) AS anchor
		FROM (
			SELECT
				a.*,
				row_number() OVER (ORDER BY a.b DESC)     AS r_b,
				row_number() OVER (ORDER BY a.p DESC)     AS r_p,
				row_number() OVER (ORDER BY a.peers DESC) AS r_peers,
				row_number() OVER (ORDER BY a.syn DESC)   AS r_syn
			FROM (
				SELECT
					m.ip                                  AS ip,
					sum(m.b)                              AS b,
					sum(m.p)                              AS p,
					sum(m.fl)                             AS fl,
					sum(m.syn)                            AS syn,
					sum(m.tcp_p)                          AS tcp_p,
					sum(m.udp_p)                          AS udp_p,
					sum(m.icmp_p)                         AS icmp_p,
					sum(m.udp_b)                          AS udp_b,
					uniqMerge(m.peers_st)                 AS peers,
					uniqMerge(m.ports_st)                 AS peer_ports,
					max(finalizeAggregation(m.peers_st))  AS max_min_peers
				FROM (
					SELECT
						h.%[1]s                                AS ip,
						h.ts                                   AS minute,
						sum(h.bytes)                           AS b,
						sum(h.packets)                         AS p,
						sum(h.flow_count)                      AS fl,
						%[2]s                                  AS syn,
						sumIf(h.packets, h.protocol = 6)       AS tcp_p,
						sumIf(h.packets, h.protocol = 17)      AS udp_p,
						sumIf(h.packets, h.protocol IN (1, 58)) AS icmp_p,
						sumIf(h.bytes, h.protocol = 17)        AS udp_b,
						uniqMergeState(h.%[3]s)                AS peers_st,
						uniqMergeState(h.%[4]s)                AS ports_st
					FROM %[5]s AS h
					WHERE %[6]s
					GROUP BY h.%[1]s, h.ts
				) AS m
				GROUP BY m.ip
			) AS a
		) AS c
		WHERE c.r_b <= @top_b OR c.r_p <= @top_p OR c.r_peers <= @top_peers
		   OR (c.r_syn <= @top_syn AND c.syn > 0)
		ORDER BY c.b DESC
	`, q.ipCol, q.synExpr, q.peersCol, q.portsCol, q.table, strings.Join(where, " AND "))

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("live threats candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		out    []ThreatCandidate
		anchor uint32
	)
	for rows.Next() {
		var c ThreatCandidate
		if err := rows.Scan(&c.Probe, &c.Bytes, &c.Packets, &c.Flows, &c.SynPackets,
			&c.TCPPackets, &c.UDPPackets, &c.ICMPPackets, &c.UDPBytes,
			&c.Peers, &c.PeerPorts, &c.MaxMinutePeers, &anchor); err != nil {
			return time.Time{}, nil, err
		}
		c.IP = cleanIPv4Mapped(c.Probe)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, nil, err
	}
	return time.Unix(int64(anchor), 0).UTC(), out, nil
}

// baselineRanges returns the time filter for the history: for each of the
// previous threatBaselineDays days, the threatBaselineMinutes minutes centred
// on the same time of day as anchor. Separate ranges (not a toHour() match)
// so the primary key on ts prunes everything else. Only Go constants are
// formatted into the SQL.
func baselineRanges(col string) string {
	half := threatBaselineMinutes * 60 / 2
	parts := make([]string, 0, threatBaselineDays)
	for d := 1; d <= threatBaselineDays; d++ {
		parts = append(parts, fmt.Sprintf(
			"(%[1]s >= toDateTime(@anchor, 'UTC') - INTERVAL %[2]d SECOND AND %[1]s < toDateTime(@anchor, 'UTC') - INTERVAL %[3]d SECOND)",
			col, d*86400+half, d*86400-half))
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// ipsInList is the typed membership test for a list of candidate probes:
// the strings are converted to IPv6 once, the column is never stringified.
// The leading key of the hot tables is ts, so this filters rows within the
// time ranges rather than pruning granules.
func ipsInList(col string) string {
	return col + " IN (SELECT toIPv6(arrayJoin(@ips)))"
}

// ThreatBaselines returns, per candidate IP (display form), the per-minute
// p50 / p95 of bytes and packets over the same time of day on the previous
// 7 days. Minutes without traffic are not samples: the percentiles describe
// the host when it is active. Hosts without history are absent from the map.
//
// It deliberately does not read the HLL peer columns: they dominate the
// bytes read (10k hosts: ~1 GB with them, ~0.3 GB without) — see
// ThreatPeerBaselines.
func (s *ClickHouseStore) ThreatBaselines(ctx context.Context, direction string, anchor time.Time, probes []string) (map[string]ThreatHistory, error) {
	q, err := threatSQLFor(direction)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ThreatHistory, len(probes))
	if len(probes) == 0 {
		return out, nil
	}
	query := fmt.Sprintf(`
		SELECT
			toString(x.ip)                  AS ip_s,
			count()                         AS samples,
			quantilesExact(0.5, 0.95)(x.mb) AS qb,
			quantilesExact(0.5, 0.95)(x.mp) AS qp
		FROM (
			SELECT
				h.%[1]s        AS ip,
				h.ts           AS minute,
				sum(h.bytes)   AS mb,
				sum(h.packets) AS mp
			FROM %[2]s AS h
			WHERE %[3]s AND %[4]s
			GROUP BY h.%[1]s, h.ts
		) AS x
		GROUP BY x.ip
	`, q.ipCol, q.table, baselineRanges("h.ts"), ipsInList("h."+q.ipCol))

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("anchor", uint32(anchor.Unix())),
		clickhouse.Named("ips", probes),
	)
	if err != nil {
		return nil, fmt.Errorf("live threats baseline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			ip     string
			h      ThreatHistory
			qb, qp []uint64
		)
		if err := rows.Scan(&ip, &h.Samples, &qb, &qp); err != nil {
			return nil, err
		}
		if len(qb) == 2 && len(qp) == 2 {
			h.BytesP50, h.BytesP95 = qb[0], qb[1]
			h.PacketsP50, h.PacketsP95 = qp[0], qp[1]
		}
		out[cleanIPv4Mapped(ip)] = h
	}
	return out, rows.Err()
}

// ThreatPeerBaselines returns, per IP (display form), the median per-minute
// number of unique remote IPs over the same history as ThreatBaselines.
// Reading the HLL column is the expensive part of the history, so the
// handler calls this only for the few hosts whose peer count could make a
// many_sources signature (at most ThreatPortLookupMax).
func (s *ClickHouseStore) ThreatPeerBaselines(ctx context.Context, direction string, anchor time.Time, probes []string) (map[string]uint64, error) {
	q, err := threatSQLFor(direction)
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint64, len(probes))
	if len(probes) == 0 {
		return out, nil
	}
	if len(probes) > ThreatPortLookupMax {
		probes = probes[:ThreatPortLookupMax]
	}
	query := fmt.Sprintf(`
		SELECT toString(x.ip) AS ip_s, toUInt64(quantileExact(0.5)(x.mpeers)) AS peers50
		FROM (
			SELECT h.%[1]s AS ip, h.ts AS minute, uniqMerge(h.%[2]s) AS mpeers
			FROM %[3]s AS h
			WHERE %[4]s AND %[5]s
			GROUP BY h.%[1]s, h.ts
		) AS x
		GROUP BY x.ip
	`, q.ipCol, q.peersCol, q.table, baselineRanges("h.ts"), ipsInList("h."+q.ipCol))
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("anchor", uint32(anchor.Unix())),
		clickhouse.Named("ips", probes),
	)
	if err != nil {
		return nil, fmt.Errorf("live threats peer baseline: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			ip  string
			p50 uint64
		)
		if err := rows.Scan(&ip, &p50); err != nil {
			return nil, err
		}
		out[cleanIPv4Mapped(ip)] = p50
	}
	return out, rows.Err()
}

// ThreatSeries returns, per IP (display form), the last ThreatSeriesMinutes
// complete minutes before anchor, one zero-filled point per minute.
func (s *ClickHouseStore) ThreatSeries(ctx context.Context, direction string, anchor time.Time, probes []string) (map[string][]model.ThreatPoint, error) {
	q, err := threatSQLFor(direction)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]model.ThreatPoint, len(probes))
	if len(probes) == 0 {
		return out, nil
	}
	start := anchor.Add(-ThreatSeriesMinutes * time.Minute)
	for _, p := range probes {
		pts := make([]model.ThreatPoint, ThreatSeriesMinutes)
		for i := range pts {
			pts[i].T = start.Add(time.Duration(i) * time.Minute)
		}
		out[cleanIPv4Mapped(p)] = pts
	}

	query := fmt.Sprintf(`
		SELECT
			toString(h.%[1]s)                AS ip_s,
			toUnixTimestamp(h.ts)            AS minute,
			sum(h.bytes)                     AS mb,
			sum(h.packets)                   AS mp
		FROM %[2]s AS h
		WHERE h.ts >= toDateTime(@anchor, 'UTC') - INTERVAL @span SECOND
		  AND h.ts < toDateTime(@anchor, 'UTC')
		  AND %[3]s
		GROUP BY h.%[1]s, h.ts
	`, q.ipCol, q.table, ipsInList("h."+q.ipCol))
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("anchor", uint32(anchor.Unix())),
		clickhouse.Named("span", ThreatSeriesMinutes*60),
		clickhouse.Named("ips", probes),
	)
	if err != nil {
		return nil, fmt.Errorf("live threats series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			ip     string
			minute uint32
			mb, mp uint64
		)
		if err := rows.Scan(&ip, &minute, &mb, &mp); err != nil {
			return nil, err
		}
		pts, ok := out[cleanIPv4Mapped(ip)]
		if !ok {
			continue
		}
		i := int((int64(minute) - start.Unix()) / 60)
		if i < 0 || i >= len(pts) {
			continue
		}
		pts[i].BPS = mb * 8 / 60
		pts[i].PPS = mp / 60
	}
	return out, rows.Err()
}

// ThreatPortLookupWindow is the flows_raw window used for a Live Threats
// window: the same complete minutes, capped at ThreatPortLookupMaxWindow.
func ThreatPortLookupWindow(window uint32) uint32 {
	return min(alertWindow(window), ThreatPortLookupMaxWindow)
}

// ThreatPorts breaks the given hosts' flows_raw traffic down by remote port
// over the lookup window ending at anchor: the source port of inbound
// traffic (reflector port), the destination port of outbound traffic. It
// returns per host the totals and the top 5 ports by bytes, plus TCP/25 for
// SMTP detection. At most ThreatPortLookupMax hosts are queried — the
// caller sends only rows that already look abnormal.
func (s *ClickHouseStore) ThreatPorts(ctx context.Context, direction string, anchor time.Time, window uint32, probes []string) (map[string]ThreatPortLookup, error) {
	q, err := threatSQLFor(direction)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ThreatPortLookup, len(probes))
	if len(probes) == 0 {
		return out, nil
	}
	if len(probes) > ThreatPortLookupMax {
		probes = probes[:ThreatPortLookupMax]
	}
	pw := ThreatPortLookupWindow(window)

	// The window totals are computed over all of the host's ports before
	// LIMIT BY keeps the top ones, so shares are exact.
	query := fmt.Sprintf(`
		SELECT
			toString(x.ip) AS ip_s, x.proto, x.port, x.b, x.c, x.tot_b, x.tot_c
		FROM (
			SELECT
				f.%[1]s                                                    AS ip,
				f.protocol                                                 AS proto,
				f.%[2]s                                                    AS port,
				sum(f.bytes * f.sampling_rate)                             AS b,
				count()                                                    AS c,
				sum(sum(f.bytes * f.sampling_rate)) OVER (PARTITION BY f.%[1]s) AS tot_b,
				sum(count()) OVER (PARTITION BY f.%[1]s)                   AS tot_c
			FROM flows_raw AS f
			WHERE f.timestamp >= toDateTime(@anchor, 'UTC') - INTERVAL @pw SECOND
			  AND f.timestamp < toDateTime(@anchor, 'UTC')
			  AND %[3]s
			GROUP BY f.%[1]s, f.protocol, f.%[2]s
		) AS x
		ORDER BY x.ip, (x.proto = 6 AND x.port = 25) DESC, x.b DESC
		LIMIT 6 BY x.ip
	`, q.ipCol, q.portCol, ipsInList("f."+q.ipCol))
	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("anchor", uint32(anchor.Unix())),
		clickhouse.Named("pw", pw),
		clickhouse.Named("ips", probes),
	)
	if err != nil {
		return nil, fmt.Errorf("live threats ports: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			ip         string
			st         ThreatPortStat
			totB, totC uint64
		)
		if err := rows.Scan(&ip, &st.Protocol, &st.Port, &st.Bytes, &st.Flows, &totB, &totC); err != nil {
			return nil, err
		}
		key := cleanIPv4Mapped(ip)
		l := out[key]
		l.WindowSeconds = pw
		l.TotalBytes, l.TotalFlows = totB, totC
		l.Ports = append(l.Ports, st)
		out[key] = l
	}
	return out, rows.Err()
}
