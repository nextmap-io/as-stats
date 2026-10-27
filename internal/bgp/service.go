package bgp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

// Errors returned by Service. Handlers map them to HTTP statuses:
// ErrInvalidRequest 400, *PolicyError 422, *LimitError 429, ErrNotFound 404,
// ErrNotReady 503, *CommandError 502.
var (
	ErrInvalidRequest = errors.New("invalid request")
	ErrNotFound       = errors.New("no active block for this target")
	ErrNotReady       = errors.New("BGP state not loaded from ClickHouse yet; retrying")
)

// LimitError is a block refused by a rate or size limit.
type LimitError struct{ Reason string }

func (e *LimitError) Error() string { return e.Reason }

// Source tells who asked for a block.
type Source string

const (
	SourceManual Source = "manual"     // BGP page / POST /bgp/blocks
	SourceAlert  Source = "alert"      // Block button on an alert (POST /alerts/{id}/block)
	SourceAuto   Source = "auto_block" // alert engine, rule action auto_block
)

// BlockRequest is one request to blackhole a target.
type BlockRequest struct {
	Target      string        // address or prefix
	Duration    time.Duration // 0 = default
	Source      Source
	Description string
	By          string // user e-mail, "engine", …
	ClientIP    string // for the audit log
	AlertID     string
	RuleName    string
	MetricValue float64
	MetricType  string
	Threshold   float64
	TopSources  []string
}

// Store persists blocks (bgp_blocks). Implemented by *store.ClickHouseStore.
type Store interface {
	ListActiveBlocks(ctx context.Context) ([]model.BGPBlock, error)
	ListBlocksSince(ctx context.Context, since time.Time) ([]model.BGPBlock, error)
	InsertBlock(ctx context.Context, b model.BGPBlock) error
	SetBlockStatus(ctx context.Context, ids []string, status, by, reason string, at time.Time) error
}

// AuditRecorder writes audit_log rows. Implemented by *store.ClickHouseStore.
type AuditRecorder interface {
	WriteAuditLog(ctx context.Context, e model.AuditLogEntry) error
}

// Limits are the rails against a runaway blackhole.
type Limits struct {
	MaxActive       int           // BGP_MAX_ACTIVE_BLOCKS (all sources)
	AutoPerHour     int           // BGP_AUTO_BLOCK_MAX_PER_HOUR (alert engine only)
	DefaultDuration time.Duration // BGP_DEFAULT_DURATION
	MaxDuration     time.Duration // BGP_MAX_DURATION
}

// Options configures a Service.
type Options struct {
	Live              Daemon // the real daemon (CommandDaemon)
	DryRun            bool   // BGP_DRY_RUN: new blocks go to a DryRunDaemon
	Policy            Policy
	Limits            Limits
	Store             Store
	Audit             AuditRecorder // nil = no service-level audit rows
	Tick              time.Duration // expiry / retry cadence (default 15s)
	ReconcileInterval time.Duration // status + RIB check cadence (default 60s)
	Communities       []string
	NextHopV4         string
	NextHopV6         string
	PeerAddress       string
	PeerAS, LocalAS   uint32
	Now               func() time.Time // tests
}

type entry struct {
	block     model.BGPBlock
	prefix    netip.Prefix
	ids       []string // every bgp_blocks row for this prefix (legacy duplicates included)
	pending   bool     // announce not confirmed by the daemon: retried every tick
	withdrawn bool     // gone from the daemon, status update to the store still due
	wdBy      string
	wdReason  string
	wdAt      time.Time
}

// Service owns the blackhole state of the API process: bgp_blocks rows, the
// daemon, the rails, expiry and reconciliation. It is the only writer of
// bgp_blocks (the collector's alert engine goes through the API).
type Service struct {
	opts Options
	dry  *DryRunDaemon
	now  func() time.Time

	opMu sync.Mutex // serialises every operation that runs a daemon command

	mu          sync.Mutex // guards the fields below
	loaded      bool
	loadErr     string
	active      map[netip.Prefix]*entry
	autoTimes   []time.Time
	session     SessionState
	sessionErr  string
	sessionAt   time.Time
	sessionSeen bool
	rib         RIBReport
	lastRecon   time.Time
	lastReconOK time.Time
}

