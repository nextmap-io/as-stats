package handler

import (
	"net/http"
	"time"

	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/config"
)

// AuthHandler handles OIDC authentication endpoints.
type AuthHandler struct {
	cfg      *config.APIConfig
	sessions *middleware.SessionStore
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(cfg *config.APIConfig, sessions *middleware.SessionStore) *AuthHandler {
	return &AuthHandler{cfg: cfg, sessions: sessions}
}

// Login redirects to the OIDC provider's authorization endpoint.
// In a full implementation, this would use coreos/go-oidc to discover
// the provider's endpoints and build the authorization URL with PKCE.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement full OIDC Authorization Code Flow with PKCE
	// 1. Generate state + code_verifier
	// 2. Store in session
	// 3. Redirect to provider's authorization_endpoint
	writeError(w, http.StatusNotImplemented, "OIDC login not yet configured")
}

// Callback handles the OIDC callback after user authentication.
func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement full OIDC callback
	// 1. Verify state parameter
	// 2. Exchange code for tokens
	// 3. Verify ID token
	// 4. Extract user info from claims
	// 5. Create session
	// 6. Set cookie and redirect to frontend
	writeError(w, http.StatusNotImplemented, "OIDC callback not yet configured")
}

// Logout clears the user session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("as_stats_session")
	if err == nil {
		h.sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "as_stats_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"status": "logged out"}})
}

// Me returns the current authenticated user.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: user})
}

// createSession creates a new session for the user and sets a cookie.
func (h *AuthHandler) createSession(w http.ResponseWriter, user middleware.UserInfo) {
	sessionID := middleware.GenerateSessionID()
	h.sessions.Set(sessionID, user, 24*time.Hour)

	http.SetCookie(w, &http.Cookie{
		Name:     "as_stats_session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
