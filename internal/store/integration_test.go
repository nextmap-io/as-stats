package store

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/config"
)

// Integration tests run only against a throwaway ClickHouse that has the
// migrations applied and some flows_raw data, e.g.
//
//	ASSTATS_IT_CLICKHOUSE=127.0.0.1:9000 go test ./internal/store -run Integration
//
// They are skipped otherwise, so `go test ./...` stays hermetic. They never
// write to tables the application reads, except the fixtures they create.
func itStore(t *testing.T) *ClickHouseStore {
	t.Helper()
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
	s, err := NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: "asstats", User: "default"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestIntegrationNameJoinsDoNotInflate pins the ReplacingMergeTree join bug:
// with two unmerged versions of an as_names / links row, a plain LEFT JOIN
// doubled every sum over the joined fact rows.
func TestIntegrationNameJoinsDoNotInflate(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()
	const asn = 64496 // RFC 5398 documentation ASN — never in generated data
	// Unique per run: re-running must not count earlier runs' flows.
	tag := fmt.Sprintf("it-dedup-%d", time.Now().UnixNano())
	exec := func(q string) {
		t.Helper()
		if err := s.conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("SYSTEM STOP MERGES as_names")
	exec("SYSTEM STOP MERGES links")
	t.Cleanup(func() {
		_ = s.conn.Exec(ctx, "SYSTEM START MERGES as_names")
		_ = s.conn.Exec(ctx, "SYSTEM START MERGES links")
	})
	exec("INSERT INTO as_names (as_number, as_name, country, updated_at) VALUES (64496, 'OLD', 'FR', now() - 10)")
	exec("INSERT INTO as_names (as_number, as_name, country, updated_at) VALUES (64496, 'NEW', 'DE', now())")
	exec(fmt.Sprintf("INSERT INTO links (tag, router_ip, description, capacity_mbps, updated_at) VALUES ('%s', toIPv6('::ffff:192.0.2.250'), 'old', 1000, now() - 10)", tag))
	exec(fmt.Sprintf("INSERT INTO links (tag, router_ip, description, capacity_mbps, updated_at) VALUES ('%s', toIPv6('::ffff:192.0.2.250'), 'new', 1000, now())", tag))
	// One flow: 1000 bytes × sampling 10 = 10000 bytes from AS64496 on the link.
	ts := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Second)
	if err := s.conn.Exec(ctx, `INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, src_as, dst_as, bytes, packets, sampling_rate, direction)
		VALUES (@ts, toIPv6('::ffff:192.0.2.250'), @tag, toIPv6('::ffff:203.0.113.9'), toIPv6('::ffff:198.51.100.9'), 64496, 0, 1000, 1, 10, 'in')`,
		clickhouse.Named("ts", ts), clickhouse.Named("tag", tag)); err != nil {
		t.Fatalf("insert flow: %v", err)
	}

	p := QueryParams{From: ts.Add(-time.Hour), To: ts.Add(time.Hour), Limit: 1000}

	top, _, err := s.LinkTopAS(ctx, tag, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || top[0].ASNumber != asn || top[0].Bytes != 10000 || top[0].ASName != "NEW" {
		t.Fatalf("LinkTopAS = %+v, want one AS64496 row named NEW with 10000 bytes", top)
	}

	pl := p
	pl.LinkTags = []string{tag}
	countries, _, err := s.TopCountry(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 1 || countries[0].Country != "DE" || countries[0].Bytes != 10000 {
		t.Fatalf("TopCountry = %+v, want DE with 10000 bytes", countries)
	}

	links, err := s.LinkList(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range links {
		if l.Tag == tag {
			found = true
			if l.BytesIn != 10000 || l.Description != "new" {
				t.Fatalf("LinkList %s = %+v, want 10000 bytes in and description new", tag, l)
			}
		}
	}
	if !found {
		t.Fatalf("LinkList: %s missing", tag)
	}

	series, err := s.LinksTrafficSeries(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, ls := range series {
		if ls.Tag != tag {
			continue
		}
		var sum uint64
		for _, pt := range ls.Points {
			sum += pt.BytesIn
		}
		if sum != 10000 || ls.Description != "new" {
			t.Fatalf("LinksTrafficSeries %s: %d bytes / %q, want 10000 / new", tag, sum, ls.Description)
		}
	}

	hits, err := s.SearchAS(ctx, "64496", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "NEW" {
		t.Fatalf("SearchAS = %+v, want a single NEW row", hits)
	}
}

// TestIntegrationASRemoteIPsMatchesFlowsRaw checks that the traffic_by_ip
// rewrite returns exactly what the old flows_raw scan returned, on a window
// aligned to the 5-minute buckets.
func TestIntegrationASRemoteIPsMatchesFlowsRaw(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()
	to := time.Now().UTC().Truncate(5 * time.Minute)
	from := to.Add(-2 * time.Hour)

	var asn uint32
	if err := s.conn.QueryRow(ctx, `
		SELECT src_as FROM flows_raw WHERE timestamp >= @from AND timestamp < @to AND src_as > 0
		GROUP BY src_as ORDER BY count() DESC LIMIT 1`,
		clickhouse.Named("from", from), clickhouse.Named("to", to)).Scan(&asn); err != nil {
		t.Skipf("no flows in window: %v", err)
	}

	rows, err := s.conn.Query(ctx, `
		SELECT replaceRegexpOne(toString(src_ip), '^::ffff:', '') AS ip,
			sum(bytes * sampling_rate), sum(packets * sampling_rate), count()
		FROM flows_raw
		WHERE src_as = @asn AND timestamp >= @from AND timestamp < @to
		GROUP BY ip`,
		clickhouse.Named("asn", asn), clickhouse.Named("from", from), clickhouse.Named("to", to))
	if err != nil {
		t.Fatal(err)
	}
	type agg struct{ b, p, f uint64 }
	want := map[string]agg{}
	for rows.Next() {
		var ip string
		var a agg
		if err := rows.Scan(&ip, &a.b, &a.p, &a.f); err != nil {
			t.Fatal(err)
		}
		want[ip] = a
	}
	_ = rows.Close()

	got, err := s.ASRemoteIPs(ctx, asn, QueryParams{From: from, To: to, Limit: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("AS%d: %d IPs, flows_raw has %d", asn, len(got), len(want))
	}
	for _, r := range got {
		w, ok := want[r.IP]
		if !ok || w.b != r.Bytes || w.p != r.Packets || w.f != r.Flows {
			t.Fatalf("AS%d %s: got %d/%d/%d, flows_raw %+v (present=%v)", asn, r.IP, r.Bytes, r.Packets, r.Flows, w, ok)
		}
	}
	t.Logf("AS%d: %d remote IPs identical", asn, len(got))
}

// TestIntegrationCIDRRangeMatchesIsIPAddressInRange is the property test for
// the buildCIDRFilter rewrite: for many CIDRs (random, derived from real
// addresses so they actually match rows, plus edge cases), the typed range
// predicate must select exactly the rows the old
// isIPAddressInRange(toString(col), normalized) predicate selected.
func TestIntegrationCIDRRangeMatchesIsIPAddressInRange(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()

	// Sample real addresses to derive CIDRs from.
	rows, err := s.conn.Query(ctx, `SELECT toString(src_ip) FROM flows_raw GROUP BY src_ip ORDER BY cityHash64(src_ip) LIMIT 60`)
	if err != nil {
		t.Fatal(err)
	}
	var sample []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			t.Fatal(err)
		}
		sample = append(sample, ip)
	}
	_ = rows.Close()
	if len(sample) == 0 {
		t.Skip("no flows")
	}

	cidrs := []string{"0.0.0.0/0", "::/0", "::ffff:0:0/96", "2001:db8::/32", "10.0.0.0/8", "192.0.2.1", "2001:db8::1"}
	for i, ip := range sample {
		ip = cleanIPv4Mapped(ip)
		if strings.Contains(ip, ":") {
			cidrs = append(cidrs, fmt.Sprintf("%s/%d", ip, []int{16, 32, 48, 64, 100, 127, 128}[i%7]))
		} else {
			cidrs = append(cidrs, fmt.Sprintf("%s/%d", ip, []int{8, 12, 16, 19, 22, 24, 30, 32}[i%8]))
		}
	}

	var totalMatched uint64
	for i, c := range cidrs {
		norm, ok := normalizeCIDRForIPv6Column(c)
		if !ok {
			t.Fatalf("normalize %q", c)
		}
		clause, args := buildCIDRFilter("f.src_ip", "p_", []string{c})
		args = append(args, clickhouse.Named("norm", norm))
		var oldN, newN, diff uint64
		q := fmt.Sprintf(`
			SELECT
				countIf(isIPAddressInRange(toString(f.src_ip), @norm)),
				countIf(%[1]s),
				countIf(isIPAddressInRange(toString(f.src_ip), @norm) != (%[1]s))
			FROM flows_raw f`, clause)
		if err := s.conn.QueryRow(ctx, q, args...).Scan(&oldN, &newN, &diff); err != nil {
			t.Fatalf("cidr %d %q: %v", i, c, err)
		}
		if diff != 0 || oldN != newN {
			t.Errorf("cidr %q: old=%d new=%d differing=%d", c, oldN, newN, diff)
		}
		totalMatched += newN
	}
	if totalMatched == 0 {
		t.Fatal("no CIDR matched any row: the comparison proved nothing")
	}

	// Bug #1, end to end: an IPv4 CIDR must match IPv4-mapped rows.
	var n uint64
	clause, args := buildCIDRFilter("f.src_ip", "v4_", []string{"0.0.0.0/0"})
	if err := s.conn.QueryRow(ctx, "SELECT count() FROM flows_raw f WHERE f.ip_version = 4 AND NOT "+clause, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d IPv4 rows fall outside 0.0.0.0/0", n)
	}
	t.Logf("%d CIDRs compared over flows_raw, %d matches, 0 differences", len(cidrs), totalMatched)
}

// TestIntegrationReadPathSmoke runs every read query over a spread of windows
// so a SQL error in any table/step branch fails here rather than in prod.
func TestIntegrationReadPathSmoke(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	windows := []time.Duration{time.Hour, 3 * time.Hour, 6 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 400 * 24 * time.Hour, 3 * 365 * 24 * time.Hour}

	for _, w := range windows {
		p := QueryParams{From: now.Add(-w), To: now, Limit: 10}
		name := w.String()
		check := func(what string, err error) {
			t.Helper()
			if err != nil {
				t.Errorf("%s [%s]: %v", what, name, err)
			}
		}
		_, _, err := s.TopAS(ctx, p)
		check("TopAS", err)
		_, _, err = s.TopIP(ctx, p)
		check("TopIP", err)
		_, _, err = s.TopPrefix(ctx, p)
		check("TopPrefix", err)
		_, _, err = s.TopCountry(ctx, p)
		check("TopCountry", err)
		_, err = s.ASTimeSeries(ctx, 1500, p)
		check("ASTimeSeries", err)
		_, err = s.ASLinkSeries(ctx, 1500, p)
		check("ASLinkSeries", err)
		_, _, _, _, err = s.ASP95(ctx, 1500, p)
		check("ASP95", err)
		_, err = s.ASPeers(ctx, 1500, p)
		check("ASPeers", err)
		_, err = s.ASRemoteIPs(ctx, 1500, p)
		check("ASRemoteIPs", err)
		_, err = s.ASTopIPs(ctx, 1500, p)
		check("ASTopIPs", err)
		_, err = s.IPTimeSeries(ctx, "198.51.96.1", p)
		check("IPTimeSeries", err)
		_, err = s.IPTopAS(ctx, "198.51.96.1", p)
		check("IPTopAS", err)
		_, err = s.IPPeerIPs(ctx, "198.51.96.1", p)
		check("IPPeerIPs", err)
		_, err = s.LinkList(ctx, p)
		check("LinkList", err)
		_, err = s.LinkTimeSeries(ctx, "link1", p)
		check("LinkTimeSeries", err)
		_, err = s.LinkASTimeSeries(ctx, "link1", p)
		check("LinkASTimeSeries", err)
		_, _, err = s.LinkTopAS(ctx, "link1", p)
		check("LinkTopAS", err)
		_, _, err = s.LinkPercentiles(ctx, "link1", p)
		check("LinkPercentiles", err)
		_, err = s.LinksTrafficSeries(ctx, p)
		check("LinksTrafficSeries", err)
		_, err = s.TopASTrafficSeries(ctx, p)
		check("TopASTrafficSeries", err)
		_, err = s.Overview(ctx, p)
		check("Overview", err)
		_, err = s.LinksCapacity(ctx, p.From, p.To)
		check("LinksCapacity", err)
		_, err = s.LinkLoadCurve(ctx, "link1", p.From, p.To)
		check("LinkLoadCurve", err)
		_, err = s.TrafficHeatmap(ctx, p)
		check("TrafficHeatmap", err)
		for _, dim := range []string{"as", "prefix", "country"} {
			_, err = s.Movers(ctx, dim, p.From, p.To, 10)
			check("Movers "+dim, err)
		}
		for _, dim := range []string{"as", "ip", "prefix"} {
			_, err = s.Talkers(ctx, dim, p.From, p.To, 10)
			check("Talkers "+dim, err)
		}
	}
	if _, _, _, err := s.IPInfo(ctx, "198.51.96.1"); err != nil {
		t.Errorf("IPInfo: %v", err)
	}
	if _, err := s.LiveStatus(ctx); err != nil {
		t.Errorf("LiveStatus: %v", err)
	}
	// Alert-engine queries over the hot tables, with a local-prefix filter.
	// A fresh flow two minutes ago (a complete minute) towards a local IPv4
	// address, so the hot-table queries have something to find even when the
	// bulk test data is older than their windows.
	if err := s.conn.Exec(ctx, `INSERT INTO flows_raw (timestamp, router_ip, link_tag, src_ip, dst_ip, src_as, dst_as, protocol, bytes, packets, sampling_rate, direction)
		VALUES (now() - 120, toIPv6('::ffff:192.0.2.1'), 'link1', toIPv6('::ffff:203.0.113.77'), toIPv6('::ffff:198.51.100.77'), 1500, 0, 17, 1500, 1, 1000, 'in')`); err != nil {
		t.Fatalf("insert hot fixture: %v", err)
	}
	local := []string{"198.51.96.0/19", "203.0.113.0/24", "2001:db8::/32"}
	vin, err := s.EvalVolumeInbound(ctx, 1, 0, 300, local)
	if err != nil {
		t.Errorf("EvalVolumeInbound: %v", err)
	} else if len(vin) == 0 {
		t.Errorf("EvalVolumeInbound: no rows over a 1 bps threshold — filter or window broken")
	}
	if _, err := s.EvalSynFlood(ctx, 1, 120, local); err != nil {
		t.Errorf("EvalSynFlood: %v", err)
	}
	if _, err := s.EvalAmplification(ctx, 1, 1, 120, local); err != nil {
		t.Errorf("EvalAmplification: %v", err)
	}
	if _, err := s.EvalProtocolFlood(ctx, 17, 1, 120, local); err != nil {
		t.Errorf("EvalProtocolFlood: %v", err)
	}
	if _, err := s.EvalConnectionFlood(ctx, 1, 120, local); err != nil {
		t.Errorf("EvalConnectionFlood: %v", err)
	}
	if _, err := s.EvalPortScan(ctx, 1, 120, local); err != nil {
		t.Errorf("EvalPortScan: %v", err)
	}
	if _, err := s.EvalVolumeOutbound(ctx, 1, 0, 120, local); err != nil {
		t.Errorf("EvalVolumeOutbound: %v", err)
	}
	if _, err := s.EvalSubnetFlood(ctx, 1, 0, 24, 120, local); err != nil {
		t.Errorf("EvalSubnetFlood: %v", err)
	}
	if _, err := s.EvalSMTPAbuse(ctx, 1, 0, 120, local); err != nil {
		t.Errorf("EvalSMTPAbuse: %v", err)
	}
	if _, lt, err := s.ThreatCandidates(ctx, "in", 300, local); err != nil {
		t.Errorf("ThreatCandidates: %v", err)
	} else if len(lt) == 0 {
		t.Errorf("ThreatCandidates: no rows")
	}
	if _, err := s.TopSourcesForTarget(ctx, net.ParseIP("198.51.100.0"), 600, 5); err != nil {
		t.Errorf("TopSourcesForTarget: %v", err)
	}
	if _, err := s.EvalLinkCapacity(ctx, 0, 3600); err != nil {
		t.Errorf("EvalLinkCapacity: %v", err)
	}
	if _, err := s.AnomalyExplain(ctx, "link1", now.Add(-time.Hour), now); err != nil {
		t.Errorf("AnomalyExplain: %v", err)
	}
}