// NewService builds a Service. Call Run to load state and start the
// expiry / reconciliation loop.
func NewService(opts Options) *Service {
	if opts.Tick <= 0 {
		opts.Tick = 15 * time.Second
	}
	if opts.ReconcileInterval <= 0 {
		opts.ReconcileInterval = 60 * time.Second
	}
	if opts.Limits.DefaultDuration <= 0 {
		opts.Limits.DefaultDuration = time.Hour
	}
	if opts.Limits.MaxDuration <= 0 {
		opts.Limits.MaxDuration = 24 * time.Hour
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		opts:   opts,
		dry:    NewDryRunDaemon(),
		now:    now,
		active: map[netip.Prefix]*entry{},
	}
}

// DryRun reports whether new blocks are dry-run.
func (s *Service) DryRun() bool { return s.opts.DryRun }

func (s *Service) daemonFor(dry bool) Daemon {
	if dry || s.opts.Live == nil {
		return s.dry
	}
	return s.opts.Live
}

// opCtx detaches a request context: once we start changing the daemon and
// the table, a client disconnect must not leave the two half-updated.
func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
}

// Block validates a request against the rails and announces it. It returns
// the block and whether it was created (false = the target was already
// blocked, nothing changed).
func (s *Service) Block(ctx context.Context, req BlockRequest) (model.BGPBlock, bool, error) {
	src := string(req.Source)
	p, err := ParseTarget(req.Target)
	if err != nil {
		metrics.BGPBlockRequests.WithLabelValues(src, "rejected").Inc()
		return model.BGPBlock{}, false, err
	}
	dur := req.Duration
	if dur == 0 {
		dur = s.opts.Limits.DefaultDuration
	}
	if dur < time.Minute || dur > s.opts.Limits.MaxDuration {
		metrics.BGPBlockRequests.WithLabelValues(src, "rejected").Inc()
		return model.BGPBlock{}, false, fmt.Errorf("%w: duration must be between 1 minute and %s", ErrInvalidRequest, s.opts.Limits.MaxDuration)
	}
	err = s.opts.Policy.Check(p)
	if cd, ok := s.opts.Live.(*CommandDaemon); ok && err == nil {
		err = cd.checkNextHop(p) // IPv6 without BGP_NEXT_HOP_V6, even in dry-run
	}
	if err != nil {
		metrics.BGPBlockRequests.WithLabelValues(src, "rejected").Inc()
		s.audit(ctx, "bgp_block", req.By, req.ClientIP, p.String(), req, "denied", err.Error())
		return model.BGPBlock{}, false, err
	}

	ctx, cancel := opCtx(ctx)
	defer cancel()
	s.opMu.Lock()
	defer s.opMu.Unlock()

	now := s.now()
	s.mu.Lock()
	if !s.loaded {
		s.mu.Unlock()
		metrics.BGPBlockRequests.WithLabelValues(src, "error").Inc()
		return model.BGPBlock{}, false, ErrNotReady
	}
	if e, ok := s.active[p]; ok && !e.withdrawn {
		b := e.block
		s.mu.Unlock()
		metrics.BGPBlockRequests.WithLabelValues(src, "existing").Inc()
		return b, false, nil
	}
	if n := s.countActiveLocked(); n >= s.opts.Limits.MaxActive {
		s.mu.Unlock()
		metrics.BGPBlockRequests.WithLabelValues(src, "limited").Inc()
		err := &LimitError{fmt.Sprintf("%d blocks are already active (BGP_MAX_ACTIVE_BLOCKS=%d): withdraw one first", n, s.opts.Limits.MaxActive)}
		s.audit(ctx, "bgp_block", req.By, req.ClientIP, p.String(), req, "denied", err.Error())
		return model.BGPBlock{}, false, err
	}
	if req.Source == SourceAuto {
		recent := s.pruneAutoLocked(now)
		if recent >= s.opts.Limits.AutoPerHour {
			s.mu.Unlock()
			metrics.BGPBlockRequests.WithLabelValues(src, "limited").Inc()
			err := &LimitError{fmt.Sprintf("%d automatic blocks in the last hour (BGP_AUTO_BLOCK_MAX_PER_HOUR=%d)", recent, s.opts.Limits.AutoPerHour)}
			s.audit(ctx, "bgp_block", req.By, req.ClientIP, p.String(), req, "denied", err.Error())
			return model.BGPBlock{}, false, err
		}
	}
	s.mu.Unlock()

	expires := now.Add(dur)
	block := model.BGPBlock{
		ID:              uuid.NewString(),
		IP:              p.Addr().String(),
		PrefixLen:       uint8(p.Bits()),
		Community:       joinComma(s.opts.Communities),
		NextHop:         s.nextHop(p),
		Reason:          src,
		Description:     truncate(req.Description, 500),
		Status:          "active",
		BlockedBy:       req.By,
		BlockedAt:       now,
		AlertID:         req.AlertID,
		RuleName:        req.RuleName,
		MetricValue:     req.MetricValue,
		MetricType:      req.MetricType,
		Threshold:       req.Threshold,
		TopSources:      req.TopSources,
		DurationSeconds: uint32(dur / time.Second),
		ExpiresAt:       &expires,
		DryRun:          s.opts.DryRun,
	}

	// Row first, route second: a crash between the two leaves an active row
	// that the next start re-announces (and expires on time), never a route
	// nobody knows about.
	if err := s.opts.Store.InsertBlock(ctx, block); err != nil {
		metrics.BGPBlockRequests.WithLabelValues(src, "error").Inc()
		return model.BGPBlock{}, false, fmt.Errorf("persist block: %w", err)
	}
	if err := s.daemonFor(block.DryRun).Announce(ctx, p); err != nil {
		metrics.BGPBlockRequests.WithLabelValues(src, "error").Inc()
		msg := errorText(err)
		if serr := s.opts.Store.SetBlockStatus(ctx, []string{block.ID}, "failed", "system", "announce failed: "+msg, s.now()); serr != nil {
			log.Printf("bgp: could not mark block %s failed: %v", block.ID, serr)
		}
		s.audit(ctx, "bgp_block", req.By, req.ClientIP, p.String(), req, "error", msg)
		return model.BGPBlock{}, false, err
	}

	s.mu.Lock()
	s.active[p] = &entry{block: block, prefix: p, ids: []string{block.ID}}
	if req.Source == SourceAuto {
		s.autoTimes = append(s.autoTimes, now)
	}
	s.updateGaugesLocked()
	s.mu.Unlock()

	metrics.BGPBlockRequests.WithLabelValues(src, "created").Inc()
	log.Printf("bgp: BLOCKED %s for %s (source=%s by=%s dry_run=%v)", p, dur, src, req.By, block.DryRun)
	s.audit(ctx, "bgp_block", req.By, req.ClientIP, p.String(), req, "success", "")
	return block, true, nil
}

