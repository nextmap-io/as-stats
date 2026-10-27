package store

import (
	"context"
	"fmt"
	"math"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/migrate"
	"github.com/nextmap-io/as-stats/internal/model"
)

// flow is a compact FlowRecord for the port / AS-grouping integration tests,
// written through the real WriteBatch (so the materialised views see exactly
// what the collector would write).
type flow struct {
	link       string
	dir        uint8 // model.Direction*
	proto      uint8
	sport      uint16
	dport      uint16
	srcAS      uint32
	dstAS      uint32
	src, dst   string
	bytes      uint64
	sampling   uint32
	n          int // copies
	tsOverride time.Time
}

func writeFlows(t *testing.T, s *ClickHouseStore, at time.Time, flows []flow) {
	t.Helper()
	var recs []*model.FlowRecord
	for _, f := range flows {
		n := max(f.n, 1)
		ts := at
		if !f.tsOverride.IsZero() {
			ts = f.tsOverride
		}
		src, dst := f.src, f.dst
		if src == "" {
			src = "198.51.100.1"
		}
		if dst == "" {
			dst = "192.0.2.1"
		}
		for range n {
			recs = append(recs, &model.FlowRecord{
				Timestamp: ts, RouterIP: net.ParseIP("192.0.2.250"), LinkTag: f.link,
				SrcIP: net.ParseIP(src), DstIP: net.ParseIP(dst), IPVersion: 4,
				SrcAS: f.srcAS, DstAS: f.dstAS, Protocol: f.proto, SrcPort: f.sport, DstPort: f.dport,
				Bytes: f.bytes, Packets: 1, SamplingRate: max(f.sampling, 1), Direction: f.dir, FlowType: "ipfix",
			})
		}
	}
	if err := s.WriteBatch(context.Background(), recs); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationPortStats seeds flows_raw with known flows in both directions
// (plus flows on no configured link) and checks the 000018 semantics end to
// end: one row per flow, direction = flows_raw.direction, the service port,
// per-protocol sums equal to flows_raw exactly, the SQL port rule equal to
// ServicePort on a grid, and the readers on top.
func TestIntegrationPortStats(t *testing.T) {
	s, _ := scratchSchema(t, "asstats_it_ports", 0)
	ctx := context.Background()
	in, out, none := model.DirectionInbound, model.DirectionOutbound, model.DirectionUnknown

	// --- The SQL port expression equals ServicePort, port by port.
	day := time.Now().UTC().Add(-26 * time.Hour).Truncate(time.Hour)
	ports := []uint16{0, 22, 53, 123, 443, 8080, 50000, 51000, 60001}
	var grid []flow
	for _, proto := range []uint8{6, 17, 1, 58, 47} {
		for _, sp := range ports {
			for _, dp := range ports {
				grid = append(grid, flow{link: fmt.Sprintf("g-%d-%d-%d", proto, sp, dp), dir: in, proto: proto, sport: sp, dport: dp, bytes: 1})
			}
		}
	}
	writeFlows(t, s, day, grid)
	rows, err := s.conn.Query(ctx, "SELECT link_tag, groupUniqArray(port), sum(flow_count) FROM traffic_by_port WHERE link_tag LIKE 'g-%' GROUP BY link_tag")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for rows.Next() {
		var tag string
		var got []uint16
		var n uint64
		if err := rows.Scan(&tag, &got, &n); err != nil {
			t.Fatal(err)
		}
		var proto uint8
		var sp, dp uint16
		if _, err := fmt.Sscanf(tag, "g-%d-%d-%d", &proto, &sp, &dp); err != nil {
			t.Fatal(err)
		}
		if want := ServicePort(proto, sp, dp); len(got) != 1 || got[0] != want || n != 1 {
			t.Errorf("proto %d %d→%d: SQL port %v (%d flow rows), ServicePort %d", proto, sp, dp, got, n, want)
		}
		checked++
	}
	_ = rows.Close()
	if checked != len(grid) {
		t.Errorf("grid: %d tuples in traffic_by_port, want %d (every flow kept, portless ones with port 0)", checked, len(grid))
	}

	// --- Known traffic, both directions, one hour ago.
	at := time.Now().UTC().Add(-time.Hour).Truncate(5 * time.Minute).Add(time.Minute)
	writeFlows(t, s, at, []flow{
		{link: "L1", dir: in, proto: 6, sport: 51000, dport: 443, bytes: 1000, sampling: 10, n: 10}, // clients → our HTTPS
		{link: "L1", dir: out, proto: 6, sport: 443, dport: 52000, bytes: 500, sampling: 10, n: 4},  // our HTTPS → clients
		{link: "L1", dir: out, proto: 6, sport: 50123, dport: 443, bytes: 300, sampling: 10, n: 6},  // our clients → remote HTTPS
		{link: "L2", dir: in, proto: 17, sport: 53, dport: 53, bytes: 100, n: 3},                    // DNS
		{link: "L2", dir: out, proto: 1, dport: 2048, bytes: 64, n: 2},                              // ICMP echo
		{link: "L2", dir: in, proto: 47, bytes: 1000, n: 2},                                         // GRE
		{link: "", dir: in, proto: 17, sport: 40000, dport: 123, bytes: 100, n: 5},                  // no link, exporter direction
		{link: "", dir: none, proto: 6, sport: 443, dport: 50000, bytes: 100, n: 1},                 // no link, no direction
	})
	from, to := at.Add(-10*time.Minute), at.Add(10*time.Minute)

	// Every flow once: sum(flow_count) over the whole table == count(flows_raw).
	raw := itUint(t, s, "SELECT count() FROM flows_raw")
	stored := itUint(t, s, "SELECT sum(flow_count) FROM traffic_by_port")
	if raw != stored {
		t.Fatalf("flows_raw %d rows, traffic_by_port %d flows: not one row per flow", raw, stored)
	}
	if n := itUint(t, s, "SELECT sum(flow_count) FROM traffic_by_port WHERE link_tag = '' AND direction != ''"); n != 0 {
		t.Errorf("%d flows on no configured link stored with a direction", n)
	}

	// TopProtocols == flows_raw per (protocol, direction), exactly.
	protos, err := s.TopProtocols(ctx, QueryParams{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		proto uint8
		dir   string
	}
	got := map[key][3]uint64{}
	pct := map[string]float64{}
	for _, p := range protos {
		got[key{p.Protocol, p.Direction}] = [3]uint64{p.Bytes, p.Packets, p.Flows}
		pct[p.Direction] += p.Percent
	}
	want := map[key][3]uint64{}
	rows, err = s.conn.Query(ctx, `
		SELECT f.protocol, f.direction, sum(f.bytes * f.sampling_rate), sum(f.packets * f.sampling_rate), count()
		FROM flows_raw f
		WHERE f.timestamp >= @from AND f.timestamp < @to AND f.link_tag != '' AND f.direction IN ('in', 'out')
		GROUP BY f.protocol, f.direction`, clickhouse.Named("from", from), clickhouse.Named("to", to))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k key
		var v [3]uint64
		if err := rows.Scan(&k.proto, &k.dir, &v[0], &v[1], &v[2]); err != nil {
			t.Fatal(err)
		}
		want[k] = v
	}
	_ = rows.Close()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("TopProtocols = %v\nflows_raw    = %v", got, want)
	}
	if tcpIn, tcpOut := got[key{6, "in"}], got[key{6, "out"}]; tcpIn[0] != 100_000 || tcpOut[0] != 4*5000+6*3000 || tcpIn == tcpOut {
		t.Errorf("TCP in %v / out %v, want 100000 / 38000 bytes", tcpIn, tcpOut)
	}
	for dir, sum := range pct {
		if math.Abs(sum-100) > 0.01 {
			t.Errorf("TopProtocols %s percentages add up to %.3f, want 100", dir, sum)
		}
	}

	// TopPorts: the service port, per direction.
	tp := func(dir string, proto uint8) []model.PortTraffic {
		t.Helper()
		r, err := s.TopPorts(ctx, QueryParams{From: from, To: to, Direction: dir, Limit: 100}, proto)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := tp("in", 6); len(r) != 1 || r[0].Port != 443 || r[0].Flows != 10 || r[0].Direction != "in" || math.Abs(r[0].Percent-100) > 0.01 {
		t.Errorf("TopPorts in/TCP = %+v, want one 443 row with 10 flows", r)
	}
	if r := tp("out", 6); len(r) != 1 || r[0].Port != 443 || r[0].Flows != 10 || r[0].Bytes != 38000 {
		t.Errorf("TopPorts out/TCP = %+v, want one 443 row with 10 flows / 38000 bytes", r)
	}
	inAll := tp("in", 0)
	var inPorts []string
	for _, r := range inAll {
		inPorts = append(inPorts, fmt.Sprintf("%d/%d", r.Protocol, r.Port))
	}
	slices.Sort(inPorts)
	if strings.Join(inPorts, ",") != "17/53,6/443" { // GRE (port 0) left out, unlinked 17/123 left out
		t.Errorf("TopPorts in = %v, want 17/53 and 6/443", inPorts)
	}
	if r := tp("out", 1); len(r) != 1 || r[0].Port != 0 || r[0].Flows != 2 {
		t.Errorf("TopPorts out/ICMP = %+v, want one port-0 row with 2 flows", r)
	}
	both := tp("", 0)
	dirs := map[string]int{}
	for _, r := range both {
		dirs[r.Direction]++
	}
	if dirs["in"] != 2 || dirs["out"] != 2 || len(dirs) != 2 {
		t.Errorf("TopPorts without direction: rows per direction %v, want in 2 / out 2 and nothing else", dirs)
	}

	// Movers by port: direction-agnostic, the unlinked NTP flows included, once.
	movers, err := s.Movers(ctx, "port", from, to, 50)
	if err != nil {
		t.Fatal(err)
	}
	mv := map[string]uint64{}
	for _, m := range movers {
		mv[m.Key] = m.Current
	}
	if mv["6/443"] != 100_000+38_000+100 || mv["17/123"] != 500 || mv["17/53"] != 300 {
		t.Errorf("Movers port = %v", mv)
	}

	// Fresh install: nothing restarted.
	if since, err := s.PortHistorySince(ctx); err != nil || !since.IsZero() {
		t.Errorf("PortHistorySince on a fresh install = %v, %v; want zero", since, err)
	}
}

// TestIntegrationPortStatsMigration upgrades a 000017 schema whose
// traffic_by_port is on the hot/cold policy (cold_after_days = 30, old
// partitions on the cold volume, as in production) while 4 goroutines keep
// INSERTing into flows_raw, one uniquely tagged 10-flow block at a time, then
// checks: no INSERT failed; per inserter, the blocks found in the new table
// form a suffix of its sequence, each exactly once with the new semantics,
// none of them in the legacy table; the legacy data is untouched and on its
// disks; readers see only the new table; the reconciler adopts hot_cold for
// the new table, treats the legacy one as a follower, and is a no-op
// afterwards. Needs a server with exactly one hot/cold policy (see
// retention_integration_test.go for the docker run line).
func TestIntegrationPortStatsMigration(t *testing.T) {
	const db = "asstats_it_m18"
	s, apply := scratchSchema(t, db, 17)
	ctx := context.Background()
	policy, coldDisks := hotColdPolicy(t, s)

	// --- 3.4 state: 4 months of old-semantics rows, traffic_by_port managed at
	// 365 days with a 30-day cold tier, the reconciler converged.
	for m := 1; m <= 4; m++ {
		itExec(t, s, `
			INSERT INTO traffic_by_port (ts, link_tag, direction, protocol, port, ip_version, bytes, packets, flow_count)
			SELECT toStartOfMonth(now() - toIntervalMonth(@m)) + toIntervalMinute(5 * number), 'old',
			       if(number % 2 = 0, 'in', 'out'), 6, if(number % 2 = 0, 443, 51000 + number % 100), 4, 1000, 1, 1
			FROM numbers(500)`, clickhouse.Named("m", m))
	}
	writeFlows(t, s, time.Now().UTC(), []flow{{link: "pre", dir: model.DirectionInbound, proto: 6, sport: 51000, dport: 443, bytes: 100, n: 7}})
	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	cold := uint32(30)
	if _, err := s.SetRetentionPolicy(ctx, "traffic_by_port", 365, true, &cold); err != nil {
		t.Fatal(err)
	}
	hotOld := func(table string) uint64 {
		return itUint(t, s, `
			SELECT count() FROM (
				SELECT partition_id FROM system.parts
				WHERE database = currentDatabase() AND table = @t AND active
				GROUP BY partition_id
				HAVING max(max_time) < now() - toIntervalDay(30) AND countIf(NOT has(@d, disk_name)) > 0)`,
			clickhouse.Named("t", table), clickhouse.Named("d", coldDisks))
	}
	for cycle := 0; cycle < 10 && (cycle == 0 || hotOld("traffic_by_port") > 0); cycle++ {
		if err := s.ReconcileRetention(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port'"); p != policy {
		t.Fatalf("setup: traffic_by_port on %q, want %q", p, policy)
	}
	legacySnapshot := func(table string) string {
		return itString(t, s, `
			SELECT arrayStringConcat(groupArray(concat(partition_id, '@', disk_name, ':', toString(r))), ',') FROM (
				SELECT partition_id, disk_name, sum(rows) AS r FROM system.parts
				WHERE database = currentDatabase() AND table = @t AND active
				GROUP BY partition_id, disk_name ORDER BY partition_id, disk_name)`, clickhouse.Named("t", table))
	}
	before := legacySnapshot("traffic_by_port")
	oldFlows := itUint(t, s, "SELECT sum(flow_count) FROM traffic_by_port")
	onCold := itUint(t, s, "SELECT count() FROM system.parts WHERE database = currentDatabase() AND table = 'traffic_by_port' AND active AND has(@d, disk_name)", clickhouse.Named("d", coldDisks))
	if oldFlows != 4*500+2*7 || onCold == 0 || hotOld("traffic_by_port") != 0 {
		t.Fatalf("setup: %d flows (want %d: the 7 'pre' flows are counted twice by the 000007 views), %d part(s) on cold, parts %s", oldFlows, 4*500+2*7, onCold, before)
	}
	t.Logf("before: traffic_by_port parts %s", before)

	// --- Continuous inserts across the migration.
	const workers = 4
	var seqs [workers]atomic.Int64 // blocks inserted per worker
	var afterApply [workers]atomic.Int64
	for w := range afterApply {
		afterApply[w].Store(-1)
	}
	var insertErrs atomic.Uint64
	var firstErr atomic.Value
	var applied atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for seq := 0; ; seq++ {
				select {
				case <-stop:
					return
				default:
				}
				if applied.Load() && afterApply[w].Load() < 0 {
					afterApply[w].Store(int64(seq))
				}
				err := s.conn.Exec(ctx, `
					INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, src_as, dst_as, protocol, src_port, dst_port, bytes, packets, sampling_rate, direction)
					SELECT now64(3), toIPv6('::ffff:192.0.2.250'), @tag, toIPv6('::ffff:198.51.100.1'), toIPv6('::ffff:192.0.2.1'), 64500, 64501, 6,
					       if(number < 5, 51000 + number, 443), if(number < 5, 443, 51000 + number), 100, 1, 1, if(number < 5, 'in', 'out')
					FROM numbers(10)`, clickhouse.Named("tag", fmt.Sprintf("m18-%d-%06d", w, seq)))
				if err != nil {
					insertErrs.Add(1)
					firstErr.CompareAndSwap(nil, err.Error())
					return // a failed block would break the sequence checks; the count says it all
				}
				seqs[w].Store(int64(seq + 1))
			}
		}(w)
	}
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	apply(0)
	took := time.Since(start)
	applied.Store(true)
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
	if n := insertErrs.Load(); n != 0 {
		t.Fatalf("%d INSERT INTO flows_raw failed during the migration, first: %v", n, firstErr.Load())
	}

	// --- Per block: where did it land?
	perBlock := func(table string) map[string][4]uint64 {
		t.Helper()
		out := map[string][4]uint64{}
		rows, err := s.conn.Query(ctx, fmt.Sprintf(`
			SELECT t.link_tag, sum(t.flow_count), sum(t.bytes), countIf(t.port != 443), uniqExact(t.direction)
			FROM %s t WHERE t.link_tag LIKE 'm18-%%' GROUP BY t.link_tag`, table))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var tag string
			var v [4]uint64
			if err := rows.Scan(&tag, &v[0], &v[1], &v[2], &v[3]); err != nil {
				t.Fatal(err)
			}
			out[tag] = v
		}
		return out
	}
	newBlocks, oldBlocks := perBlock("traffic_by_port"), perBlock("traffic_by_port_legacy")
	var total, inNew, inOld, gap, partialOld int
	for w := 0; w < workers; w++ {
		n := int(seqs[w].Load())
		first := -1
		for seq := 0; seq < n; seq++ {
			tag := fmt.Sprintf("m18-%d-%06d", w, seq)
			total++
			nb, inN := newBlocks[tag]
			ob, inO := oldBlocks[tag]
			switch {
			case inN:
				inNew++
				if first < 0 {
					first = seq
				}
				if nb != [4]uint64{10, 1000, 0, 2} {
					t.Errorf("%s in the new table: flows/bytes/non-443 rows/directions = %v, want [10 1000 0 2]", tag, nb)
				}
				if inO {
					t.Errorf("%s is in both tables", tag)
				}
			case first >= 0:
				t.Errorf("%s missing from the new table after %s of the same inserter landed there", tag, fmt.Sprintf("m18-%d-%06d", w, first))
			case inO:
				inOld++
				if ob[0] != 20 {
					partialOld++ // inserted between the two DROP VIEWs: legacy only, irrelevant
				}
			default:
				gap++
			}
		}
		if a := afterApply[w].Load(); a >= 0 && a < int64(n) && (first < 0 || int64(first) > a) {
			t.Errorf("inserter %d: block %d started after the migration returned but the new table starts at %d", w, a, first)
		}
	}
	t.Logf("migration 000018 took %s; %d blocks of 10 flows: %d before the swap (legacy, %d partial), %d in the gap between DROP VIEW and CREATE VIEW, %d in the new table",
		took, total, inOld, partialOld, gap, inNew)
	if inNew == 0 || inOld == 0 {
		t.Fatalf("inserts did not straddle the migration (legacy %d, new %d)", inOld, inNew)
	}

	// --- Schema after 000018.
	for name, want := range map[string]uint64{"traffic_by_port_mv": 1, "traffic_by_port_legacy": 1, "traffic_by_port_in_mv": 0, "traffic_by_port_out_mv": 0, "traffic_by_port_new": 0} {
		if got := itUint(t, s, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = @n", clickhouse.Named("n", name)); got != want {
			t.Errorf("%s: %d table(s), want %d", name, got, want)
		}
	}
	if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port'"); p != "default" {
		t.Errorf("new traffic_by_port storage policy = %q, want default (until the reconciler adopts)", p)
	}
	if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port_legacy'"); p != policy {
		t.Errorf("traffic_by_port_legacy storage policy = %q, want %q", p, policy)
	}
	// The legacy data is untouched: same partitions, disks and row counts for
	// everything that existed before the migration.
	if got := itUint(t, s, "SELECT sum(flow_count) FROM traffic_by_port_legacy WHERE link_tag IN ('old', 'pre')"); got != oldFlows {
		t.Errorf("legacy flows %d, want %d", got, oldFlows)
	}
	after := legacySnapshot("traffic_by_port_legacy")
	for _, part := range strings.Split(before, ",") {
		if strings.Contains(part, "@default:") {
			continue // the current month also received the inserters' rows until the swap
		}
		if !slices.Contains(strings.Split(after, ","), part) {
			t.Errorf("legacy partition %s changed: before %s, after %s", part, before, after)
		}
	}
	mstore, err := migrate.NewClickHouseStore(s.conn, db)
	if err != nil {
		t.Fatal(err)
	}
	// Every migration is applied (apply(0)), so the baseline is the latest level.
	if v, err := mstore.DetectBaseline(ctx); err != nil || v != 19 {
		t.Errorf("DetectBaseline after every migration = %d, %v; want 19", v, err)
	}

	// --- Readers see the new table only.
	wide := QueryParams{From: time.Now().UTC().AddDate(0, -6, 0), To: time.Now().UTC().Add(time.Hour), Limit: 1000}
	ports, err := s.TopPorts(ctx, wide, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ports {
		if p.Port != 443 || p.Flows != uint64(5*inNew) {
			t.Errorf("TopPorts over the legacy months returned %+v, want only 443 with %d flows", p, 5*inNew)
		}
	}
	protos, err := s.TopProtocols(ctx, wide)
	if err != nil {
		t.Fatal(err)
	}
	if len(protos) != 2 || protos[0].Flows != uint64(5*inNew) || protos[1].Flows != uint64(5*inNew) {
		t.Errorf("TopProtocols over the legacy months = %+v, want TCP in and out with %d flows each", protos, 5*inNew)
	}
	since, err := s.PortHistorySince(ctx)
	if err != nil || since.IsZero() || time.Since(since) > time.Hour || since.Before(start.Add(-5*time.Minute)) {
		t.Errorf("PortHistorySince = %v, %v; want the upgrade time (%v)", since, err, start)
	}

	// --- Reconciler: adopts hot_cold for the new table, follower for the legacy.
	itExec(t, s, `
		INSERT INTO traffic_by_port (ts, link_tag, direction, protocol, port, ip_version, bytes, packets, flow_count)
		SELECT toStartOfMonth(now() - toIntervalMonth(2)) + toIntervalMinute(5 * number), 'new-old', 'in', 17, 53, 4, 10, 1, 1
		FROM numbers(100)`)
	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	pols, err := s.retentionPoliciesByTable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pols["traffic_by_port_legacy"]; ok {
		t.Error("a retention policy row was seeded for traffic_by_port_legacy")
	}
	if p := pols["traffic_by_port"]; p.TTLDays != 365 || p.ColdAfterDays != 30 {
		t.Errorf("traffic_by_port policy = %+v, want the pre-upgrade 365 / 30", p)
	}
	for cycle := 1; cycle <= 10; cycle++ {
		if err := s.ReconcileRetention(ctx); err != nil {
			t.Fatal(err)
		}
		if hotOld("traffic_by_port") == 0 && hotOld("traffic_by_port_legacy") == 0 {
			t.Logf("reconciler converged after %d cycle(s)", cycle)
			break
		}
	}
	for _, table := range []string{"traffic_by_port", "traffic_by_port_legacy"} {
		if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = @t", clickhouse.Named("t", table)); p != policy {
			t.Errorf("%s storage policy = %q, want %q", table, p, policy)
		}
		q := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = @t", clickhouse.Named("t", table))
		if ttl, ok := parseTableTTL(q); !ok || !ttl.matches(365, 30) {
			t.Errorf("%s TTL = %+v, want 365 days / cold after 30", table, ttl)
		}
		if n := hotOld(table); n != 0 {
			t.Errorf("%s: %d partition(s) older than 30 days still hot", table, n)
		}
	}
	snapshot := func() string {
		return itString(t, s, `
			SELECT concat(
				(SELECT arrayStringConcat(groupArray(concat(name, ':', create_table_query)), '\n') FROM (SELECT name, create_table_query FROM system.tables WHERE database = currentDatabase() ORDER BY name)),
				(SELECT arrayStringConcat(groupArray(concat(table, '/', name, '@', disk_name)), '\n') FROM (SELECT table, name, disk_name FROM system.parts WHERE database = currentDatabase() AND active AND table LIKE 'traffic_by_port%' ORDER BY table, name)),
				toString((SELECT count() FROM system.mutations WHERE database = currentDatabase())))`)
	}
	snap := snapshot()
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != snap {
		t.Error("second reconcile cycle changed the schema, the part placement or queued a mutation")
	}
	st, err := s.StorageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tb := range st.Tables {
		if tb.Table == "traffic_by_port_legacy" {
			found = true
			if !tb.Legacy || tb.RetentionFollows != "traffic_by_port" || tb.TTLDays != 365 || tb.ColdAfterDays != 30 || tb.ColdBytes == 0 {
				t.Errorf("traffic_by_port_legacy storage stats = %+v", tb)
			}
		}
	}
	if !found {
		t.Error("traffic_by_port_legacy missing from StorageStats")
	}
}

