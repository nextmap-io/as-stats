package netflow

import (
	"fmt"
	"net"
	"testing"
)

// TestPrefixStringMatchesNetIP pins the hand-rolled IPv4 formatting of
// prefixString to the net.IP one it replaced (fmt "%s/%d" over maskIP), for
// every mask length, on 4-byte, IPv4-mapped 16-byte and IPv6 addresses.
func TestPrefixStringMatchesNetIP(t *testing.T) {
	ips := []net.IP{
		net.IP{0, 0, 0, 0},
		net.IP{255, 255, 255, 255},
		net.IP{10, 1, 2, 3},
		net.IP{192, 168, 100, 254},
		net.IP{1, 0, 128, 7},
		net.ParseIP("203.0.113.77"), // 16-byte IPv4-mapped form
		net.ParseIP("2001:db8:abcd:12:ffff::1"),
		net.ParseIP("fe80::1"),
		net.ParseIP("::"),
	}
	for _, ip := range ips {
		bits := 128
		if ip.To4() != nil {
			bits = 32
		}
		for m := 1; m <= bits; m++ {
			want := fmt.Sprintf("%s/%d", maskIP(ip, uint8(m)), m)
			if got := prefixString(ip, m); got != want {
				t.Fatalf("prefixString(%s, %d) = %q, want %q", ip, m, got, want)
			}
		}
		for _, m := range []int{-1, 0, bits + 1, 200} {
			if got := prefixString(ip, m); got != "" {
				t.Errorf("prefixString(%s, %d) = %q, want \"\"", ip, m, got)
			}
		}
	}
	if got := prefixString(nil, 24); got != "" {
		t.Errorf("prefixString(nil, 24) = %q, want \"\"", got)
	}
}

func BenchmarkPrefixString(b *testing.B) {
	v4 := net.IP{192, 168, 100, 254}
	v6 := net.ParseIP("2001:db8:abcd:12:ffff::1")
	b.ReportAllocs()
	for b.Loop() {
		_ = prefixString(v4, 24)
		_ = prefixString(v6, 48)
	}
}
