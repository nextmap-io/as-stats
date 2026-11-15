package exporters

import (
	"net"
	"testing"
)

func TestParseEmptyAcceptsAll(t *testing.T) {
	for _, s := range []string{"", " ", " , "} {
		a, err := Parse(s)
		if err != nil || a != nil {
			t.Fatalf("Parse(%q) = %v, %v; want nil, nil", s, a, err)
		}
		if !a.Allows(net.ParseIP("203.0.113.9")) {
			t.Errorf("a nil allow-list must accept every source")
		}
	}
}

func TestAllows(t *testing.T) {
	a, err := Parse(" 192.0.2.1 , 198.51.100.0/24,2001:db8:1::/48, ::ffff:203.0.113.0/120")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cases := []struct {
		ip   string
		want bool
	}{
		{"192.0.2.1", true},
		{"192.0.2.2", false},
		{"198.51.100.200", true},
		{"198.51.101.1", false},
		{"2001:db8:1:ffff::1", true},
		{"2001:db8:2::1", false},
		{"203.0.113.77", true}, // IPv4-mapped CIDR entry matches the plain IPv4 source
		{"203.0.114.1", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if got := a.Allows(ip); got != tc.want {
			t.Errorf("Allows(%s) = %v, want %v", tc.ip, got, tc.want)
		}
		// A dual-stack socket reports IPv4 sources as 16-byte mapped
		// addresses; a 4-byte slice must give the same answer.
		if v4 := ip.To4(); v4 != nil {
			if got := a.Allows(v4); got != tc.want {
				t.Errorf("Allows(4-byte %s) = %v, want %v", tc.ip, got, tc.want)
			}
		}
	}
	if a.Allows(nil) {
		t.Errorf("Allows(nil) must be false when a list is configured")
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"router1", "192.0.2.1/33", "192.0.2.300", "fe80::1%eth0", "10.0.0.0/8,nope"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted a malformed entry", bad)
		}
	}
}

func TestString(t *testing.T) {
	var none *AllowList
	if none.String() != "any" {
		t.Errorf("nil String() = %q", none.String())
	}
	a, err := Parse("192.0.2.1,198.51.100.7/24")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.String(), "192.0.2.1/32, 198.51.100.0/24"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
