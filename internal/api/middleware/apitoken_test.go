package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

func TestIsTokenUsable(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	neverExpires := time.Unix(0, 0).UTC()

	cases := []struct {
		name string
		tok  model.APIToken
		want bool
	}{
		{
			name: "active, never expires",
			tok:  model.APIToken{Revoked: false, ExpiresAt: neverExpires},
			want: true,
		},
		{
			name: "active, expires in the future",
			tok:  model.APIToken{Revoked: false, ExpiresAt: now.Add(time.Hour)},
			want: true,
		},
		{
			name: "active but expired",
			tok:  model.APIToken{Revoked: false, ExpiresAt: now.Add(-time.Hour)},
			want: false,
		},
		{
			name: "revoked, never expires",
			tok:  model.APIToken{Revoked: true, ExpiresAt: neverExpires},
			want: false,
		},
		{
			name: "revoked and still within validity",
			tok:  model.APIToken{Revoked: true, ExpiresAt: now.Add(time.Hour)},
			want: false,
		},
		{
			name: "expires exactly now (not after) is not usable",
			tok:  model.APIToken{Revoked: false, ExpiresAt: now},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTokenUsable(tc.tok, now); got != tc.want {
				t.Errorf("IsTokenUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsReadOnlyMethod(t *testing.T) {
	readOnly := []string{http.MethodGet, http.MethodHead}
	for _, m := range readOnly {
		if !isReadOnlyMethod(m) {
			t.Errorf("isReadOnlyMethod(%q) = false, want true", m)
		}
	}
	writeMethods := []string{
		http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodOptions, http.MethodConnect,
	}
	for _, m := range writeMethods {
		if isReadOnlyMethod(m) {
			t.Errorf("isReadOnlyMethod(%q) = true, want false", m)
		}
	}
}

func TestBearerToken(t *testing.T) {
	if tok, ok := bearerToken("Bearer ast_abc"); !ok || tok != "ast_abc" {
		t.Errorf("bearerToken(valid) = %q,%v", tok, ok)
	}
	if _, ok := bearerToken("Basic xyz"); ok {
		t.Errorf("bearerToken(Basic) should be false")
	}
	if _, ok := bearerToken(""); ok {
		t.Errorf("bearerToken(empty) should be false")
	}
}
