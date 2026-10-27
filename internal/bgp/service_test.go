package bgp

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// memStore is an in-memory bgp_blocks.
type memStore struct {
	mu      sync.Mutex
	rows    []model.BGPBlock
	failAll error // every call fails with it
	failSet error // SetBlockStatus fails with it
}

func (m *memStore) ListActiveBlocks(context.Context) ([]model.BGPBlock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll != nil {
		return nil, m.failAll
	}
	var out []model.BGPBlock
	for _, r := range m.rows {
		if r.Status == "active" {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) ListBlocksSince(_ context.Context, since time.Time) ([]model.BGPBlock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll != nil {
		return nil, m.failAll
	}
	var out []model.BGPBlock
	for _, r := range m.rows {
		if !r.BlockedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) InsertBlock(_ context.Context, b model.BGPBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll != nil {
		return m.failAll
	}
	m.rows = append(m.rows, b)
	return nil
}

func (m *memStore) SetBlockStatus(_ context.Context, ids []string, status, by, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failAll != nil {
		return m.failAll
	}
	if m.failSet != nil {
		return m.failSet
	}
	for i := range m.rows {
		for _, id := range ids {
			if m.rows[i].ID == id && m.rows[i].Status == "active" {
				m.rows[i].Status, m.rows[i].UnblockedBy, m.rows[i].UnblockReason = status, by, reason
				t := at
				m.rows[i].UnblockedAt = &t
			}
		}
	}
	return nil
}

func (m *memStore) byStatus(status string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.rows {
		if r.Status == status {
			out = append(out, r.IP)
		}
	}
	sort.Strings(out)
	return out
}

// fakeDaemon records calls and keeps a RIB.
type fakeDaemon struct {
	mu        sync.Mutex
	rib       map[netip.Prefix]bool
	calls     []string
	fail      error // announce / withdraw fail with it
	state     string
	noRoutes  bool
	noStatus  bool
	routesErr error
}

func newFakeDaemon() *fakeDaemon {
	return &fakeDaemon{rib: map[netip.Prefix]bool{}, state: "established"}
}

func (d *fakeDaemon) Announce(_ context.Context, p netip.Prefix) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, "announce "+p.String())
	if d.fail != nil {
		return d.fail
	}
	d.rib[p] = true
	return nil
}

func (d *fakeDaemon) Withdraw(_ context.Context, p netip.Prefix) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, "withdraw "+p.String())
	if d.fail != nil {
		return d.fail
	}
	delete(d.rib, p)
	return nil
}

func (d *fakeDaemon) Status(context.Context) (SessionState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.noStatus {
		return SessionState{State: "unknown"}, ErrUnsupported
	}
	return SessionState{State: d.state}, nil
}

func (d *fakeDaemon) Routes(context.Context) ([]netip.Prefix, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.noRoutes {
		return nil, ErrUnsupported
	}
	if d.routesErr != nil {
		return nil, d.routesErr
	}
	var out []netip.Prefix
	for p := range d.rib {
		out = append(out, p)
	}
	return out, nil
}

func (d *fakeDaemon) callList() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func (d *fakeDaemon) has(p string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rib[netip.MustParsePrefix(p)]
}

type memAudit struct {
	mu   sync.Mutex
	rows []model.AuditLogEntry
}

func (a *memAudit) WriteAuditLog(_ context.Context, e model.AuditLogEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, e)
	return nil
}

func (a *memAudit) actions() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, r := range a.rows {
		out = append(out, r.Action+":"+r.Result)
	}
	return strings.Join(out, " ")
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type harness struct {
	svc   *Service
	store *memStore
	d     *fakeDaemon
	audit *memAudit
	clk   *clock
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	h := &harness{store: &memStore{}, d: newFakeDaemon(), audit: &memAudit{}, clk: &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}}
	opts := Options{
		Live:   h.d,
		Policy: testPolicy(),
		Limits: Limits{MaxActive: 3, AutoPerHour: 2, DefaultDuration: time.Hour, MaxDuration: 24 * time.Hour},
		Store:  h.store, Audit: h.audit,
		ReconcileInterval: time.Minute,
		Communities:       []string{"65535:666", "65535:65281"},
		NextHopV4:         "192.0.2.254",
		Now:               h.clk.now,
	}
	if mutate != nil {
		mutate(&opts)
	}
	h.svc = NewService(opts)
	h.svc.Reconcile(context.Background()) // load
	return h
}

