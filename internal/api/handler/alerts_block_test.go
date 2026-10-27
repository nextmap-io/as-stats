package handler

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/model"
)

func TestIsHostTarget(t *testing.T) {
	cases := []struct {
		ip       string
		ruleType string
		want     bool
	}{
		{"192.0.2.10", "volume_in", true},
		{"2001:db8::10", "syn_flood", true},
		{"192.0.2.10", "", true}, // rule deleted since: the address still decides
		{"::", "disk_usage", false},
		{"::", "", false},
		{"0.0.0.0", "volume_in", false},
		{"100::1234:5678:9abc:def0", "", false}, // synthetic link-label id (0100::/64)
		{"100::1", "link_capacity", false},
		{"2001:db8::1", "anomaly", false},
		{"198.51.100.0", "subnet_flood", false},
		{"", "volume_in", false},
	}
	for _, c := range cases {
		if got := isHostTarget(net.ParseIP(c.ip), c.ruleType); got != c.want {
			t.Errorf("isHostTarget(%q, %q) = %v, want %v", c.ip, c.ruleType, got, c.want)
		}
	}
}

func TestBlockAlertBGPConflictWhenBGPDisabled(t *testing.T) {
	for name, h := range map[string]*Handler{
		"feature off": {FeatureAlerts: true, FeatureBGP: false, BGP: bgp.NewService(bgp.Options{})},
		"nil service": {FeatureAlerts: true, FeatureBGP: true},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/x/block", strings.NewReader("{}"))
		h.BlockAlertBGP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", name, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "BGP blackholing is not enabled") {
			t.Errorf("%s: body = %s", name, rec.Body.String())
		}
	}
}

func TestValidateRuleRejectsCustom(t *testing.T) {
	base := model.AlertRule{Name: "r", WindowSeconds: 60, Severity: "warning"}

	custom := base
	custom.RuleType = "custom"
	err := validateRule(custom)
	if err == nil || ruleErrStatus(err) != http.StatusUnprocessableEntity {
		t.Fatalf("custom: err=%v status=%d, want 422", err, ruleErrStatus(err))
	}

	unknown := base
	unknown.RuleType = "nope"
	if err := validateRule(unknown); err == nil || ruleErrStatus(err) != http.StatusBadRequest {
		t.Fatalf("unknown type: err=%v, want 400", err)
	}

	ok := base
	ok.RuleType = "volume_in"
	if err := validateRule(ok); err != nil {
		t.Fatalf("volume_in rejected: %v", err)
	}
}

func TestValidateRuleBaselineFactor(t *testing.T) {
	base := model.AlertRule{Name: "r", WindowSeconds: 60, Severity: "warning"}
	cases := []struct {
		ruleType string
		factor   float64
		ok       bool
	}{
		{"volume_in", 0, true},
		{"volume_in", 2, true},
		{"volume_out", 100, true},
		{"volume_out", 0.5, false},
		{"volume_in", 101, false},
		{"volume_in", -1, false},
		{"port_scan", 2, false},
		{"syn_flood", 3, false},
	}
	for _, c := range cases {
		r := base
		r.RuleType, r.BaselineFactor = c.ruleType, c.factor
		err := validateRule(r)
		if (err == nil) != c.ok {
			t.Errorf("%s factor %v: err=%v, want ok=%v", c.ruleType, c.factor, err, c.ok)
		}
		if err != nil && ruleErrStatus(err) != http.StatusBadRequest {
			t.Errorf("%s factor %v: status %d, want 400", c.ruleType, c.factor, ruleErrStatus(err))
		}
	}
}