// Unblock withdraws an active block. A target with no active block is
// withdrawn anyway when the last RIB check listed it as an orphan (a route
// in the daemon that as-stats does not know about), so the BGP page can clean
// those up; otherwise it is ErrNotFound.
func (s *Service) Unblock(ctx context.Context, target netip.Prefix, by, clientIP, reason string) error {
	ctx, cancel := opCtx(ctx)
	defer cancel()
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	if !s.loaded {
		s.mu.Unlock()
		return ErrNotReady
	}
	e, ok := s.active[target]
	orphan := !ok && s.rib.hasOrphan(target)
	s.mu.Unlock()

	params := map[string]string{"target": target.String(), "reason": reason}
	switch {
	case ok:
		err := s.withdrawEntryLocked(ctx, e, "manual", by, reason)
		s.auditMap(ctx, "bgp_unblock", by, clientIP, target.String(), params, err)
		return err
	case orphan:
		err := s.liveDaemon().Withdraw(ctx, target)
		result := "ok"
		if err != nil {
			result = "error"
		}
		metrics.BGPWithdrawals.WithLabelValues("orphan", result).Inc()
		if err == nil {
			s.mu.Lock()
			s.rib.removeOrphan(target)
			s.mu.Unlock()
		}
		params["orphan"] = "true"
		s.auditMap(ctx, "bgp_unblock", by, clientIP, target.String(), params, err)
		return err
	default:
		return ErrNotFound
	}
}