func (h *harness) block(t *testing.T, target string, src Source) (model.BGPBlock, bool, error) {
	t.Helper()
	return h.svc.Block(context.Background(), BlockRequest{Target: target, Source: src, By: "tester"})
}

func TestServiceBlockAndIdempotency(t *testing.T) {
	h := newHarness(t, nil)
	b, created, err := h.block(t, "198.51.100.10", SourceManual)
	if err != nil || !created {
		t.Fatalf("Block = %v, %v", created, err)
	}
	if b.IP != "198.51.100.10" || b.PrefixLen != 32 || b.Status != "active" || b.ExpiresAt == nil || !b.ExpiresAt.Equal(h.clk.now().Add(time.Hour)) {
		t.Errorf("block = %+v", b)
	}
	if b.Community != "65535:666,65535:65281" || b.NextHop != "192.0.2.254" || b.BlockedBy != "tester" {
		t.Errorf("block attributes = %+v", b)
	}
	if !h.d.has("198.51.100.10/32") {
		t.Fatal("route not announced")
	}
	again, created, err := h.block(t, "::ffff:198.51.100.10", SourceAuto)
	if err != nil || created || again.ID != b.ID {
		t.Fatalf("second Block = %+v, %v, %v; want the existing block", again, created, err)
	}
	if n := len(h.d.callList()); n != 1 {
		t.Errorf("%d daemon calls, want 1", n)
	}
	if got := h.store.byStatus("active"); len(got) != 1 {
		t.Errorf("active rows = %v", got)
	}
	if !strings.Contains(h.audit.actions(), "bgp_block:success") {
		t.Errorf("audit = %s", h.audit.actions())
	}
}

func TestServiceBlockRejections(t *testing.T) {
	h := newHarness(t, nil)
	for target, check := range map[string]func(error) bool{
		"203.0.113.1":     func(err error) bool { var pe *PolicyError; return errors.As(err, &pe) },
		"198.51.100.1":    func(err error) bool { var pe *PolicyError; return errors.As(err, &pe) }, // protected
		"198.51.100.0/24": func(err error) bool { var pe *PolicyError; return errors.As(err, &pe) },
		"not an ip":       func(err error) bool { return errors.Is(err, ErrInvalidRequest) },
	} {
		if _, _, err := h.block(t, target, SourceManual); !check(err) {
			t.Errorf("Block(%q) = %v", target, err)
		}
	}
	if _, _, err := h.svc.Block(context.Background(), BlockRequest{Target: "198.51.100.10", Duration: 25 * time.Hour}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("25h block = %v, want ErrInvalidRequest", err)
	}
	if _, _, err := h.svc.Block(context.Background(), BlockRequest{Target: "198.51.100.10", Duration: time.Second}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("1s block = %v, want ErrInvalidRequest", err)
	}
	if calls := h.d.callList(); len(calls) != 0 {
		t.Errorf("rejected requests reached the daemon: %v", calls)
	}
	if len(h.store.rows) != 0 {
		t.Errorf("rejected requests were stored: %+v", h.store.rows)
	}
	if !strings.Contains(h.audit.actions(), "bgp_block:denied") {
		t.Errorf("policy refusals are not audited: %s", h.audit.actions())
	}
}

