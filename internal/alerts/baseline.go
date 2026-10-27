package alerts

import (
	"context"
	"log"
	"math"
	"net"
	"time"

	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// Baseline mode (alert_rules.baseline_factor, volume_in / volume_out).
//
// A static per-host threshold fires on every host whose normal traffic is
// above it: in production the 500 Mb/s volume rules fired 2,678 times in 10
// days on 8–11 busy servers at their usual level (CLAUDE.md "Alert engine").
// With a factor f > 0 a violation of the absolute threshold is kept only when
// the host is also at least f times its usual level — the Live Threats
// history (same time of day ±30 min over the previous 7 days, store
// ThreatBaselines) but its per-minute p99, never below the floors. The p99,
// not the p95 Live Threats ranks with: a bursty server whose bursts fill
// only ~5 % of its minutes has a p95 that sometimes falls below the burst
// level, and the replay test caught one such burst firing at 2x the p95.
// A host with too little history (store.BaselineMinSamples minutes) is judged
// on the threshold alone, so a flood on a new or usually idle host still
// fires; a rule without a factor (the 2 Gb/s critical rule) is never gated.

// UsualLevel is the alert baseline of one host: per-minute p99 as rates,
// floors applied.
func UsualLevel(h store.ThreatHistory) (bps, pps float64) {
	return math.Max(float64(h.BytesP99)*8/60, store.BaselineFloorBPS),
		math.Max(float64(h.PacketsP99)/60, store.BaselineFloorPPS)
}

// baselineDirection is the hot table side a rule's targets live on.
func baselineDirection(ruleType string) (string, bool) {
	switch ruleType {
	case "volume_in":
		return "in", true
	case "volume_out":
		return "out", true
	}
	return "", false
}

// BaselineCapable reports whether a rule type supports baseline_factor.
func BaselineCapable(ruleType string) bool {
	_, ok := baselineDirection(ruleType)
	return ok
}

// baselineVerdict decides one violation of a baseline rule. keep reports
// whether it still fires; info goes into the alert details.
func baselineVerdict(rule model.AlertRule, v store.AlertViolation, h store.ThreatHistory) (keep bool, info map[string]any) {
	info = map[string]any{
		"factor":  rule.BaselineFactor,
		"samples": h.Samples,
	}
	if h.Samples < store.BaselineMinSamples {
		info["new_host"] = true
		return true, info
	}
	usualBPS, usualPPS := UsualLevel(h)
	ratio := 0.0
	if rule.ThresholdBps > 0 {
		ratio = math.Max(ratio, v.BPS/usualBPS)
	}
	if rule.ThresholdPps > 0 {
		ratio = math.Max(ratio, v.PPS/usualPPS)
	}
	info["usual_bps"] = math.Round(usualBPS)
	info["usual_pps"] = math.Round(usualPPS)
	info["p50_bps"] = math.Round(float64(h.BytesP50) * 8 / 60)
	info["ratio"] = math.Round(ratio*100) / 100
	return ratio >= rule.BaselineFactor, info
}

// baselineProbe is the hot-table column's textual form of an address.
func baselineProbe(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return "::ffff:" + v4.String()
	}
	return ip.String()
}

// applyBaseline filters the violations of a baseline rule (see above). On a
// baseline lookup error it fails open: the absolute threshold decides, as it
// did before baseline mode existed.
func (e *Engine) applyBaseline(ctx context.Context, rule model.AlertRule, vs []store.AlertViolation) []store.AlertViolation {
	direction, ok := baselineDirection(rule.RuleType)
	if !ok || rule.BaselineFactor <= 0 || len(vs) == 0 {
		return vs
	}
	probes := make([]string, 0, len(vs))
	for _, v := range vs {
		if v.TargetIP != nil {
			probes = append(probes, baselineProbe(v.TargetIP))
		}
	}
	hist, err := e.store.ThreatBaselines(ctx, direction, e.now().Truncate(time.Minute), probes)
	if err != nil {
		log.Printf("alert engine: rule %s: baseline lookup failed, using the absolute threshold: %v", rule.Name, err)
		return vs
	}
	// Keys are display forms from ClickHouse; normalise through net.IP so an
	// IPv6 address matches whatever spelling either side printed.
	byIP := make(map[string]store.ThreatHistory, len(hist))
	for k, h := range hist {
		if ip := net.ParseIP(k); ip != nil {
			byIP[ip.String()] = h
		}
	}
	kept := make([]store.AlertViolation, 0, len(vs))
	for _, v := range vs {
		if v.TargetIP == nil {
			kept = append(kept, v)
			continue
		}
		keep, info := baselineVerdict(rule, v, byIP[v.TargetIP.String()])
		if !keep {
			metrics.AlertsBaselineSuppressed.WithLabelValues(rule.RuleType).Inc()
			continue
		}
		if v.Extra == nil {
			v.Extra = map[string]any{}
		}
		v.Extra["usual"] = info
		kept = append(kept, v)
	}
	return kept
}
