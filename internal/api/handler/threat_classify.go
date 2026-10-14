package handler

import (
	"math"
	"slices"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Live Threats classification. Pure functions over one host's numbers, so
// the calibration is unit-tested (threat_classify_test.go) — including real
// production rows that must stay "normal". CLAUDE.md "Live Threats"
// documents every constant below; keep the two in sync.

// Baseline floors: the usual p95 is never taken below these, so a nearly
// idle host does not get a huge ratio from a few packets.
const (
	threatFloorBPS = 1_000_000 // 1 Mb/s
	threatFloorPPS = 1_000
)

// Level thresholds.
const (
	threatIncidentRulePct = 100.0 // closest rule at or past its threshold
	threatIncidentRatio   = 10.0  // attack signature AND this far above usual
	threatWatchRulePct    = 50.0  // half-way to a rule (and above its usual)
	threatWatchRatio      = 3.0   // well above usual…
	threatWatchMinBPS     = 20_000_000
	threatWatchMinPPS     = 5_000 // …and not trivially small
	threatNewMinBPS       = 100_000_000
	threatNewMinPPS       = 20_000 // a host with no history this busy
)

// Signature thresholds.
const (
	sigSynMinPPS          = 1_000
	sigSynShare           = 0.5
	sigUDPFloodShare      = 0.8
	sigUDPFloodMinPPS     = 10_000
	sigICMPFloodShare     = 0.8
	sigICMPFloodMinPPS    = 2_000
	sigAmpPortShare       = 0.5
	sigAmpMinPeers        = 50
	sigAmpMinAvgPacket    = 400
	sigSmallMaxAvgPacket  = 100
	sigSmallMinPPS        = 5_000
	sigManySourcesMin     = 1_000
	sigManySourcesFactor  = 10
	sigPortScanMinPorts   = 500
	sigHostSweepMinPeers  = 1_000
	sigHostSweepMaxPacket = 200
	sigSMTPMinFlowsPerMin = 100
	sigOutFloodRatio      = 5.0
	sigOutFloodMinPPS     = 10_000
)

// Signature kinds.
const (
	sigSynFlood      = "syn_flood"
	sigUDPFlood      = "udp_flood"
	sigICMPFlood     = "icmp_flood"
	sigAmplification = "amplification"
	sigSmallPackets  = "small_packets"
	sigManySources   = "many_sources"
	sigPortScan      = "port_scan"
	sigHostSweep     = "host_sweep"
	sigSMTPBurst     = "smtp_burst"
	sigOutboundFlood = "outbound_flood"
)

// attackSignatures are the kinds that, combined with a ratio ≥
// threatIncidentRatio, make an incident on their own.
var attackSignatures = map[string]bool{
	sigSynFlood: true, sigUDPFlood: true, sigICMPFlood: true, sigAmplification: true,
	sigPortScan: true, sigHostSweep: true, sigOutboundFlood: true,
}

// reflectorPorts are UDP source ports abused for reflection/amplification;
// 0 is IP fragments (no L4 header after the first fragment).
var reflectorPorts = map[uint16]bool{
	0: true, 19: true, 53: true, 111: true, 123: true, 137: true, 161: true, 389: true,
	1900: true, 3283: true, 3702: true, 5353: true, 11211: true, 27015: true,
}

// threatInput is everything known about one host.
type threatInput struct {
	Direction      string // "in" | "out"
	WindowSeconds  uint32
	Now            model.ThreatNow
	UDPBytes       uint64 // window bytes over UDP
	Bytes          uint64 // window bytes
	MaxMinutePeers uint64
	Baseline       model.ThreatBaseline
	PeersP50       uint64                  // usual per-minute peers; 0 = not looked up
	Ports          *store.ThreatPortLookup // nil until looked up
}

// threatVerdict is the classification of one host.
type threatVerdict struct {
	Level       string
	Score       float64
	Ratio       float64
	Signatures  []model.ThreatSignature
	ClosestRule string
	RulePct     float64
}

// hasHistory reports whether the host was seen at this time of day before.
func (in threatInput) hasHistory() bool { return in.Baseline.Samples > 0 }

// aboveUsual reports whether the host is above its usual p95 (strictly — a
// host sitting exactly at its p95 is at its usual level), or is new.
func (in threatInput) aboveUsual(ratio float64) bool { return !in.hasHistory() || ratio > 1 }

// usualThresholds are the per-host "above usual" limits: the p95 with floors.
func usualThresholds(b model.ThreatBaseline) (bps, pps float64) {
	return math.Max(float64(b.BPSP95), threatFloorBPS), math.Max(float64(b.PPSP95), threatFloorPPS)
}

// threatRatio is max(bps / usual bps, pps / usual pps), floors applied.
func threatRatio(now model.ThreatNow, b model.ThreatBaseline) float64 {
	tb, tp := usualThresholds(b)
	return math.Max(float64(now.BPS)/tb, float64(now.PPS)/tp)
}

// classifyThreat computes ratio, signatures, closest rule, level and score.
func classifyThreat(in threatInput, rules []model.AlertRule) threatVerdict {
	v := threatVerdict{Ratio: threatRatio(in.Now, in.Baseline)}
	var firingPct float64
	v.ClosestRule, v.RulePct, firingPct = closestRule(in.Direction, in.Now, rules)
	v.Signatures = threatSignatures(in, v.Ratio)

	// "Above usual" gates the shape signatures (in threatSignatures) and the
	// half-way-to-a-rule watch: a busy host at its usual level stays quiet.
	aboveUsual := in.aboveUsual(v.Ratio)
	attack := false
	for _, s := range v.Signatures {
		attack = attack || attackSignatures[s.Kind]
	}
	n := in.Now
	switch {
	case firingPct >= threatIncidentRulePct || (attack && v.Ratio >= threatIncidentRatio):
		v.Level = model.ThreatLevelIncident
	case (v.RulePct >= threatWatchRulePct && aboveUsual) ||
		len(v.Signatures) > 0 ||
		(in.hasHistory() && v.Ratio >= threatWatchRatio && (n.BPS >= threatWatchMinBPS || n.PPS >= threatWatchMinPPS)) ||
		(!in.hasHistory() && (n.BPS >= threatNewMinBPS || n.PPS >= threatNewMinPPS)):
		v.Level = model.ThreatLevelWatch
	default:
		v.Level = model.ThreatLevelNormal
	}

	// Score orders rows within a level: the deviation, or the rule
	// proximity scaled so 100 % of a rule weighs like a 10× deviation,
	// plus 2 per signature.
	score := math.Max(v.Ratio, v.RulePct/10) + 2*float64(len(v.Signatures))
	v.Score = round(score, 1)
	v.Ratio = round(v.Ratio, 2)
	v.RulePct = round(v.RulePct, 1)
	return v
}

// threatSignatures returns the attack shapes the host matches. Shape-only
// signatures (UDP / ICMP floods, amplification, small packets, host sweep)
// count only when the host is above its usual level or has no history — an
// authoritative DNS server is all small UDP packets every day.
func threatSignatures(in threatInput, ratio float64) []model.ThreatSignature {
	n := in.Now
	aboveUsual := in.aboveUsual(ratio)
	sigs := []model.ThreatSignature{}
	add := func(kind string, ports []model.ThreatPort) {
		sigs = append(sigs, model.ThreatSignature{Kind: kind, TopPorts: ports})
	}
	share := func(part uint64) float64 {
		if n.PPS == 0 {
			return 0
		}
		return float64(part) / float64(n.PPS)
	}

	if in.Direction == "out" {
		if n.PeerPorts >= sigPortScanMinPorts {
			add(sigPortScan, nil)
		}
		if aboveUsual && n.Peers >= sigHostSweepMinPeers && n.AvgPacketBytes < sigHostSweepMaxPacket {
			add(sigHostSweep, nil)
		}
		if ports, ok := smtpBurst(in.Ports); ok {
			add(sigSMTPBurst, ports)
		}
		if ratio >= sigOutFloodRatio && n.PPS >= sigOutFloodMinPPS {
			add(sigOutboundFlood, nil)
		}
	} else {
		if n.SynPPS >= sigSynMinPPS && float64(n.SynPPS) >= sigSynShare*float64(n.TCPPPS) {
			add(sigSynFlood, nil)
		}
		if aboveUsual && n.PPS >= sigUDPFloodMinPPS && share(n.UDPPPS) >= sigUDPFloodShare {
			add(sigUDPFlood, nil)
		}
		if aboveUsual && n.PPS >= sigICMPFloodMinPPS && share(n.ICMPPPS) >= sigICMPFloodShare {
			add(sigICMPFlood, nil)
		}
		if aboveUsual && n.Peers >= sigAmpMinPeers && n.AvgPacketBytes >= sigAmpMinAvgPacket {
			if ports, ok := amplificationPorts(in.Ports); ok {
				add(sigAmplification, ports)
			}
		}
		if n.Peers >= sigManySourcesMin &&
			(!in.hasHistory() || (in.PeersP50 > 0 && in.MaxMinutePeers >= sigManySourcesFactor*in.PeersP50)) {
			add(sigManySources, nil)
		}
	}
	if aboveUsual && n.PPS >= sigSmallMinPPS && n.AvgPacketBytes < sigSmallMaxAvgPacket {
		add(sigSmallPackets, nil)
	}
	return sigs
}

// amplificationPorts reports whether one reflector UDP source port carries
// at least sigAmpPortShare of the host's bytes, and returns the top UDP
// source ports with their byte shares.
func amplificationPorts(l *store.ThreatPortLookup) ([]model.ThreatPort, bool) {
	if l == nil || l.TotalBytes == 0 {
		return nil, false
	}
	var top []model.ThreatPort
	hit := false
	for _, p := range l.Ports { // already ordered by bytes desc (TCP/25 first, ignored here)
		if p.Protocol != 17 {
			continue
		}
		pct := float64(p.Bytes) / float64(l.TotalBytes)
		if reflectorPorts[p.Port] && pct >= sigAmpPortShare {
			hit = true
		}
		if len(top) < 3 {
			top = append(top, model.ThreatPort{Port: p.Port, Protocol: p.Protocol, Pct: round(pct*100, 1)})
		}
	}
	return top, hit
}

// smtpBurst reports whether a local source opened at least
// sigSMTPMinFlowsPerMin flows per minute to TCP/25, with that port's share
// of the host's flows.
func smtpBurst(l *store.ThreatPortLookup) ([]model.ThreatPort, bool) {
	if l == nil || l.WindowSeconds == 0 || l.TotalFlows == 0 {
		return nil, false
	}
	minutes := float64(l.WindowSeconds) / 60
	for _, p := range l.Ports {
		if p.Protocol == 6 && p.Port == 25 && float64(p.Flows)/minutes >= sigSMTPMinFlowsPerMin {
			return []model.ThreatPort{{Port: 25, Protocol: 6, Pct: round(float64(p.Flows)/float64(l.TotalFlows)*100, 1)}}, true
		}
	}
	return nil, false
}

// needsPortLookup reports whether a host should get the flows_raw port
// breakdown: it already looks abnormal, or (inbound) it has the hot-table
// shape of an amplification attack that only the ports can confirm.
func needsPortLookup(in threatInput, v threatVerdict) bool {
	if v.Level != model.ThreatLevelNormal {
		return true
	}
	if in.Direction != "in" || in.Bytes == 0 {
		return false
	}
	return in.aboveUsual(v.Ratio) && float64(in.UDPBytes) >= 0.5*float64(in.Bytes) &&
		in.Now.Peers >= sigAmpMinPeers && in.Now.AvgPacketBytes >= sigAmpMinAvgPacket
}

// closestRule compares a host with the enabled rules of its direction and
// returns the one it is closest to, as a percentage of its threshold, plus
// the highest percentage among non-"info" rules — only those make an
// incident on their own (the default 50 Mb/s "Sustained outbound
// exfiltration" info rule would otherwise flag every busy server). Peer
// counts are over the Live Threats window, which may be longer than the
// rule's own window — an over-estimate, never an under-estimate.
func closestRule(direction string, n model.ThreatNow, rules []model.AlertRule) (name string, worst, firing float64) {
	pct := func(v float64, th uint64) float64 {
		if th == 0 {
			return 0
		}
		return v / float64(th) * 100
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		var p float64
		switch {
		case direction == "in" && r.RuleType == "volume_in",
			direction == "out" && r.RuleType == "volume_out":
			if r.ThresholdBps > 0 {
				p = pct(float64(n.BPS), r.ThresholdBps)
			} else {
				p = pct(float64(n.PPS), r.ThresholdPps)
			}
		case direction == "in" && r.RuleType == "syn_flood":
			p = pct(float64(n.SynPPS), r.ThresholdPps)
		case direction == "in" && r.RuleType == "icmp_flood":
			p = pct(float64(n.ICMPPPS), r.ThresholdPps)
		case direction == "in" && r.RuleType == "udp_flood":
			p = pct(float64(n.UDPPPS), r.ThresholdPps)
		case direction == "in" && r.RuleType == "amplification":
			// The rule needs both conditions: proximity is the farther one.
			p = pct(float64(n.Peers), r.ThresholdCount)
			if r.ThresholdBps > 0 {
				p = math.Min(p, pct(float64(n.BPS), r.ThresholdBps))
			}
		case direction == "in" && r.RuleType == "connection_flood":
			// threshold_count is flows per rule window.
			p = pct(n.FlowsPerSec*float64(max(r.WindowSeconds, 60)), r.ThresholdCount)
		case direction == "in" && r.RuleType == "subnet_flood":
			// Per-host vs the /N aggregate threshold: a lower bound.
			p = pct(float64(n.BPS), r.ThresholdBps)
		case direction == "out" && r.RuleType == "port_scan":
			p = pct(float64(n.PeerPorts), r.ThresholdCount)
		default:
			continue
		}
		if p > worst {
			worst, name = p, r.Name
		}
		if r.Severity != "info" && p > firing {
			firing = p
		}
	}
	return name, worst, firing
}

// threatSince returns the first minute of the run of minutes above the
// host's usual p95 (floors applied) that reaches the end of the series, or
// "" when the last complete minute is not above it.
func threatSince(series []model.ThreatPoint, b model.ThreatBaseline) string {
	tb, tp := usualThresholds(b)
	above := func(p model.ThreatPoint) bool { return float64(p.BPS) > tb || float64(p.PPS) > tp }
	i := len(series)
	for i > 0 && above(series[i-1]) {
		i--
	}
	if i == len(series) {
		return ""
	}
	return series[i].T.UTC().Format(time.RFC3339)
}

// threatLevelRank orders levels for display: incident first.
func threatLevelRank(level string) int {
	switch level {
	case model.ThreatLevelIncident:
		return 0
	case model.ThreatLevelWatch:
		return 1
	}
	return 2
}

// sortThreatRows orders rows by level, then score, then bps (all descending).
func sortThreatRows(rows []model.ThreatRow) {
	slices.SortStableFunc(rows, func(a, b model.ThreatRow) int {
		if d := threatLevelRank(a.Level) - threatLevelRank(b.Level); d != 0 {
			return d
		}
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		case a.Now.BPS > b.Now.BPS:
			return -1
		case a.Now.BPS < b.Now.BPS:
			return 1
		}
		return 0
	})
}

func round(v float64, decimals int) float64 {
	p := math.Pow10(decimals)
	return math.Round(v*p) / p
}