// TestIntegrationPortStatsFreshInstall: every migration on an empty database.
// The legacy table exists, empty, and is left alone by the reconciler; inserts
// land in traffic_by_port once; the guard refuses a re-run.
func TestIntegrationPortStatsFreshInstall(t *testing.T) {
	const db = "asstats_it_fresh18"
	s, _ := scratchSchema(t, db, 0)
	ctx := context.Background()
	for name, want := range map[string]uint64{"traffic_by_port": 1, "traffic_by_port_mv": 1, "traffic_by_port_legacy": 1, "traffic_by_port_in_mv": 0, "traffic_by_port_out_mv": 0, "traffic_by_port_new": 0} {
		if got := itUint(t, s, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = @n", clickhouse.Named("n", name)); got != want {
			t.Errorf("%s: %d table(s), want %d", name, got, want)
		}
	}
	writeFlows(t, s, time.Now().UTC(), []flow{{link: "fresh", dir: model.DirectionOutbound, proto: 17, sport: 53, dport: 40000, bytes: 100, sampling: 10, n: 3}})
	if got := itString(t, s, "SELECT concat(toString(sum(flow_count)), '/', toString(sum(bytes)), '/', toString(any(port)), '/', any(direction)) FROM traffic_by_port WHERE link_tag = 'fresh'"); got != "3/3000/53/out" {
		t.Errorf("fresh insert in traffic_by_port = %s, want 3/3000/53/out", got)
	}
	if n := itUint(t, s, "SELECT count() FROM traffic_by_port_legacy"); n != 0 {
		t.Errorf("traffic_by_port_legacy holds %d rows on a fresh install", n)
	}
	legacyBefore := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port_legacy'")
	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetRetentionPolicy(ctx, "traffic_by_port", 200, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	q := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port'")
	if ttl, ok := parseTableTTL(q); !ok || !ttl.matches(200, 0) {
		t.Errorf("traffic_by_port TTL = %+v, want 200 days", ttl)
	}
	if after := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'traffic_by_port_legacy'"); after != legacyBefore {
		t.Error("the reconciler altered an empty traffic_by_port_legacy")
	}
	if since, err := s.PortHistorySince(ctx); err != nil || !since.IsZero() {
		t.Errorf("PortHistorySince on a fresh install = %v, %v; want zero", since, err)
	}
	mstore, err := migrate.NewClickHouseStore(s.conn, db)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := mstore.DetectBaseline(ctx); err != nil || v != 19 {
		t.Errorf("DetectBaseline = %d, %v; want 19", v, err)
	}
	err = s.conn.Exec(ctx, "SELECT throwIf(count() > 0, 'already applied') FROM system.tables WHERE database = currentDatabase() AND name IN ('traffic_by_port_legacy', 'traffic_by_port_mv')")
	if err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Errorf("000018 guard did not refuse a re-run: %v", err)
	}
}
