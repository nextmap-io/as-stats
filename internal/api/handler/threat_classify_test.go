package handler

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/alerts"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// ruleSink captures the default rules EnsureDefaultRules seeds.
type ruleSink struct{ rules []model.AlertRule }

func (s *ruleSink) ListAlertRules(context.Context) ([]model.AlertRule, error) { return nil, nil }
func (s *ruleSink) UpsertAlertRule(_ context.Context, r model.AlertRule) error {
	s.rules = append(s.rules, r)
	return nil
}

func defaultRules(t *testing.T) []model.AlertRule {
	t.Helper()
	var s ruleSink
	if err := alerts.EnsureDefaultRules(context.Background(), &s); err != nil {
		t.Fatal(err)
	}
	return s.rules
}

const mbps = 1_000_000

// prodRow builds an inbound input from a production snapshot line
// (Mb/s, per-protocol pps, peers, average packet size). The snapshot gives
// bps percentiles only; pps percentiles are derived with the same packet
// size, the way the hot table would report them.
func prodRow(nowMbps, p50Mbps, p95Mbps float64, pps, udp, icmp, syn, srcs, sports, pkt uint64) threatInput {
	ppsOf := func(m float64) uint64 { return uint64(m * mbps / 8 / float64(pkt)) }
	return threatInput{
		Direction: "in", WindowSeconds: 300,
		Now: model.ThreatNow{
			BPS: uint64(nowMbps * mbps), PPS: pps,
			TCPPPS: pps - udp - icmp, UDPPPS: udp, ICMPPPS: icmp, SynPPS: syn,
			AvgPacketBytes: pkt, Peers: srcs, PeerPorts: sports,
		},
		Bytes:          uint64(nowMbps * mbps / 8 * 300),
		UDPBytes:       udp * pkt * 300,
		MaxMinutePeers: srcs / 3,
		Baseline: model.ThreatBaseline{
			Samples: 420,
			BPSP50:  uint64(p50Mbps * mbps), BPSP95: uint64(p95Mbps * mbps),
			PPSP50: ppsOf(p50Mbps), PPSP95: ppsOf(p95Mbps),
		},
		PeersP50: srcs / 4,
	}
}

func sigKinds(v threatVerdict) []string {
	out := []string{}
	for _, s := range v.Signatures {
		out = append(out, s.Kind)
	}
	return out
}

// TestClassifyProdSnapshot pins the calibration on a real inbound snapshot:
// busy hosts at their usual level must stay "normal" — including one at 63 %
// of the default 500 Mb/s volume_in rule, which is its everyday level.
func TestClassifyProdSnapshot(t *testing.T) {
	rules := defaultRules(t)
	cases := []struct {
		name string
		in   threatInput
	}{
		{"85.208.145.137 busy", prodRow(316.6, 171.4, 815.2, 33467, 0, 0, 0, 89, 277, 1182)},
		{"85.208.144.167 busy, many sources", prodRow(213.9, 155.6, 563.4, 26353, 8, 5, 3, 1346, 2256, 1015)},
		{"85.208.144.225 busy", prodRow(34.1, 20.2, 219.5, 3726, 131, 2, 0, 980, 55, 1144)},
		{"85.208.145.26", prodRow(23.2, 18.2, 42.4, 2352, 799, 2, 0, 578, 60, 1232)},
		{"2a09:8740::7", prodRow(15.9, 9.8, 16.1, 1747, 0, 1, 0, 148, 2, 1141)},
		{"2a09:8740:0:3::12:8224 calm after an attack", prodRow(0.5, 1.2, 122.2, 630, 1, 1, 0, 981, 490, 97)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := classifyThreat(tc.in, rules)
			if v.Level != model.ThreatLevelNormal || len(v.Signatures) != 0 {
				t.Fatalf("level=%s sigs=%v ratio=%.2f rule=%q %.1f%%, want normal without signatures",
					v.Level, sigKinds(v), v.Ratio, v.ClosestRule, v.RulePct)
			}
			if needsPortLookup(tc.in, v) {
				t.Errorf("normal busy host sent to the flows_raw port lookup")
			}
		})
	}
	// The 63 % proximity is still reported, it just does not raise the level.
	v := classifyThreat(cases[0].in, rules)
	if v.ClosestRule != "High inbound volume" || v.RulePct < 63 || v.RulePct > 64 {
		t.Errorf("closest rule = %q %.1f%%, want High inbound volume ~63%%", v.ClosestRule, v.RulePct)
	}
}

