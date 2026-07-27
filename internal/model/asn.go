package model

// Private-use ASN ranges, per RFC 6996 (16-bit) and RFC 6996 §5 (32-bit).
// These bounds are the single source of truth for "is this ASN private?" —
// both the collector's enrichment path and the AS-keyed read queries in
// internal/store derive their behaviour from them, so they must not be
// re-typed as loose literals anywhere else.
const (
	PrivateAS16Min uint32 = 64512
	PrivateAS16Max uint32 = 65534
	PrivateAS32Min uint32 = 4200000000
	PrivateAS32Max uint32 = 4294967294
)

// PrivateASGroup is the synthetic ASN under which every private-use ASN is
// reported. Downstream/customer networks announce dozens of distinct private
// ASNs that will never resolve to a public name, and on a real deployment they
// account for a third of all traffic — as separate rows they push every real
// peer off the Top-AS views.
//
// 4294967295 is reserved by RFC 7300 (last ASN of the 32-bit space) and can
// never be observed in a flow, so it cannot collide with a real AS. AS 0 is
// deliberately NOT used: it already means "unknown / not resolved".
const PrivateASGroup uint32 = 4294967295

// PrivateASName is the display name carried by the PrivateASGroup row. Queries
// that LEFT JOIN as_names substitute it for the (always empty) joined name.
const PrivateASName = "Private / Internal"

// IsPrivateAS reports whether asn falls in a private-use range and should
// therefore be collapsed into PrivateASGroup by the AS-keyed read paths.
// PrivateASGroup itself is not "private" — it is the group, not a member.
func IsPrivateAS(asn uint32) bool {
	return (asn >= PrivateAS16Min && asn <= PrivateAS16Max) ||
		(asn >= PrivateAS32Min && asn <= PrivateAS32Max)
}
