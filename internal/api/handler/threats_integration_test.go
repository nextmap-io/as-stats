package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/alerts"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Live Threats end to end against a throwaway ClickHouse with the
// migrations applied:
//
//	ASSTATS_IT_CLICKHOUSE=127.0.0.1:9000 go test ./internal/api/handler -run Integration -v
//
// It seeds flows_raw (the hot tables fill through their MVs) for hosts in
// 198.18.0.0/15 and 2001:db8:7::/48 — ranges no other fixture uses — with 7
// days of same-time-of-day history, then checks every level and signature
// through the HTTP handler. Skipped when the variable is unset.

var threatITPrefixes = []string{"198.18.0.0/15", "2001:db8:7::/48"}

// threatSeed describes one host's synthetic traffic. Counters are per flow
// record before sampling.
type threatSeed struct {
	local      string // local host
	dir        string // "in": local is the destination; "out": the source
	mins       []int  // minutes ago (0 = the current, still filling minute)
	flows      int    // flow records per minute
	proto      uint8
	tcpFlags   uint8
	pool       int    // distinct remote IPs drawn from
	remotePort string // SQL over j (record index in the minute) and mi
	bytes      int    // bytes per record
	packets    int    // packets per record
	sampling   int
	spikeEvery int     // history only: every Nth minute is a spike
	spikeX     float64 // spike factor
}

func minutesRange(from, to int) []int { // inclusive, minutes ago
	out := make([]int, 0, to-from+1)
	for m := from; m <= to; m++ {
		out = append(out, m)
	}
	return out
}

// historyMinutes are the 60 minutes centred on now, 1..7 days ago.
func historyMinutes() []int {
	var out []int
	for d := 1; d <= 7; d++ {
		out = append(out, minutesRange(d*1440-29, d*1440+30)...)
	}
	return out
}

func seedThreat(t *testing.T, ctx context.Context, conn clickhouse.Conn, seed int, s threatSeed) {
	t.Helper()
	localExpr, remoteExpr := "toIPv6(@local)", "toIPv6(IPv4NumToString(toUInt32(184549376 + cityHash64(@seed, mi, j) % @pool)))"
	src, dst := remoteExpr, localExpr
	srcPort, dstPort := s.remotePort, "toUInt16(1024 + cityHash64(@seed, mi, j, 7) % 60000)"
	if s.dir == "out" {
		src, dst = localExpr, remoteExpr
		srcPort, dstPort = dstPort, srcPort
	}
	spike := 1.0
	if s.spikeX > 0 {
		spike = s.spikeX
	}
	every := s.spikeEvery
	if every == 0 {
		every = 1 << 30
	}
	q := `
		INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, protocol, src_port, dst_port,
		                       tcp_flags, bytes, packets, sampling_rate, direction)
		SELECT
			toStartOfMinute(now()) - toIntervalSecond(mi * 60) + toIntervalSecond(5 + j % 50),
			toIPv6('::ffff:192.0.2.1'), 'it-threats',
			` + src + `, ` + dst + `, @proto, ` + srcPort + `, ` + dstPort + `, @flags,
			toUInt64(@bytes * k),
			greatest(1, toUInt64(round(@packets * k))), @sampling, @dir
		FROM (
			SELECT mi, j,
				(0.85 + (cityHash64(@seed, mi, j, 1) % 300) / 1000.0)
					* if(mi >= 1440 AND mi % @every = 0, @spike, 1.0) AS k -- jitter, history spikes
			FROM (
				SELECT arrayElement(@mins, toUInt32(intDiv(number, @flows) + 1)) AS mi, number % @flows AS j
				FROM numbers(@n)
			)
		)`
	err := conn.Exec(ctx, q,
		clickhouse.Named("local", s.local),
		clickhouse.Named("seed", seed),
		clickhouse.Named("pool", max(s.pool, 1)),
		clickhouse.Named("proto", s.proto),
		clickhouse.Named("flags", s.tcpFlags),
		clickhouse.Named("bytes", s.bytes),
		clickhouse.Named("packets", s.packets),
		clickhouse.Named("sampling", s.sampling),
		clickhouse.Named("every", every),
		clickhouse.Named("spike", spike),
		clickhouse.Named("dir", s.dir),
		clickhouse.Named("mins", s.mins),
		clickhouse.Named("flows", s.flows),
		clickhouse.Named("n", len(s.mins)*s.flows),
	)
	if err != nil {
		t.Fatalf("seed %s: %v", s.local, err)
	}
}

