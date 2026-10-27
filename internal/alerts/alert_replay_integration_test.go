package alerts

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/migrate"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
	schema "github.com/nextmap-io/as-stats/migrations"
)

// Alert replay: the 3.6 engine and this one, evaluated minute by minute over
// the same day of synthetic traffic in a throwaway ClickHouse database.
//
//	ASSTATS_IT_CLICKHOUSE=127.0.0.1:9000 go test ./internal/alerts -run IntegrationAlertReplay -v
//	ASSTATS_IT_REPLAY=full …   # the whole day (default 08:30–22:30, every attack)
//
// The database (asstats_replay, dropped afterwards) gets every migration,
// 7 days of same-time-of-day history written straight into the hot 1-minute
// tables (only bytes / packets are read from history), and the replay day in
// flows_raw, so the hot tables fill through their materialised views exactly
// as in production. Hosts (198.51.100.0/24, 2001:db8:5::/48):
//
//   - benign: diurnal busy servers with short bursts (the production pattern:
//     usual p50 ~200 Mb/s, p95 ~800 Mb/s), a busy IPv6 sender, servers
//     answering thousands of clients (TCP 443, UDP 53 → client ports), a NAT /
//     P2P gateway connecting to random high ports with 8 % unanswered SYNs;
//   - attacks: SYN flood, UDP flood, NTP amplification, an outbound SYN scan
//     of sequential ports, a quiet host suddenly exfiltrating, a 2.5 Gb/s
//     flood on one of the busy servers, a 2.2 Gb/s flood on a host with no
//     history.
//
// "Old" is the 3.6.1 decision logic over the same store queries: rules
// without baseline factors, port_scan = the hot-table unique destination
// ports (PortScanCandidates, the 3.6 query), and the 3.6 lifecycle (in-memory
// cooldown, a new alert whenever no alert is *active*). "New" is the real
// Engine with this release's default rules. A third run, the new engine with
// the OLD rules, shows what an install whose rules were edited (so not
// reconciled) gets: re-arm + port-scan heuristic, no baselines. The alert
// lifecycle is kept in memory (replayStore) so a 24 h replay runs in minutes;
// the evaluation queries are the production ones.

var replayPrefixes = []string{"198.51.100.0/24", "2001:db8:5::/48"}

// seedHost is one traffic component of one host. Several components can
// share an IP (a quiet profile plus an attack overlay).
type seedHost struct {
	ip      string
	dir     string // "in": the host is the destination; "out": the source
	proto   uint8
	bps     string // SQL bits/s over m (absolute minute) and mday (minute of day)
	pkt     int    // bytes per packet
	history bool   // also write 7 days of hot-table history with the same bps
	from    int    // replay-day minutes [from, to) with traffic
	to      int    // 0 = 1440
	rows    int    // flow records per minute in flows_raw
	flags   uint8
	lport   string // SQL local port over j (record), mi (minute of the replay day)
	rport   string // SQL remote port
	rip     string // SQL UInt32 remote IPv4 over j, mi, m; "" = hash over `remotes`
	remotes int
	x       float64 // replay-day multiplier (growth vs history); 0 = 1
}

// replayScenario labels an IP and what each engine must do with it.
type replayScenario struct {
	label  string
	ip     string
	benign bool
	// attacks: [from, to) replay minutes and the rules the new engine must fire.
	from, to int
	// expect: rule name → replay minute from which the new engine must fire it.
	expect map[string]int
}

const (
	ruleHighIn   = "High inbound volume"
	ruleCritIn   = "Critical inbound volume"
	ruleHighOut  = "High outbound volume (compromised host)"
	ruleExfil    = "Sustained outbound exfiltration"
	rulePortScan = "Port scan from internal host"
	ruleSyn      = "SYN flood"
	ruleUDP      = "UDP flood"
	ruleAmp      = "Reflection/amplification attack"
	ruleCarpetW  = "Carpet bomb /24 (warning)"
)

func diurnal(lo, hi float64) string {
	return fmt.Sprintf("(%g + %g * (0.5 - 0.5 * cos(2 * pi() * (mday - 240) / 1440)))", lo, hi-lo)
}
func jitter(seed int) string {
	return fmt.Sprintf("(0.85 + (cityHash64(m, %d, 1) %% 300) / 1000)", seed)
}
func burst(seed, pct int, x float64) string {
	return fmt.Sprintf("if(cityHash64(m, %d, 2) %% 100 < %d, %g, 1)", seed, pct, x)
}

