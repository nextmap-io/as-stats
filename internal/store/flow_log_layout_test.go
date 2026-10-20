package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/nextmap-io/as-stats/internal/model"
)

// recordingConn is a driver.Conn that records every SQL statement and fails
// it, so a store method can be run up to its first query without a server.
// Methods not overridden panic (nil embedded interface) — none are reached.
type recordingConn struct {
	driver.Conn
	queries []string
}

var errRecorded = errors.New("recorded")

func (c *recordingConn) Query(_ context.Context, query string, _ ...any) (driver.Rows, error) {
	c.queries = append(c.queries, query)
	return nil, errRecorded
}

func (c *recordingConn) Exec(_ context.Context, query string, _ ...any) error {
	c.queries = append(c.queries, query)
	return errRecorded
}

// flowLogBucketPredicate is the part of flowLogTimeFilter that 24.3 needs to
// use the (toStartOfFifteenMinutes(ts), dst_ip, …) key.
var flowLogBucketPredicate = regexp.MustCompile(`toStartOfFifteenMinutes\((\w+)\.ts\) BETWEEN toStartOfFifteenMinutes\(@from\) AND toStartOfFifteenMinutes\(@to_incl\)`)

// TestFlowLogQueriesCarryBucketPredicate runs every flow-log reader against a
// recording connection and checks each query reads flowLogReadTable (the
// Merge over flows_log + flows_log_legacy) with the 15-minute key predicate.
// Add any new flow-log reader here.
func TestFlowLogQueriesCarryBucketPredicate(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	readers := map[string]func(*ClickHouseStore) error{
		"SearchFlowLog": func(s *ClickHouseStore) error {
			_, err := s.SearchFlowLog(context.Background(), model.FlowSearchFilters{From: from, To: to, DstIP: "192.0.2.1", SrcIP: "198.51.100.0/24"})
			return err
		},
		"FlowLogTimeSeries": func(s *ClickHouseStore) error {
			_, err := s.FlowLogTimeSeries(context.Background(), model.FlowSearchFilters{From: from, To: to, DstIP: "192.0.2.1"})
			return err
		},
	}
	for _, dim := range []string{"src_dst_ip", "src_dst_as", "dst_port_proto"} {
		readers["Conversations/"+dim] = func(s *ClickHouseStore) error {
			_, err := s.Conversations(context.Background(), QueryParams{From: from, To: to}, dim, true)
			return err
		}
	}
	for name, run := range readers {
		t.Run(name, func(t *testing.T) {
			rc := &recordingConn{}
			if err := run(&ClickHouseStore{conn: rc}); !errors.Is(err, errRecorded) {
				t.Fatalf("expected the recorded query error, got %v", err)
			}
			if len(rc.queries) != 1 {
				t.Fatalf("expected 1 query, got %d", len(rc.queries))
			}
			q := rc.queries[0]
			m := regexp.MustCompile(`FROM (\w+) (\w+)`).FindStringSubmatch(q)
			if m == nil || m[1] != flowLogReadTable {
				t.Fatalf("query does not read %s:\n%s", flowLogReadTable, q)
			}
			pm := flowLogBucketPredicate.FindStringSubmatch(q)
			if pm == nil || pm[1] != m[2] {
				t.Fatalf("query lacks the 15-minute bucket predicate on alias %q:\n%s", m[2], q)
			}
		})
	}
}

// TestNoDirectFlowLogReads scans the Go sources: reads must go through
// flowLogReadTable + flowLogTimeFilter, never name flows_log in a FROM/JOIN.
func TestNoDirectFlowLogReads(t *testing.T) {
	bad := regexp.MustCompile(`(?i)\b(FROM|JOIN)\s+(asstats\.)?flows_log(_legacy|_all)?\b`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if bad.MatchString(line) {
				t.Errorf("%s:%d reads the flow log directly (use flowLogReadTable + flowLogTimeFilter): %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFlowLogTimeFilter(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		to, wantIncl time.Time
	}{
		// Whole second: @to is exclusive, the last included second is to-1s,
		// so a window ending on a bucket boundary does not scan the next bucket.
		{from.Add(15 * time.Minute), from.Add(15*time.Minute - time.Second)},
		// Sub-second @to: the second it falls in is (partly) included.
		{from.Add(15*time.Minute + 500*time.Millisecond), from.Add(15 * time.Minute)},
	}
	for _, c := range cases {
		cond, args := flowLogTimeFilter("f", from, c.to)
		if !strings.Contains(cond, "f.ts >= @from AND f.ts < @to AND ") || !flowLogBucketPredicate.MatchString(cond) {
			t.Fatalf("unexpected condition %q", cond)
		}
		got := map[string]any{}
		for _, a := range args {
			na := a.(driver.NamedValue)
			got[na.Name] = na.Value
		}
		if got["from"] != from || got["to"] != c.to || got["to_incl"] != c.wantIncl {
			t.Errorf("to=%v: args %v, want to_incl %v", c.to, got, c.wantIncl)
		}
	}
}
