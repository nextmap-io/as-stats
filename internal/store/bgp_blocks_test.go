package store

import "testing"

func TestNormalizeBlockIP(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantIP     string
		wantPrefix uint8
		wantErr    bool
	}{
		{name: "IPv4", raw: "192.0.2.1", wantIP: "192.0.2.1", wantPrefix: 32},
		{name: "mapped IPv4", raw: "::ffff:192.0.2.1", wantIP: "192.0.2.1", wantPrefix: 32},
		{name: "IPv6", raw: "2001:0db8:0:0::1", wantIP: "2001:db8::1", wantPrefix: 128},
		{name: "invalid", raw: "not-an-ip", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIP, gotPrefix, err := normalizeBlockIP(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBlockIP: %v", err)
			}
			if gotIP != tt.wantIP || gotPrefix != tt.wantPrefix {
				t.Fatalf("normalizeBlockIP(%q) = (%q, %d), want (%q, %d)", tt.raw, gotIP, gotPrefix, tt.wantIP, tt.wantPrefix)
			}
		})
	}
}