func TestServiceRails(t *testing.T) {
	h := newHarness(t, nil)
	// Auto: 2 per hour.
	for _, ip := range []string{"198.51.100.20", "198.51.100.21"} {
		if _, _, err := h.block(t, ip, SourceAuto); err != nil {
			t.Fatalf("auto %s: %v", ip, err)
		}
	}
	var le *LimitError
	if _, _, err := h.block(t, "198.51.100.22", SourceAuto); !errors.As(err, &le) || !strings.Contains(err.Error(), "BGP_AUTO_BLOCK_MAX_PER_HOUR") {
		t.Fatalf("third auto block = %v, want the hourly rail", err)
	}
	// Manual blocks are not bound by the hourly rail, but by max active (3).
	if _, _, err := h.block(t, "198.51.100.30", SourceManual); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.block(t, "198.51.100.31", SourceManual); !errors.As(err, &le) || !strings.Contains(err.Error(), "BGP_MAX_ACTIVE_BLOCKS") {
		t.Fatalf("fourth block = %v, want the max-active rail", err)
	}
	// An hour later the hourly budget is back, max active still applies.
	h.clk.add(61 * time.Minute)
	h.svc.Reconcile(context.Background()) // all three expire (1h default)
	if got := h.store.byStatus("active"); len(got) != 0 {
		t.Fatalf("active after expiry = %v", got)
	}
	if _, _, err := h.block(t, "198.51.100.22", SourceAuto); err != nil {
		t.Fatalf("auto block after an hour: %v", err)
	}
}

// TestServiceAutoRailSurvivesRestart: the hourly budget is rebuilt from
// bgp_blocks, so restarting the API does not reset it.
func TestServiceAutoRailSurvivesRestart(t *testing.T) {
	h := newHarness(t, nil)
	for _, ip := range []string{"198.51.100.20", "198.51.100.21"} {
		if _, _, err := h.block(t, ip, SourceAuto); err != nil {
			t.Fatal(err)
		}
	}
	h2 := &harness{store: h.store, d: h.d, audit: h.audit, clk: h.clk}
	h2.svc = NewService(Options{Live: h.d, Policy: testPolicy(), Limits: Limits{MaxActive: 10, AutoPerHour: 2}, Store: h.store, Now: h.clk.now})
	h2.svc.Reconcile(context.Background())
	var le *LimitError
	if _, _, err := h2.block(t, "198.51.100.22", SourceAuto); !errors.As(err, &le) {
		t.Fatalf("auto block after restart = %v, want the hourly rail", err)
	}
}

func TestServiceAnnounceFailure(t *testing.T) {
	h := newHarness(t, nil)
	h.d.fail = &CommandError{Argv: []string{"gobgp"}, Err: errors.New("exit status 1"), Output: "connection refused"}
	_, _, err := h.block(t, "198.51.100.10", SourceManual)
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("Block with a dead daemon = %v", err)
	}
	if got := h.store.byStatus("failed"); len(got) != 1 {
		t.Fatalf("failed rows = %v (the row must not stay active)", got)
	}
	if st := h.svc.Status(context.Background(), false); st.ActiveBlocks != 0 {
		t.Errorf("active blocks = %d", st.ActiveBlocks)
	}
	if !strings.Contains(h.audit.actions(), "bgp_block:error") {
		t.Errorf("audit = %s", h.audit.actions())
	}
}

func TestServiceExpiryAndRetry(t *testing.T) {
	h := newHarness(t, nil)
	if _, _, err := h.svc.Block(context.Background(), BlockRequest{Target: "198.51.100.10", Duration: 10 * time.Minute, By: "x"}); err != nil {
		t.Fatal(err)
	}
	h.clk.add(9 * time.Minute)
	h.svc.Reconcile(context.Background())
	if !h.d.has("198.51.100.10/32") {
		t.Fatal("withdrawn before expiry")
	}
	// Daemon down at expiry: the route stays, the row stays active, retried.
	h.clk.add(2 * time.Minute)
	h.d.fail = errors.New("daemon down")
	h.d.routesErr = errors.New("daemon down")
	h.svc.Reconcile(context.Background())
	if got := h.store.byStatus("active"); len(got) != 1 {
		t.Fatalf("active rows after a failed withdraw = %v", got)
	}
	h.d.fail, h.d.routesErr = nil, nil
	h.svc.Reconcile(context.Background())
	if h.d.has("198.51.100.10/32") || len(h.store.byStatus("withdrawn")) != 1 {
		t.Fatalf("not withdrawn on retry: rib=%v rows=%+v", h.d.rib, h.store.rows)
	}
	if r := h.store.rows[0]; r.UnblockedBy != "system" || !strings.Contains(r.UnblockReason, "expired") {
		t.Errorf("withdrawn row = %+v", r)
	}
	if !strings.Contains(h.audit.actions(), "bgp_expire:success") {
		t.Errorf("audit = %s", h.audit.actions())
	}
}

