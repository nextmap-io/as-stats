package store

import (
	"strings"
	"testing"
)

func TestBuildCIDRFilter_Empty(t *testing.T) {
	clause, args := buildCIDRFilter("dst_ip", "test_", nil)
	if clause != "1=1" {
		t.Errorf("expected '1=1' for empty prefixes, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("expected no args for empty prefixes, got %d", len(args))
	}
}

func TestBuildCIDRFilter_ValidCIDRs(t *testing.T) {
	prefixes := []string{"10.0.0.0/8", "192.168.0.0/16", "2001:db8::/32"}
	clause, args := buildCIDRFilter("dst_ip", "p_", prefixes)

	// Should produce 3 OR-joined parameterized conditions
	if !strings.Contains(clause, "isIPAddressInRange(toString(dst_ip), @p_cidr0)") {
		t.Errorf("missing first parameter ref in clause: %s", clause)
	}
	if !strings.Contains(clause, "@p_cidr1") || !strings.Contains(clause, "@p_cidr2") {
		t.Errorf("missing additional parameter refs in clause: %s", clause)
	}
	if strings.Count(clause, " OR ") != 2 {
		t.Errorf("expected 2 ORs joining 3 conditions, got %s", clause)
	}
	if len(args) != 3 {
		t.Errorf("expected 3 named args, got %d", len(args))
	}
}

func TestBuildCIDRFilter_InvalidEntriesSkipped(t *testing.T) {
	prefixes := []string{
		"10.0.0.0/8",                                  // valid CIDR
		"' OR 1=1; DROP TABLE flows_raw; --",          // SQL injection attempt
		"192.168.1.1",                                 // bare IP — should be accepted
		"not-an-ip",                                   // garbage — should be skipped
		"10.0.0.0/99",                                 // invalid CIDR mask
		"172.16.0.0/12",                               // valid CIDR
	}
	clause, args := buildCIDRFilter("src_ip", "x_", prefixes)

	// Should keep 3 valid entries (10/8, 192.168.1.1, 172.16/12)
	if len(args) != 3 {
		t.Errorf("expected 3 args (2 CIDR + 1 IP), got %d (clause=%s)", len(args), clause)
	}

	// Crucially: no SQL injection should appear in the clause string
	if strings.Contains(clause, "DROP TABLE") || strings.Contains(clause, "1=1") || strings.Contains(clause, ";") {
		t.Errorf("SQL injection leaked into clause: %s", clause)
	}
	if strings.Contains(clause, "'") {
		t.Errorf("clause should never contain literal quotes (parameterized): %s", clause)
	}
}

func TestBuildCIDRFilter_AllInvalid(t *testing.T) {
	prefixes := []string{"foo", "bar", "10.0.0.0/99"}
	clause, args := buildCIDRFilter("dst_ip", "i_", prefixes)
	if clause != "1=1" {
		t.Errorf("expected '1=1' when all prefixes invalid, got %q", clause)
	}
	if len(args) != 0 {
		t.Errorf("expected no args when all prefixes invalid, got %d", len(args))
	}
}

func TestBuildCIDRFilter_NamingPrefixes(t *testing.T) {
	// Different prefixes should not collide in the same query
	clauseA, argsA := buildCIDRFilter("dst_ip", "a_", []string{"10.0.0.0/8"})
	clauseB, argsB := buildCIDRFilter("src_ip", "b_", []string{"192.168.0.0/16"})

	if !strings.Contains(clauseA, "@a_cidr0") {
		t.Errorf("clause A missing prefix: %s", clauseA)
	}
	if !strings.Contains(clauseB, "@b_cidr0") {
		t.Errorf("clause B missing prefix: %s", clauseB)
	}
	if len(argsA) != 1 || len(argsB) != 1 {
		t.Errorf("expected 1 arg each, got A=%d B=%d", len(argsA), len(argsB))
	}
}