// withdrawEntryLocked withdraws e from its daemon and marks its rows. Caller
// holds opMu.
func (s *Service) withdrawEntryLocked(ctx context.Context, e *entry, trigger, by, reason string) error {
	if !e.withdrawn {
		d := s.daemonFor(e.block.DryRun)
		if err := d.Withdraw(ctx, e.prefix); err != nil {
			// A daemon that restarted has no such route, and some CLIs refuse
			// to delete what is not there. When the RIB can be listed and the
			// route is not in it, the withdrawal has effectively happened.
			if !absentFromRIB(ctx, d, e.prefix) {
				metrics.BGPWithdrawals.WithLabelValues(trigger, "error").Inc()
				return err
			}
			log.Printf("bgp: withdraw of %s failed but the daemon does not carry it: treating as withdrawn (%v)", e.prefix, err)
		}
		metrics.BGPWithdrawals.WithLabelValues(trigger, "ok").Inc()
		s.mu.Lock()
		e.withdrawn, e.wdBy, e.wdReason, e.wdAt = true, by, reason, s.now()
		s.mu.Unlock()
		log.Printf("bgp: WITHDRAWN %s (trigger=%s by=%s)", e.prefix, trigger, by)
	}
	return s.persistWithdrawLocked(ctx, e)
}

// absentFromRIB reports whether the daemon can list its routes and p is not
// among them.
func absentFromRIB(ctx context.Context, d Daemon, p netip.Prefix) bool {
	routes, err := d.Routes(ctx)
	if err != nil {
		return false
	}
	for _, r := range routes {
		if r == p {
			return false
		}
	}
	return true
}

// persistWithdrawLocked writes a withdrawal to the store. The route is
// already gone; on failure the entry stays (as withdrawn) and the loop
// retries, so a restart cannot re-announce it from a stale "active" row.
func (s *Service) persistWithdrawLocked(ctx context.Context, e *entry) error {
	if err := s.opts.Store.SetBlockStatus(ctx, e.ids, "withdrawn", e.wdBy, e.wdReason, e.wdAt); err != nil {
		log.Printf("bgp: %s withdrawn from the daemon but not marked in bgp_blocks (will retry): %v", e.prefix, err)
		return nil // the route is withdrawn: the caller's request succeeded
	}
	s.mu.Lock()
	if cur, ok := s.active[e.prefix]; ok && cur == e {
		delete(s.active, e.prefix)
	}
	s.updateGaugesLocked()
	s.mu.Unlock()
	return nil
}

// Run loads the active blocks and runs the expiry / reconciliation loop
// until ctx is done. Routes are left in the daemon on shutdown: they are
// in bgp_blocks and the next start takes them over.
func (s *Service) Run(ctx context.Context) {
	s.Reconcile(ctx)
	t := time.NewTicker(s.opts.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Reconcile(ctx)
		}
	}
}

