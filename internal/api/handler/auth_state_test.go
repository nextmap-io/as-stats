package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/config"
	"golang.org/x/oauth2"
)

func newTestAuthHandler(t *testing.T) (*AuthHandler, *middleware.SessionStore) {
	t.Helper()
	sessions := middleware.NewSessionStore()
	h := NewAuthHandler(&config.APIConfig{}, sessions)
	h.oauth2 = &oauth2.Config{
		ClientID:    "client",
		RedirectURL: "https://as-stats.example/auth/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://idp.example/authorize",
			TokenURL: "https://idp.example/token",
		},
	}
	return h, sessions
}

func startLogin(t *testing.T, h *AuthHandler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("no state in %q", loc)
	}
	return state
}

// A pending login's state is not a session.
func TestLoginStateIsNotASession(t *testing.T) {
	h, sessions := newTestAuthHandler(t)
	state := startLogin(t, h)

	protected := middleware.AuthMiddleware(&config.APIConfig{}, sessions, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	for _, id := range []string{state} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req.AddCookie(&http.Cookie{Name: "as_stats_session", Value: id})
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("cookie %q: status %d, want 401", id, rec.Code)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req.Header.Set("Authorization", "Bearer "+id)
		rec = httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("bearer %q: status %d, want 401", id, rec.Code)
		}
	}

	// The state still completes its own login exactly once.
	if _, ok := h.logins.take(state, time.Now()); !ok {
		t.Fatal("login state lost")
	}
}

// Defence in depth: a session without a granted role never authenticates.
func TestSessionWithoutRoleRejected(t *testing.T) {
	sessions := middleware.NewSessionStore()
	sessions.Set("roleless", middleware.UserInfo{Sub: "x"}, time.Minute)
	sessions.Set("viewer", middleware.UserInfo{Sub: "y", Role: "viewer"}, time.Minute)
	protected := middleware.AuthMiddleware(&config.APIConfig{}, sessions, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	for id, want := range map[string]int{"roleless": http.StatusUnauthorized, "viewer": http.StatusOK} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req.AddCookie(&http.Cookie{Name: "as_stats_session", Value: id})
		rec := httptest.NewRecorder()
		protected.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("session %q: status %d, want %d", id, rec.Code, want)
		}
	}
}

func TestLoginStatesSingleUseAndExpiry(t *testing.T) {
	l := newLoginStates()
	now := time.Now()
	if !l.put("s1", "v1", now) {
		t.Fatal("put refused")
	}
	if v, ok := l.take("s1", now); !ok || v != "v1" {
		t.Fatalf("take = %q, %v", v, ok)
	}
	if _, ok := l.take("s1", now); ok {
		t.Fatal("state accepted twice")
	}
	l.put("s2", "v2", now)
	if _, ok := l.take("s2", now.Add(loginStateTTL+time.Second)); ok {
		t.Fatal("expired state accepted")
	}
}

func TestLoginStatesBounded(t *testing.T) {
	l := newLoginStates()
	now := time.Now()
	for i := 0; i < maxPendingLogins; i++ {
		l.put(fmt.Sprint("s", i), "v", now)
	}
	if l.put("extra", "v", now) {
		t.Fatal("put accepted beyond maxPendingLogins")
	}
	// Once the pending logins expire, new ones are accepted again.
	if !l.put("extra", "v", now.Add(loginStateTTL+time.Second)) {
		t.Fatal("put refused after expiry")
	}
}
