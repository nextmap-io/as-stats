package store

import (
	"context"
	"os"
	"testing"
	"time"

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