// TestServiceWithdrawStoreFailure: the route is gone but the row could not
// be updated. The entry is kept and retried; it is never re-announced.
func TestServiceWithdrawStoreFailure(t *testing.T) {
	h := newHarness(t, nil)
	if _, _, err := h.block(t, "198.51.100.10", SourceManual); err != nil {
		t.Fatal(err)
	}
	h.store.failSet = errors.New("clickhouse down")
	if err := h.svc.Unblock(context.Background(), netip.MustParsePrefix("198.51.100.10/32"), "op", "", "done"); err != nil {
		t.Fatalf("Unblock = %v (the route is withdrawn: success)", err)
	}
	h.clk.add(2 * time.Minute)
	h.svc.Reconcile(context.Background())
	if h.d.has("198.51.100.10/32") {
		t.Fatal("re-announced a withdrawn block")
	}
	h.store.failSet = nil
	h.svc.Reconcile(context.Background())
	if got := h.store.byStatus("withdrawn"); len(got) != 1 {
		t.Fatalf("withdrawal not persisted on retry: %+v", h.store.rows)
	}
	if r := h.store.rows[0]; r.UnblockedBy != "op" || r.UnblockReason != "done" {
		t.Errorf("row = %+v", r)
	}
	if st := h.svc.Status(context.Background(), false); st.ActiveBlocks != 0 {
		t.Errorf("active = %d", st.ActiveBlocks)
	}
}

func TestServiceUnblock(t *testing.T) {
	h := newHarness(t, nil)
	if _, _, err := h.block(t, "198.51.100.10", SourceManual); err != nil {
		t.Fatal(err)
	}
	p := netip.MustParsePrefix("198.51.100.10/32")
	if err := h.svc.Unblock(context.Background(), p, "op", "1.2.3.4", "false positive"); err != nil {
		t.Fatal(err)
	}
	if h.d.has(p.String()) || len(h.store.byStatus("withdrawn")) != 1 {
		t.Fatal("not withdrawn")
	}
	if err := h.svc.Unblock(context.Background(), p, "op", "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Unblock = %v, want ErrNotFound", err)
	}
	// Daemon refuses the withdraw and still carries the route: the block stays.
	if _, _, err := h.block(t, "198.51.100.11", SourceManual); err != nil {
		t.Fatal(err)
	}
	h.d.fail = errors.New("refused")
	if err := h.svc.Unblock(context.Background(), netip.MustParsePrefix("198.51.100.11/32"), "op", "", ""); err == nil {
		t.Fatal("Unblock succeeded although the daemon refused and still carries the route")
	}
	if got := h.store.byStatus("active"); len(got) != 1 {
		t.Errorf("active rows = %v", got)
	}
	// Daemon refuses because it no longer has the route (restarted): done.
	h.d.mu.Lock()
	delete(h.d.rib, netip.MustParsePrefix("198.51.100.11/32"))
	h.d.mu.Unlock()
	if err := h.svc.Unblock(context.Background(), netip.MustParsePrefix("198.51.100.11/32"), "op", "", ""); err != nil {
		t.Fatalf("Unblock of a route the daemon lost = %v", err)
	}
}

