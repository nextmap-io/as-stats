package bgp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("http://api:8080", "short"); err == nil {
		t.Error("short token accepted")
	}
	if _, err := NewClient("api:8080", testToken); err == nil {
		t.Error("URL without scheme accepted")
	}
	if _, err := NewClient("http://api:8080/", testToken); err != nil {
		t.Error(err)
	}
}

func TestClientAutoBlock(t *testing.T) {
	status := http.StatusCreated
	var got AutoBlockRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != InternalBlockPath || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"rail"}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL+"/", testToken)
	if err != nil {
		t.Fatal(err)
	}
	req := AutoBlockRequest{IP: "198.51.100.10", AlertID: "a", RuleName: "r", TopSources: []string{"203.0.113.1"}}
	created, err := c.AutoBlock(context.Background(), req)
	if err != nil || !created {
		t.Fatalf("201: %v %v", created, err)
	}
	if auth != "Bearer "+testToken {
		t.Errorf("Authorization = %q", auth)
	}
	if got.IP != req.IP || got.RuleName != "r" || len(got.TopSources) != 1 {
		t.Errorf("body = %+v", got)
	}

	status = http.StatusOK
	if created, err := c.AutoBlock(context.Background(), req); err != nil || created {
		t.Errorf("200: %v %v", created, err)
	}
	for _, s := range []int{http.StatusTooManyRequests, http.StatusUnprocessableEntity, http.StatusUnauthorized, http.StatusConflict} {
		status = s
		if _, err := c.AutoBlock(context.Background(), req); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "rail") {
			t.Errorf("%d: err = %v, want ErrRefused", s, err)
		}
	}
	status = http.StatusBadGateway
	if _, err := c.AutoBlock(context.Background(), req); err == nil || errors.Is(err, ErrRefused) {
		t.Errorf("502: err = %v", err)
	}
}

// TestClientDoesNotFollowRedirects: the bearer token must never be replayed
// to wherever a redirect points.
func TestClientDoesNotFollowRedirects(t *testing.T) {
	var leaked bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+InternalBlockPath, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL, testToken)
	if _, err := c.AutoBlock(context.Background(), AutoBlockRequest{IP: "198.51.100.10"}); err == nil {
		t.Error("a redirect was treated as success")
	}
	if leaked {
		t.Error("token sent to the redirect target")
	}
}
