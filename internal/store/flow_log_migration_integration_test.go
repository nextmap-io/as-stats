package store

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/migrate"
	"github.com/nextmap-io/as-stats/internal/model"
	schema "github.com/nextmap-io/as-stats/migrations"
)

// scratchSchema creates an empty database named db on the ASSTATS_IT_CLICKHOUSE
// server (dropped again at the end of the test), applies the first `upTo`
// embedded migrations with the real cmd/migrate runner, and returns a store
// connected to it. upTo = 0 applies them all.
func scratchSchema(t *testing.T, db string, upTo int) (*ClickHouseStore, func(n int)) {
	t.Helper()
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
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
	t.Cleanup(func() { _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC") })

	mconn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: db, Username: "default"}, MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mconn.Close() })
	migs, err := migrate.Load(schema.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	mstore, err := migrate.NewClickHouseStore(mconn, db)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(n int) {
		t.Helper()
		if n == 0 {
			n = len(migs)
		}
		r := migrate.Runner{Store: mstore, Migrations: migs[:n], Owner: "it"}
		if err := r.Run(ctx); err != nil {
			t.Fatalf("migrate to %06d: %v", n, err)
		}
	}
	apply(upTo)

	s, err := NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: db, User: "default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, apply
}

// hotColdPolicy returns a storage policy qualifying for cold-tier adoption, or
// skips the test.
func hotColdPolicy(t *testing.T, s *ClickHouseStore) (string, []string) {
	t.Helper()
	pols, err := s.storagePolicyVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := coldPolicyCandidates(pols)
	if len(c) != 1 {
		t.Skipf("needs exactly one hot/cold storage policy on the server, found %v (see retention_integration_test.go)", c)
	}
	for _, v := range pols[c[0]] {
		if v.Name == coldVolumeName {
			return c[0], v.Disks
		}
	}
	t.Fatalf("policy %s has no cold volume", c[0])
	return "", nil
}

