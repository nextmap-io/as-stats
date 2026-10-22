package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// TestIntegrationColdTier exercises the hot/cold tier end to end on flows_log.
//
// It needs a throwaway ClickHouse whose configuration declares a storage
// policy with a volume named `cold` (and the migrations applied), e.g. the
// shipped clickhouse/config.d/storage-hot-cold.xml.example with the cold disk
// on a directory or tmpfs inside the container:
//
//	docker run -d --name ch-cold -p 127.0.0.1:19000:9000 \
//	  --tmpfs /var/lib/clickhouse-cold:uid=101,gid=101 \
//	  -v $PWD/clickhouse/config.d/storage-hot-cold.xml.example:/etc/clickhouse-server/config.d/storage-hot-cold.xml:ro \
//	  -e CLICKHOUSE_DB=asstats -e CLICKHOUSE_SKIP_USER_SETUP=1 clickhouse/clickhouse-server:24.3-alpine
//	(or :26.8.33.10-alpine — CI runs both; apply migrations/*.up.sql with
//	 clickhouse-client --database asstats, as the go-integration CI job does)
//	ASSTATS_IT_CLICKHOUSE=127.0.0.1:19000 go test ./internal/store -run IntegrationColdTier -v
//
// It is skipped when no such policy exists. It switches flows_log to that
// policy (irreversible — throwaway servers only), inserts one part per day
// for the last 20 days, sets cold_after_days and checks that the reconciler
// moves the old partitions (oldest first, maxColdMovesPerCycle per cycle),
// leaves recent ones hot, is a no-op once converged, and keeps the move
// clause across ttl_days changes. The flows_log retention policy is restored
// at the end.
func TestIntegrationColdTier(t *testing.T) {
	s := itStore(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if err := s.conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	var policy string
	if err := s.conn.QueryRow(ctx, `
		SELECT policy_name FROM system.storage_policies
		WHERE volume_name = @v ORDER BY policy_name LIMIT 1
	`, clickhouse.Named("v", coldVolumeName)).Scan(&policy); err != nil {
		t.Skipf("no storage policy with a '%s' volume on this server: %v", coldVolumeName, err)
	}
	exec(fmt.Sprintf("ALTER TABLE flows_log MODIFY SETTING storage_policy = '%s'", policy))

	if err := s.EnsureRetentionPolicies(ctx, 0); err != nil {
		t.Fatal(err)
	}
	before, err := s.retentionPoliciesByTable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	orig, ok := before["flows_log"]
	if !ok {
		t.Fatal("flows_log has no retention policy after EnsureRetentionPolicies")
	}
	t.Cleanup(func() {
		origCold := orig.ColdAfterDays
		if _, err := s.SetRetentionPolicy(ctx, "flows_log", orig.TTLDays, orig.Enabled, &origCold); err != nil {
			t.Logf("restore flows_log policy: %v", err)
		}
		_ = s.ReconcileRetention(ctx)
	})

	// Start from a TTL without a move clause, as on a deployment that predates
	// the cold tier: these parts carry no move TTL info.
	noCold := uint32(0)
	if _, err := s.SetRetentionPolicy(ctx, "flows_log", 180, true, &noCold); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}

	// One INSERT spanning 20 days = one part per daily partition, all on hot.
	exec(`INSERT INTO flows_log (ts, link_tag, bytes, packets, flow_count)
		SELECT now() - toIntervalDay(number + 1) - toIntervalHour(1), 'it-cold', 1000, 1, 1 FROM numbers(20)`)

	const coldDays = 5
	coldDisks, err := s.coldDisksByTable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(coldDisks["flows_log"]) == 0 {
		t.Fatalf("flows_log not seen with a cold volume after switching to %q", policy)
	}

	// What the first cycle must move: eligible partitions, oldest first.
	eligible := func() []string {
		t.Helper()
		rows, err := s.conn.Query(ctx, `
			SELECT partition_id FROM system.parts
			WHERE database = currentDatabase() AND table = 'flows_log' AND active
			GROUP BY partition_id
			HAVING max(max_time) < now() - toIntervalDay(@d)
			   AND countIf(NOT has(@cold, disk_name)) > 0
			ORDER BY max(max_time)
		`, clickhouse.Named("d", uint64(coldDays)), clickhouse.Named("cold", coldDisks["flows_log"]))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}
	// partitionDisks: partition id -> set of disks holding its active parts.
	partitionDisks := func() map[string]map[string]bool {
		t.Helper()
		rows, err := s.conn.Query(ctx, `
			SELECT partition_id, disk_name FROM system.parts
			WHERE database = currentDatabase() AND table = 'flows_log' AND active
		`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		out := map[string]map[string]bool{}
		for rows.Next() {
			var id, disk string
			if err := rows.Scan(&id, &disk); err != nil {
				t.Fatal(err)
			}
			if out[id] == nil {
				out[id] = map[string]bool{}
			}
			out[id][disk] = true
		}
		return out
	}
	isCold := func(disks map[string]bool) bool {
		if len(disks) == 0 {
			return false
		}
		for d := range disks {
			found := false
			for _, c := range coldDisks["flows_log"] {
				if c == d {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	dayID := func(daysAgo int) string {
		return time.Now().UTC().Add(-time.Duration(daysAgo)*24*time.Hour - time.Hour).Format("20060102")
	}

	todo := eligible()
	if len(todo) < 10 {
		t.Fatalf("expected >= 10 partitions older than %d days on hot, got %v", coldDays, todo)
	}

	coldPtr := uint32(coldDays)
	if _, err := s.SetRetentionPolicy(ctx, "flows_log", 180, true, &coldPtr); err != nil {
		t.Fatal(err)
	}

	// Cycle 1: the move clause is applied and exactly the 3 oldest eligible
	// partitions move.
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	ttl := liveFlowsLogTTL(t, s)
	if ttl.DeleteDays != 180 || ttl.ColdDays != coldDays || ttl.Colds != 1 {
		t.Fatalf("after cycle 1 TTL = %+v, want delete 180 / cold %d", ttl, coldDays)
	}
	pd := partitionDisks()
	for i, id := range todo {
		if got := isCold(pd[id]); got != (i < maxColdMovesPerCycle) {
			t.Errorf("cycle 1: partition %s (#%d oldest) cold=%v, want %v", id, i, got, i < maxColdMovesPerCycle)
		}
	}

	// Further cycles converge.
	for i := 0; i < len(todo)/maxColdMovesPerCycle+1; i++ {
		if err := s.ReconcileRetention(ctx); err != nil {
			t.Fatal(err)
		}
	}
	pd = partitionDisks()
	for _, id := range todo {
		if !isCold(pd[id]) {
			t.Errorf("partition %s still on %v after convergence", id, pd[id])
		}
	}
	for d := 1; d <= 3; d++ {
		if id := dayID(d); isCold(pd[id]) {
			t.Errorf("recent partition %s (%d day(s) ago) moved to cold", id, d)
		}
	}

	// Converged: another cycle changes nothing and the TTL is not re-issued.
	if diverged, err := s.applyTTLIfDiverged(ctx, "flows_log", 180, coldDays); err != nil || diverged {
		t.Errorf("applyTTLIfDiverged on a converged table = %v, %v; want false (no ALTER)", diverged, err)
	}
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if after := partitionDisks(); fmt.Sprint(after) != fmt.Sprint(pd) {
		t.Errorf("no-op cycle moved parts:\nbefore %v\nafter  %v", pd, after)
	}

	// Storage stats expose the split.
	stats, err := s.StorageStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawCold bool
	for _, d := range stats.Disks {
		if d.Name == coldDisks["flows_log"][0] && d.Path != "" && d.TotalBytes > 0 {
			sawCold = true
		}
	}
	if !sawCold {
		t.Errorf("StorageStats.Disks = %+v, want the cold disk with path and size", stats.Disks)
	}
	for _, tb := range stats.Tables {
		if tb.Table != "flows_log" {
			continue
		}
		if !tb.HasColdVolume || tb.StoragePolicy != policy || tb.ColdAfterDays != coldDays || tb.ColdBytes == 0 || tb.HotBytes == 0 || tb.ColdBytes+tb.HotBytes != tb.BytesOnDisk {
			t.Errorf("flows_log stats = %+v", tb)
		}
	}

	// Raising and lowering ttl_days (cold_after_days omitted) keeps the move clause.
	for _, days := range []uint32{200, 60} {
		if _, err := s.SetRetentionPolicy(ctx, "flows_log", days, true, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.ReconcileRetention(ctx); err != nil {
			t.Fatal(err)
		}
		if ttl := liveFlowsLogTTL(t, s); ttl.DeleteDays != days || ttl.ColdDays != coldDays || ttl.Colds != 1 || ttl.Others != 0 {
			t.Errorf("ttl_days=%d: live TTL %+v, want delete %d with the cold clause kept", days, ttl, days)
		}
		live, err := s.liveTTLDays(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if live["flows_log"] != days {
			t.Errorf("liveTTLDays(flows_log) = %d, want the DELETE interval %d", live["flows_log"], days)
		}
	}

	// Removing the cold tier removes the move clause.
	if _, err := s.SetRetentionPolicy(ctx, "flows_log", 60, true, &noCold); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileRetention(ctx); err != nil {
		t.Fatal(err)
	}
	if ttl := liveFlowsLogTTL(t, s); ttl.DeleteDays != 60 || ttl.Colds != 0 {
		t.Errorf("cold tier removed: live TTL %+v, want delete 60 and no move clause", ttl)
	}
}

func liveFlowsLogTTL(t *testing.T, s *ClickHouseStore) tableTTL {
	t.Helper()
	var q string
	if err := s.conn.QueryRow(context.Background(), `
		SELECT create_table_query FROM system.tables
		WHERE database = currentDatabase() AND name = 'flows_log'
	`).Scan(&q); err != nil {
		t.Fatal(err)
	}
	ttl, ok := parseTableTTL(q)
	if !ok {
		t.Fatalf("no TTL in %q", q)
	}
	t.Logf("flows_log TTL: %+v", ttl)
	return ttl
}
