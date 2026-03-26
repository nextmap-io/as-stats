package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
	"github.com/nextmap-io/as-stats/internal/config"
	"golang.org/x/oauth2"
)

// AuthHandler handles OIDC authentication endpoints.
type AuthHandler struct {
	cfg      *config.APIConfig
	sessions *middleware.SessionStore
	provider *oidc.Provider
	oauth2   *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewAuthHandler creates a new AuthHandler.
// If OIDC is enabled, it discovers the provider's configuration.
func NewAuthHandler(cfg *config.APIConfig, sessions *middleware.SessionStore) *AuthHandler {
	h := &AuthHandler{cfg: cfg, sessions: sessions}

	if cfg.AuthEnabled && cfg.OIDCIssuer != "" {
		provider, err := oidc.NewProvider(context.Background(), cfg.OIDCIssuer)
		if err != nil {
			log.Printf("WARNING: failed to discover OIDC provider at %s: %v", cfg.OIDCIssuer, err)
			return h
		}

		h.provider = provider
		h.verifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})
		h.oauth2 = &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCSecret,
			RedirectURL:  cfg.OIDCRedirect,
			Endpoint:     provider.Endpoint(),
			Scopes:       cfg.OIDCScopes,
		}

		log.Printf("OIDC provider configured: %s", cfg.OIDCIssuer)
	}

	return h
}

// Login redirects to the OIDC provider's authorization endpoint.
// Uses Authorization Code Flow with PKCE.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.oauth2 == nil {
		writeError(w, http.StatusServiceUnavailable, "OIDC not configured")
		return
	}

	// Generate state (CSRF protection for OAuth flow)
	state, err := randomString(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	// Generate PKCE code verifier + challenge
	codeVerifier, err := randomString(64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate code verifier")
		return
	}
	codeChallenge := computeS256Challenge(codeVerifier)

	// Store state + verifier in session (5 min TTL)
	h.sessions.Set("oidc_state:"+state, middleware.UserInfo{Sub: codeVerifier}, 5*time.Minute)

	// Build authorization URL with PKCE
	authURL := h.oauth2.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles the OIDC callback after user authentication.
func (h *AuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if h.oauth2 == nil {
		writeError(w, http.StatusServiceUnavailable, "OIDC not configured")
		return
	}

	// Check for error from provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		log.Printf("OIDC error: %s - %s", errParam, desc)
		writeError(w, http.StatusBadRequest, "authentication failed")
		return
	}

	// Validate state parameter (CSRF protection)
	state := r.URL.Query().Get("state")
	if state == "" {
		writeError(w, http.StatusBadRequest, "missing state parameter")
		return
	}
	stateSession, ok := h.sessions.Get("oidc_state:" + state)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid or expired state")
		return
	}
	codeVerifier := stateSession.Sub
	h.sessions.Delete("oidc_state:" + state)

	// Exchange authorization code for tokens (with PKCE verifier)
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	token, err := h.oauth2.Exchange(r.Context(), code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		log.Printf("OIDC token exchange error: %v", err)
		writeError(w, http.StatusBadRequest, "token exchange failed")
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		writeError(w, http.StatusBadRequest, "missing ID token")
		return
	}

	idToken, err := h.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Printf("OIDC ID token verification error: %v", err)
		writeError(w, http.StatusBadRequest, "invalid ID token")
		return
	}

	// Extract claims
	var claims struct {
		Sub    string   `json:"sub"`
		Name   string   `json:"name"`
		Email  string   `json:"email"`
		Groups []string `json:"groups"`
		Roles  []string `json:"roles"`
	}
	if err := idToken.Claims(&claims); err != nil {
		log.Printf("OIDC claims extraction error: %v", err)
		writeError(w, http.StatusBadRequest, "failed to extract claims")
		return
	}

	// Map roles from claims
	role := "viewer"
	for _, g := range append(claims.Groups, claims.Roles...) {
		if g == "admin" || g == "admins" {
			role = "admin"
			break
		}
	}

	user := middleware.UserInfo{
		Sub:    claims.Sub,
		Name:   claims.Name,
		Email:  claims.Email,
		Groups: claims.Groups,
		Role:   role,
	}

	h.createSession(w, user)

	// Redirect to frontend
	http.Redirect(w, r, "/", http.StatusFound)
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
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
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
		SameSite: http.SameSiteStrictMode,
	})
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func computeS256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// extractRoles maps OIDC group/role claims to application roles.
func extractRoles(groups []string) string {
	for _, g := range groups {
		lower := strings.ToLower(g)
		if lower == "admin" || lower == "admins" || lower == "as-stats-admin" {
			return "admin"
		}
	}
	return "viewer"
}
