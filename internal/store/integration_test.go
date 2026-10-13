package store

import (
	"context"
	"fmt"
	"os"
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
	if _, err := s.EvalLinkCapacity(ctx, 0, 3600); err != nil {
		t.Errorf("EvalLinkCapacity: %v", err)
	}
	if _, err := s.AnomalyExplain(ctx, "link1", now.Add(-time.Hour), now); err != nil {
		t.Errorf("AnomalyExplain: %v", err)
	}
}
