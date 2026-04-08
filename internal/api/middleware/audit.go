package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// AuditRecorder writes audit log entries. Implemented by *store.ClickHouseStore.
type AuditRecorder interface {
	WriteAuditLog(ctx context.Context, e model.AuditLogEntry) error
}

// Audit returns a middleware that records sensitive actions to the audit_log.
//
// Only requests matching `actionMap` are logged (by URL prefix).
// This keeps the audit log focused on compliance-relevant actions rather
// than spamming every GET request.
func Audit(rec AuditRecorder, actionMap map[string]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Match action
			action := matchAction(r.URL.Path, r.Method, actionMap)
			if action == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			rw := &statusRecorder{ResponseWriter: w, status: 200}

			// For POST bodies: capture a bounded copy for audit params
			var paramsBlob string
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
				if r.Body != nil {
					b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
					paramsBlob = string(b)
					r.Body = io.NopCloser(bytes.NewReader(b))
				}
			} else {
				// For GET, capture the query string (without auth headers)
				paramsBlob = r.URL.RawQuery
			}

			start := time.Now()
			next.ServeHTTP(rw, r)

			// Build audit entry
			entry := model.AuditLogEntry{
				Timestamp: start,
				Action:    action,
				Resource:  r.URL.Path,
				Params:    paramsBlob,
				ClientIP:  realIP(r),
				UserAgent: r.UserAgent(),
			}
			if user := GetUser(r.Context()); user != nil {
				entry.UserSub = user.Sub
				entry.UserEmail = user.Email
				entry.UserRole = user.Role
			}
			if rw.status < 400 {
				entry.Result = "success"
			} else if rw.status == 401 || rw.status == 403 {
				entry.Result = "denied"
			} else {
				entry.Result = "error"
			}

			// Write async (don't block response)
			go func(e model.AuditLogEntry) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := rec.WriteAuditLog(ctx, e); err != nil {
					// Log-only failure — don't bubble up
					_ = err
				}
			}(entry)
		})
	}
}

// matchAction returns the audit action name for a given path/method,
// or empty string if the request should not be audited.
func matchAction(path, method string, m map[string]string) string {
	for prefix, action := range m {
		// prefix can be "METHOD /path" or just "/path"
		parts := strings.SplitN(prefix, " ", 2)
		var methodPrefix, pathPrefix string
		if len(parts) == 2 {
			methodPrefix, pathPrefix = parts[0], parts[1]
		} else {
			pathPrefix = parts[0]
		}
		if methodPrefix != "" && methodPrefix != method {
			continue
		}
		if strings.HasPrefix(path, pathPrefix) {
			return action
		}
	}
	return ""
}

// DefaultAuditActions returns the default set of actions to audit.
// Covers flow search, exports, alert management, link config, webhooks.
func DefaultAuditActions() map[string]string {
	return map[string]string{
		"GET /api/v1/flows/search":   "flow_search",
		"POST /api/v1/alerts/":       "alert_action",
		"POST /api/v1/admin/links":   "link_create",
		"DELETE /api/v1/admin/links": "link_delete",
		"POST /api/v1/admin/rules":   "rule_create",
		"PUT /api/v1/admin/rules":    "rule_update",
		"DELETE /api/v1/admin/rules": "rule_delete",
		"POST /api/v1/admin/webhooks": "webhook_create",
		"PUT /api/v1/admin/webhooks":  "webhook_update",
		"DELETE /api/v1/admin/webhooks": "webhook_delete",
	}
}

// statusRecorder wraps ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
