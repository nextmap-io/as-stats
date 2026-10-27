package alerts

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// Baseline factors of the seeded volume rules (validated by the replay test,
// alert_replay_integration_test.go). The 2 Gb/s critical rule stays
// absolute: a flood that large must fire whatever the host's history.
const (
	factorHighVolume = 2.0 // "High inbound volume", "High outbound volume"
	factorExfil      = 3.0 // "Sustained outbound exfiltration" (info)
)

// Descriptions of the rules this release changes (seeded and reconciled).
const (
	descHighInbound  = "Warning when a single IP receives > 500 Mbps for 60s AND at least 2x its usual level (same time of day, last 7 days). A host without that history is judged on the 500 Mbps alone."
	descHighOutbound = "An internal host is sending > 500 Mbps outbound AND at least 2x its usual level (DDoS source / data dump). A host without that history is judged on the 500 Mbps alone."
	descExfil        = "An internal host has been sending > 50 Mbps outbound for 5 minutes AND at least 3x its usual level (slow exfil). A host without that history is judged on the 50 Mbps alone."
	descPortScan     = "An internal host sent connection probes (TCP SYN without ACK; UDP or flagless TCP to ports < 1024) to > 1000 distinct destination ports in 60s (likely compromised)"
)

// seededUpgrade is one rule as 3.6 seeded it (every field the upgrade
// compares) and what this release changes on it.
type seededUpgrade struct {
	was         model.AlertRule
	factor      float64
	description string
}

// seededUpgrades: the 3.6.x seeds, verbatim (EnsureDefaultRules of 3.6.1).
var seededUpgrades = []seededUpgrade{
	{
		was: model.AlertRule{Name: "High inbound volume", RuleType: "volume_in", ThresholdBps: 500_000_000,
			WindowSeconds: 60, Severity: "warning",
			Description: "Warning when a single IP receives > 500 Mbps of inbound traffic for 60s"},
		factor: factorHighVolume, description: descHighInbound,
	},
	{
		was: model.AlertRule{Name: "High outbound volume (compromised host)", RuleType: "volume_out", ThresholdBps: 500_000_000,
			WindowSeconds: 60, Severity: "warning",
			Description: "An internal host is sending > 500 Mbps outbound (DDoS source / data dump)"},
		factor: factorHighVolume, description: descHighOutbound,
	},
	{
		was: model.AlertRule{Name: "Sustained outbound exfiltration", RuleType: "volume_out", ThresholdBps: 50_000_000,
			WindowSeconds: 300, Severity: "info",
			Description: "An internal host has been sending > 50 Mbps outbound for 5 minutes (slow exfil)"},
		factor: factorExfil, description: descExfil,
	},
	{
		// The port_scan metric changes for every port_scan rule (it is the
		// rule type's definition); only the seeded text is updated here.
		was: model.AlertRule{Name: "Port scan from internal host", RuleType: "port_scan", ThresholdCount: 1_000,
			WindowSeconds: 60, Severity: "warning",
			Description: "An internal host hit > 1000 unique destination ports in 60s (likely compromised)"},
		description: descPortScan,
	},
}

// stillSeeded reports whether r is exactly as 3.6 seeded it in every field
// an upgrade could conflict with: name, type, thresholds, window, severity,
// description, scope, and no baseline factor yet. Enabled, cooldown, action
// and webhooks are not compared — the upgrade keeps them as they are.
func stillSeeded(r, was model.AlertRule) bool {
	return r.Name == was.Name && r.RuleType == was.RuleType &&
		r.ThresholdBps == was.ThresholdBps && r.ThresholdPps == was.ThresholdPps &&
		r.ThresholdCount == was.ThresholdCount && r.WindowSeconds == was.WindowSeconds &&
		r.Severity == was.Severity && r.Description == was.Description &&
		r.TargetFilter == "" && r.HostgroupID == "" && r.BaselineFactor == 0
}

// ReconcileSeededRules upgrades the 3.6 default rules that are still exactly
// as seeded: the 500 Mb/s volume rules get baseline_factor 2, the 50 Mb/s
// exfiltration rule 3, and the four get this release's description.
// EnsureDefaultRules only seeds an empty table, so without this an existing
// install would keep the rules that produced the false positives.
//
// It runs at every collector start and is idempotent: an upgraded rule has a
// new description, so it never matches again, and a rule an operator edited
// (threshold, window, severity, description, scope or factor) is never
// touched. Returns the number of rules upgraded.
func ReconcileSeededRules(ctx context.Context, s interface {
	ListAlertRules(ctx context.Context) ([]model.AlertRule, error)
	UpsertAlertRule(ctx context.Context, r model.AlertRule) error
}) (int, error) {
	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		return 0, fmt.Errorf("list rules: %w", err)
	}
	n := 0
	for _, r := range rules {
		for _, up := range seededUpgrades {
			if !stillSeeded(r, up.was) {
				continue
			}
			r.BaselineFactor = up.factor
			r.Description = up.description
			// ReplacingMergeTree(updated_at): the new version must sort last.
			r.UpdatedAt = time.Now().UTC().Add(time.Second)
			if err := s.UpsertAlertRule(ctx, r); err != nil {
				return n, fmt.Errorf("upgrade rule %q: %w", r.Name, err)
			}
			log.Printf("alert engine: upgraded seeded rule %q (baseline_factor=%g)", r.Name, up.factor)
			n++
			break
		}
	}
	return n, nil
}
