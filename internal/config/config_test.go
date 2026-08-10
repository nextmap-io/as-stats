package config

import (
	"strings"
	"testing"
)

func setAPIAuthTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AUTH_ENABLED", "")
	t.Setenv("ALLOW_UNAUTHENTICATED", "")
	t.Setenv("OIDC_ISSUER_URL", "https://issuer.example.com")
	t.Setenv("OIDC_CLIENT_ID", "test-client")
	t.Setenv("FEATURE_REPORTS", "false")
	t.Setenv("BGP_ENABLED", "false")
}

func TestLoadAPIAuthDefaultsToEnabled(t *testing.T) {
	setAPIAuthTestEnv(t)

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if !cfg.AuthEnabled {
		t.Fatal("LoadAPI() AuthEnabled = false, want true")
	}
}

func TestLoadAPIRejectsInvalidAuthBoolean(t *testing.T) {
	setAPIAuthTestEnv(t)
	t.Setenv("AUTH_ENABLED", "definitely")

	_, err := LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "invalid AUTH_ENABLED") {
		t.Fatalf("LoadAPI() error = %v, want invalid AUTH_ENABLED", err)
	}
}

func TestLoadAPIRejectsInvalidUnauthenticatedOptOutBoolean(t *testing.T) {
	setAPIAuthTestEnv(t)
	t.Setenv("ALLOW_UNAUTHENTICATED", "definitely")

	_, err := LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "invalid ALLOW_UNAUTHENTICATED") {
		t.Fatalf("LoadAPI() error = %v, want invalid ALLOW_UNAUTHENTICATED", err)
	}
}

func TestLoadAPIRequiresExplicitUnauthenticatedOptOut(t *testing.T) {
	setAPIAuthTestEnv(t)
	t.Setenv("AUTH_ENABLED", "false")

	_, err := LoadAPI()
	if err == nil || !strings.Contains(err.Error(), "ALLOW_UNAUTHENTICATED=true") {
		t.Fatalf("LoadAPI() error = %v, want explicit unauthenticated opt-out error", err)
	}
}

func TestLoadAPIAllowsExplicitUnauthenticatedMode(t *testing.T) {
	setAPIAuthTestEnv(t)
	t.Setenv("AUTH_ENABLED", "false")
	t.Setenv("ALLOW_UNAUTHENTICATED", "true")
	t.Setenv("OIDC_ISSUER_URL", "")
	t.Setenv("OIDC_CLIENT_ID", "")

	cfg, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if cfg.AuthEnabled {
		t.Fatal("LoadAPI() AuthEnabled = true, want false")
	}
}

func TestEnvBoolUsesFallbackOnlyWhenUnset(t *testing.T) {
	t.Setenv("TEST_BOOL", "")
	got, err := envBool("TEST_BOOL", true)
	if err != nil || !got {
		t.Fatalf("envBool() = %v, %v; want true, nil", got, err)
	}

	t.Setenv("TEST_BOOL", "0")
	got, err = envBool("TEST_BOOL", true)
	if err != nil || got {
		t.Fatalf("envBool() = %v, %v; want false, nil", got, err)
	}
}
