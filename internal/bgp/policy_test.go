package bgp

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{in: "192.0.2.1", want: "192.0.2.1/32"},
		{in: " 192.0.2.1 ", want: "192.0.2.1/32"},
		{in: "::ffff:192.0.2.1", want: "192.0.2.1/32"},
		{in: "::ffff:192.0.2.0/120", want: "192.0.2.0/24"},
		{in: "2001:DB8::1", want: "2001:db8::1/128"},
		{in: "192.0.2.0/30", want: "192.0.2.0/30"},
		{in: "192.0.2.1/30", err: true}, // host bits: never widened silently
		{in: "::ffff:0:0/64", err: true},
		{in: "fe80::1%eth0", err: true},
		{in: "", err: true},
		{in: "192.0.2.1; rm -rf /", err: true},
		{in: "$(id)", err: true},
		{in: "192.0.2.1 -a ipv6", err: true},
		{in: "example.com", err: true},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseTarget(%q) = %s, want an error", c.in, got)
			} else if !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("ParseTarget(%q) error %v is not ErrInvalidRequest", c.in, err)
			}
			continue
		}
		if err != nil || got.String() != c.want {
			t.Errorf("ParseTarget(%q) = %s, %v; want %s", c.in, got, err, c.want)
		}
	}
}

func testPolicy() Policy {
	allowed, _ := ParsePrefixList("198.51.100.0/24, 2001:db8:100::/48")
	protected, _ := ParsePrefixList("198.51.100.53, 198.51.100.1")
	return Policy{Allowed: allowed, AllowedSource: "test", Protected: protected, MinLenV4: 32, MinLenV6: 128}
}

func TestPolicyCheck(t *testing.T) {
	pol := testPolicy()
	cases := []struct {
		target string
		ok     bool
		reason string
	}{
		{"198.51.100.10", true, ""},
		{"2001:db8:100::10", true, ""},
		{"::ffff:198.51.100.10", true, ""},
		{"203.0.113.10", false, "outside the allowed prefixes"},
		{"2001:db8:200::1", false, "outside the allowed prefixes"},
		{"198.51.100.0/30", false, "shorter than the allowed /32"},
		{"198.51.100.53", false, "protected"},
		{"198.51.100.1", false, "protected"},
		{"0.0.0.0", false, "reserved"},
		{"127.0.0.1", false, "reserved"},
		{"224.0.0.1", false, "reserved"},
		{"::1", false, "reserved"},
		{"fe80::1", false, "reserved"},
		{"100::1", false, "reserved"},
		{"255.255.255.255", false, "reserved"},
	}
	for _, c := range cases {
		p, err := ParseTarget(c.target)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", c.target, err)
		}
		err = pol.Check(p)
		if c.ok {
			if err != nil {
				t.Errorf("Check(%s) = %v, want ok", c.target, err)
			}
			continue
		}
		var pe *PolicyError
		if !errors.As(err, &pe) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("Check(%s) = %v, want a PolicyError containing %q", c.target, err, c.reason)
		}
	}
}

func TestPolicyPrefixLengths(t *testing.T) {
	pol := testPolicy()
	pol.MinLenV4, pol.MinLenV6 = 28, 64
	for target, ok := range map[string]bool{
		"198.51.100.16/28":    true,
		"198.51.100.0/27":     false,
		"2001:db8:100::/64":   true,
		"2001:db8:100::/63":   false,
		"198.51.100.48/30":    true,  // .48-.51
		"198.51.100.52/30":    false, // contains the protected .53
		"2001:db8:100:1::/64": true,
	} {
		p := netip.MustParsePrefix(target)
		err := pol.Check(p)
		if (err == nil) != ok {
			t.Errorf("Check(%s) = %v, want ok=%v", target, err, ok)
		}
	}
	// The hard floors win over a misconfigured policy.
	pol.MinLenV4, pol.MinLenV6 = 8, 16
	pol.Allowed = append(pol.Allowed, netip.MustParsePrefix("203.0.113.0/24"))
	if err := pol.Check(netip.MustParsePrefix("203.0.113.0/24")); err != nil {
		t.Errorf("/24 at the floor: %v", err)
	}
	pol.Allowed = append(pol.Allowed, netip.MustParsePrefix("10.0.0.0/8"))
	if err := pol.Check(netip.MustParsePrefix("10.0.0.0/16")); err == nil {
		t.Error("a /16 passed the /24 floor")
	}
	if err := pol.Check(netip.MustParsePrefix("2001:db8:100::/47")); err == nil {
		t.Error("a /47 passed the /48 floor")
	}
}

func TestPolicyFailsClosedWithoutAllowList(t *testing.T) {
	pol := Policy{MinLenV4: 32, MinLenV6: 128, AllowedSource: "none"}
	err := pol.Check(netip.MustParsePrefix("198.51.100.10/32"))
	var pe *PolicyError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "no allow-list") {
		t.Fatalf("Check without allow-list = %v, want a refusal", err)
	}
}

func TestParsePrefixList(t *testing.T) {
	got, err := ParsePrefixList("198.51.100.7/24, 2001:db8::1, ::ffff:192.0.2.0/120,,")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"198.51.100.0/24", "2001:db8::1/128", "192.0.2.0/24"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("[%d] = %s, want %s", i, got[i], want[i])
		}
	}
	if _, err := ParsePrefixList("not-a-prefix"); err == nil {
		t.Error("invalid entry accepted")
	}
}