// clientPorts draws client ports from a pool whose size follows the day
// (lo..hi ports, jittered): how many distinct ports a server replies to in a
// minute then crosses the 3.6 port_scan threshold on and off.
func clientPorts(lo, hi float64, seed int) string {
	return "toUInt16(1024 + cityHash64(@seed, m, j, 3) % toUInt64(greatest(1, " + diurnal(lo, hi) + " * " + jitter(seed) + ")))"
}

const (
	randHighPort = "toUInt16(1024 + cityHash64(@seed, m, j, 3) % 64511)"
	mbps         = 1e6
)

func replayHosts() ([]seedHost, []replayScenario) {
	h := []seedHost{
		// ── benign ─────────────────────────────────────────────────────
		// Busy web server, bursty (8 % of minutes x2.6): in and out. The
		// replay day is 25 % busier than its history.
		{ip: "198.51.100.10", dir: "in", proto: 6, bps: diurnal(120*mbps, 330*mbps) + "*" + jitter(10) + "*" + burst(10, 8, 2.6),
			pkt: 1200, history: true, rows: 6, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 5000, x: 1.25},
		{ip: "198.51.100.10", dir: "out", proto: 6, bps: diurnal(100*mbps, 300*mbps) + "*" + jitter(11) + "*" + burst(11, 8, 2.8),
			pkt: 1200, history: true, rows: 6, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 5000, x: 1.25},
		// Busy sender (backups / CDN origin).
		{ip: "198.51.100.11", dir: "out", proto: 6, bps: diurnal(180*mbps, 420*mbps) + "*" + jitter(12) + "*" + burst(12, 10, 2.2),
			pkt: 1400, history: true, rows: 6, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 500},
		// Busy IPv6 sender, 30–110 Mb/s.
		{ip: "2001:db8:5::12:8224", dir: "out", proto: 6, bps: diurnal(30*mbps, 110*mbps) + "*" + jitter(13) + "*" + burst(13, 5, 2),
			pkt: 1300, history: true, rows: 6, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 900},
		// Servers answering clients: replies to ephemeral ports.
		{ip: "198.51.100.20", dir: "out", proto: 6, bps: diurnal(40*mbps, 120*mbps) + "*" + jitter(20),
			pkt: 1200, history: true, rows: 1500, flags: 0x18, lport: "toUInt16(443)", rport: clientPorts(500, 3000, 20), remotes: 20000},
		{ip: "198.51.100.21", dir: "out", proto: 6, bps: diurnal(20*mbps, 60*mbps) + "*" + jitter(21),
			pkt: 900, history: true, rows: 1300, flags: 0x1a, lport: "toUInt16(8443)", rport: randHighPort, remotes: 20000},
		{ip: "198.51.100.22", dir: "out", proto: 17, bps: diurnal(10*mbps, 40*mbps) + "*" + jitter(22),
			pkt: 300, history: true, rows: 1300, lport: "toUInt16(53)", rport: clientPorts(600, 2500, 22), remotes: 20000},
		// NAT / P2P gateway: outbound to random high ports, 8 % unanswered SYN.
		{ip: "198.51.100.23", dir: "out", proto: 6, bps: diurnal(10*mbps, 40*mbps) + "*" + jitter(23),
			pkt: 800, history: true, rows: 1300, flags: 0x1b, lport: randHighPort, rport: clientPorts(600, 4000, 23), remotes: 20000},
		{ip: "198.51.100.23", dir: "out", proto: 6, bps: "200000",
			pkt: 60, rows: 110, flags: 0x02, lport: randHighPort, rport: randHighPort, remotes: 20000},

		// ── attacks: quiet usual profiles … ───────────────────────────
		{ip: "198.51.100.40", dir: "in", proto: 6, bps: diurnal(5*mbps, 20*mbps) + "*" + jitter(40),
			pkt: 900, history: true, rows: 20, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 300},
		{ip: "198.51.100.41", dir: "in", proto: 6, bps: diurnal(2*mbps, 10*mbps) + "*" + jitter(41),
			pkt: 900, history: true, rows: 20, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 300},
		{ip: "198.51.100.42", dir: "in", proto: 6, bps: diurnal(5*mbps, 15*mbps) + "*" + jitter(42),
			pkt: 900, history: true, rows: 20, flags: 0x18, lport: "toUInt16(443)", rport: randHighPort, remotes: 300},
		{ip: "198.51.100.50", dir: "out", proto: 6, bps: diurnal(1*mbps, 5*mbps) + "*" + jitter(50),
			pkt: 900, history: true, rows: 30, flags: 0x1b, lport: randHighPort, rport: "toUInt16(443)", remotes: 300},
		{ip: "198.51.100.51", dir: "out", proto: 6, bps: diurnal(2*mbps, 6*mbps) + "*" + jitter(51),
			pkt: 900, history: true, rows: 30, flags: 0x18, lport: "toUInt16(22)", rport: randHighPort, remotes: 50},

		// ── … and the attacks themselves (replay day only) ─────────────
		// A1 SYN flood 09:00–09:15: 80k SYN/s of 60-byte packets.
		{ip: "198.51.100.40", dir: "in", proto: 6, bps: "38400000", pkt: 60, from: 540, to: 555,
			rows: 600, flags: 0x02, lport: "toUInt16(443)", rport: randHighPort, remotes: 50000},
		// A2 UDP flood 11:00–11:10: 150k pps x 1000 B = 1.2 Gb/s.
		{ip: "198.51.100.41", dir: "in", proto: 17, bps: "1200000000", pkt: 1000, from: 660, to: 670,
			rows: 500, lport: randHighPort, rport: randHighPort, remotes: 5000},
		// A3 NTP amplification 13:00–13:10: 3 Gb/s from 12k reflectors, port 123, 468 B.
		{ip: "198.51.100.42", dir: "in", proto: 17, bps: "3000000000", pkt: 468, from: 780, to: 790,
			rows: 12000, lport: randHighPort, rport: "toUInt16(123)", rip: "toUInt32(3405803776 + (j + mi * 7) % 20000)"},
		// A4 outbound SYN scan 15:00–15:30: 2000 sequential ports a minute, one target.
		{ip: "198.51.100.50", dir: "out", proto: 6, bps: "16000", pkt: 60, from: 900, to: 930,
			rows: 2000, flags: 0x02, lport: "toUInt16(40000 + j % 100)", rport: "toUInt16(1 + ((mi - 900) * 2000 + j) % 65535)", remotes: 1},
		// A5 exfiltration from a quiet host: 150 Mb/s 17:00–19:00, then 700 Mb/s to 19:20.
		{ip: "198.51.100.51", dir: "out", proto: 6, bps: "150000000", pkt: 1400, from: 1020, to: 1140,
			rows: 50, flags: 0x18, lport: "toUInt16(22)", rport: randHighPort, remotes: 3},
		{ip: "198.51.100.51", dir: "out", proto: 6, bps: "700000000", pkt: 1400, from: 1140, to: 1160,
			rows: 50, flags: 0x18, lport: "toUInt16(22)", rport: randHighPort, remotes: 3},
		// A6 2.5 Gb/s UDP flood on the busy web server 20:00–20:05.
		{ip: "198.51.100.10", dir: "in", proto: 17, bps: "2500000000", pkt: 1200, from: 1200, to: 1205,
			rows: 500, lport: randHighPort, rport: randHighPort, remotes: 5000},
		// A7 2.2 Gb/s UDP flood on a host never seen before, 22:00–22:05.
		{ip: "198.51.100.60", dir: "in", proto: 17, bps: "2200000000", pkt: 1200, from: 1320, to: 1325,
			rows: 500, lport: randHighPort, rport: randHighPort, remotes: 5000},
	}
	sc := []replayScenario{
		{label: "busy web server, bursty (in+out)", ip: "198.51.100.10", benign: true},
		{label: "busy sender", ip: "198.51.100.11", benign: true},
		{label: "busy IPv6 sender", ip: "2001:db8:5::12:8224", benign: true},
		{label: "TCP 443 server answering clients", ip: "198.51.100.20", benign: true},
		{label: "TCP 8443 server answering clients", ip: "198.51.100.21", benign: true},
		{label: "DNS server answering clients", ip: "198.51.100.22", benign: true},
		{label: "NAT/P2P gateway", ip: "198.51.100.23", benign: true},
		{label: "A1 SYN flood", ip: "198.51.100.40", from: 540, to: 555, expect: map[string]int{ruleSyn: 540}},
		{label: "A2 UDP flood 1.2G", ip: "198.51.100.41", from: 660, to: 670, expect: map[string]int{ruleUDP: 660, ruleHighIn: 660}},
		{label: "A3 NTP amplification 3G", ip: "198.51.100.42", from: 780, to: 790, expect: map[string]int{ruleAmp: 780, ruleCritIn: 780, ruleHighIn: 780, ruleUDP: 780}},
		{label: "A4 outbound SYN port scan", ip: "198.51.100.50", from: 900, to: 930, expect: map[string]int{rulePortScan: 900}},
		{label: "A5 exfiltration 150M then 700M", ip: "198.51.100.51", from: 1020, to: 1160, expect: map[string]int{ruleExfil: 1020, ruleHighOut: 1140}},
		{label: "A6 2.5G flood on the busy server", ip: "198.51.100.10", from: 1200, to: 1205, expect: map[string]int{ruleCritIn: 1200, ruleHighIn: 1200}},
		{label: "A7 2.2G flood on a new host", ip: "198.51.100.60", from: 1320, to: 1325, expect: map[string]int{ruleCritIn: 1320, ruleHighIn: 1320}},
		{label: "/24 aggregate (carpet bomb rule)", ip: "198.51.100.0"},
	}
	return h, sc
}

