package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/bgp"
	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/migrate"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
	schema "github.com/nextmap-io/as-stats/migrations"
)

// BGP blackholing end to end: the real router (CSRF, admin group, internal
// token route), bgp.Service, bgp_blocks on ClickHouse and a fake BGP daemon —
// a shell script that records every call and keeps a RIB in a file. No
// router is involved.
//
//	ASSTATS_IT_CLICKHOUSE=127.0.0.1:9000 go test ./internal/api -run Integration -v
//
// The test builds its own scratch database with the real migration runner.

const fakeDaemon = `#!/bin/sh
# fake BGP daemon: <dir> <action> [prefix] [communities] [next_hop]
dir=$1; action=$2; shift 2
echo "$action $* secret=${CLICKHOUSE_PASSWORD:-none} token=${BGP_API_TOKEN:-none} env_prefix=${ASSTATS_BGP_PREFIX:-}" >> "$dir/calls"
touch "$dir/rib"
case "$action" in
  announce)
    [ -f "$dir/fail" ] && { echo "connection refused" >&2; exit 1; }
    grep -qx "$1" "$dir/rib" || echo "$1" >> "$dir/rib" ;;
  withdraw)
    [ -f "$dir/fail" ] && { echo "connection refused" >&2; exit 1; }
    grep -vx "$1" "$dir/rib" > "$dir/rib.tmp"; mv "$dir/rib.tmp" "$dir/rib" ;;
  status) echo "  BGP state:          Established" ;;
  routes) cat "$dir/rib" ;;
esac
`

const itToken = "it-token-0123456789abcdef0123456789abcdef"

type bgpIT struct {
	t    *testing.T
	srv  *httptest.Server
	dir  string
	conn clickhouse.Conn
}

func (b *bgpIT) do(method, path, body string, hdr map[string]string) (int, string) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.srv.URL+path, strings.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// write sends a state-changing request with a valid double-submit CSRF pair.
func (b *bgpIT) write(method, path, body string) (int, string) {
	return b.do(method, path, body, map[string]string{"Cookie": "as_stats_csrf=itcsrf", "X-CSRF-Token": "itcsrf"})
}

func (b *bgpIT) calls() string {
	data, _ := os.ReadFile(filepath.Join(b.dir, "calls"))
	return string(data)
}

func (b *bgpIT) rib() string {
	data, _ := os.ReadFile(filepath.Join(b.dir, "rib"))
	return string(data)
}

func (b *bgpIT) status() bgp.StatusReport {
	b.t.Helper()
	code, body := b.do(http.MethodGet, "/api/v1/bgp/status", "", nil)
	if code != http.StatusOK {
		b.t.Fatalf("status: %d %s", code, body)
	}
	var env struct {
		Data bgp.StatusReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		b.t.Fatal(err)
	}
	return env.Data
}

