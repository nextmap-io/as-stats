package middleware

import (
	"net/http"
	"testing"
)

// TestRealIPIgnoresSpoofedHeadersFromUntrustedPeer: the rate-limit bucket key
// must not be attacker-controlled. nginx appends to X-Forwarded-For, so the
// left-most entry is whatever the client sent.
func TestRealIPIgnoresSpoofedHeadersFromUntrustedPeer(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		want       string
	}{
		{
			name:       "direct client cannot pick its bucket",
			remoteAddr: "203.0.113.9:5555",
			xff:        "1.2.3.4",
			want:       "203.0.113.9",
		},
		{
			name:       "direct client cannot use X-Real-IP either",
			remoteAddr: "203.0.113.9:5555",
			xri:        "1.2.3.4",
			want:       "203.0.113.9",
		},
		{
			name:       "behind trusted proxy: right-most entry wins",
			remoteAddr: "127.0.0.1:5555",
			xff:        "1.2.3.4, 198.51.100.7",
			want:       "198.51.100.7",
		},
		{
			name:       "no headers at all",
			remoteAddr: "198.51.100.20:443",
			want:       "198.51.100.20",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				r.Header.Set("X-Real-IP", tc.xri)
			}
			if got := realIP(r); got != tc.want {
				t.Errorf("realIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