// ── in-memory alert lifecycle ─────────────────────────────────────────────

type memAlert struct {
	model.Alert
	reopens int
}

// replayStore is the production store for every evaluation query, with the
// rules and the alert lifecycle kept in memory on the replay clock.
type replayStore struct {
	*store.ClickHouseStore
	rules  []model.AlertRule
	now    func() time.Time
	alerts []*memAlert
}

func (r *replayStore) ListAlertRules(context.Context) ([]model.AlertRule, error) {
	return r.rules, nil
}
func (r *replayStore) ListWebhooks(context.Context) ([]model.WebhookConfig, error) { return nil, nil }
func (r *replayStore) ListHostgroups(context.Context) ([]model.Hostgroup, error)   { return nil, nil }
func (r *replayStore) TopSourcesForTarget(context.Context, net.IP, uint32, int) ([]string, error) {
	return nil, nil // enrichment is not what is replayed
}

func (r *replayStore) latest(ruleID string, ip net.IP) *memAlert {
	for i := len(r.alerts) - 1; i >= 0; i-- {
		if a := r.alerts[i]; a.RuleID == ruleID && net.ParseIP(a.TargetIP).Equal(ip) {
			return a
		}
	}
	return nil
}
func (r *replayStore) LatestAlert(_ context.Context, ruleID string, ip net.IP) (store.AlertState, error) {
	a := r.latest(ruleID, ip)
	if a == nil {
		return store.AlertState{}, nil
	}
	return store.AlertState{ID: a.ID, Status: a.Status, TriggeredAt: a.TriggeredAt, LastSeenAt: a.LastSeenAt, MetricValue: a.MetricValue}, nil
}
func (r *replayStore) InsertAlert(_ context.Context, a model.Alert) error {
	a.Status = "active"
	r.alerts = append(r.alerts, &memAlert{Alert: a})
	return nil
}
func (r *replayStore) byID(id string) *memAlert {
	for _, a := range r.alerts {
		if a.ID == id {
			return a
		}
	}
	return nil
}
func (r *replayStore) ReopenAlert(_ context.Context, id string, ts time.Time, v float64) error {
	if a := r.byID(id); a != nil {
		a.Status, a.LastSeenAt, a.ResolvedAt = "active", ts, nil
		a.MetricValue = max(a.MetricValue, v)
		a.reopens++
	}
	return nil
}
func (r *replayStore) UpdateAlertLastSeen(_ context.Context, id string, ts time.Time) error {
	if a := r.byID(id); a != nil {
		a.LastSeenAt = ts
	}
	return nil
}
func (r *replayStore) AutoResolveStaleAlerts(_ context.Context, olderThan time.Duration) error {
	now := r.now()
	for _, a := range r.alerts {
		if a.Status == "active" && now.Sub(a.LastSeenAt) > olderThan {
			a.Status = "resolved"
			ts := now
			a.ResolvedAt = &ts
		}
	}
	return nil
}