// Reconcile runs one cycle: load (until it succeeds), expire, retry pending
// announces and store updates, and every ReconcileInterval check the session
// and the daemon's RIB.
func (s *Service) Reconcile(ctx context.Context) {
	s.mu.Lock()
	loaded := s.loaded
	s.mu.Unlock()
	if !loaded {
		if err := s.restore(ctx); err != nil {
			s.mu.Lock()
			s.loadErr = err.Error()
			s.mu.Unlock()
			log.Printf("bgp: could not load active blocks (blocking is refused until this succeeds): %v", err)
			return
		}
	}
	s.expireAndRetry(ctx)
	s.mu.Lock()
	due := s.now().Sub(s.lastRecon) >= s.opts.ReconcileInterval
	s.mu.Unlock()
	if due {
		s.checkDaemon(ctx)
	}
}

func (s *Service) restore(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	rows, err := s.opts.Store.ListActiveBlocks(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	recent, err := s.opts.Store.ListBlocksSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	active := map[netip.Prefix]*entry{}
	for _, b := range rows {
		p, err := TargetFromParts(b.IP, int(b.PrefixLen))
		if err != nil {
			// Legacy rows stored IPv6 host routes as /32: a block is a host
			// route unless proven otherwise.
			p, err = ParseTarget(b.IP)
		}
		if err != nil {
			log.Printf("bgp: ignoring bgp_blocks row %s with invalid target %s/%d", b.ID, b.IP, b.PrefixLen)
			continue
		}
		if e, ok := active[p]; ok {
			e.ids = append(e.ids, b.ID) // legacy duplicate row: withdraw marks it too
			continue
		}
		active[p] = &entry{block: b, prefix: p, ids: []string{b.ID}, pending: true}
	}
	var auto []time.Time
	for _, b := range recent {
		if b.Reason == string(SourceAuto) && b.BlockedAt.After(now.Add(-time.Hour)) {
			auto = append(auto, b.BlockedAt)
		}
	}
	s.mu.Lock()
	s.active = active
	s.autoTimes = auto
	s.loaded = true
	s.loadErr = ""
	s.updateGaugesLocked()
	s.mu.Unlock()
	if len(active) > 0 {
		log.Printf("bgp: loaded %d active block(s); re-announcing the unexpired ones", len(active))
	}
	return nil
}

// expireAndRetry withdraws expired blocks and retries what failed before.
func (s *Service) expireAndRetry(ctx context.Context) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	now := s.now()
	for _, e := range s.snapshot() {
		switch {
		case e.withdrawn:
			_ = s.persistWithdrawLocked(ctx, e)
		case e.block.ExpiresAt != nil && !e.block.ExpiresAt.After(now):
			reason := "block duration expired"
			if e.pending {
				reason = "block expired while the API was stopped or the daemon unreachable"
			}
			if err := s.withdrawEntryLocked(ctx, e, "expired", "system", reason); err != nil {
				log.Printf("bgp: auto-withdraw of %s failed, retrying in %s: %v", e.prefix, s.opts.Tick, err)
			} else {
				s.auditMap(ctx, "bgp_expire", "system", "", e.prefix.String(), map[string]string{"id": e.block.ID}, nil)
			}
		case e.pending:
			if err := s.daemonFor(e.block.DryRun).Announce(ctx, e.prefix); err != nil {
				log.Printf("bgp: re-announce of %s failed, retrying in %s: %v", e.prefix, s.opts.Tick, err)
				continue
			}
			metrics.BGPReannounced.Inc()
			s.mu.Lock()
			e.pending = false
			s.mu.Unlock()
		}
	}
}

