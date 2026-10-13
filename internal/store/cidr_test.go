package store

import (
	"strings"
	"testing"
)

// Bug #1 regression: IPv4 CIDRs must be mapped onto the ::ffff:a.b.c.d range
// the IPv6 columns actually store, or every IPv4 row silently falls outside.
func TestCIDRBounds(t *testing.T) {
	cases := []struct {
		in, lo, hi string
	}{
		{"192.0.2.0/24", "::ffff:192.0.2.0", "::ffff:192.0.2.255"},
		{"198.51.96.0/19", "::ffff:198.51.96.0", "::ffff:198.51.127.255"},
		{"192.0.2.7/24", "::ffff:192.0.2.0", "::ffff:192.0.2.255"}, // host bits masked off
		{"192.0.2.7", "::ffff:192.0.2.7", "::ffff:192.0.2.7"},
		{"0.0.0.0/0", "::ffff:0.0.0.0", "::ffff:255.255.255.255"},          // all IPv4, no IPv6
		{"::ffff:192.0.2.0/120", "::ffff:192.0.2.0", "::ffff:192.0.2.255"}, // already mapped
		{"2001:db8::/32", "2001:db8::", "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"},
		{"2001:db8::1", "2001:db8::1", "2001:db8::1"},
		{"fe80::/10", "fe80::", "febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
	}
	for _, c := range cases {
		lo, hi, ok := cidrBounds(c.in)
		if !ok || lo != c.lo || hi != c.hi {
			t.Errorf("cidrBounds(%q) = %q, %q, %v; want %q, %q", c.in, lo, hi, ok, c.lo, c.hi)
		}
	}
	for _, bad := range []string{"", "nope", "10.0.0.0/99", "' OR 1=1 --", "1.2.3.4/"} {
		if _, _, ok := cidrBounds(bad); ok {
			t.Errorf("cidrBounds(%q) accepted garbage", bad)
		}
	}
}

func TestNormalizeCIDRForIPv6ColumnMappedInput(t *testing.T) {
	// An already-mapped CIDR used to come back as "/216".
	if got, ok := normalizeCIDRForIPv6Column("::ffff:192.0.2.0/120"); !ok || got != "::ffff:192.0.2.0/120" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if got, ok := normalizeCIDRForIPv6Column("192.0.2.0/24"); !ok || got != "::ffff:192.0.2.0/120" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestCIDRRangeSQL(t *testing.T) {
	got := CIDRRangeSQL("t.ip_address", []string{"192.0.2.0/24", "garbage", "2001:db8::/32"})
	want := "((t.ip_address BETWEEN toIPv6('::ffff:192.0.2.0') AND toIPv6('::ffff:192.0.2.255')) OR " +
		"(t.ip_address BETWEEN toIPv6('2001:db8::') AND toIPv6('2001:db8:ffff:ffff:ffff:ffff:ffff:ffff')))"
	if got != want {
		t.Fatalf("CIDRRangeSQL =\n%s\nwant\n%s", got, want)
	}
	if CIDRRangeSQL("c", nil) != "1=1" || CIDRRangeSQL("c", []string{"x"}) != "1=1" {
		t.Fatal("empty / all-invalid input must yield 1=1")
	}
	// Literal bounds are re-rendered from parsed addresses: nothing from the
	// input can reach the SQL string.
	inj := CIDRRangeSQL("c", []string{"10.0.0.0/8') OR 1=1 --"})
	if strings.Contains(inj, "OR 1=1") {
		t.Fatalf("injection leaked: %s", inj)
	}
	priv := PrivateRangeSQL("t.ip_address")
	for _, frag := range []string{"'::ffff:10.0.0.0'", "'::ffff:172.31.255.255'", "'fc00::'", "'fe80::'"} {
		if !strings.Contains(priv, frag) {
			t.Errorf("PrivateRangeSQL missing %s: %s", frag, priv)
		}
	}
}