// legacyStore evaluates port_scan the 3.6 way: every distinct destination
// port in the hot table.
type legacyStore struct{ *replayStore }

func (l legacyStore) EvalPortScan(ctx context.Context, th uint64, window uint32, prefixes []string) ([]store.AlertViolation, error) {
	return l.PortScanCandidates(ctx, th, window, prefixes)
}

// oldEngine is the 3.6.1 lifecycle (handleViolation of 3.6.1) over the same
// rule evaluations.
type oldEngine struct {
	e        *Engine
	rs       *replayStore
	cooldown map[string]time.Time
}

func (o *oldEngine) cycle(ctx context.Context) {
	now := o.rs.now()
	_ = o.rs.AutoResolveStaleAlerts(ctx, o.e.staleAfter)
	for _, rule := range o.rs.rules {
		if !rule.Enabled {
			continue
		}
		vs, metricType, ok := o.e.ruleViolations(ctx, rule, nil)
		if !ok {
			continue
		}
		for _, v := range vs {
			key := rule.ID + "|" + v.TargetIP.String()
			active := o.rs.latest(rule.ID, v.TargetIP)
			if active != nil && active.Status != "active" {
				active = nil
			}
			if t, ok := o.cooldown[key]; ok && now.Sub(t) < time.Duration(rule.CooldownSeconds)*time.Second {
				if active != nil {
					active.LastSeenAt = now
				}
				continue
			}
			if active != nil {
				active.LastSeenAt = now
				continue
			}
			_ = o.rs.InsertAlert(ctx, model.Alert{
				ID: fmt.Sprintf("old-%d", len(o.rs.alerts)), RuleID: rule.ID, RuleName: rule.Name,
				Severity: rule.Severity, TriggeredAt: now, LastSeenAt: now, TargetIP: v.TargetIP.String(),
				MetricValue: v.MetricValue, MetricType: metricType,
			})
			o.cooldown[key] = now
		}
	}
}

