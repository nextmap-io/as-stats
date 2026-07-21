package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// APITokenPrefix is the fixed prefix on every minted read-only API token. The
// auth middleware uses it to distinguish an API token from a legacy session-id
// bearer without a database round-trip. Must match store.apiTokenPrefix.
const APITokenPrefix = "ast_"

// RoleViewer is the read-only role assigned to every API-token principal.
const RoleViewer = "viewer"

// tokenAuthContextKey marks a request as authenticated via an API token (as
// opposed to an OIDC browser session). CSRF is bypassed only for such requests.
const tokenAuthContextKey contextKey = "token_auth"

// APITokenStore is the subset of the store the token authenticator needs. Kept
// as an interface here so the middleware package does not import store (mirrors
// the AuditRecorder pattern).
type APITokenStore interface {
	LookupAPIToken(ctx context.Context, plaintext string) (model.APIToken, error)
	TouchAPIToken(ctx context.Context, id string, ts time.Time) error
}

// APITokenAuthenticator validates Bearer API tokens and throttles last-used
// touches. The token set is admin-managed and therefore bounded, so the
// per-token lastTouch map cannot grow unbounded from untrusted input.
type APITokenAuthenticator struct {
	store         APITokenStore
	touchInterval time.Duration

	mu        sync.Mutex
	lastTouch map[string]time.Time
}

// NewAPITokenAuthenticator constructs an authenticator over the given store,
// touching last_used_at at most once per minute per token.
func NewAPITokenAuthenticator(store APITokenStore) *APITokenAuthenticator {
	return &APITokenAuthenticator{
		store:         store,
		touchInterval: time.Minute,
		lastTouch:     make(map[string]time.Time),
	}
}

// Authenticate resolves a plaintext token to a read-only viewer principal. It
// returns (nil, false) for any invalid, expired, or revoked token. On success it
// schedules a throttled, asynchronous last-used touch that never blocks or fails
// the request.
func (a *APITokenAuthenticator) Authenticate(ctx context.Context, plaintext string) (*UserInfo, bool) {
	if a == nil || a.store == nil {
		return nil, false
	}
	rec, err := a.store.LookupAPIToken(ctx, plaintext)
	if err != nil {
		return nil, false
	}
	// Defense in depth: re-check the revoked/expiry gate in Go even though the
	// lookup query already enforces it.
	if !IsTokenUsable(rec, time.Now()) {
		return nil, false
	}
	a.scheduleTouch(rec.ID)
	return &UserInfo{
		Sub:  "token:" + rec.ID,
		Name: rec.Name,
		Role: RoleViewer,
	}, true
}

// scheduleTouch records a last-used update at most once per touchInterval per
// token, running the DB write in the background so the request is never blocked.
func (a *APITokenAuthenticator) scheduleTouch(id string) {
	now := time.Now()
	a.mu.Lock()
	if last, ok := a.lastTouch[id]; ok && now.Sub(last) < a.touchInterval {
		a.mu.Unlock()
		return
	}
	a.lastTouch[id] = now
	a.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := a.store.TouchAPIToken(ctx, id, now); err != nil {
			log.Printf("api token touch %s failed: %v", id, err)
		}
	}()
}

// IsTokenUsable reports whether a token may authenticate a request: it must not
// be revoked and must not be expired. An ExpiresAt with Unix() <= 0 means the
// token never expires. Pure function — unit-tested directly.
func IsTokenUsable(t model.APIToken, now time.Time) bool {
	if t.Revoked {
		return false
	}
	if t.ExpiresAt.Unix() <= 0 {
		return true // never expires
	}
	return t.ExpiresAt.After(now)
}

// isReadOnlyMethod reports whether an HTTP method is safe for API-token access.
// Tokens are strictly read-only, so only GET and HEAD are permitted.
func isReadOnlyMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// IsTokenAuth reports whether the request was authenticated via an API token.
// Used by the CSRF middleware to bypass CSRF for token auth only.
func IsTokenAuth(ctx context.Context) bool {
	v, _ := ctx.Value(tokenAuthContextKey).(bool)
	return v
}

// hasBearerPrefix reports whether an Authorization header value is a Bearer
// credential, returning the raw token.
func bearerToken(authHeader string) (string, bool) {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(authHeader, "Bearer "), true
}
