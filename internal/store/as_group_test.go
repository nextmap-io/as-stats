package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// TestGroupedASMatchesModel checks that the SQL fold reproduces model.IsPrivateAS
// exactly, by evaluating the generated expression in Go. If the two ever drift,
// the collector and the read path disagree about what "private" means and the
// grouped row stops matching its own drill-down.
func TestGroupedASMatchesModel(t *testing.T) {
	expr := groupedAS("t.as_number")

	for _, asn := range []uint32{0, 1, 15169, 64511, 64512, 65009, 65534, 65535,
		4199999999, 4200000000, 4294967294, model.PrivateASGroup} {
		want := asn
		if model.IsPrivateAS(asn) {
			want = model.PrivateASGroup
		}
		if got := evalGroupedAS(expr, "t.as_number", asn); got != want {
			t.Errorf("SQL fold of %d = %d, want %d", asn, got, want)
		}
	}
}

// evalGroupedAS interprets the `if((c >= a AND c <= b) OR (c >= x AND c <= y), g, c)`
// shape groupedAS emits, by reading the bounds back out of the generated SQL.
// It deliberately re-derives them from the string rather than from the model
// constants, so a hand-edited literal in the SQL would be caught.
func evalGroupedAS(expr, col string, asn uint32) uint32 {
	var lo1, hi1, lo2, hi2, group uint32
	pattern := fmt.Sprintf("if((%[1]s >= %%d AND %[1]s <= %%d) OR (%[1]s >= %%d AND %[1]s <= %%d), %%d, %[1]s)", col)
	if _, err := fmt.Sscanf(expr, pattern, &lo1, &hi1, &lo2, &hi2, &group); err != nil {
		panic("groupedAS shape changed: " + expr)
	}
	if (asn >= lo1 && asn <= hi1) || (asn >= lo2 && asn <= hi2) {
		return group
	}
	return asn
}

// TestGroupedASName verifies the sentinel row is labelled rather than left with
// the empty name a LEFT JOIN on as_names would produce for it.
func TestGroupedASName(t *testing.T) {
	got := groupedASName(groupedAS("t.as_number"), "any(an.as_name)")
	if !strings.Contains(got, fmt.Sprintf("= %d,", model.PrivateASGroup)) {
		t.Errorf("label expression does not test the sentinel: %q", got)
	}
	if !strings.Contains(got, "'"+model.PrivateASName+"'") {
		t.Errorf("label expression does not carry %q: %q", model.PrivateASName, got)
	}
	if !strings.HasSuffix(got, "any(an.as_name))") {
		t.Errorf("label expression must fall back to the joined name: %q", got)
	}
}

// TestPerASListsGroupPrivateASNs runs every reader that returns one row per AS
// against a recording connection and checks it groups on the private-use fold
// of its AS column — so no view lists 64512, 65001, 4200000001… one by one
// while the others show a single "Private / Internal" row. AnomalyExplain
// (QueryRow first) is covered by TestIntegrationPrivateASGrouping. Add any new
// per-AS reader here.
func TestPerASListsGroupPrivateASNs(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	p := QueryParams{From: from, To: to, Limit: 10}
	ctx := context.Background()
	readers := map[string]struct {
		col string
		run func(*ClickHouseStore) error
	}{
		"TopAS":     {"t.as_number", func(s *ClickHouseStore) error { _, _, err := s.TopAS(ctx, p); return err }},
		"LinkTopAS": {"t.as_number", func(s *ClickHouseStore) error { _, _, err := s.LinkTopAS(ctx, "l", p); return err }},
		"IPTopAS":   {"t.as_number", func(s *ClickHouseStore) error { _, err := s.IPTopAS(ctx, "192.0.2.1", p); return err }},
		"ASPeers":   {"f.dst_as", func(s *ClickHouseStore) error { _, err := s.ASPeers(ctx, 64500, p); return err }},
		"Movers/as": {"t.as_number", func(s *ClickHouseStore) error { _, err := s.Movers(ctx, "as", from, to, 10); return err }},
		"Talkers/as": {"t.as_number", func(s *ClickHouseStore) error {
			_, err := s.Talkers(ctx, "as", from, to, 10)
			return err
		}},
		"Conversations/src_dst_as": {"t.src_as", func(s *ClickHouseStore) error {
			_, err := s.Conversations(ctx, p, "src_dst_as", true)
			return err
		}},
	}
	for name, r := range readers {
		t.Run(name, func(t *testing.T) {
			rc := &recordingConn{}
			if err := r.run(&ClickHouseStore{conn: rc}); !errors.Is(err, errRecorded) {
				t.Fatalf("expected the recorded query error, got %v", err)
			}
			if q := rc.queries[0]; !strings.Contains(q, groupedAS(r.col)) {
				t.Errorf("query does not group %s through groupedAS:\n%s", r.col, q)
			}
		})
	}
}
