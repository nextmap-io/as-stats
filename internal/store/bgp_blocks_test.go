package store

import "testing"

func TestNormalizeBlockTarget(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		prefixLen  uint8
		wantIP     string
		wantPrefix uint8
		wantErr    bool
	}{
		{name: "IPv4 host", raw: "192.0.2.1", prefixLen: 32, wantIP: "192.0.2.1", wantPrefix: 32},
		{name: "IPv4 unset length", raw: "192.0.2.1", prefixLen: 0, wantIP: "192.0.2.1", wantPrefix: 32},
		{name: "mapped IPv4 host", raw: "::ffff:192.0.2.1", prefixLen: 32, wantIP: "192.0.2.1", wantPrefix: 32},
		{name: "mapped IPv4 /30 stored as 126", raw: "::ffff:192.0.2.0", prefixLen: 126, wantIP: "192.0.2.0", wantPrefix: 30},
		{name: "IPv4 /28", raw: "192.0.2.16", prefixLen: 28, wantIP: "192.0.2.16", wantPrefix: 28},
		{name: "IPv6 host", raw: "2001:0db8:0:0::1", prefixLen: 128, wantIP: "2001:db8::1", wantPrefix: 128},
		{name: "IPv6 legacy /32 is a host route", raw: "2001:db8::1", prefixLen: 32, wantIP: "2001:db8::1", wantPrefix: 128},
		{name: "IPv6 /64", raw: "2001:db8::", prefixLen: 64, wantIP: "2001:db8::", wantPrefix: 64},
		{name: "invalid", raw: "not-an-ip", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIP, gotPrefix, err := normalizeBlockTarget(tt.raw, tt.prefixLen)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBlockTarget: %v", err)
			}
			if gotIP != tt.wantIP || gotPrefix != tt.wantPrefix {
				t.Fatalf("normalizeBlockTarget(%q, %d) = (%q, %d), want (%q, %d)", tt.raw, tt.prefixLen, gotIP, gotPrefix, tt.wantIP, tt.wantPrefix)
			}
		})
	}
}
