package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/config"
)

type contextKey string

const (
	userContextKey contextKey = "user"
)

// UserInfo represents the authenticated user.
type UserInfo struct {
	Sub    string   `json:"sub"`
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	Groups []string `json:"groups,omitempty"`
	Role   string   `json:"role"` // "admin" or "viewer"
}

// session stores a user session in memory.
type session struct {
	User      UserInfo
	ExpiresAt time.Time
}

// SessionStore manages user sessions.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*session
}

// NewSessionStore creates a new in-memory session store.
func NewSessionStore() *SessionStore {
	s := &SessionStore{
		sessions: make(map[string]*session),
	}
	// Cleanup expired sessions periodically
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			s.cleanup()
		}
	}()
	return s
}

func (s *SessionStore) Set(id string, user UserInfo, ttl time.Duration) {
	s.mu.Lock()
	s.sessions[id] = &session{User: user, ExpiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
}

func (s *SessionStore) Get(id string) (*UserInfo, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok || time.Now().After(sess.ExpiresAt) {
		return nil, false
	}
	return &sess.User, true
}

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func (s *SessionStore) cleanup() {
	now := time.Now()
	s.mu.Lock()
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}

// AuthMiddleware creates an OIDC authentication middleware.
// When AUTH_ENABLED=false, this middleware is not applied at all.
func AuthMiddleware(cfg *config.APIConfig, sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check session cookie
			cookie, err := r.Cookie("as_stats_session")
			if err == nil {
				if user, ok := sessions.Get(cookie.Value); ok {
					ctx := context.WithValue(r.Context(), userContextKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Check Authorization header (Bearer token - for API clients)
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				if user, ok := sessions.Get(token); ok {
					ctx := context.WithValue(r.Context(), userContextKey, user)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Not authenticated
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
		})
	}
}

// RequireRole returns a middleware that requires the user to have the specified role.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := GetUser(r.Context())
			if user == nil || user.Role != role {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetUser extracts the authenticated user from the request context.
func GetUser(ctx context.Context) *UserInfo {
	user, _ := ctx.Value(userContextKey).(*UserInfo)
	return user
}

// GenerateSessionID creates a random session ID.
func GenerateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate secure session ID: %v", err))
	}
	return hex.EncodeToString(b)
}
