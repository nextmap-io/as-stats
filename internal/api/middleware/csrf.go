package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const csrfTokenHeader = "X-CSRF-Token"
const csrfCookieName = "as_stats_csrf"

// CSRF returns middleware that protects state-changing requests (POST, PUT, DELETE)
// using the double-submit cookie pattern.
// - On GET requests, a CSRF token is set as a cookie.
// - On POST/PUT/DELETE, the middleware checks that the X-CSRF-Token header
//   matches the cookie value.
func CSRF() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				// Safe methods: ensure CSRF cookie exists
				if _, err := r.Cookie(csrfCookieName); err != nil {
					setCSRFCookie(w)
				}
				next.ServeHTTP(w, r)

			default:
				// State-changing methods: validate CSRF token
				cookie, err := r.Cookie(csrfCookieName)
				if err != nil {
					http.Error(w, `{"error":"missing CSRF cookie"}`, http.StatusForbidden)
					return
				}

				headerToken := r.Header.Get(csrfTokenHeader)
				if headerToken == "" || headerToken != cookie.Value {
					http.Error(w, `{"error":"invalid CSRF token"}`, http.StatusForbidden)
					return
				}

				next.ServeHTTP(w, r)
			}
		})
	}
}

func setCSRFCookie(w http.ResponseWriter) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return
	}
	token := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: false, // JS needs to read this cookie
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}