// checkDaemon refreshes the session state and compares the daemon's RIB
// with the active blocks: a missing route is re-announced, an unknown one
// is reported as an orphan (never withdrawn automatically).
func (s *Service) checkDaemon(ctx context.Context) {
	s.refreshSession(ctx)

	s.opMu.Lock()
	defer s.opMu.Unlock()
	now := s.now()
	report := RIBReport{CheckedAt: &now}
	routes, err := s.liveDaemon().Routes(ctx)
	switch {
	case errors.Is(err, ErrUnsupported):
		report.Supported = false
	case err != nil:
		report.Supported = true
		report.Error = errorText(err)
	default:
		report.Supported = true
		inRIB := map[netip.Prefix]bool{}
		for _, p := range routes {
			inRIB[p] = true
		}
		report.Routes = len(routes)
		known := map[netip.Prefix]bool{}
		for _, e := range s.snapshot() {
			known[e.prefix] = true
			if e.block.DryRun != s.liveIsDry() || e.withdrawn || e.pending || inRIB[e.prefix] {
				continue
			}
			log.Printf("bgp: %s is active but missing from the daemon's RIB; re-announcing", e.prefix)
			if err := s.daemonFor(e.block.DryRun).Announce(ctx, e.prefix); err != nil {
				report.Missing = append(report.Missing, e.prefix.String())
				continue
			}
			metrics.BGPReannounced.Inc()
		}
		for _, p := range routes {
			if !known[p] && len(report.Orphans) < 100 {
				report.Orphans = append(report.Orphans, p.String())
			}
		}
		sort.Strings(report.Orphans)
	}
	metrics.BGPRIBMissing.Set(float64(len(report.Missing)))
	metrics.BGPRIBOrphans.Set(float64(len(report.Orphans)))
	s.mu.Lock()
	s.rib = report
	s.lastRecon = now
	if report.Error == "" {
		s.lastReconOK = now
	}
	s.mu.Unlock()
}

// liveDaemon is the daemon whose RIB is reconciled: the real one, or the
// dry-run one when there is no real daemon.
func (s *Service) liveDaemon() Daemon { return s.daemonFor(false) }

func (s *Service) liveIsDry() bool { return s.opts.Live == nil }

// refreshSession runs the status command. When the session comes back to
// established after being down, every live block is re-pushed: a restarted
// daemon (gobgpd keeps its RIB in memory) has forgotten them.
func (s *Service) refreshSession(ctx context.Context) {
	st, err := s.liveDaemon().Status(ctx)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, seen := s.session.State, s.sessionSeen
	s.session, s.sessionAt, s.sessionSeen = st, now, true
	s.sessionErr = ""
	switch {
	case errors.Is(err, ErrUnsupported):
		metrics.BGPDaemonUp.Set(-1)
	case err != nil:
		s.sessionErr = errorText(err)
		metrics.BGPDaemonUp.Set(0)
	case st.State == "established" || st.State == "dry-run":
		metrics.BGPDaemonUp.Set(1)
	default:
		metrics.BGPDaemonUp.Set(0)
	}
	if err == nil && seen && prev != "established" && st.State == "established" {
		for _, e := range s.active {
			if !e.block.DryRun && !e.withdrawn {
				e.pending = true
			}
		}
		log.Printf("bgp: session re-established (was %q); re-announcing active blocks", prev)
	}
}

func (s *Service) snapshot() []*entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entry, 0, len(s.active))
	for _, e := range s.active {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].prefix.String() < out[j].prefix.String() })
	return out
}

func (s *Service) countActiveLocked() int {
	n := 0
	for _, e := range s.active {
		if !e.withdrawn {
			n++
		}
	}
	return n
}

func (s *Service) pruneAutoLocked(now time.Time) int {
	keep := s.autoTimes[:0]
	for _, t := range s.autoTimes {
		if t.After(now.Add(-time.Hour)) {
			keep = append(keep, t)
		}
	}
	s.autoTimes = keep
	return len(keep)
}

func (s *Service) updateGaugesLocked() {
	live, dry := 0, 0
	for _, e := range s.active {
		if e.withdrawn {
			continue
		}
		if e.block.DryRun {
			dry++
		} else {
			live++
		}
	}
	metrics.BGPBlocksActive.WithLabelValues("live").Set(float64(live))
	metrics.BGPBlocksActive.WithLabelValues("dry_run").Set(float64(dry))
}

func (s *Service) nextHop(p netip.Prefix) string {
	if p.Addr().Is4() {
		return s.opts.NextHopV4
	}
	return s.opts.NextHopV6
}

