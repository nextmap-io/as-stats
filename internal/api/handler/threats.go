package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/nextmap-io/as-stats/internal/model"
)

// LiveThreats handles GET /api/v1/threats/live?window=300&limit=50
//
// Returns a snapshot of the top destinations from traffic_by_dst_1min over
// the requested window, annotated with the closest matching alert rule and
// a status. This is the data that powers the "Live Threats" page — a
// pre-trigger view that shows what's brewing before any alert fires.
//
// Response shape: { "data": [LiveThreat, ...] }
//
// Gated by FEATURE_ALERTS because the data source (traffic_by_dst_1min) and
// alert rules only exist when the alert engine is enabled.
func (h *Handler) LiveThreats(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureAlerts {
		writeError(w, http.StatusNotFound, "alerts feature disabled")
		return
	}

	// window in seconds, default 5 min, store clamps to [60, 3600]
	window := uint32(300)
	if v := r.URL.Query().Get("window"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil && n > 0 {
			window = uint32(n)
		}
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	threats, err := h.Store.LiveThreats(r.Context(), window, limit, h.LocalPrefixes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Annotate each row with the closest matching active rule. We do this in
	// the handler (not the store) so that the live page reflects rule changes
	// immediately without having to push them down into a SQL JOIN.
	rules, err := h.Store.ListAlertRules(r.Context())
	if err != nil {
		// Non-fatal — return raw threats without rule annotation.
		log.Printf("live threats: could not load rules for annotation: %v", err)
		writeJSON(w, http.StatusOK, Response{Data: threats})
		return
	}

	annotated := make([]model.LiveThreat, len(threats))
	for i, t := range threats {
		annotated[i] = annotateThreat(t, rules)
	}
	writeJSON(w, http.StatusOK, Response{Data: annotated})
}

// annotateThreat compares a threat row to the active alert rules and returns
// it enriched with `worst_pct` (highest percentage of any matching threshold)
// and `status` ("ok" / "warn" / "critical"). Disabled rules are ignored.
func annotateThreat(t model.LiveThreat, rules []model.AlertRule) model.LiveThreat {
	worst := 0.0
	worstName := ""

	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		var pct float64
		switch r.RuleType {
		case "volume_in":
			if r.ThresholdBps > 0 {
				pct = float64(t.BPS) / float64(r.ThresholdBps) * 100
			} else if r.ThresholdPps > 0 {
				pct = float64(t.PPS) / float64(r.ThresholdPps) * 100
			}
		case "syn_flood":
			if r.ThresholdPps > 0 {
				pct = float64(t.SynPPS) / float64(r.ThresholdPps) * 100
			}
		case "amplification":
			if r.ThresholdCount > 0 {
				pct = float64(t.UniqueSourceIPs) / float64(r.ThresholdCount) * 100
			}
		case "icmp_flood":
			// ICMP is a subset — we don't have per-protocol pps in LiveThreat, skip.
			continue
		case "udp_flood":
			continue
		case "subnet_flood":
			// Approximate: compare per-host bps to the subnet aggregate threshold.
			// Underestimates the real danger (the actual aggregate is higher), but
			// still useful to show that a destination is part of a /24 under strain.
			if r.ThresholdBps > 0 {
				pct = float64(t.BPS) / float64(r.ThresholdBps) * 100
			}
		default:
			// volume_out / port_scan are source-based, not relevant here.
			continue
		}
		if pct > worst {
			worst = pct
			worstName = r.Name
		}
	}

	t.WorstPercent = worst
	t.WorstRule = worstName
	switch {
	case worst >= 100:
		t.Status = "critical"
	case worst >= 50:
		t.Status = "warn"
	default:
		t.Status = "ok"
	}
	return t
}