// ── harness ───────────────────────────────────────────────────────────────

func replayDB(t *testing.T, addr, db string) (clickhouse.Conn, *store.ClickHouseStore) {
	t.Helper()
	ctx := context.Background()
	admin, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: "default", Username: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" SYNC"); err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ASSTATS_IT_REPLAY_KEEP") == "" {
		t.Cleanup(func() { _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC") })
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr}, Auth: clickhouse.Auth{Database: db, Username: "default"},
		Settings:    clickhouse.Settings{"async_insert": 0, "max_execution_time": 600},
		DialTimeout: 10 * time.Second, MaxOpenConns: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migs, err := migrate.Load(schema.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrate.NewClickHouseStore(conn, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{Store: ms, Migrations: migs, Owner: "replay"}).Run(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The history reaches 8 days back: lift the TTLs of the scratch tables
	// so no background TTL merge deletes it mid-run.
	for _, q := range []string{
		"ALTER TABLE traffic_by_dst_1min MODIFY TTL ts + INTERVAL 60 DAY SETTINGS materialize_ttl_after_modify = 0",
		"ALTER TABLE traffic_by_src_1min MODIFY TTL ts + INTERVAL 60 DAY SETTINGS materialize_ttl_after_modify = 0",
		"ALTER TABLE flows_raw MODIFY TTL toDateTime(timestamp) + INTERVAL 60 DAY SETTINGS materialize_ttl_after_modify = 0",
	} {
		if err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st, err := store.NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: db, User: "default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return conn, st
}

func ipVersion(ip string) uint8 {
	if net.ParseIP(ip).To4() != nil {
		return 4
	}
	return 6
}

// seedReplay writes the replay day (flows_raw) and the history (hot tables).
func seedReplay(t *testing.T, ctx context.Context, conn clickhouse.Conn, d0 time.Time, hosts []seedHost) {
	t.Helper()
	start := time.Now()
	for i, h := range hosts {
		seed := i + 1
		to := h.to
		if to == 0 {
			to = 1440
		}
		x := h.x
		if x == 0 {
			x = 1
		}
		rip := h.rip
		if rip == "" {
			rip = "toUInt32(3405803776 + cityHash64(@seed, m, j) % @remotes)"
		}
		remote := "toIPv6(IPv4NumToString(" + rip + "))"
		local := "toIPv6(@ip)"
		src, dst, sport, dport := remote, local, h.rport, h.lport
		if h.dir == "out" {
			src, dst, sport, dport = local, remote, h.lport, h.rport
		}
		q := strings.NewReplacer("{src}", src, "{dst}", dst, "{sport}", sport, "{dport}", dport, "{bps}", h.bps).Replace(`
			INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, ip_version, protocol,
			                       src_port, dst_port, tcp_flags, bytes, packets, sampling_rate, direction)
			SELECT
				toDateTime64(@d0 + mi * 60 + j % 57, 3, 'UTC'),
				toIPv6('::ffff:192.0.2.1'), 'replay',
				{src}, {dst}, @ipv, @proto, {sport}, {dport}, @flags,
				b, greatest(toUInt64(1), intDiv(b, @pkt)), 1, @dir
			FROM (
				SELECT mi, j, m, mday, toUInt64(greatest(1, ({bps}) * @x * 60 / 8 / @rows)) AS b
				FROM (
					SELECT mi, j, intDiv(@d0, 60) + mi AS m, (intDiv(@d0, 60) + mi) % 1440 AS mday
					FROM (SELECT toUInt64(@from) + intDiv(number, @rows) AS mi, number % @rows AS j FROM numbers(@n))
				)
			)`)
		err := conn.Exec(ctx, q,
			clickhouse.Named("d0", uint64(d0.Unix())),
			clickhouse.Named("seed", seed),
			clickhouse.Named("remotes", max(h.remotes, 1)),
			clickhouse.Named("ip", h.ip),
			clickhouse.Named("ipv", ipVersion(h.ip)),
			clickhouse.Named("proto", h.proto),
			clickhouse.Named("flags", h.flags),
			clickhouse.Named("pkt", h.pkt),
			clickhouse.Named("x", x),
			clickhouse.Named("rows", h.rows),
			clickhouse.Named("from", h.from),
			clickhouse.Named("n", (to-h.from)*h.rows),
			clickhouse.Named("dir", h.dir),
		)
		if err != nil {
			t.Fatalf("seed flows %s %s: %v", h.ip, h.dir, err)
		}
		if !h.history {
			continue
		}
		table, ipCol, hll := "traffic_by_dst_1min", "dst_ip", "syn_count, unique_src_ips, unique_src_ports"
		hllVals := "0, arrayReduce('uniqState', [toIPv6('::ffff:203.0.113.1')]), arrayReduce('uniqState', [toUInt16(443)])"
		if h.dir == "out" {
			table, ipCol, hll = "traffic_by_src_1min", "src_ip", "unique_dst_ips, unique_dst_ports"
			hllVals = "arrayReduce('uniqState', [toIPv6('::ffff:203.0.113.1')]), arrayReduce('uniqState', [toUInt16(443)])"
		}
		hstart := d0.Add(-7*24*time.Hour - time.Hour)
		hq := strings.NewReplacer("{table}", table, "{ipcol}", ipCol, "{hll}", hll, "{hllvals}", hllVals, "{bps}", h.bps).Replace(`
			INSERT INTO {table} (ts, {ipcol}, protocol, bytes, packets, flow_count, {hll})
			SELECT toDateTime(m * 60, 'UTC'), toIPv6(@ip), @proto, b, greatest(toUInt64(1), intDiv(b, @pkt)), toUInt64(@rows), {hllvals}
			FROM (
				SELECT m, toUInt64(greatest(0, ({bps}) * 60 / 8)) AS b
				FROM (SELECT intDiv(@hstart, 60) + number AS m, (intDiv(@hstart, 60) + number) % 1440 AS mday FROM numbers(@n))
			)`)
		err = conn.Exec(ctx, hq,
			clickhouse.Named("hstart", uint64(hstart.Unix())),
			clickhouse.Named("ip", h.ip),
			clickhouse.Named("proto", h.proto),
			clickhouse.Named("pkt", h.pkt),
			clickhouse.Named("rows", h.rows),
			clickhouse.Named("n", int(d0.Sub(hstart)/time.Minute)),
		)
		if err != nil {
			t.Fatalf("seed history %s %s: %v", h.ip, h.dir, err)
		}
	}
	var rows uint64
	if err := conn.QueryRow(ctx, "SELECT count() FROM flows_raw").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d flows_raw rows + 7 days of hot-table history in %v", rows, time.Since(start).Round(time.Second))
}

// replayRun is one engine's result.
type replayRun struct {
	name    string
	alerts  []*memAlert
	elapsed time.Duration
}

func (r replayRun) forIP(ip string) []*memAlert {
	var out []*memAlert
	want := net.ParseIP(ip)
	for _, a := range r.alerts {
		if net.ParseIP(a.TargetIP).Equal(want) {
			out = append(out, a)
		}
	}
	return out
}

func TestIntegrationAlertReplay(t *testing.T) {
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
	ctx := context.Background()
	conn, st := replayDB(t, addr, "asstats_replay")

	// The replay day is yesterday (UTC); its history the 7 days before.
	d0 := time.Now().UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
	hosts, scenarios := replayHosts()
	seedReplay(t, ctx, conn, d0, hosts)

	// Rules: this release's defaults ("new") and the same rules as 3.6
	// seeded them ("old": no baseline factor), IP-scoped types only.
	sink := &ruleSink{}
	if err := EnsureDefaultRules(ctx, sink); err != nil {
		t.Fatal(err)
	}
	var newRules, oldRules []model.AlertRule
	for _, r := range sink.rules {
		switch r.RuleType {
		case "disk_usage", "link_capacity", "anomaly":
			continue
		}
		newRules = append(newRules, r)
		o := r
		o.BaselineFactor = 0
		oldRules = append(oldRules, o)
	}
	sort.Slice(newRules, func(i, j int) bool { return newRules[i].Name < newRules[j].Name })
	sort.Slice(oldRules, func(i, j int) bool { return oldRules[i].Name < oldRules[j].Name })

	var clock time.Time
	now := func() time.Time { return clock }
	st.SetClock(now)

	first, last := 510, 1350 // 08:30–22:30 covers every attack
	if os.Getenv("ASSTATS_IT_REPLAY") == "full" {
		first, last = 1, 1440
	}
	const evalEvery = time.Minute

	run := func(name string, rules []model.AlertRule, legacy bool) replayRun {
		rs := &replayStore{ClickHouseStore: st, rules: rules, now: now}
		var step func()
		if legacy {
			e := New(legacyStore{rs}, nil, replayPrefixes, evalEvery, 5*time.Minute)
			e.SetClock(now)
			o := &oldEngine{e: e, rs: rs, cooldown: map[string]time.Time{}}
			step = func() { o.cycle(ctx) }
		} else {
			e := New(rs, nil, replayPrefixes, evalEvery, 5*time.Minute)
			e.SetClock(now)
			e.SetRearmWindow(DefaultRearmWindow)
			step = func() { e.evaluateOnce(ctx) }
		}
		start := time.Now()
		for i := first; i <= last; i++ {
			clock = d0.Add(time.Duration(i)*time.Minute + 30*time.Second)
			step()
		}
		return replayRun{name: name, alerts: rs.alerts, elapsed: time.Since(start)}
	}

	queriesFrom := time.Now().UTC()
	oldRun := run("old (3.6.1)", oldRules, true)
	mixRun := run("new engine, old rules", oldRules, false)
	newRun := run("new", newRules, false)
	runs := []replayRun{oldRun, mixRun, newRun}
	for _, r := range runs {
		t.Logf("%-22s %4d alerts, %d evaluation cycles in %v (%v / cycle)", r.name, len(r.alerts), last-first+1,
			r.elapsed.Round(time.Millisecond), (r.elapsed / time.Duration(last-first+1)).Round(time.Millisecond))
	}

	// ── report ─────────────────────────────────────────────────────────
	minuteOf := func(ts time.Time) int { return int(ts.Sub(d0) / time.Minute) }
	hhmm := func(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
	// An attack scenario owns the alerts its IP triggers during the attack
	// (+10 min); a benign one the alerts of its IP outside those windows.
	inScope := func(sc replayScenario, a *memAlert) bool {
		m := minuteOf(a.TriggeredAt)
		if sc.to > 0 {
			return m >= sc.from && m <= sc.to+10
		}
		for _, o := range scenarios {
			if o.to > 0 && o.ip == sc.ip && m >= o.from && m <= o.to+10 {
				return false
			}
		}
		return true
	}
	scoped := func(r replayRun, sc replayScenario) []*memAlert {
		var out []*memAlert
		for _, a := range r.forIP(sc.ip) {
			if inScope(sc, a) {
				out = append(out, a)
			}
		}
		return out
	}
	cell := func(as []*memAlert) string {
		re := 0
		for _, a := range as {
			re += a.reopens
		}
		if re == 0 {
			return fmt.Sprint(len(as))
		}
		return fmt.Sprintf("%d (+%d re-open)", len(as), re)
	}
	t.Logf("alerts created (= notifications) per scenario and rule; re-opens are silent")
	t.Logf("%-34s %-40s %5s %-18s %-18s %s", "scenario", "rule", "old", "new eng, old rules", "new", "new: first alert (delay)")
	for _, sc := range scenarios {
		per := [3]map[string][]*memAlert{{}, {}, {}}
		seen := map[string]bool{}
		var names []string
		for k, r := range runs {
			for _, a := range scoped(r, sc) {
				per[k][a.RuleName] = append(per[k][a.RuleName], a)
				if !seen[a.RuleName] {
					seen[a.RuleName] = true
					names = append(names, a.RuleName)
				}
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			t.Logf("%-34s %-40s %5d %-18d %-18d", sc.label, "(none)", 0, 0, 0)
		}
		for _, n := range names {
			detail := ""
			for _, a := range per[2][n] {
				detail += hhmm(minuteOf(a.TriggeredAt))
				if start, ok := sc.expect[n]; ok {
					detail += fmt.Sprintf(" (+%.1f min)", a.TriggeredAt.Sub(d0.Add(time.Duration(start)*time.Minute)).Minutes())
				}
				detail += " "
			}
			t.Logf("%-34s %-40s %5d %-18s %-18s %s", sc.label, n, len(per[0][n]), cell(per[1][n]), cell(per[2][n]), detail)
		}
	}

	// ── assertions ─────────────────────────────────────────────────────
	for _, sc := range scenarios {
		newAlerts := scoped(newRun, sc)
		if sc.benign {
			for _, a := range newAlerts {
				t.Errorf("new engine alerted on benign %s (%s): %s at %s value %.0f", sc.label, sc.ip, a.RuleName, hhmm(minuteOf(a.TriggeredAt)), a.MetricValue)
			}
			if len(scoped(oldRun, sc)) == 0 {
				t.Errorf("old engine did not alert on benign %s: the replay does not reproduce the production pattern", sc.label)
			}
			continue
		}
		for rule, start := range sc.expect {
			var hit *memAlert
			for _, a := range newAlerts {
				if a.RuleName == rule && minuteOf(a.TriggeredAt) >= start {
					hit = a
					break
				}
			}
			if hit == nil {
				t.Errorf("%s: new engine did not fire %q during the attack", sc.label, rule)
				continue
			}
			if delay := hit.TriggeredAt.Sub(d0.Add(time.Duration(start) * time.Minute)); delay > 4*time.Minute {
				t.Errorf("%s: %q fired %v after the attack started", sc.label, rule, delay)
			}
		}
	}

	// ── cost of the new queries ────────────────────────────────────────
	if err := conn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		t.Logf("flush logs: %v", err)
		return
	}
	rows, err := conn.Query(ctx, `
		SELECT multiIf(position(q.query, 'probe_ports') > 0, 'port_scan flows_raw step',
		               position(q.query, 'unique_dst_ports) AS port_count') > 0, 'port_scan hot-table step (= 3.6 query)',
		               position(q.query, 'quantilesExact(0.5, 0.95)') > 0, 'baseline (ThreatBaselines)',
		               'other') AS kind,
		       count(), round(avg(q.query_duration_ms), 1), max(q.query_duration_ms),
		       round(avg(q.read_rows)), formatReadableSize(avg(q.read_bytes))
		FROM system.query_log AS q
		WHERE q.event_time >= toDateTime(@from, 'UTC') AND q.type = 'QueryFinish'
		  AND q.current_database = 'asstats_replay' AND q.query_kind = 'Select'
		GROUP BY kind ORDER BY kind`, clickhouse.Named("from", uint32(queriesFrom.Unix())))
	if err != nil {
		t.Logf("query_log: %v", err)
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			kind, size string
			n          uint64
			avg        float64
			mx         uint64
			readRows   float64
		)
		if err := rows.Scan(&kind, &n, &avg, &mx, &readRows, &size); err != nil {
			t.Fatal(err)
		}
		t.Logf("cost %-40s %6d queries, avg %6.1f ms, max %5d ms, avg %9.0f rows / %s read", kind, n, avg, mx, readRows, size)
	}
}

// ruleSink captures EnsureDefaultRules' seeds.
type ruleSink struct{ rules []model.AlertRule }

func (r *ruleSink) ListAlertRules(context.Context) ([]model.AlertRule, error) { return nil, nil }
func (r *ruleSink) UpsertAlertRule(_ context.Context, x model.AlertRule) error {
	r.rules = append(r.rules, x)
	return nil
}
