package config

import (
	"strings"
	"testing"
	"time"
)

func setBGPEnv(t *testing.T) {
	t.Helper()
	setAPIAuthTestEnv(t)
	for k, v := range map[string]string{
		"BGP_ENABLED":      "true",
		"BGP_PEER_ADDRESS": "192.0.2.2",
		"BGP_NEXT_HOP":     "192.0.2.254",
		"BGP_LOCAL_AS":     "64501",
		"BGP_PEER_AS":      "64500",
		"BGP_API_TOKEN":    "",
	} {
		t.Setenv(k, v)
	}
}

// TestLoadAPIBGPDefaults: a 3.6 BGP configuration (no new variable) still
// loads, with the safety rails at their defaults.
func TestLoadAPIBGPDefaults(t *testing.T) {
	setBGPEnv(t)
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.BGP
	if s.DryRun || s.MinLenV4 != 32 || s.MinLenV6 != 128 || s.Limits.MaxActive != 50 || s.Limits.AutoPerHour != 10 ||
		s.Limits.DefaultDuration != time.Hour || s.Limits.MaxDuration != 24*time.Hour {
		t.Errorf("defaults = %+v", s)
	}
	if got := strings.Join(s.Command.Communities, ","); got != "65535:666,65535:65281" {
		t.Errorf("communities = %s (NO_EXPORT by default)", got)
	}
}

func TestLoadAPIBGPOverrides(t *testing.T) {
	setBGPEnv(t)
	t.Setenv("BGP_EXTRA_COMMUNITIES", "")
	t.Setenv("BGP_AUTO_BLOCK_MAX_PER_HOUR", "0")
	t.Setenv("BGP_MIN_PREFIX_LEN_V4", "28")
	t.Setenv("BGP_DRY_RUN", "true")
	t.Setenv("BGP_MAX_DURATION", "6h")
	t.Setenv("BGP_ALLOWED_PREFIXES", "198.51.100.0/24")
	cfg, err := LoadAPI()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.BGP
	if !s.DryRun || s.MinLenV4 != 28 || s.Limits.AutoPerHour != 0 || s.Limits.MaxDuration != 6*time.Hour || len(s.Allowed) != 1 {
		t.Errorf("overrides = %+v", s)
	}
	if got := strings.Join(s.Command.Communities, ","); got != "65535:666" {
		t.Errorf("BGP_EXTRA_COMMUNITIES= must drop NO_EXPORT: %s", got)
	}
}

func TestLoadAPIBGPRejects(t *testing.T) {
	for key, val := range map[string]string{
		"BGP_DRY_RUN":           "yes please",
		"BGP_MAX_ACTIVE_BLOCKS": "many",
		"BGP_MAX_DURATION":      "a day",
		"BGP_API_TOKEN":         "too-short",
		"BGP_ANNOUNCE_CMD":      "gobgp global rib add {prefix}; echo",
		"BGP_LOCAL_AS":          "AS64501",
	} {
		t.Run(key, func(t *testing.T) {
			setBGPEnv(t)
			t.Setenv(key, val)
			if _, err := LoadAPI(); err == nil {
				t.Errorf("%s=%q accepted", key, val)
			}
		})
	}
}