// RIBReport is the result of the last comparison with the daemon's RIB.
type RIBReport struct {
	Supported bool       `json:"supported"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Error     string     `json:"error,omitempty"`
	Routes    int        `json:"routes"`
	Missing   []string   `json:"missing,omitempty"`
	Orphans   []string   `json:"orphans,omitempty"`
}

func (r RIBReport) hasOrphan(p netip.Prefix) bool {
	for _, o := range r.Orphans {
		if o == p.String() {
			return true
		}
	}
	return false
}

func (r *RIBReport) removeOrphan(p netip.Prefix) {
	out := r.Orphans[:0]
	for _, o := range r.Orphans {
		if o != p.String() {
			out = append(out, o)
		}
	}
	r.Orphans = out
}

// StatusReport is GET /bgp/status.
type StatusReport struct {
	Enabled         bool       `json:"enabled"`
	DryRun          bool       `json:"dry_run"`
	Ready           bool       `json:"ready"`
	LoadError       string     `json:"load_error,omitempty"`
	State           string     `json:"state"`
	StateError      string     `json:"state_error,omitempty"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	Uptime          int64      `json:"uptime"`
	PeerAddress     string     `json:"peer_address,omitempty"`
	PeerAS          uint32     `json:"peer_as,omitempty"`
	LocalAS         uint32     `json:"local_as,omitempty"`
	NextHopV4       string     `json:"next_hop_v4,omitempty"`
	NextHopV6       string     `json:"next_hop_v6,omitempty"`
	Communities     []string   `json:"communities"`
	RoutesAnnounced int        `json:"routes_announced"` // active live blocks
	ActiveBlocks    int        `json:"active_blocks"`
	// Rails.
	MaxActiveBlocks        int      `json:"max_active_blocks"`
	AutoBlocksLastHour     int      `json:"auto_blocks_last_hour"`
	AutoBlocksPerHour      int      `json:"auto_blocks_per_hour"`
	DefaultDurationMinutes int      `json:"default_duration_minutes"`
	MaxDurationMinutes     int      `json:"max_duration_minutes"`
	MinPrefixLenV4         int      `json:"min_prefix_len_v4"`
	MinPrefixLenV6         int      `json:"min_prefix_len_v6"`
	AllowedPrefixes        []string `json:"allowed_prefixes"`
	AllowedPrefixCount     int      `json:"allowed_prefix_count"`
	AllowedSource          string   `json:"allowed_source"`
	ProtectedPrefixes      []string `json:"protected_prefixes"`
	IPv6Enabled            bool     `json:"ipv6_enabled"`
	// Reconciliation.
	RIB              RIBReport  `json:"rib"`
	LastReconcileAt  *time.Time `json:"last_reconcile_at,omitempty"`
	LastReconcileOK  *time.Time `json:"last_reconcile_ok_at,omitempty"`
	RIBCheckInterval int        `json:"rib_check_interval_seconds"`
}