// TestServiceRestore: a restart takes over the active rows — unexpired ones
// re-announced with their original expiry, expired ones withdrawn, dry-run
// ones never sent to the real daemon, legacy duplicates marked together.
func TestServiceRestore(t *testing.T) {
	clk := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	exp := func(d time.Duration) *time.Time { t := clk.t.Add(d); return &t }
	st := &memStore{rows: []model.BGPBlock{
		{ID: "a", IP: "198.51.100.10", PrefixLen: 32, Status: "active", BlockedAt: clk.t.Add(-time.Hour), ExpiresAt: exp(30 * time.Minute)},
		{ID: "a2", IP: "198.51.100.10", PrefixLen: 32, Status: "active", BlockedAt: clk.t.Add(-time.Hour), ExpiresAt: exp(30 * time.Minute)},
		{ID: "b", IP: "198.51.100.11", PrefixLen: 32, Status: "active", BlockedAt: clk.t.Add(-2 * time.Hour), ExpiresAt: exp(-time.Minute)},
		{ID: "c", IP: "198.51.100.12", PrefixLen: 32, Status: "active", BlockedAt: clk.t, ExpiresAt: exp(time.Hour), DryRun: true},
		{ID: "d", IP: "2001:db8:100::1", PrefixLen: 32, Status: "active", BlockedAt: clk.t, ExpiresAt: exp(time.Hour)}, // legacy IPv6 /32 = host
	}}
	d := newFakeDaemon()
	d.rib[netip.MustParsePrefix("198.51.100.11/32")] = true // survived from before
	svc := NewService(Options{Live: d, Policy: testPolicy(), Limits: Limits{MaxActive: 10, AutoPerHour: 1}, Store: st, Now: clk.now, NextHopV6: "2001:db8::fe"})

	if _, _, err := svc.Block(context.Background(), BlockRequest{Target: "198.51.100.30"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Block before load = %v, want ErrNotReady", err)
	}
	svc.Reconcile(context.Background())

	if !d.has("198.51.100.10/32") || !d.has("2001:db8:100::1/128") {
		t.Errorf("unexpired blocks not re-announced: %v", d.callList())
	}
	if d.has("198.51.100.11/32") {
		t.Error("expired block not withdrawn")
	}
	for _, c := range d.callList() {
		if strings.Contains(c, "198.51.100.12") {
			t.Errorf("dry-run block reached the real daemon: %s", c)
		}
	}
	if got := st.byStatus("withdrawn"); len(got) != 1 || got[0] != "198.51.100.11" {
		t.Errorf("withdrawn rows = %v", got)
	}
	// The original expiry is kept.
	clk.add(31 * time.Minute)
	svc.Reconcile(context.Background())
	if d.has("198.51.100.10/32") {
		t.Error("restored block outlived its original expiry")
	}
	if got := st.byStatus("withdrawn"); len(got) != 3 { // b, a, a2
		t.Errorf("withdrawn rows = %v (legacy duplicate rows must be closed too)", got)
	}
}

func TestServiceLoadRetriesUntilTheStoreAnswers(t *testing.T) {
	st := &memStore{failAll: errors.New("connection refused")}
	svc := NewService(Options{Live: newFakeDaemon(), Policy: testPolicy(), Limits: Limits{MaxActive: 10}, Store: st})
	svc.Reconcile(context.Background())
	rep := svc.Status(context.Background(), false)
	if rep.Ready || !strings.Contains(rep.LoadError, "connection refused") {
		t.Fatalf("status = %+v", rep)
	}
	if _, _, err := svc.Block(context.Background(), BlockRequest{Target: "198.51.100.10"}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Block = %v", err)
	}
	st.mu.Lock()
	st.failAll = nil
	st.mu.Unlock()
	svc.Reconcile(context.Background())
	if _, _, err := svc.Block(context.Background(), BlockRequest{Target: "198.51.100.10"}); err != nil {
		t.Fatalf("Block after recovery = %v", err)
	}
}

func TestServiceRIBReconciliation(t *testing.T) {
	h := newHarness(t, nil)
	if _, _, err := h.block(t, "198.51.100.10", SourceManual); err != nil {
		t.Fatal(err)
	}
	// The daemon restarted and lost the route; someone added one by hand.
	h.d.mu.Lock()
	h.d.rib = map[netip.Prefix]bool{netip.MustParsePrefix("198.51.100.99/32"): true}
	h.d.mu.Unlock()
	h.clk.add(2 * time.Minute)
	h.svc.Reconcile(context.Background())
	if !h.d.has("198.51.100.10/32") {
		t.Error("missing route not re-announced")
	}
	rep := h.svc.Status(context.Background(), false)
	if !rep.RIB.Supported || len(rep.RIB.Orphans) != 1 || rep.RIB.Orphans[0] != "198.51.100.99/32" || len(rep.RIB.Missing) != 0 {
		t.Fatalf("rib report = %+v", rep.RIB)
	}
	if !h.d.has("198.51.100.99/32") {
		t.Error("an orphan was withdrawn automatically")
	}
	// An operator can withdraw the orphan explicitly.
	if err := h.svc.Unblock(context.Background(), netip.MustParsePrefix("198.51.100.99/32"), "op", "", "cleanup"); err != nil {
		t.Fatal(err)
	}
	if h.d.has("198.51.100.99/32") {
		t.Error("orphan not withdrawn")
	}
	if rep := h.svc.Status(context.Background(), false); len(rep.RIB.Orphans) != 0 {
		t.Errorf("orphans after cleanup = %v", rep.RIB.Orphans)
	}
}

// TestServiceSessionRecovery: without a routes command, a session coming
// back to established triggers a re-announce of every live block.
func TestServiceSessionRecovery(t *testing.T) {
	h := newHarness(t, nil)
	h.d.noRoutes = true
	if _, _, err := h.block(t, "198.51.100.10", SourceManual); err != nil {
		t.Fatal(err)
	}
	h.d.mu.Lock()
	h.d.state = "idle"
	h.d.rib = map[netip.Prefix]bool{}
	h.d.mu.Unlock()
	h.clk.add(2 * time.Minute)
	h.svc.Reconcile(context.Background())
	if rep := h.svc.Status(context.Background(), false); rep.State != "idle" || rep.RIB.Supported {
		t.Fatalf("status = %+v", rep)
	}
	h.d.mu.Lock()
	h.d.state = "established"
	h.d.mu.Unlock()
	h.clk.add(2 * time.Minute)
	h.svc.Reconcile(context.Background()) // sees established, marks pending
	h.svc.Reconcile(context.Background()) // re-announces
	if !h.d.has("198.51.100.10/32") {
		t.Fatalf("not re-announced after the session came back: %v", h.d.callList())
	}
}

func TestServiceDryRun(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.DryRun = true })
	b, created, err := h.block(t, "198.51.100.10", SourceManual)
	if err != nil || !created || !b.DryRun {
		t.Fatalf("dry-run Block = %+v, %v, %v", b, created, err)
	}
	if calls := h.d.callList(); len(calls) != 0 {
		t.Fatalf("dry run ran daemon commands: %v", calls)
	}
	rep := h.svc.Status(context.Background(), false)
	if !rep.DryRun || rep.ActiveBlocks != 1 || rep.RoutesAnnounced != 0 {
		t.Errorf("status = %+v", rep)
	}
	h.clk.add(61 * time.Minute)
	h.svc.Reconcile(context.Background())
	if got := h.store.byStatus("withdrawn"); len(got) != 1 {
		t.Errorf("dry-run block did not expire: %+v", h.store.rows)
	}
	if calls := h.d.callList(); len(calls) != 0 {
		t.Errorf("dry run ran daemon commands: %v", calls)
	}
}

func TestServiceIPv6NeedsNextHop(t *testing.T) {
	tpl, _ := ParseTemplate("true {prefix} {next_hop}")
	cd := NewCommandDaemon(CommandConfig{Announce: tpl, Withdraw: tpl, NextHopV4: "192.0.2.254"})
	h := newHarness(t, func(o *Options) { o.Live = cd; o.DryRun = true })
	var pe *PolicyError
	if _, _, err := h.block(t, "2001:db8:100::1", SourceManual); !errors.As(err, &pe) {
		t.Fatalf("IPv6 block without BGP_NEXT_HOP_V6 = %v, want a PolicyError even in dry-run", err)
	}
}