func TestClassifyThreat(t *testing.T) {
	rules := defaultRules(t)
	history := func(bpsP95, ppsP95 uint64) model.ThreatBaseline {
		return model.ThreatBaseline{Samples: 400, BPSP50: bpsP95 / 2, BPSP95: bpsP95, PPSP50: ppsP95 / 2, PPSP95: ppsP95}
	}
	ntpPorts := &store.ThreatPortLookup{WindowSeconds: 300, TotalBytes: 1000, TotalFlows: 100,
		Ports: []store.ThreatPortStat{{Protocol: 17, Port: 123, Bytes: 930, Flows: 90}, {Protocol: 17, Port: 53, Bytes: 50, Flows: 5}}}
	cases := []struct {
		name  string
		in    threatInput
		level string
		sigs  []string
	}{
		{
			name: "host jumping x20 without a signature",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 100 * mbps, PPS: 10_000, TCPPPS: 10_000, AvgPacketBytes: 1250, Peers: 40},
				Baseline: history(6*mbps, 600), PeersP50: 30, MaxMinutePeers: 35},
			level: model.ThreatLevelWatch, sigs: []string{},
		},
		{
			name: "small host x4 stays below the absolute floor",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 4 * mbps, PPS: 400, TCPPPS: 400, AvgPacketBytes: 1250, Peers: 5},
				Baseline: history(1*mbps, 100)},
			level: model.ThreatLevelNormal, sigs: []string{},
		},
		{
			name: "SYN flood",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 12 * mbps, PPS: 25_500, TCPPPS: 25_500, SynPPS: 25_000, AvgPacketBytes: 60, Peers: 4800},
				Baseline: history(2*mbps, 300), PeersP50: 20, MaxMinutePeers: 4000},
			level: model.ThreatLevelIncident, sigs: []string{sigSynFlood, sigManySources, sigSmallPackets},
		},
		{
			name: "NTP amplification on a new host (ports looked up)",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 200 * mbps, PPS: 53_000, UDPPPS: 53_000, AvgPacketBytes: 468, Peers: 300},
				Bytes: 1000, UDPBytes: 1000, Ports: ntpPorts},
			level: model.ThreatLevelIncident, sigs: []string{sigUDPFlood, sigAmplification},
		},
		{
			name: "amplification shape below the volume floors still gets a signature",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 8 * mbps, PPS: 2100, UDPPPS: 2100, AvgPacketBytes: 476, Peers: 120},
				Baseline: history(2*mbps, 400), Bytes: 1000, UDPBytes: 990, Ports: ntpPorts},
			level: model.ThreatLevelWatch, sigs: []string{sigAmplification},
		},
		{
			name: "authoritative DNS at its usual level: small UDP packets are normal",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 13 * mbps, PPS: 20_000, UDPPPS: 20_000, AvgPacketBytes: 80, Peers: 900},
				Baseline: history(20*mbps, 30_000), PeersP50: 400, MaxMinutePeers: 400},
			level: model.ThreatLevelNormal, sigs: []string{},
		},
		{
			name: "the same DNS server flooded x5",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 64 * mbps, PPS: 150_000, UDPPPS: 150_000, AvgPacketBytes: 53, Peers: 900},
				Baseline: history(20*mbps, 30_000), PeersP50: 400, MaxMinutePeers: 500},
			level: model.ThreatLevelIncident, // udp_flood rule (100k pps) at 150 %
			sigs:  []string{sigUDPFlood, sigSmallPackets},
		},
		{
			name: "ICMP flood",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 5 * mbps, PPS: 9_000, ICMPPPS: 8_800, TCPPPS: 200, AvgPacketBytes: 70, Peers: 30},
				Baseline: history(1*mbps, 100)},
			level: model.ThreatLevelWatch, sigs: []string{sigICMPFlood, sigSmallPackets},
		},
		{
			name: "many sources ten times the usual",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 3 * mbps, PPS: 900, TCPPPS: 900, AvgPacketBytes: 400, Peers: 5000},
				Baseline: history(2*mbps, 400), PeersP50: 100, MaxMinutePeers: 2000},
			level: model.ThreatLevelWatch, sigs: []string{sigManySources},
		},
		{
			name: "critical volume rule fires even at the usual level",
			in: threatInput{Direction: "in", Now: model.ThreatNow{BPS: 2500 * mbps, PPS: 250_000, TCPPPS: 250_000, AvgPacketBytes: 1250, Peers: 80},
				Baseline: history(3000*mbps, 300_000), PeersP50: 60, MaxMinutePeers: 70},
			level: model.ThreatLevelIncident, sigs: []string{},
		},
		{
			name:  "new host, moderate traffic",
			in:    threatInput{Direction: "in", Now: model.ThreatNow{BPS: 30 * mbps, PPS: 3000, TCPPPS: 3000, AvgPacketBytes: 1250, Peers: 10}},
			level: model.ThreatLevelNormal, sigs: []string{},
		},
		{
			name:  "new host, heavy traffic",
			in:    threatInput{Direction: "in", Now: model.ThreatNow{BPS: 150 * mbps, PPS: 15_000, TCPPPS: 15_000, AvgPacketBytes: 1250, Peers: 10}},
			level: model.ThreatLevelWatch, sigs: []string{},
		},
		{
			name: "outbound port scan",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 160_000, PPS: 333, TCPPPS: 333, AvgPacketBytes: 60, Peers: 1, PeerPorts: 2000},
				Baseline: history(1*mbps, 200)},
			level: model.ThreatLevelIncident, // port_scan rule (1000 ports) at 200 %
			sigs:  []string{sigPortScan},
		},
		{
			name: "outbound scan below the rule",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 100_000, PPS: 200, TCPPPS: 200, AvgPacketBytes: 60, Peers: 1, PeerPorts: 700},
				Baseline: history(1*mbps, 200)},
			level: model.ThreatLevelWatch, sigs: []string{sigPortScan},
		},
		{
			name: "outbound host sweep",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 2 * mbps, PPS: 3000, TCPPPS: 3000, AvgPacketBytes: 80, Peers: 4000, PeerPorts: 2},
				Baseline: history(1*mbps, 200)},
			level: model.ThreatLevelWatch, sigs: []string{sigHostSweep},
		},
		{
			name: "outbound SMTP burst",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 2 * mbps, PPS: 400, TCPPPS: 400, AvgPacketBytes: 600, Peers: 600, PeerPorts: 3},
				Baseline: history(1*mbps, 200),
				Ports: &store.ThreatPortLookup{WindowSeconds: 300, TotalBytes: 1000, TotalFlows: 1600,
					Ports: []store.ThreatPortStat{{Protocol: 6, Port: 25, Bytes: 900, Flows: 1500}, {Protocol: 17, Port: 53, Bytes: 10, Flows: 100}}}},
			level: model.ThreatLevelWatch, sigs: []string{sigSMTPBurst},
		},
		{
			name: "outbound flood x20",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 400 * mbps, PPS: 80_000, UDPPPS: 80_000, AvgPacketBytes: 625, Peers: 2},
				Baseline: history(20*mbps, 4000)},
			level: model.ThreatLevelIncident, sigs: []string{sigOutboundFlood},
		},
		{
			name: "busy server sending at its usual level: the info exfiltration rule is not an incident",
			in: threatInput{Direction: "out", Now: model.ThreatNow{BPS: 120 * mbps, PPS: 12_000, TCPPPS: 12_000, AvgPacketBytes: 1250, Peers: 300, PeerPorts: 400},
				Baseline: history(200*mbps, 20_000)},
			level: model.ThreatLevelNormal, sigs: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := classifyThreat(tc.in, rules)
			got := sigKinds(v)
			if v.Level != tc.level || !slices.Equal(got, tc.sigs) {
				t.Fatalf("level=%s sigs=%v (ratio=%.2f rule=%q %.1f%%), want %s %v",
					v.Level, got, v.Ratio, v.ClosestRule, v.RulePct, tc.level, tc.sigs)
			}
		})
	}
}

