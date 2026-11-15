package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

type auditSink struct {
	mu      sync.Mutex
	entries []model.AuditLogEntry
	done    chan struct{}
}

func (s *auditSink) WriteAuditLog(_ context.Context, e model.AuditLogEntry) error {
	s.mu.Lock()
	s.entries = append(s.entries, e)
	s.mu.Unlock()
	s.done <- struct{}{}
	return nil
}

func TestRedactAuditParamsWebhook(t *testing.T) {
	body := `{"name":"ops","webhook_type":"slack",` +
		`"url":"https://hooks.slack.com/services/T000/B000/XXXXSECRET?x=1",` +
		`"headers":"{\"Authorization\":\"Bearer s3cr3t\",\"X-Api-Key\":\"k3y\"}",` +
		`"min_severity":"warning","enabled":true}`
	got := redactAuditParams(body, true)
	for _, secret := range []string{"XXXXSECRET", "services/T000", "s3cr3t", "k3y", "x=1"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted params still contain %q: %s", secret, got)
		}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("redacted params are not JSON: %v (%s)", err, got)
	}
	if m["url"] != "https://hooks.slack.com/***" {
		t.Errorf("url = %v, want scheme + host only", m["url"])
	}
	var h map[string]string
	if err := json.Unmarshal([]byte(m["headers"].(string)), &h); err != nil {
		t.Fatalf("headers: %v", err)
	}
	if h["Authorization"] != "***" || h["X-Api-Key"] != "***" || len(h) != 2 {
		t.Errorf("headers = %v, want names kept and values redacted", h)
	}
	if m["name"] != "ops" || m["min_severity"] != "warning" || m["enabled"] != true {
		t.Errorf("non-secret fields changed: %v", m)
	}
}

func TestRedactAuditParamsCases(t *testing.T) {
	cases := []struct{ in, want string }{
		{``, ``},
		{`{"headers":{"Authorization":"x"}}`, `{"headers":{"Authorization":"***"}}`},
		{`{"headers":"not json"}`, `{"headers":"***"}`},
		{`{"url":"https://user:pw@example.net:8443/hook"}`, `{"url":"https://example.net:8443/***"}`},
		{`{"url":"not a url"}`, `{"url":"***"}`},
		{`{"smtp":{"Password":"pw"},"items":[{"token":"t"}]}`, `{"items":[{"token":"***"}],"smtp":{"Password":"***"}}`},
		{`{"name":"edge","prefixes":["192.0.2.0/24"]}`, `{"name":"edge","prefixes":["192.0.2.0/24"]}`},
		{`{"url":"https://hooks.slack.com/services/T/B/SEC`, `[body not recorded: not a JSON object]`}, // truncated capture
		{`plain text`, `[body not recorded: not a JSON object]`},
	}
	for _, c := range cases {
		if got := redactAuditParams(c.in, true); got != c.want {
			t.Errorf("redactAuditParams(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	// Query strings (GET) are kept.
	if got := redactAuditParams("src_ip=192.0.2.1&period=1h", false); got != "src_ip=192.0.2.1&period=1h" {
		t.Errorf("query string changed: %s", got)
	}
}

func TestAuditMiddlewareRedactsAndCoversHostgroups(t *testing.T) {
	sink := &auditSink{done: make(chan struct{}, 8)}
	var seen string
	h := Audit(sink, DefaultAuditActions())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The handler still receives the original body.
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		seen = string(b[:n])
		w.WriteHeader(http.StatusCreated)
	}))

	body := `{"name":"x","webhook_type":"generic","url":"https://example.net/hook/SECRETPATH"}`
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", strings.NewReader(body)))
	if seen != body {
		t.Errorf("handler body = %q, want the original", seen)
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/admin/hostgroups", strings.NewReader(`{"name":"dns"}`)),
		httptest.NewRequest(http.MethodPut, "/api/v1/admin/hostgroups/abc", strings.NewReader(`{"name":"dns2"}`)),
		httptest.NewRequest(http.MethodDelete, "/api/v1/admin/hostgroups/abc", nil),
	} {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	for i := 0; i < 4; i++ {
		select {
		case <-sink.done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d audit entries written, want 4", i)
		}
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	actions := map[string]model.AuditLogEntry{}
	for _, e := range sink.entries {
		actions[e.Action] = e
	}
	if e := actions["webhook_create"]; strings.Contains(e.Params, "SECRETPATH") || !strings.Contains(e.Params, "https://example.net/***") {
		t.Errorf("webhook_create params = %s", e.Params)
	}
	for _, a := range []string{"hostgroup_create", "hostgroup_update", "hostgroup_delete"} {
		if _, ok := actions[a]; !ok {
			t.Errorf("no %s audit entry (got %v)", a, actions)
		}
	}
}
