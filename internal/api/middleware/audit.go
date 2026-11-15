package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
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
				Params:    redactAuditParams(paramsBlob, r.Method != http.MethodGet && r.Method != http.MethodHead),
				ClientIP:  ClientIP(r),
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

			// Write async with a fresh context (response is already sent).
			// Failures are logged for ops visibility — losing audit entries
			// silently is unacceptable for compliance.
			go func(e model.AuditLogEntry) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := rec.WriteAuditLog(ctx, e); err != nil {
					log.Printf("AUDIT LOST: action=%s user=%s ip=%s err=%v",
						e.Action, e.UserEmail, e.ClientIP, err)
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
		"GET /api/v1/flows/search":        "flow_search",
		"POST /api/v1/alerts/":            "alert_action",
		"POST /api/v1/admin/links":        "link_create",
		"DELETE /api/v1/admin/links":      "link_delete",
		"PUT /api/v1/admin/retention":     "retention_update",
		"POST /api/v1/admin/rules":        "rule_create",
		"PUT /api/v1/admin/rules":         "rule_update",
		"DELETE /api/v1/admin/rules":      "rule_delete",
		"POST /api/v1/admin/webhooks":     "webhook_create",
		"PUT /api/v1/admin/webhooks":      "webhook_update",
		"DELETE /api/v1/admin/webhooks":   "webhook_delete",
		"POST /api/v1/admin/hostgroups":   "hostgroup_create",
		"PUT /api/v1/admin/hostgroups":    "hostgroup_update",
		"DELETE /api/v1/admin/hostgroups": "hostgroup_delete",
		"POST /api/v1/admin/reports":      "report_create",
		"PUT /api/v1/admin/reports":       "report_update",
		"DELETE /api/v1/admin/reports":    "report_delete",
		"POST /api/v1/admin/tokens":       "token_create",
		"DELETE /api/v1/admin/tokens":     "token_revoke",
	}
}

// redactedValue replaces secrets in audit params.
const redactedValue = "***"

// secretParamKeys are body fields whose value is never written to the audit
// log. Matched case-insensitively, at any depth.
var secretParamKeys = map[string]bool{
	"password": true, "pass": true, "secret": true, "token": true,
	"api_key": true, "apikey": true, "authorization": true, "smtp_pass": true,
}

// redactAuditParams strips secrets from a request body before it is stored
// in audit_log.params:
//
//   - "url" fields keep only scheme and host (a Slack / Teams / Discord
//     webhook URL is itself the credential): https://hooks.slack.com/***;
//   - every value of a "headers" field (a JSON object, or a string holding
//     one, as webhooks send it) becomes ***, the header names stay;
//   - fields named like a secret (secretParamKeys) become ***.
//
// A body that is not a JSON object (or was truncated at the 4 KiB capture
// limit) cannot be redacted field by field and is replaced as a whole. A
// query string (GET) is kept as is.
func redactAuditParams(params string, isBody bool) string {
	if !isBody || strings.TrimSpace(params) == "" {
		return params
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(params), &obj); err != nil {
		return "[body not recorded: not a JSON object]"
	}
	redactMap(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return "[body not recorded]"
	}
	return string(out)
}

func redactMap(m map[string]any) {
	for k, v := range m {
		lk := strings.ToLower(k)
		switch {
		case secretParamKeys[lk]:
			m[k] = redactedValue
		case lk == "url" || strings.HasSuffix(lk, "_url"):
			if s, ok := v.(string); ok {
				m[k] = redactURL(s)
			} else {
				m[k] = redactedValue
			}
		case lk == "headers":
			m[k] = redactHeaders(v)
		default:
			redactValue(v)
		}
	}
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		redactMap(t)
	case []any:
		for _, e := range t {
			redactValue(e)
		}
	}
}

// redactURL keeps scheme and host: enough to tell which service a webhook
// points to, never the path, query or user info that carry its secret.
func redactURL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return redactedValue
	}
	return u.Scheme + "://" + u.Hostname() + portSuffix(u) + "/" + redactedValue
}

func portSuffix(u *url.URL) string {
	if p := u.Port(); p != "" {
		return ":" + p
	}
	return ""
}

// redactHeaders keeps header names and replaces every value.
func redactHeaders(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k := range t {
			out[k] = redactedValue
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return t
		}
		var h map[string]any
		if err := json.Unmarshal([]byte(t), &h); err != nil {
			return redactedValue
		}
		b, err := json.Marshal(redactHeaders(h))
		if err != nil {
			return redactedValue
		}
		return string(b)
	case nil:
		return nil
	default:
		return redactedValue
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
