package handler

import (
	"cmp"
	"log"
	"net/http"
	"slices"
	"strconv"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

const (
	threatDefaultWindow = 300
	threatMinWindow     = 60
	threatMaxWindow     = 3600
	threatDefaultLimit  = 25
	threatMaxLimit      = 100
)

// LiveThreats handles GET /api/v1/threats/live
//
//	?direction=in|out  (default in: local destinations; out: local sources)
//	&window=60..3600   (seconds, default 300, whole complete minutes)
//	&limit=1..100      (default 25)
//	&all=0|1           (default 0: only rows whose level is not "normal")
//
// Each local host seen in the window is compared with its own history (same
// time of day, previous 7 days), matched against attack signatures and the
// active alert rules, and given a level: a busy host at its usual level is
// "normal" and hidden unless all=1. See CLAUDE.md "Live Threats".
//
// Gated by FEATURE_ALERTS: the hot tables and the rules exist only then.
func (h *Handler) LiveThreats(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}
	q := r.URL.Query()

	direction := q.Get("direction")
	switch direction {
	case "":
		direction = "in"
	case "in", "out":
	default:
		writeError(w, http.StatusBadRequest, "direction must be in or out")
		return
	}

	window := uint32(threatDefaultWindow)
	if v := q.Get("window"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid window")
			return
		}
		window = uint32(min(max(n, threatMinWindow), threatMaxWindow))
	}
	window -= window % 60 // complete minutes, as the store reads them

	limit := threatDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = min(n, threatMaxLimit)
	}
	all := q.Get("all") == "1" || q.Get("all") == "true"

	ctx := r.Context()
	anchor, cands, err := h.Store.ThreatCandidates(ctx, direction, window, h.LocalPrefixes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	result := model.LiveThreats{
		Direction:     direction,
		WindowSeconds: window,
		Checked:       len(cands),
		Rows:          []model.ThreatRow{},
	}
	if len(cands) == 0 {
		writeJSON(w, http.StatusOK, Response{Data: result})
		return
	}

	probes := make([]string, len(cands))
	for i, c := range cands {
		probes[i] = c.Probe
	}
	history, err := h.Store.ThreatBaselines(ctx, direction, anchor, probes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The usual peer count only matters for many_sources (inbound, ≥ 1000
	// peers, with history); reading it is the heavy part of the history,
	// so only those hosts get it.
	var peerProbes []string
	var peerCands []store.ThreatCandidate
	if direction == "in" {
		for _, c := range cands {
			if _, ok := history[c.IP]; ok && c.Peers >= sigManySourcesMin {
				peerCands = append(peerCands, c)
			}
		}
		slices.SortFunc(peerCands, func(a, b store.ThreatCandidate) int { return cmp.Compare(b.Peers, a.Peers) })
		for _, c := range peerCands[:min(len(peerCands), store.ThreatPortLookupMax)] {
			peerProbes = append(peerProbes, c.Probe)
		}
	}
	usualPeers, err := h.Store.ThreatPeerBaselines(ctx, direction, anchor, peerProbes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rules, err := h.Store.ListAlertRules(ctx)
	if err != nil {
		// Non-fatal: classify on baselines and signatures alone.
		log.Printf("live threats: could not load rules: %v", err)
		rules = nil
	}

	// Pass 1: classify on the hot-table numbers.
	inputs := make([]threatInput, len(cands))
	verdicts := make([]threatVerdict, len(cands))
	for i, c := range cands {
		inputs[i] = threatInputFor(direction, window, c, history[c.IP], usualPeers[c.IP])
		verdicts[i] = classifyThreat(inputs[i], rules)
	}

	// Pass 2: the flows_raw port breakdown, for at most
	// store.ThreatPortLookupMax hosts that already look abnormal (most
	// abnormal first), then reclassify those.
	var lookup []int
	for i := range cands {
		if needsPortLookup(inputs[i], verdicts[i]) {
			lookup = append(lookup, i)
		}
	}
	if len(lookup) > 0 {
		slices.SortStableFunc(lookup, func(a, b int) int {
			if d := threatLevelRank(verdicts[a].Level) - threatLevelRank(verdicts[b].Level); d != 0 {
				return d
			}
			switch {
			case verdicts[a].Score > verdicts[b].Score:
				return -1
			case verdicts[a].Score < verdicts[b].Score:
				return 1
			}
			return 0
		})
		if len(lookup) > store.ThreatPortLookupMax {
			lookup = lookup[:store.ThreatPortLookupMax]
		}
		lp := make([]string, len(lookup))
		for j, i := range lookup {
			lp[j] = cands[i].Probe
		}
		ports, err := h.Store.ThreatPorts(ctx, direction, anchor, window, lp)
		if err != nil {
			// Non-fatal: port-based signatures are simply absent.
			log.Printf("live threats: port lookup: %v", err)
		} else {
			for _, i := range lookup {
				if l, ok := ports[cands[i].IP]; ok {
					inputs[i].Ports = &l
					verdicts[i] = classifyThreat(inputs[i], rules)
				}
			}
		}
	}

	rows := make([]model.ThreatRow, 0, len(cands))
	byBytes := make(map[string]uint64, len(cands))
	for i, c := range cands {
		v := verdicts[i]
		if v.Level == model.ThreatLevelNormal {
			result.Normal++
			if !all {
				continue
			}
		}
		byBytes[c.IP] = c.Bytes
		rows = append(rows, model.ThreatRow{
			IP:          c.IP,
			Level:       v.Level,
			Score:       v.Score,
			Now:         inputs[i].Now,
			Baseline:    inputs[i].Baseline,
			Ratio:       v.Ratio,
			Signatures:  v.Signatures,
			ClosestRule: v.ClosestRule,
			RulePct:     v.RulePct,
		})
	}
	if all {
		// The candidates arrive ordered by bytes; keep that order.
		slices.SortStableFunc(rows, func(a, b model.ThreatRow) int {
			switch {
			case byBytes[a.IP] > byBytes[b.IP]:
				return -1
			case byBytes[a.IP] < byBytes[b.IP]:
				return 1
			}
			return 0
		})
	} else {
		sortThreatRows(rows)
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}

	if len(rows) > 0 {
		probeOf := make(map[string]string, len(cands))
		for _, c := range cands {
			probeOf[c.IP] = c.Probe
		}
		sp := make([]string, len(rows))
		for i, row := range rows {
			sp[i] = probeOf[row.IP]
		}
		series, err := h.Store.ThreatSeries(ctx, direction, anchor, sp)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range rows {
			rows[i].Series = series[rows[i].IP]
			if rows[i].Series == nil {
				rows[i].Series = []model.ThreatPoint{}
			}
			rows[i].Since = threatSince(rows[i].Series, rows[i].Baseline)
		}
	}
	result.Rows = rows
	writeJSON(w, http.StatusOK, Response{Data: result})
}

// threatInputFor converts a candidate's window totals and its per-minute
// history into rates.
func threatInputFor(direction string, window uint32, c store.ThreatCandidate, hist store.ThreatHistory, usualPeers uint64) threatInput {
	w := float64(window)
	now := model.ThreatNow{
		BPS:         uint64(float64(c.Bytes) * 8 / w),
		PPS:         uint64(float64(c.Packets) / w),
		FlowsPerSec: round(float64(c.Flows)/w, 1),
		TCPPPS:      uint64(float64(c.TCPPackets) / w),
		UDPPPS:      uint64(float64(c.UDPPackets) / w),
		ICMPPPS:     uint64(float64(c.ICMPPackets) / w),
		SynPPS:      uint64(float64(c.SynPackets) / w),
		Peers:       c.Peers,
		PeerPorts:   c.PeerPorts,
	}
	if c.Packets > 0 {
		now.AvgPacketBytes = c.Bytes / c.Packets
	}
	return threatInput{
		Direction:      direction,
		WindowSeconds:  window,
		Now:            now,
		UDPBytes:       c.UDPBytes,
		Bytes:          c.Bytes,
		MaxMinutePeers: c.MaxMinutePeers,
		Baseline: model.ThreatBaseline{
			Samples: hist.Samples,
			BPSP50:  hist.BytesP50 * 8 / 60,
			BPSP95:  hist.BytesP95 * 8 / 60,
			PPSP50:  hist.PacketsP50 / 60,
			PPSP95:  hist.PacketsP95 / 60,
		},
		PeersP50: usualPeers,
	}
}
