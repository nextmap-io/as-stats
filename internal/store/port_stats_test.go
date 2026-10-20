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
)

// TestServicePort pins the service-port rule of traffic_by_port_mv (migration
// 000018). TestIntegrationPortStats checks the SQL gives the same answers.
func TestServicePort(t *testing.T) {
	cases := []struct {
		name     string
		protocol uint8
		src, dst uint16
		want     uint16
	}{
		{"client to HTTPS server", 6, 51000, 443, 443},
		{"HTTPS server to client", 6, 443, 51000, 443},
		{"alternate HTTP vs ephemeral", 6, 8080, 50000, 8080},
		{"ephemeral vs alternate HTTP", 6, 50000, 8080, 8080},
		{"DNS server to server", 17, 53, 53, 53},
		{"source port 0", 17, 0, 123, 123},
		{"destination port 0", 17, 123, 0, 123},
		{"ICMP type/code in dst_port", 1, 0, 2048, 0},
		{"ICMPv6", 58, 0, 32768, 0},
		{"GRE, no ports", 47, 0, 0, 0},
		{"both high", 17, 60001, 50000, 50000},
	}
	for _, c := range cases {
		if got := ServicePort(c.protocol, c.src, c.dst); got != c.want {
			t.Errorf("%s: ServicePort(%d, %d, %d) = %d, want %d", c.name, c.protocol, c.src, c.dst, got, c.want)
		}
	}
}

// TestPortReadersReadOnlyTheNewTable runs every traffic_by_port reader against
// a recording connection: each reads traffic_by_port (aliased, so the alias
// rules hold) and nothing else — never the pre-3.5 traffic_by_port_legacy,
// whose rows count every flow twice.
func TestPortReadersReadOnlyTheNewTable(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	readers := map[string]func(*ClickHouseStore) error{
		"TopProtocols": func(s *ClickHouseStore) error {
			_, err := s.TopProtocols(context.Background(), QueryParams{From: from, To: to})
			return err
		},
		"TopProtocols/in": func(s *ClickHouseStore) error {
			_, err := s.TopProtocols(context.Background(), QueryParams{From: from, To: to, Direction: "in", LinkTags: []string{"a", "b"}})
			return err
		},
		"TopPorts": func(s *ClickHouseStore) error {
			_, err := s.TopPorts(context.Background(), QueryParams{From: from, To: to, Direction: "out"}, 6)
			return err
		},
		"Movers/port": func(s *ClickHouseStore) error {
			_, err := s.Movers(context.Background(), "port", from, to, 10)
			return err
		},
	}
	tables := regexp.MustCompile(`(?:FROM|JOIN)\s+(\w+)`)
	for name, run := range readers {
		t.Run(name, func(t *testing.T) {
			rc := &recordingConn{}
			if err := run(&ClickHouseStore{conn: rc}); !errors.Is(err, errRecorded) {
				t.Fatalf("expected the recorded query error, got %v", err)
			}
			q := rc.queries[0]
			if !strings.Contains(q, "FROM traffic_by_port t") {
				t.Errorf("query does not read traffic_by_port aliased t:\n%s", q)
			}
			for _, m := range tables.FindAllStringSubmatch(q, -1) {
				if strings.HasPrefix(m[1], "traffic_by_port") && m[1] != "traffic_by_port" {
					t.Errorf("query reads %s:\n%s", m[1], q)
				}
			}
			if strings.HasPrefix(name, "TopP") && !strings.Contains(q, "t.direction") {
				t.Errorf("Inbound / Outbound reader without a direction predicate:\n%s", q)
			}
		})
	}
}

// TestPortDirectionFilter: with no direction asked, the rows of flows on no
// configured link (stored with an empty direction) are left out, never shown as in or out.
func TestPortDirectionFilter(t *testing.T) {
	for dir, want := range map[string]string{
		"in":  "AND t.direction = 'in'",
		"out": "AND t.direction = 'out'",
		"":    "AND t.direction IN ('in', 'out')",
		"xx":  "AND t.direction IN ('in', 'out')",
	} {
		if got := portDirectionFilter(dir); got != want {
			t.Errorf("portDirectionFilter(%q) = %q, want %q", dir, got, want)
		}
	}
}

// TestNoLegacyPortReads fails on any Go source outside tests that names
// traffic_by_port_legacy, except the retention code that ages it out and the
// port_stats.go history probe (system.parts only).
func TestNoLegacyPortReads(t *testing.T) {
	allowed := map[string]bool{"retention.go": true, "port_stats.go": true}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); path != root && (n == "node_modules" || n == "frontend" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir, base := filepath.Base(filepath.Dir(path)), filepath.Base(path)
		if dir == "migrate" || (dir == "store" && allowed[base]) {
			return nil // schema probe, retention, history probe
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue // prose about the table is fine
			}
			if strings.Contains(line, "traffic_by_port_legacy") {
				t.Errorf("%s:%d names traffic_by_port_legacy: only the retention code may touch it", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