func itExec(t *testing.T, s *ClickHouseStore, q string, args ...any) {
	t.Helper()
	if err := s.conn.Exec(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func itUint(t *testing.T, s *ClickHouseStore, q string, args ...any) uint64 {
	t.Helper()
	var n uint64
	if err := s.conn.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func itString(t *testing.T, s *ClickHouseStore, q string, args ...any) string {
	t.Helper()
	var v string
	if err := s.conn.QueryRow(context.Background(), q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// insertFlowLogDay writes one day of flows straight into a flow-log table, at
// day daysAgo (UTC): n rows evenly spread over the day, dst 192.0.2.0 +
// (i % dsts), src 198.51.100.0 + (i % 200), all TCP/443, 1000 bytes, 1 flow.
func insertFlowLogDay(t *testing.T, s *ClickHouseStore, table string, daysAgo, n, dsts int, tag string) {
	t.Helper()
	itExec(t, s, fmt.Sprintf(`
		INSERT INTO %s (ts, router_ip, link_tag, src_ip, dst_ip, src_as, dst_as, protocol, src_port, dst_port, tcp_flags, ip_version, bytes, packets, flow_count)
		SELECT toStartOfDay(now() - toIntervalDay(@d)) + toIntervalSecond(intDiv(number * 86399, @n)),
		       toIPv6('::ffff:192.0.2.250'), @tag,
		       toIPv6(IPv4NumToString(toUInt32(3325256704 + number %% 200))),
		       toIPv6(IPv4NumToString(toUInt32(3221225984 + number %% @dsts))),
		       64500, 64501, 6, 40000 + number %% 1000, 443, 2, 4, 1000, 1, 1
		FROM numbers(@n)`, table),
		clickhouse.Named("d", daysAgo), clickhouse.Named("n", n), clickhouse.Named("dsts", dsts), clickhouse.Named("tag", tag))
}

// TestIntegrationFlowLogMigration upgrades a 000016 schema whose flows_log is on
// the hot/cold policy and partly on the cold volume, while a goroutine keeps
// INSERTing into flows_raw, then checks: no insert failed and no row was lost or
// duplicated (count(flows_raw) == sum(flow_count) over flows_log_all), the old
// data is still served by every reader through flows_log_all, the reconciler
// adopts the hot/cold policy for the new table, applies the flows_log policy to
// both tables, moves old partitions of both, and is a no-op afterwards; and
// EXPLAIN shows the new key in use. Needs a server with exactly one hot/cold
// policy (see retention_integration_test.go for the docker run line).
func TestIntegrationFlowLogMigration(t *testing.T) {
	s, apply := scratchSchema(t, "asstats_it_m17", 16)
	ctx := context.Background()
	policy, coldDisks := hotColdPolicy(t, s)
	const tag = "it-m17"

	// --- 3.3 state: flows_log on hot_cold, 20 old days, the oldest 5 on cold.
	itExec(t, s, fmt.Sprintf("ALTER TABLE flows_log MODIFY SETTING storage_policy = '%s'", policy))
	for d := 2; d <= 21; d++ {
		insertFlowLogDay(t, s, "flows_log", d, 2000, 50, tag+"-old")
	}
	for d := 17; d <= 21; d++ {
		pid := time.Now().UTC().AddDate(0, 0, -d).Format("20060102")
		itExec(t, s, fmt.Sprintf("ALTER TABLE flows_log MOVE PARTITION ID '%s' TO VOLUME 'cold'", pid))
	}
	legacyRows := itUint(t, s, "SELECT sum(flow_count) FROM flows_log")
	legacyCold := itUint(t, s, "SELECT count() FROM system.parts WHERE database = currentDatabase() AND table = 'flows_log' AND active AND has(@d, disk_name)", clickhouse.Named("d", coldDisks))
	if legacyCold == 0 {
		t.Fatal("setup: no flows_log part on the cold volume")
	}

	// --- Continuous INSERTs into flows_raw across the migration.
	var inserted, insertErrs atomic.Uint64
	var firstErr atomic.Value
	stop := make(chan struct{})
	var wg sync.WaitGroup
	insert := func(seq uint64) error {
		return s.conn.Exec(ctx, `
			INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, src_as, dst_as, protocol, src_port, dst_port, bytes, packets, sampling_rate, direction)
			SELECT now64(3), toIPv6('::ffff:192.0.2.250'), @tag,
			       toIPv6('::ffff:198.51.100.1'), toIPv6(concat('::ffff:203.0.113.', toString(number))),
			       64500, 64501, 17, @seq % 60000, 53, 100, 1, 1, 'in'
			FROM numbers(10)`, clickhouse.Named("tag", tag), clickhouse.Named("seq", seq))
	}
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for seq := uint64(w); ; seq += 4 {
				select {
				case <-stop:
					return
				default:
				}
				if err := insert(seq); err != nil {
					insertErrs.Add(1)
					firstErr.CompareAndSwap(nil, err.Error())
					continue
				}
				inserted.Add(10)
			}
		}(w)
	}
	time.Sleep(500 * time.Millisecond)
	before := inserted.Load()
	start := time.Now()
	apply(0)
	migrateTook := time.Since(start)
	during := inserted.Load() - before
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
	t.Logf("migration 000017 took %s; %d rows inserted before, %d while migrating, %d total", migrateTook, before, during, inserted.Load())

	if n := insertErrs.Load(); n != 0 {
		t.Fatalf("%d INSERT INTO flows_raw failed during the migration, first: %v", n, firstErr.Load())
	}
	raw := itUint(t, s, "SELECT count() FROM flows_raw WHERE link_tag = @t", clickhouse.Named("t", tag))
	logged := itUint(t, s, "SELECT sum(flow_count) FROM flows_log_all WHERE link_tag = @t", clickhouse.Named("t", tag))
	inLegacy := itUint(t, s, "SELECT sum(flow_count) FROM flows_log_legacy WHERE link_tag = @t", clickhouse.Named("t", tag))
	inNew := itUint(t, s, "SELECT sum(flow_count) FROM flows_log WHERE link_tag = @t", clickhouse.Named("t", tag))
	t.Logf("flows_raw %d rows; flows_log_legacy %d + flows_log %d = %d", raw, inLegacy, inNew, logged)
	if raw != inserted.Load() || logged != raw || inLegacy+inNew != raw {
		t.Fatalf("lost or duplicated rows: inserted %d, flows_raw %d, flows_log_all %d (legacy %d + new %d)", inserted.Load(), raw, logged, inLegacy, inNew)
	}
	if inLegacy == 0 || inNew == 0 {
		t.Fatalf("inserts did not straddle the swap (legacy %d, new %d)", inLegacy, inNew)
	}

	// --- Schema after 000017.
	if k := itString(t, s, "SELECT sorting_key FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log'"); !strings.HasPrefix(k, "toStartOfFifteenMinutes(ts), dst_ip, ts") {
		t.Errorf("flows_log sorting key = %q", k)
	}
	if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log'"); p != "default" {
		t.Errorf("new flows_log storage policy = %q, want default", p)
	}
	if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log_legacy'"); p != policy {
		t.Errorf("flows_log_legacy storage policy = %q, want %q", p, policy)
	}
	if got := itUint(t, s, "SELECT sum(flow_count) FROM flows_log_legacy WHERE link_tag = @t", clickhouse.Named("t", tag+"-old")); got != legacyRows {
		t.Errorf("legacy rows %d, want %d", got, legacyRows)
	}
	merged := itString(t, s, "SELECT arrayStringConcat(groupArray(t), ',') FROM (SELECT DISTINCT _table AS t FROM flows_log_all ORDER BY t)")
	if merged != "flows_log,flows_log_legacy" {
		t.Errorf("flows_log_all reads %q, want flows_log,flows_log_legacy", merged)
	}

	// --- Readers still serve the legacy data through flows_log_all.
	day := time.Now().UTC().AddDate(0, 0, -19).Truncate(24 * time.Hour)
	f := model.FlowSearchFilters{From: day, To: day.Add(24 * time.Hour), DstIP: "192.0.2.7", Limit: 1000}
	rows, err := s.SearchFlowLog(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	var legacyFlows uint64
	for _, r := range rows {
		legacyFlows += r.FlowCount
	}
	if legacyFlows != 40 { // 2000 rows / 50 dst IPs
		t.Errorf("SearchFlowLog on a legacy day (partition on the cold volume): %d flows, want 40", legacyFlows)
	}
	series, err := s.FlowLogTimeSeries(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	var sb uint64
	for _, p := range series {
		sb += p.BytesIn
	}
	if sb != 40*1000 {
		t.Errorf("FlowLogTimeSeries on legacy day: %d bytes, want 40000", sb)
	}
	conv, err := s.Conversations(ctx, QueryParams{From: day, To: day.Add(24 * time.Hour), Limit: 1000}, "dst_port_proto", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(conv) != 1 || conv[0].Flows != 2000 {
		t.Errorf("Conversations on legacy day = %+v, want one 6/443 row with 2000 flows", conv)
	}
	now := time.Now().UTC()
	rows, err = s.SearchFlowLog(ctx, model.FlowSearchFilters{From: now.Add(-time.Hour), To: now.Add(time.Minute), SrcIP: "198.51.100.1", Limit: 100000})
	if err != nil {
		t.Fatal(err)
	}
	var fl uint64
	for _, r := range rows {
		fl += r.FlowCount
	}
	if fl != raw {
		t.Errorf("SearchFlowLog across the swap: %d flows, want %d", fl, raw)
	}

	// --- Old days in the new table too, then the reconciler.
	for d := 16; d <= 18; d++ {
		insertFlowLogDay(t, s, "flows_log", d, 1000, 50, tag+"-new-old")
	}
	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	pols, err := s.retentionPoliciesByTable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pols["flows_log_legacy"]; ok {
		t.Error("a retention policy row was seeded for flows_log_legacy")
	}
	cold := uint32(14)
	if _, err := s.SetRetentionPolicy(ctx, "flows_log", 180, true, &cold); err != nil {
		t.Fatal(err)
	}
	hotOld := func(table string) uint64 {
		return itUint(t, s, `
			SELECT count() FROM (
				SELECT partition_id FROM system.parts
				WHERE database = currentDatabase() AND table = @t AND active
				GROUP BY partition_id
				HAVING max(max_time) < now() - toIntervalDay(14) AND countIf(NOT has(@d, disk_name)) > 0)`,
			clickhouse.Named("t", table), clickhouse.Named("d", coldDisks))
	}
	t.Logf("before reconcile: old partitions still hot: flows_log %d, flows_log_legacy %d", hotOld("flows_log"), hotOld("flows_log_legacy"))
	for cycle := 1; cycle <= 10; cycle++ {
		if err := s.ReconcileRetention(ctx); err != nil {
			t.Fatal(err)
		}
		if hotOld("flows_log") == 0 && hotOld("flows_log_legacy") == 0 {
			t.Logf("converged after %d reconcile cycle(s)", cycle)
			break
		}
	}
	for _, table := range []string{"flows_log", "flows_log_legacy"} {
		if p := itString(t, s, "SELECT storage_policy FROM system.tables WHERE database = currentDatabase() AND name = @t", clickhouse.Named("t", table)); p != policy {
			t.Errorf("%s storage policy = %q, want %q", table, p, policy)
		}
		q := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = @t", clickhouse.Named("t", table))
		if ttl, ok := parseTableTTL(q); !ok || !ttl.matches(180, 14) {
			t.Errorf("%s TTL = %+v, want 180 days / cold after 14", table, ttl)
		}
		if n := hotOld(table); n != 0 {
			t.Errorf("%s: %d partition(s) older than 14 days still hot", table, n)
		}
	}

	// Second run is a no-op: same definitions, same part placement, no mutation.
	snapshot := func() string {
		return itString(t, s, `
			SELECT concat(
				(SELECT arrayStringConcat(groupArray(concat(name, ':', create_table_query)), '\n') FROM (SELECT name, create_table_query FROM system.tables WHERE database = currentDatabase() ORDER BY name)),
				(SELECT arrayStringConcat(groupArray(concat(table, '/', name, '@', disk_name)), '\n') FROM (SELECT table, name, disk_name FROM system.parts WHERE database = currentDatabase() AND active AND table LIKE 'flows_log%' ORDER BY table, name)),
				toString((SELECT count() FROM system.mutations WHERE database = currentDatabase())))`)
	}
	before2 := snapshot()
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); after != before2 {
		t.Error("second reconcile cycle changed the schema, the part placement or queued a mutation")
	}

	// Admin › Storage shows the legacy table read-only with flows_log's policy.
	st, err := s.StorageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tb := range st.Tables {
		if tb.Table == "flows_log_legacy" {
			found = true
			if !tb.Legacy || tb.RetentionFollows != "flows_log" || tb.TTLDays != 180 || tb.ColdAfterDays != 14 || tb.ColdBytes == 0 {
				t.Errorf("flows_log_legacy storage stats = %+v", tb)
			}
		}
	}
	if !found {
		t.Error("flows_log_legacy missing from StorageStats")
	}

	// --- EXPLAIN: the new key is used for a 15-minute window and a dst_ip lookup.
	// 4 M rows / 500 dst IPs on one day: ~490 granules, ~5 per 15-min bucket.
	insertFlowLogDay(t, s, "flows_log", 1, 4_000_000, 500, tag+"-explain")
	from := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	itExec(t, s, fmt.Sprintf("OPTIMIZE TABLE flows_log PARTITION ID '%s' FINAL", from.Format("20060102")))
	window := explainPrimaryKeyGranules(t, s, "flows_log", "", from.Add(10*time.Hour), from.Add(10*time.Hour+15*time.Minute))
	dst1h := explainPrimaryKeyGranules(t, s, "flows_log", "f.dst_ip = toIPv6('192.0.2.7')", from.Add(10*time.Hour), from.Add(11*time.Hour))
	dst24h := explainPrimaryKeyGranules(t, s, "flows_log", "f.dst_ip = toIPv6('192.0.2.7')", from, from.Add(24*time.Hour))
	t.Logf("EXPLAIN flows_log (one day, %d granules): 15-min window %d granules; dst_ip over 1 h %d; dst_ip over 24 h %d",
		window[1], window[0], dst1h[0], dst24h[0])
	if window[0]*20 > window[1] || dst1h[0]*20 > dst1h[1] || dst24h[0] >= dst24h[1] {
		t.Errorf("primary key barely used: window %v, dst 1 h %v, dst 24 h %v", window, dst1h, dst24h)
	}
	// Through the Merge table, the bucket predicate reaches both tables.
	viaMerge := explainPrimaryKeyGranules(t, s, flowLogReadTable, "f.dst_ip = toIPv6('192.0.2.7')", from.Add(10*time.Hour), from.Add(11*time.Hour))
	if viaMerge != dst1h {
		t.Errorf("via %s: %v granules, want %v as on flows_log", flowLogReadTable, viaMerge, dst1h)
	}
}

var granulesPattern = regexp.MustCompile(`Granules: (\d+)/(\d+)`)

// explainPrimaryKeyGranules returns the selected/total granules of the
// PrimaryKey step of EXPLAIN indexes=1 for a flow-log query on table.
func explainPrimaryKeyGranules(t *testing.T, s *ClickHouseStore, table, extra string, from, to time.Time) [2]uint64 {
	t.Helper()
	cond, args := flowLogTimeFilter("f", from, to)
	if extra != "" {
		cond += " AND " + extra
	}
	rows, err := s.conn.Query(context.Background(), fmt.Sprintf("EXPLAIN indexes = 1 SELECT sum(f.bytes) FROM %s f WHERE %s", table, cond), args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, l)
	}
	inPK := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "PrimaryKey" {
			inPK = true
		}
		if m := granulesPattern.FindStringSubmatch(l); inPK && m != nil {
			a, _ := strconv.ParseUint(m[1], 10, 64)
			b, _ := strconv.ParseUint(m[2], 10, 64)
			return [2]uint64{a, b}
		}
	}
	t.Fatalf("no PrimaryKey granules in EXPLAIN:\n%s", strings.Join(lines, "\n"))
	return [2]uint64{}
}

// TestIntegrationFlowLogFreshInstall applies every migration to an empty
// database: flows_log has the new layout, flows_log_legacy is empty and left
// alone by the reconciler, and flows_raw inserts land in the new table.
func TestIntegrationFlowLogFreshInstall(t *testing.T) {
	s, _ := scratchSchema(t, "asstats_it_fresh", 0)
	ctx := context.Background()
	if k := itString(t, s, "SELECT sorting_key FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log'"); !strings.HasPrefix(k, "toStartOfFifteenMinutes(ts), dst_ip, ts") {
		t.Errorf("flows_log sorting key = %q", k)
	}
	for _, name := range []string{"flows_log_legacy", "flows_log_all", "flows_log_mv"} {
		if itUint(t, s, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = @n", clickhouse.Named("n", name)) != 1 {
			t.Errorf("%s missing", name)
		}
	}
	if itUint(t, s, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log_new'") != 0 {
		t.Error("flows_log_new left behind")
	}
	itExec(t, s, `INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, bytes, packets, sampling_rate)
		VALUES (now64(3), toIPv6('::ffff:192.0.2.250'), 'fresh', toIPv6('::ffff:198.51.100.1'), toIPv6('::ffff:192.0.2.1'), 100, 1, 10)`)
	if got := itString(t, s, "SELECT arrayStringConcat(groupArray(_table), ',') FROM flows_log_all WHERE link_tag = 'fresh'"); got != "flows_log" {
		t.Errorf("fresh insert landed in %q, want flows_log", got)
	}

	legacyBefore := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log_legacy'")
	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetRetentionPolicy(ctx, "flows_log", 90, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	q := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log'")
	if ttl, ok := parseTableTTL(q); !ok || !ttl.matches(90, 0) {
		t.Errorf("flows_log TTL = %+v, want 90 days", ttl)
	}
	if after := itString(t, s, "SELECT create_table_query FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log_legacy'"); after != legacyBefore {
		t.Error("the reconciler altered an empty flows_log_legacy")
	}

	// Re-running 000017 by hand is refused before it touches anything.
	err := s.conn.Exec(ctx, "SELECT throwIf(count() > 0, 'already applied') FROM system.tables WHERE database = currentDatabase() AND name = 'flows_log_legacy'")
	if err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Errorf("guard statement did not raise through Exec: %v", err)
	}
}
