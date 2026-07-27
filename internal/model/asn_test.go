package model

import "testing"

// TestIsPrivateAS pins the private-use range boundaries. Widening them by one
// would start folding real, routable ASNs into the "Private / Internal" row —
// their traffic would silently vanish from Top AS.
func TestIsPrivateAS(t *testing.T) {
	cases := []struct {
		asn  uint32
		want bool
	}{
		{0, false},          // unknown / not resolved
		{15169, false},      // ordinary public ASN
		{64511, false},      // just below the 16-bit range
		{64512, true},       // first 16-bit private
		{65009, true},       // observed in production
		{65534, true},       // last 16-bit private
		{65535, false},      // reserved, not private-use
		{4199999999, false}, // just below the 32-bit range
		{4200000000, true},  // first 32-bit private
		{4294967294, true},  // last 32-bit private
		{PrivateASGroup, false},
	}
	for _, c := range cases {
		if got := IsPrivateAS(c.asn); got != c.want {
			t.Errorf("IsPrivateAS(%d) = %v, want %v", c.asn, got, c.want)
		}
	}
}

// TestPrivateASGroupSentinel guards the two properties the grouping relies on:
// the sentinel must not itself be a group member (that would make the SQL fold
// idempotent by accident rather than by design), and it must not be 0, which
// already means "unknown".
func TestPrivateASGroupSentinel(t *testing.T) {
	if PrivateASGroup == 0 {
		t.Fatal("PrivateASGroup must not be 0 — that ASN already means unknown")
	}
	if IsPrivateAS(PrivateASGroup) {
		t.Fatal("PrivateASGroup must fall outside the private-use ranges")
	}
	if PrivateASGroup != PrivateAS32Max+1 {
		t.Errorf("PrivateASGroup = %d, want the reserved last ASN %d", PrivateASGroup, uint32(PrivateAS32Max+1))
	}
}