func TestClassifyAmplificationPorts(t *testing.T) {
	in := threatInput{Direction: "in", Now: model.ThreatNow{BPS: 200 * mbps, PPS: 53_000, UDPPPS: 53_000, AvgPacketBytes: 468, Peers: 300},
		Bytes: 1000, UDPBytes: 1000}
	v := classifyThreat(in, nil)
	if !needsPortLookup(in, v) {
		t.Fatal("amplification-shaped host not sent to the port lookup")
	}
	in.Ports = &store.ThreatPortLookup{WindowSeconds: 300, TotalBytes: 1000,
		Ports: []store.ThreatPortStat{{Protocol: 17, Port: 123, Bytes: 924}, {Protocol: 17, Port: 443, Bytes: 40}}}
	v = classifyThreat(in, nil)
	var amp *model.ThreatSignature
	for i := range v.Signatures {
		if v.Signatures[i].Kind == sigAmplification {
			amp = &v.Signatures[i]
		}
	}
	if amp == nil || len(amp.TopPorts) != 2 || amp.TopPorts[0] != (model.ThreatPort{Port: 123, Protocol: 17, Pct: 92.4}) {
		t.Fatalf("amplification signature = %+v", amp)
	}
	// A non-reflector port carrying the bytes (QUIC) is not amplification.
	in.Ports.Ports[0].Port = 443
	if v := classifyThreat(in, nil); slices.Contains(sigKinds(v), sigAmplification) {
		t.Fatal("UDP/443 classified as amplification")
	}
}