func TestIntegrationLiveThreats(t *testing.T) {
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
	ctx := context.Background()
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: "asstats", Username: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	st, err := store.NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: "asstats", User: "default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := alerts.EnsureDefaultRules(ctx, st); err != nil {
		t.Fatal(err)
	}

	// Hosts unique to this run, so re-running against the same database
	// does not double the fixtures.
	octet := time.Now().Unix()%250 + 1
	run, run6 := fmt.Sprintf("198.18.%d.", octet), fmt.Sprintf("2001:db8:7:%x::5", octet)

	hist := historyMinutes()
	steady := append(slices.Clone(hist), minutesRange(0, 64)...)
	before := append(slices.Clone(hist), minutesRange(9, 64)...) // history + the hour before the jump
	jump := minutesRange(0, 8)
	randPort := "toUInt16(1024 + cityHash64(@seed, mi, j, 3) % 60000)"
	seeds := []threatSeed{
		// Busy but steady, history with 10 % spike minutes (p95 well above p50).
		{local: run + "10", dir: "in", mins: steady, flows: 60, proto: 6, tcpFlags: 0x18, pool: 90,
			remotePort: "toUInt16(443)", bytes: 37500, packets: 32, sampling: 1000, spikeEvery: 10, spikeX: 2.7},
		{local: run + "11", dir: "in", mins: steady, flows: 300, proto: 6, tcpFlags: 0x18, pool: 2000,
			remotePort: randPort, bytes: 5000, packets: 5, sampling: 1000, spikeEvery: 10, spikeX: 2.7},
		{local: run6, dir: "in", mins: steady, flows: 30, proto: 6, tcpFlags: 0x18, pool: 900,
			remotePort: randPort, bytes: 7500, packets: 7, sampling: 1000, spikeEvery: 10, spikeX: 7},
		// Jumps x20 for the last 8 minutes.
		{local: run + "20", dir: "in", mins: before, flows: 10, proto: 6, tcpFlags: 0x18, pool: 30,
			remotePort: randPort, bytes: 3750, packets: 3, sampling: 1000},
		{local: run + "20", dir: "in", mins: jump, flows: 50, proto: 6, tcpFlags: 0x18, pool: 30,
			remotePort: randPort, bytes: 15000, packets: 12, sampling: 1000},
		// SYN flood on a quiet host.
		{local: run + "30", dir: "in", mins: before, flows: 10, proto: 6, tcpFlags: 0x18, pool: 20,
			remotePort: randPort, bytes: 1500, packets: 2, sampling: 1000},
		{local: run + "30", dir: "in", mins: jump, flows: 500, proto: 6, tcpFlags: 0x02, pool: 5000,
			remotePort: randPort, bytes: 180, packets: 3, sampling: 1000},
		// NTP amplification on a host with no history.
		{local: run + "40", dir: "in", mins: jump, flows: 300, proto: 17, pool: 300,
			remotePort: "toUInt16(123)", bytes: 5148, packets: 11, sampling: 1000},
		// Outbound: a busy steady sender and a port scanner.
		{local: run + "12", dir: "out", mins: steady, flows: 60, proto: 6, tcpFlags: 0x18, pool: 200,
			remotePort: "toUInt16(443)", bytes: 12500, packets: 10, sampling: 1000, spikeEvery: 10, spikeX: 2.5},
		{local: run + "50", dir: "out", mins: before, flows: 10, proto: 6, tcpFlags: 0x18, pool: 20,
			remotePort: "toUInt16(443)", bytes: 750, packets: 1, sampling: 1000},
		{local: run + "50", dir: "out", mins: jump, flows: 2000, proto: 6, tcpFlags: 0x02, pool: 1,
			remotePort: "toUInt16(j)", bytes: 60, packets: 1, sampling: 10},
	}
	for i, s := range seeds {
		seedThreat(t, ctx, conn, i+1, s)
	}

	queriesFrom := time.Now().UTC() // query_log report below covers the handler calls and the timed ones
	h := &Handler{Store: st, FeatureAlerts: true, LocalPrefixes: threatITPrefixes}
	get := func(query string) model.LiveThreats {
		t.Helper()
		rec := httptest.NewRecorder()
		start := time.Now()
		h.LiveThreats(rec, httptest.NewRequest(http.MethodGet, "/api/v1/threats/live?"+query, nil))
		t.Logf("GET /threats/live?%s: %v", query, time.Since(start))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d %s", query, rec.Code, rec.Body.String())
		}
		var env struct {
			Data model.LiveThreats `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		for _, r := range env.Data.Rows {
			var sigs []string
			for _, s := range r.Signatures {
				sigs = append(sigs, s.Kind)
			}
			t.Logf("  %-16s %-8s score=%-6.1f ratio=%-7.2f bps=%-11d pps=%-7d peers=%-5d ports=%-5d samples=%-3d p95=%-11d rule=%q %.1f%% sigs=%v since=%s",
				r.IP, r.Level, r.Score, r.Ratio, r.Now.BPS, r.Now.PPS, r.Now.Peers, r.Now.PeerPorts,
				r.Baseline.Samples, r.Baseline.BPSP95, r.ClosestRule, r.RulePct, sigs, r.Since)
		}
		return env.Data
	}
	find := func(d model.LiveThreats, ip string) *model.ThreatRow {
		for i := range d.Rows {
			if d.Rows[i].IP == ip {
				return &d.Rows[i]
			}
		}
		return nil
	}
	hasSig := func(r *model.ThreatRow, kind string) *model.ThreatSignature {
		for i := range r.Signatures {
			if r.Signatures[i].Kind == kind {
				return &r.Signatures[i]
			}
		}
		return nil
	}
	expect := func(d model.LiveThreats, ip, level string, sigs ...string) *model.ThreatRow {
		t.Helper()
		r := find(d, ip)
		if r == nil {
			t.Errorf("%s: missing from rows", ip)
			return nil
		}
		if r.Level != level {
			t.Errorf("%s: level %s, want %s", ip, r.Level, level)
		}
		for _, k := range sigs {
			if hasSig(r, k) == nil {
				t.Errorf("%s: no %s signature in %+v", ip, k, r.Signatures)
			}
		}
		if len(r.Series) != store.ThreatSeriesMinutes {
			t.Errorf("%s: %d series points", ip, len(r.Series))
		}
		return r
	}

	in := get("direction=in")
	if in.Direction != "in" || in.WindowSeconds != 300 || in.Checked < 6 || in.Normal < 3 {
		t.Errorf("envelope: direction=%s window=%d checked=%d normal=%d", in.Direction, in.WindowSeconds, in.Checked, in.Normal)
	}
	for _, ip := range []string{run + "10", run + "11", run6} {
		if r := find(in, ip); r != nil {
			t.Errorf("busy steady host %s listed as %s (ratio %.2f, sigs %+v)", ip, r.Level, r.Ratio, r.Signatures)
		}
	}
	if r := expect(in, run+"20", model.ThreatLevelWatch); r != nil && r.Since == "" {
		t.Errorf("jumper: since is empty")
	}
	expect(in, run+"30", model.ThreatLevelIncident, sigSynFlood)
	if r := expect(in, run+"40", model.ThreatLevelIncident, sigAmplification, sigUDPFlood); r != nil {
		if amp := hasSig(r, sigAmplification); amp != nil && (len(amp.TopPorts) == 0 || amp.TopPorts[0].Port != 123 || amp.TopPorts[0].Pct < 90) {
			t.Errorf("ntp: top ports %+v, want 123 first with > 90 %%", amp.TopPorts)
		}
		if r.Baseline.Samples != 0 {
			t.Errorf("ntp: %d history samples, want none", r.Baseline.Samples)
		}
	}
	if len(in.Rows) > 0 && in.Rows[0].Level != model.ThreatLevelIncident {
		t.Errorf("first row is %s, want an incident first", in.Rows[0].Level)
	}

	all := get("direction=in&all=1&limit=100")
	busy := find(all, run+"10")
	if busy == nil || busy.Level != model.ThreatLevelNormal {
		t.Errorf("all=1: busy host missing or not normal: %+v", busy)
	} else {
		if busy.Baseline.Samples < 300 || busy.Baseline.BPSP95 <= busy.Baseline.BPSP50 {
			t.Errorf("busy baseline %+v: want >= 300 samples and p95 > p50", busy.Baseline)
		}
		if busy.ClosestRule == "" {
			t.Errorf("busy host has no closest rule")
		}
	}
	for i := 1; i < len(all.Rows); i++ {
		if all.Rows[i].Now.BPS > all.Rows[i-1].Now.BPS+1 {
			t.Errorf("all=1 not ordered by volume at %d", i)
		}
	}

	out := get("direction=out")
	expect(out, run+"50", model.ThreatLevelIncident, sigPortScan)
	if r := find(out, run+"12"); r != nil {
		t.Errorf("busy steady sender listed as %s", r.Level)
	}

	// The same four queries the handler runs, timed one by one.
	timed := func(name string, f func() error) {
		t.Helper()
		start := time.Now()
		if err := f(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-12s %v", name, time.Since(start))
	}
	var (
		anchor time.Time
		cands  []store.ThreatCandidate
	)
	timed("candidates", func() (err error) {
		anchor, cands, err = st.ThreatCandidates(ctx, "in", 300, threatITPrefixes)
		return err
	})
	probes := make([]string, len(cands))
	for i, c := range cands {
		probes[i] = c.Probe
	}
	timed("baseline", func() error { _, err := st.ThreatBaselines(ctx, "in", anchor, probes); return err })
	timed("peers(25)", func() error { _, err := st.ThreatPeerBaselines(ctx, "in", anchor, probes); return err })
	timed("ports", func() error { _, err := st.ThreatPorts(ctx, "in", anchor, 300, probes); return err })
	timed("series", func() error { _, err := st.ThreatSeries(ctx, "in", anchor, probes); return err })
	if err := conn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
		t.Logf("flush logs: %v", err)
		return
	}
	rows, err := conn.Query(ctx, `
		SELECT query_duration_ms, read_rows, formatReadableSize(read_bytes), substring(replaceRegexpAll(query, '\\s+', ' '), 1, 70)
		FROM system.query_log
		WHERE type = 'QueryFinish' AND event_time_microseconds >= @since
		  AND (query LIKE '%traffic_by_dst_1min AS h%' OR query LIKE '%traffic_by_src_1min AS h%' OR query LIKE '%FROM flows_raw AS f%')
		ORDER BY event_time_microseconds`, clickhouse.Named("since", queriesFrom))
	if err != nil {
		t.Logf("query_log: %v", err)
		return
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			ms, readRows uint64
			size, query  string
		)
		if err := rows.Scan(&ms, &readRows, &size, &query); err == nil {
			t.Logf("query_log: %4d ms %8d rows %10s  %s", ms, readRows, size, query)
		}
	}
}