func (b *bgpIT) blocks(path string) []model.BGPBlock {
	b.t.Helper()
	code, body := b.do(http.MethodGet, path, "", nil)
	if code != http.StatusOK {
		b.t.Fatalf("%s: %d %s", path, code, body)
	}
	var env struct {
		Data []model.BGPBlock `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		b.t.Fatal(err)
	}
	return env.Data
}

func (b *bgpIT) waitFor(what string, cond func() bool) {
	b.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			b.t.Fatalf("timed out waiting for %s (calls:\n%s)", what, b.calls())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func scratchDB(t *testing.T, addr, db string) (*store.ClickHouseStore, clickhouse.Conn) {
	t.Helper()
	ctx := context.Background()
	admin, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: "default", Username: "default"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" SYNC"); err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC") })
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Auth: clickhouse.Auth{Database: db, Username: "default"}, MaxOpenConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migs, err := migrate.Load(schema.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	mstore, err := migrate.NewClickHouseStore(conn, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := (migrate.Runner{Store: mstore, Migrations: migs, Owner: "it"}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := mstore.DetectBaseline(ctx); err != nil || v != uint64(len(migs)) {
		t.Fatalf("DetectBaseline = %d, %v; want %d", v, err, len(migs))
	}
	s, err := store.NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: db, User: "default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, conn
}

// startAPI builds the real router with BGP on, the fake daemon as backend.
func startAPI(t *testing.T, ctx context.Context, s *store.ClickHouseStore, dir string, dryRun bool) *httptest.Server {
	t.Helper()
	script := filepath.Join(dir, "fakebgp.sh")
	raw := bgp.RawSettings{
		AnnounceCmd:      script + " " + dir + " announce {prefix} {communities} {next_hop}",
		WithdrawCmd:      script + " " + dir + " withdraw {prefix}",
		StatusCmd:        script + " " + dir + " status",
		RoutesCmd:        script + " " + dir + " routes",
		StatusCmdSet:     true,
		RoutesCmdSet:     true,
		Community:        "65535:666",
		ExtraCommunities: "65535:65281",
		NextHop:          "192.0.2.254",
		NextHopV6:        "2001:db8::fe",
		PeerAddress:      "192.0.2.2",
		PeerAS:           64500,
		LocalAS:          64501,
		DryRun:           dryRun,
		AllowedPrefixes:  "198.51.100.0/24, 2001:db8:100::/48",
		MaxActive:        3,
		AutoPerHour:      1,
	}
	settings, err := bgp.ParseSettings(raw)
	if err != nil {
		t.Fatal(err)
	}
	settings.Tick = 200 * time.Millisecond
	settings.ReconcileInterval = time.Second
	cfg := &config.APIConfig{
		AuthEnabled:   false,
		FeatureAlerts: true,
		BGPEnabled:    true,
		BGP:           settings,
		BGPAPIToken:   itToken,
	}
	srv := httptest.NewServer(NewRouter(ctx, s, cfg, "", nil))
	t.Cleanup(srv.Close)
	return srv
}

func TestIntegrationBGPBlackholing(t *testing.T) {
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
	t.Setenv("CLICKHOUSE_PASSWORD", "must-not-leak")
	t.Setenv("BGP_API_TOKEN", itToken)
	s, conn := scratchDB(t, addr, "asstats_it_bgp")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fakebgp.sh"), []byte(fakeDaemon), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &bgpIT{t: t, srv: startAPI(t, ctx, s, dir, false), dir: dir, conn: conn}
	b.waitFor("the service to load", func() bool { return b.status().Ready })

	// --- Feature discovery and status reflect the daemon.
	if code, body := b.do(http.MethodGet, "/api/v1/features", "", nil); code != 200 || !strings.Contains(body, `"bgp":true`) {
		t.Fatalf("features: %d %s", code, body)
	}
	st := b.status()
	if st.State != "established" || st.AllowedSource != "BGP_ALLOWED_PREFIXES" || st.MaxActiveBlocks != 3 || !st.IPv6Enabled {
		t.Errorf("status = %+v", st)
	}

	// --- CSRF and validation.
	if code, _ := b.do(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.10"}`, nil); code != http.StatusForbidden {
		t.Errorf("POST without CSRF: %d, want 403", code)
	}
	for body, want := range map[string]int{
		`{"ip":"203.0.113.10"}`:                          http.StatusUnprocessableEntity, // outside the allow-list
		`{"ip":"198.51.100.0/24"}`:                       http.StatusUnprocessableEntity, // shorter than /32
		`{"ip":"192.0.2.254"}`:                           http.StatusUnprocessableEntity, // next hop: protected and outside
		`{"ip":"198.51.100.10; touch /tmp/pwned"}`:       http.StatusBadRequest,
		`{"ip":"198.51.100.10","duration_minutes":-1}`:   http.StatusBadRequest,
		`{"ip":"198.51.100.10","duration_minutes":2000}`: http.StatusBadRequest,
		`not json`: http.StatusBadRequest,
	} {
		if code, resp := b.write(http.MethodPost, "/api/v1/bgp/blocks", body); code != want {
			t.Errorf("POST %s: %d %s, want %d", body, code, resp, want)
		}
	}
	if c := b.calls(); strings.Contains(c, "announce") {
		t.Fatalf("a refused request reached the daemon:\n%s", c)
	}

	// --- Manual block, idempotent.
	code, body := b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.10","duration_minutes":30,"description":"manual test"}`)
	if code != http.StatusCreated {
		t.Fatalf("block: %d %s", code, body)
	}
	if code, body := b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.10"}`); code != http.StatusOK {
		t.Errorf("second block: %d %s, want 200 (existing)", code, body)
	}
	calls := b.calls()
	if n := strings.Count(calls, "announce 198.51.100.10/32"); n != 1 {
		t.Errorf("%d announces for .10, want 1:\n%s", n, calls)
	}
	if !strings.Contains(calls, "announce 198.51.100.10/32 65535:666,65535:65281 192.0.2.254 secret=none token=none env_prefix=198.51.100.10/32") {
		t.Errorf("announce arguments / environment wrong:\n%s", calls)
	}
	// IPv6 host route.
	if code, body := b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"2001:db8:100::10"}`); code != http.StatusCreated {
		t.Fatalf("IPv6 block: %d %s", code, body)
	}
	if !strings.Contains(b.calls(), "announce 2001:db8:100::10/128 65535:666,65535:65281 2001:db8::fe") {
		t.Errorf("IPv6 announce wrong:\n%s", b.calls())
	}

	// --- Collector auto-block: token, hourly rail.
	if code, _ := b.do(http.MethodPost, bgp.InternalBlockPath, `{"ip":"198.51.100.20"}`, nil); code != http.StatusUnauthorized {
		t.Errorf("internal without token: %d", code)
	}
	if code, _ := b.do(http.MethodPost, bgp.InternalBlockPath, `{"ip":"198.51.100.20"}`, map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 40)}); code != http.StatusUnauthorized {
		t.Errorf("internal with a wrong token: %d", code)
	}
	client, err := bgp.NewClient(b.srv.URL, itToken)
	if err != nil {
		t.Fatal(err)
	}
	alertID := uuid.NewString()
	if created, err := client.AutoBlock(ctx, bgp.AutoBlockRequest{IP: "198.51.100.20", Description: "auto", AlertID: alertID, RuleName: "Critical inbound volume", MetricValue: 5e9, MetricType: "bps", Threshold: 2e9, TopSources: []string{"203.0.113.1"}}); err != nil || !created {
		t.Fatalf("auto block: %v %v", created, err)
	}
	// Max active is 3: .10, 2001:db8:100::10, .20.
	if code, body := b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.30"}`); code != http.StatusTooManyRequests || !strings.Contains(body, "BGP_MAX_ACTIVE_BLOCKS") {
		t.Errorf("fourth block: %d %s, want 429", code, body)
	}
	// Free one slot; the next auto block hits the hourly rail (1/h).
	if code, body := b.write(http.MethodDelete, "/api/v1/bgp/blocks/2001:db8:100::10", `{"description":"done"}`); code != http.StatusOK {
		t.Fatalf("unblock v6: %d %s", code, body)
	}
	if _, err := client.AutoBlock(ctx, bgp.AutoBlockRequest{IP: "198.51.100.21"}); !errors.Is(err, bgp.ErrRefused) || !strings.Contains(err.Error(), "429") {
		t.Errorf("second auto block in the hour: %v, want a 429 refusal", err)
	}
	if code, body := b.write(http.MethodDelete, "/api/v1/bgp/blocks/2001:db8:100::10", ``); code != http.StatusNotFound {
		t.Errorf("second unblock: %d %s, want 404", code, body)
	}

	active := b.blocks("/api/v1/bgp/blocks")
	byIP := map[string]model.BGPBlock{}
	for _, blk := range active {
		byIP[blk.IP] = blk
	}
	if len(active) != 2 || byIP["198.51.100.10"].Reason != "manual" || byIP["198.51.100.10"].BlockedBy != "anonymous" {
		t.Fatalf("active = %+v", active)
	}
	auto := byIP["198.51.100.20"]
	if auto.Reason != "auto_block" || auto.BlockedBy != "engine" || auto.AlertID != alertID || auto.RuleName != "Critical inbound volume" || len(auto.TopSources) != 1 || auto.DryRun {
		t.Errorf("auto block row = %+v", auto)
	}
	if exp := byIP["198.51.100.10"].ExpiresAt; exp == nil || exp.Sub(byIP["198.51.100.10"].BlockedAt) != 30*time.Minute || exp.Location() != time.UTC {
		t.Errorf("expires_at = %v (blocked_at %v)", exp, byIP["198.51.100.10"].BlockedAt)
	}

	// --- Block from an alert (and the 422 for a label-targeted alert).
	host := model.Alert{ID: uuid.NewString(), RuleID: uuid.NewString(), RuleName: "SYN flood", Severity: "critical", TriggeredAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), TargetIP: "198.51.100.50", MetricValue: 60000, Threshold: 50000, MetricType: "pps"}
	label := model.Alert{ID: uuid.NewString(), RuleID: uuid.NewString(), RuleName: "Link full", Severity: "warning", TriggeredAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(), TargetIP: "100::1234", MetricType: "percent"}
	for _, a := range []model.Alert{host, label} {
		if err := s.InsertAlert(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if code, body := b.write(http.MethodPost, "/api/v1/alerts/"+label.ID+"/block", `{}`); code != http.StatusUnprocessableEntity {
		t.Errorf("block label alert: %d %s, want 422", code, body)
	}
	code, body = b.write(http.MethodPost, "/api/v1/alerts/"+host.ID+"/block", `{"duration_minutes":15}`)
	if code != http.StatusCreated || !strings.Contains(body, `"reason":"alert"`) || !strings.Contains(body, host.ID) || !strings.Contains(body, `"rule_name":"SYN flood"`) {
		t.Fatalf("block from alert: %d %s", code, body)
	}

	// --- Reconciliation: the daemon loses a route; an unknown route appears.
	if err := os.WriteFile(filepath.Join(dir, "rib"), []byte("198.51.100.10/32\n198.51.100.50/32\n198.51.100.99/32\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.waitFor("the lost route to be re-announced", func() bool { return strings.Contains(b.rib(), "198.51.100.20/32") })
	b.waitFor("the orphan to be reported", func() bool {
		o := b.status().RIB.Orphans
		return len(o) == 1 && o[0] == "198.51.100.99/32"
	})
	if !strings.Contains(b.rib(), "198.51.100.99/32") {
		t.Error("an orphan was withdrawn automatically")
	}
	if code, body := b.write(http.MethodDelete, "/api/v1/bgp/blocks/198.51.100.99", ``); code != http.StatusOK {
		t.Errorf("withdraw orphan: %d %s", code, body)
	}
	if strings.Contains(b.rib(), "198.51.100.99/32") {
		t.Error("orphan still in the RIB")
	}

	// Free a slot (max active is 3: .10, .20, .50).
	if code, body := b.write(http.MethodDelete, "/api/v1/bgp/blocks/198.51.100.50", `{"description":"mitigated"}`); code != http.StatusOK {
		t.Fatalf("unblock .50: %d %s", code, body)
	}

	// --- Daemon failure: 502, row marked failed, nothing active.
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, body = b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.40"}`)
	if code != http.StatusBadGateway || strings.Contains(body, "connection refused") {
		t.Errorf("block with a failing daemon: %d %s (502, without the command output)", code, body)
	}
	if err := os.Remove(filepath.Join(dir, "fail")); err != nil {
		t.Fatal(err)
	}

	// --- Restart: stop this API, expire .10 behind its back, clear the RIB
	// (the daemon restarted too) and start a new one on the same table.
	cancel()
	b.srv.Close()
	if err := conn.Exec(context.Background(), `ALTER TABLE bgp_blocks UPDATE expires_at = now() - INTERVAL 1 MINUTE WHERE ip = toIPv6('198.51.100.10') AND status = 'active' SETTINGS mutations_sync = 1`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rib"), []byte("198.51.100.10/32\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	b.srv = startAPI(t, ctx2, s, dir, false)
	b.waitFor("the restarted service to load", func() bool { return b.status().Ready })
	b.waitFor("the restore", func() bool {
		r := b.rib()
		return strings.Contains(r, "198.51.100.20/32") && !strings.Contains(r, "198.51.100.10/32")
	})
	history := b.blocks("/api/v1/bgp/blocks/history?limit=50")
	statusOf := map[string]model.BGPBlock{}
	for _, h := range history {
		statusOf[h.IP] = h
	}
	if h := statusOf["198.51.100.10"]; h.Status != "withdrawn" || h.UnblockedBy != "system" || h.UnblockedAt == nil {
		t.Errorf(".10 after restart = %+v", h)
	}
	if h := statusOf["2001:db8:100::10"]; h.Status != "withdrawn" || h.UnblockedBy != "anonymous" || h.UnblockReason != "done" || h.PrefixLen != 128 {
		t.Errorf("v6 history = %+v", h)
	}
	if h := statusOf["198.51.100.40"]; h.Status != "failed" || !strings.Contains(h.UnblockReason, "announce failed") {
		t.Errorf(".40 history = %+v", h)
	}
	if n := len(b.blocks("/api/v1/bgp/blocks")); n != 1 { // .20
		t.Errorf("%d active after restart, want 1", n)
	}
	// The hourly auto rail survived the restart.
	client2, _ := bgp.NewClient(b.srv.URL, itToken)
	if _, err := client2.AutoBlock(context.Background(), bgp.AutoBlockRequest{IP: "198.51.100.22"}); !errors.Is(err, bgp.ErrRefused) {
		t.Errorf("auto block after restart: %v, want the hourly rail", err)
	}

	// --- Audit trail.
	var audits uint64
	if err := conn.QueryRow(context.Background(), `SELECT count() FROM audit_log WHERE action IN ('bgp_block', 'bgp_unblock', 'bgp_expire')`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits < 8 {
		t.Errorf("%d BGP audit rows, want every block / unblock / refusal / expiry", audits)
	}
	var who string
	if err := conn.QueryRow(context.Background(), `SELECT user_email FROM audit_log WHERE action = 'bgp_block' AND result = 'success' AND resource = 'bgp:198.51.100.20/32'`).Scan(&who); err != nil || who != "engine" {
		t.Errorf("auto-block audit row: %q %v", who, err)
	}

	// --- Dry run: same table, a dry-run API runs no command for new blocks.
	cancel2()
	b.srv.Close()
	ctx3, cancel3 := context.WithCancel(context.Background())
	defer cancel3()
	before := b.calls()
	b.srv = startAPI(t, ctx3, s, dir, true)
	b.waitFor("the dry-run service to load", func() bool { return b.status().Ready })
	code, body = b.write(http.MethodPost, "/api/v1/bgp/blocks", `{"ip":"198.51.100.60"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"dry_run":true`) {
		t.Fatalf("dry-run block: %d %s", code, body)
	}
	if strings.Contains(strings.TrimPrefix(b.calls(), before), "198.51.100.60") {
		t.Errorf("dry run reached the daemon:\n%s", b.calls())
	}
	if st := b.status(); !st.DryRun || st.ActiveBlocks != 2 || st.RoutesAnnounced != 1 {
		t.Errorf("dry-run status = %+v", st)
	}
}
