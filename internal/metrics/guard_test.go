package metrics

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func TestAccessGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	cases := []struct {
		name       string
		cidrs      []string
		user, pass string
		remote     string
		auth       bool
		want       int
		enabled    bool
	}{
		{"open", nil, "", "", "203.0.113.1:1", false, http.StatusNoContent, false},
		{"cidr allows", []string{"10.0.0.0/8"}, "", "", "10.1.2.3:1", false, http.StatusNoContent, true},
		{"cidr denies", []string{"10.0.0.0/8"}, "", "", "203.0.113.1:1", false, http.StatusForbidden, true},
		{"bare ip", []string{"203.0.113.1"}, "", "", "203.0.113.1:1", false, http.StatusNoContent, true},
		{"basic missing", nil, "u", "p", "203.0.113.1:1", false, http.StatusUnauthorized, true},
		{"basic ok", nil, "u", "p", "203.0.113.1:1", true, http.StatusNoContent, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, enabled := AccessGuard(ok, tc.cidrs, tc.user, tc.pass, remoteHost)
			if enabled != tc.enabled {
				t.Errorf("enabled = %v, want %v", enabled, tc.enabled)
			}
			req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
			req.RemoteAddr = tc.remote
			if tc.auth {
				req.SetBasicAuth(tc.user, tc.pass)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