func TestClosestRuleProtocolFloods(t *testing.T) {
	rules := defaultRules(t)
	name, pct, _ := closestRule("in", model.ThreatNow{PPS: 15_000, ICMPPPS: 15_000}, rules, 0, false)
	if name != "ICMP flood" || round(pct, 1) != 75 {
		t.Errorf("ICMP: %q %.1f, want ICMP flood 75", name, pct)
	}
	name, pct, _ = closestRule("in", model.ThreatNow{PPS: 80_000, UDPPPS: 80_000}, rules, 0, false)
	if name != "UDP flood" || round(pct, 1) != 80 {
		t.Errorf("UDP: %q %.1f, want UDP flood 80", name, pct)
	}
	// Outbound rows compare with outbound rules only.
	name, pct, firing := closestRule("out", model.ThreatNow{BPS: 100 * mbps, PeerPorts: 10}, rules, 0, false)
	if name != "Sustained outbound exfiltration" || round(pct, 1) != 200 || round(firing, 1) != 20 {
		t.Errorf("out: %q %.1f firing %.1f, want exfiltration 200 / firing 20", name, pct, firing)
	}
	if name, _, _ := closestRule("in", model.ThreatNow{PeerPorts: 5000}, rules, 0, false); name == "Port scan from internal host" {
		t.Error("inbound row compared with the outbound port_scan rule")
	}
}

// A rule with a baseline_factor fires (and so makes an incident) only when
// the host is that far above its usual level, as in the alert engine.
func TestClosestRuleBaselineGate(t *testing.T) {
	rules := defaultRules(t)
	busy := model.ThreatNow{BPS: 610 * mbps} // 122 % of the 500 Mb/s rule
	name, pct, firing := closestRule("in", busy, rules, 1.2, true)
	// Only the 2 Gb/s rule (no factor) counts: 30.5 %.
	if name != "High inbound volume" || round(pct, 0) != 122 || round(firing, 1) != 30.5 {
		t.Errorf("busy host at 1.2x usual: %q %.0f%% firing %.1f, want 122%% and firing 30.5 (the 2 Gb/s rule)", name, pct, firing)
	}
	if _, _, firing := closestRule("in", busy, rules, 2.5, true); round(firing, 0) != 122 {
		t.Errorf("2.5x usual: firing %.0f, want 122", firing)
	}
	if _, _, firing := closestRule("in", busy, rules, 1.2, false); round(firing, 0) != 122 {
		t.Errorf("no history: firing %.0f, want 122", firing)
	}
	// The 2 Gb/s rule has no factor: a flood on a busy host still fires.
	if _, _, firing := closestRule("in", model.ThreatNow{BPS: 2500 * mbps}, rules, 1.5, true); firing < 100 {
		t.Errorf("2.5 Gb/s on a busy host: firing %.0f, want >= 100", firing)
	}
}

func TestThreatSince(t *testing.T) {
	base := time.Date(2026, 10, 13, 21, 0, 0, 0, time.UTC)
	series := make([]model.ThreatPoint, 60)
	for i := range series {
		series[i] = model.ThreatPoint{T: base.Add(time.Duration(i) * time.Minute), BPS: 5 * mbps, PPS: 500}
	}
	b := model.ThreatBaseline{Samples: 100, BPSP95: 6 * mbps, PPSP95: 600}
	if got := threatSince(series, b); got != "" {
		t.Errorf("calm series: since = %q", got)
	}
	series[30].BPS = 50 * mbps // an earlier, finished spike
	for i := 52; i < 60; i++ {
		series[i].BPS = 50 * mbps
	}
	if got, want := threatSince(series, b), "2026-10-13T21:52:00Z"; got != want {
		t.Errorf("since = %q, want %q", got, want)
	}
	for i := range series {
		series[i].BPS = 50 * mbps
	}
	if got, want := threatSince(series, b), "2026-10-13T21:00:00Z"; got != want {
		t.Errorf("whole series above: since = %q, want %q", got, want)
	}
}

func TestSortThreatRows(t *testing.T) {
	rows := []model.ThreatRow{
		{IP: "a", Level: model.ThreatLevelWatch, Score: 9},
		{IP: "b", Level: model.ThreatLevelIncident, Score: 3},
		{IP: "c", Level: model.ThreatLevelWatch, Score: 12},
		{IP: "d", Level: model.ThreatLevelNormal, Score: 50},
	}
	sortThreatRows(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.IP)
	}
	if !slices.Equal(got, []string{"b", "c", "a", "d"}) {
		t.Errorf("order = %v", got)
	}
}
