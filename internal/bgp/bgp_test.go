package bgp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNoopBlocker_Announce(t *testing.T) {
	b := NewNoop()
	err := b.Announce(context.Background(), net.ParseIP("192.0.2.1"), time.Hour, "test reason")
	if err != nil {
		t.Fatalf("Announce returned unexpected error: %v", err)
	}
}

func TestNoopBlocker_Withdraw(t *testing.T) {
	b := NewNoop()
	err := b.Withdraw(context.Background(), net.ParseIP("192.0.2.1"))
	if err != nil {
		t.Fatalf("Withdraw returned unexpected error: %v", err)
	}
}

func TestNoopBlocker_List(t *testing.T) {
	b := NewNoop()
	list, err := b.List(context.Background())
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d entries", len(list))
	}
}

func TestNoopBlocker_Status(t *testing.T) {
	b := NewNoop()
	st, err := b.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned unexpected error: %v", err)
	}
	if st.Enabled {
		t.Error("expected Enabled=false for noop blocker")
	}
	if st.State != "disabled" {
		t.Errorf("expected State=%q, got %q", "disabled", st.State)
	}
}

func TestHostPrefixLen(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want uint8
	}{
		{name: "IPv4", ip: "192.0.2.1", want: 32},
		{name: "IPv4 mapped", ip: "::ffff:192.0.2.1", want: 32},
		{name: "IPv6", ip: "2001:db8::1", want: 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) returned nil", tt.ip)
			}
			if got := HostPrefixLen(ip); got != tt.want {
				t.Fatalf("HostPrefixLen(%q) = %d, want %d", tt.ip, got, tt.want)
			}
		})
	}
}
