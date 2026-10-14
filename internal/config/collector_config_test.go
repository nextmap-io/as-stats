package config

import (
	"testing"
	"time"
)

func clearCollectorEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"COLLECTOR_BATCH_SIZE", "COLLECTOR_FLUSH_INTERVAL", "COLLECTOR_FLUSH_TIMEOUT",
		"COLLECTOR_SHUTDOWN_TIMEOUT", "FEATURE_REPORTS",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadCollectorDefaults(t *testing.T) {
	clearCollectorEnv(t)

	cfg, err := LoadCollector()
	if err != nil {
		t.Fatalf("LoadCollector() error = %v", err)
	}
	if cfg.BatchSize != 50000 {
		t.Errorf("BatchSize = %d, want 50000", cfg.BatchSize)
	}
	if cfg.FlushInterval != 15*time.Second {
		t.Errorf("FlushInterval = %s, want 15s", cfg.FlushInterval)
	}
	if cfg.FlushTimeout != 30*time.Second {
		t.Errorf("FlushTimeout = %s, want 30s", cfg.FlushTimeout)
	}
	if cfg.ShutdownTimeout != 40*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 40s", cfg.ShutdownTimeout)
	}
}

func TestParseSamplingOverride(t *testing.T) {
	got, err := parseSamplingOverride(" 2001:db8::1=1000 , 192.0.2.1=512")
	if err != nil {
		t.Fatalf("parseSamplingOverride() error = %v", err)
	}
	if len(got) != 2 || got["2001:db8::1"] != 1000 || got["192.0.2.1"] != 512 {
		t.Errorf("parseSamplingOverride() = %v", got)
	}

	for _, bad := range []string{"2001:db8::1", "router1=1000", "192.0.2.1=0", "192.0.2.1=-5", "192.0.2.1=x"} {
		if _, err := parseSamplingOverride(bad); err == nil {
			t.Errorf("parseSamplingOverride(%q) accepted a malformed entry", bad)
		}
	}
}

func TestLoadCollectorRejectsNonPositiveDurations(t *testing.T) {
	for _, k := range []string{"COLLECTOR_FLUSH_INTERVAL", "COLLECTOR_FLUSH_TIMEOUT", "COLLECTOR_SHUTDOWN_TIMEOUT"} {
		t.Run(k, func(t *testing.T) {
			clearCollectorEnv(t)
			t.Setenv(k, "0s")
			if _, err := LoadCollector(); err == nil {
				t.Fatalf("LoadCollector() accepted %s=0s", k)
			}
		})
	}
}