// Status reports the session and the rails. refresh runs the status command
// now (the "Check now" button); otherwise the last result is returned.
func (s *Service) Status(ctx context.Context, refresh bool) StatusReport {
	s.mu.Lock()
	stale := !s.sessionSeen || (refresh && s.now().Sub(s.sessionAt) > 2*time.Second)
	s.mu.Unlock()
	if stale {
		s.refreshSession(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	r := StatusReport{
		Enabled:                true,
		DryRun:                 s.opts.DryRun,
		Ready:                  s.loaded,
		LoadError:              s.loadErr,
		State:                  s.session.State,
		StateError:             s.sessionErr,
		Uptime:                 s.session.Uptime,
		PeerAddress:            s.opts.PeerAddress,
		PeerAS:                 s.opts.PeerAS,
		LocalAS:                s.opts.LocalAS,
		NextHopV4:              s.opts.NextHopV4,
		NextHopV6:              s.opts.NextHopV6,
		Communities:            append([]string{}, s.opts.Communities...),
		MaxActiveBlocks:        s.opts.Limits.MaxActive,
		AutoBlocksLastHour:     s.pruneAutoLocked(now),
		AutoBlocksPerHour:      s.opts.Limits.AutoPerHour,
		DefaultDurationMinutes: int(s.opts.Limits.DefaultDuration / time.Minute),
		MaxDurationMinutes:     int(s.opts.Limits.MaxDuration / time.Minute),
		MinPrefixLenV4:         effectiveMin(s.opts.Policy.MinLenV4, MinPrefixLenFloorV4, 32),
		MinPrefixLenV6:         effectiveMin(s.opts.Policy.MinLenV6, MinPrefixLenFloorV6, 128),
		AllowedPrefixCount:     len(s.opts.Policy.Allowed),
		AllowedSource:          s.opts.Policy.AllowedSource,
		IPv6Enabled:            s.opts.NextHopV6 != "" || s.liveIsDry(),
		RIB:                    s.rib,
		RIBCheckInterval:       int(s.opts.ReconcileInterval / time.Second),
	}
	if !s.sessionAt.IsZero() {
		t := s.sessionAt
		r.CheckedAt = &t
	}
	if !s.lastRecon.IsZero() {
		t := s.lastRecon
		r.LastReconcileAt = &t
	}
	if !s.lastReconOK.IsZero() {
		t := s.lastReconOK
		r.LastReconcileOK = &t
	}
	for i, p := range s.opts.Policy.Allowed {
		if i == 50 {
			break
		}
		r.AllowedPrefixes = append(r.AllowedPrefixes, p.String())
	}
	for _, p := range s.opts.Policy.Protected {
		r.ProtectedPrefixes = append(r.ProtectedPrefixes, p.String())
	}
	for _, e := range s.active {
		if e.withdrawn {
			continue
		}
		r.ActiveBlocks++
		if !e.block.DryRun {
			r.RoutesAnnounced++
		}
	}
	return r
}

func effectiveMin(v, floor, max int) int {
	if v < floor {
		v = floor
	}
	if v > max {
		v = max
	}
	return v
}

// audit writes one audit_log row for a state change. Failures are logged:
// losing a blackhole audit row silently is not acceptable.
func (s *Service) audit(ctx context.Context, action, by, clientIP, resource string, req BlockRequest, result, errMsg string) {
	params := map[string]string{
		"source":      string(req.Source),
		"duration":    req.Duration.String(),
		"description": truncate(req.Description, 300),
		"dry_run":     fmt.Sprint(s.opts.DryRun),
	}
	if req.AlertID != "" {
		params["alert_id"] = req.AlertID
	}
	if req.RuleName != "" {
		params["rule"] = req.RuleName
	}
	s.write(ctx, action, by, clientIP, resource, params, result, errMsg)
}

func (s *Service) auditMap(ctx context.Context, action, by, clientIP, resource string, params map[string]string, err error) {
	result, msg := "success", ""
	if err != nil {
		result, msg = "error", errorText(err)
	}
	s.write(ctx, action, by, clientIP, resource, params, result, msg)
}

func (s *Service) write(ctx context.Context, action, by, clientIP, resource string, params map[string]string, result, errMsg string) {
	if s.opts.Audit == nil {
		return
	}
	blob, _ := json.Marshal(params)
	e := model.AuditLogEntry{
		Timestamp:    s.now(),
		UserEmail:    by,
		Action:       action,
		Resource:     "bgp:" + resource,
		Params:       string(blob),
		ClientIP:     clientIP,
		Result:       result,
		ErrorMessage: errMsg,
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.opts.Audit.WriteAuditLog(actx, e); err != nil {
		log.Printf("AUDIT LOST: action=%s user=%s resource=%s err=%v", action, by, resource, err)
	}
}

// errorText is the client-safe text of an error: a command failure without
// its output.
func errorText(err error) string {
	var ce *CommandError
	if errors.As(err, &ce) {
		return ce.Short()
	}
	return err.Error()
}

func joinComma(ss []string) string { return strings.Join(ss, ",") }
