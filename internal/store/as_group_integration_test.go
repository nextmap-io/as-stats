package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// TestIntegrationPrivateASGrouping: IPTopAS, ASPeers and AnomalyExplain fold
// private-use ASNs into the single labelled PrivateASGroup row, with the
// bytes of every member, like Top AS. (Before 3.5 the IP detail page listed
// 64512, 65001, 4200000001… one by one, nameless.)
func TestIntegrationPrivateASGrouping(t *testing.T) {
	s, _ := scratchSchema(t, "asstats_it_asgroup", 0)
	ctx := context.Background()
	at := time.Now().UTC().Add(-30 * time.Minute).Truncate(5 * time.Minute).Add(time.Minute)
	in, out := model.DirectionInbound, model.DirectionOutbound
	writeFlows(t, s, at, []flow{
		// Into 192.0.2.10 on link L from three private ASNs and one public.
		{link: "L", dir: in, proto: 6, sport: 443, dport: 50000, srcAS: 64512, dstAS: 64500, dst: "192.0.2.10", bytes: 1000},
		{link: "L", dir: in, proto: 6, sport: 443, dport: 50001, srcAS: 65001, dstAS: 64500, dst: "192.0.2.10", bytes: 1000, n: 2},
		{link: "L", dir: in, proto: 6, sport: 443, dport: 50002, srcAS: 4200000001, dstAS: 64500, dst: "192.0.2.10", bytes: 1000},
		{link: "L", dir: in, proto: 6, sport: 443, dport: 50003, srcAS: 15169, dstAS: 64500, dst: "192.0.2.10", bytes: 3000},
		// From AS64500 to private and public peers.
		{link: "L", dir: out, proto: 6, sport: 50004, dport: 443, srcAS: 64500, dstAS: 64513, src: "192.0.2.11", dst: "198.51.100.9", bytes: 700},
		{link: "L", dir: out, proto: 6, sport: 50005, dport: 443, srcAS: 64500, dstAS: 4200000002, src: "192.0.2.11", dst: "198.51.100.9", bytes: 700},
		{link: "L", dir: out, proto: 6, sport: 50006, dport: 443, srcAS: 64500, dstAS: 13335, src: "192.0.2.11", dst: "198.51.100.9", bytes: 1000},
	})
	p := QueryParams{From: at.Add(-10 * time.Minute), To: at.Add(10 * time.Minute), Limit: 20}
	label := func(rows []model.ASTraffic) string {
		var parts []string
		for _, r := range rows {
			parts = append(parts, fmt.Sprintf("%d %q %d", r.ASNumber, r.ASName, r.Bytes))
		}
		return strings.Join(parts, "; ")
	}
	group := fmt.Sprintf("%d %q", model.PrivateASGroup, model.PrivateASName)

	ipAS, err := s.IPTopAS(ctx, "192.0.2.10", p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := label(ipAS), fmt.Sprintf("%s 4000; 15169 \"\" 3000", group); got != want {
		t.Errorf("IPTopAS = %s\nwant      %s", got, want)
	}
	peers, err := s.ASPeers(ctx, 64500, p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := label(peers), fmt.Sprintf("%s 1400; 13335 \"\" 1000", group); got != want {
		t.Errorf("ASPeers = %s\nwant      %s", got, want)
	}
	expl, err := s.AnomalyExplain(ctx, "L", p.From, p.To)
	if err != nil {
		t.Fatal(err)
	}
	var ex []string
	for _, a := range expl.TopASes {
		ex = append(ex, fmt.Sprintf("%d %q %d", a.ASNumber, a.ASName, a.Bytes))
	}
	if got, want := strings.Join(ex, "; "), fmt.Sprintf("%s 4000; 15169 \"\" 3000; 64500 \"\" 2400", group); got != want {
		t.Errorf("AnomalyExplain top ASes = %s\nwant                       %s", got, want)
	}
}
