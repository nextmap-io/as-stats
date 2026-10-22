package store

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/nextmap-io/as-stats/internal/model"
)

// retentionTable describes a TTL-bearing table: the timestamp column its TTL is
// keyed on, the expression used in `ALTER TABLE ... MODIFY TTL <expr> + INTERVAL`
// (usually identical to the column, but flows_raw wraps a DateTime64 column in
// toDateTime()), and the default retention in days as encoded in the migrations.
type retentionTable struct {
	TTLColumn   string // raw column name persisted in retention_policies.ttl_column
	TTLExpr     string // expression used when issuing ALTER ... MODIFY TTL
	DefaultDays uint32
}

// retentionTables is the single source of truth for which tables are under
// retention management. It doubles as a whitelist: SetRetentionPolicy and
// ReconcileRetention only ever interpolate table/column names that originate
// here, never raw user input. Values mirror the TTLs in migrations 000001,
// 000004, 000005, 000007, 000008, 000009 and 000011.
var retentionTables = map[string]retentionTable{
	// Core tables (migrations 000001 / 000004 / 000005)
	"flows_raw":              {TTLColumn: "timestamp", TTLExpr: "toDateTime(timestamp)", DefaultDays: 3},
	"traffic_by_as":          {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 90},
	"traffic_by_as_hourly":   {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 730},
	"traffic_by_as_daily":    {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 1825},
	"traffic_by_link":        {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 90},
	"traffic_by_link_hourly": {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 730},
	"traffic_by_link_daily":  {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 1825},
	"traffic_by_ip":          {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 14},
	"traffic_by_ip_as":       {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 14},
	"traffic_by_prefix":      {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 30},

	// Optional / feature-gated tables (migrations 000007 / 000008 / 000009 / 000011)
	"flows_log":              {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 180}, // seed overridden by FLOW_LOG_RETENTION_DAYS
	"flows_log_legacy":       {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 180}, // follows flows_log, see retentionFollowers
	"traffic_by_port":        {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 365},
	"traffic_by_port_legacy": {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 365}, // follows traffic_by_port, see retentionFollowers
	"traffic_by_dst_1min":    {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 7},
	"traffic_by_src_1min":    {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 7},
	"alerts":                 {TTLColumn: "triggered_at", TTLExpr: "triggered_at", DefaultDays: 365},
	"audit_log":              {TTLColumn: "ts", TTLExpr: "ts", DefaultDays: 365},
	"bgp_blocks":             {TTLColumn: "blocked_at", TTLExpr: "blocked_at", DefaultDays: 365},
}

// retentionFollowers maps a whitelisted table that has no retention policy of
// its own to the table whose policy it follows. The reconciler applies the
// leader's ttl_days / cold_after_days (TTL, expired-partition and cold sweeps)
// to the follower; no retention_policies row is ever seeded for it and the API
// refuses to set one, so there is a single knob.
//
// flows_log_legacy is the pre-3.4 flows_log (old sort key), renamed by
// migration 000017 instead of being copied; traffic_by_port_legacy is the
// pre-3.5 traffic_by_port (every flow counted twice), renamed by migration
// 000018 and never read. They only ever lose data: once a follower has no
// active part left (or does not exist) the reconciler leaves it alone, and a
// later release can drop it.
var retentionFollowers = map[string]string{
	"flows_log_legacy":       "flows_log",
	"traffic_by_port_legacy": "traffic_by_port",
}

// followersOf returns the tables that follow leader's retention policy, sorted.
func followersOf(leader string) []string {
	var out []string
	for f, l := range retentionFollowers {
		if l == leader {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// coldVolumeName is the storage-policy volume the cold tier moves data to. The
// reconciler only manages a cold tier on tables whose live storage policy has
// a volume with exactly this name (see README › "Cold storage on a second disk").
const coldVolumeName = "cold"

// buildModifyTTLStatement constructs the ALTER ... MODIFY TTL statement for a
// table in the whitelist. Both the table name and the TTL expression come from
// the hardcoded retentionTables map (never user input); `days` / `coldDays` are
// integers so they cannot carry injection. Returns ok=false for unknown tables
// and for a cold tier that is not strictly shorter than the retention.
//
// coldDays > 0 prepends a move clause, giving
//
//	<expr> + INTERVAL c DAY TO VOLUME 'cold', <expr> + INTERVAL d DAY
//
// MODIFY TTL replaces the whole table TTL, so coldDays = 0 also *removes* a
// previously applied move clause.
//
// `materialize_ttl_after_modify = 0` is NOT optional. ClickHouse defaults it to
// 1, which makes every MODIFY TTL enqueue a MATERIALIZE TTL mutation that
// rewrites every active part of the table. On a large table that is a
// multi-tens-of-GB write amplification on the very disk the operator is usually
// trying to free — it is what turned a full disk into a 20h ingestion outage in
// production. With the setting off the change is metadata-only: existing parts
// keep their old TTL info and are evicted lazily by normal TTL merges, new and
// merged parts pick up the new retention immediately. The same holds for the
// move clause: parts that existed before it carry no move TTL and never move
// on their own — sweepColdPartitions moves them.
func buildModifyTTLStatement(table string, days, coldDays uint32) (string, bool) {
	rt, ok := retentionTables[table]
	if !ok {
		return "", false
	}
	if coldDays > 0 && coldDays >= days {
		return "", false
	}
	ttl := fmt.Sprintf("%s + INTERVAL %d DAY", rt.TTLExpr, days)
	if coldDays > 0 {
		ttl = fmt.Sprintf("%s + INTERVAL %d DAY TO VOLUME '%s', %s", rt.TTLExpr, coldDays, coldVolumeName, ttl)
	}
	return fmt.Sprintf(
		"ALTER TABLE %s MODIFY TTL %s SETTINGS materialize_ttl_after_modify = 0",
		table, ttl,
	), true
}

// validateRetention checks a (ttl_days, cold_after_days) pair: ttl_days >= 1,
// and cold_after_days is either 0 (no cold tier) or 1 <= c < ttl_days.
func validateRetention(days, coldDays uint32) error {
	if days < 1 {
		return fmt.Errorf("ttl_days must be >= 1")
	}
	if coldDays > 0 && coldDays >= days {
		return fmt.Errorf("cold_after_days (%d) must be 0 (no cold tier) or lower than ttl_days (%d)", coldDays, days)
	}
	return nil
}

// EnsureRetentionPolicies makes sure retention_policies carries one row per
// TTL-bearing table. It is idempotent *per table*: only tables that have no
// policy yet are inserted, so operator edits made via the API are never
// clobbered on restart, and a run that dies halfway (connection drop, restart)
// simply finishes the job on the next start. Gating the whole loop on a single
// "is the table empty?" probe used to leave every table after the failure point
// permanently unseeded — and therefore permanently unmanaged — because the
// table was no longer empty.
//
// Seeding prefers the TTL a table *actually* carries over the migration
// default. Otherwise the first start after upgrading an existing deployment
// would seed the hardcoded default, the reconciler would see divergence, and it
// would ALTER the live table — silently re-extending a retention an operator had
// deliberately shortened (and, before materialize_ttl_after_modify was pinned,
// rewriting the whole table to do it). The migration default is only used for
// tables that do not exist yet.
//
// flowLogDays comes from FLOW_LOG_RETENTION_DAYS; see warnFlowLogRetentionEnvIgnored
// for how it is honoured after the first run.
func (s *ClickHouseStore) EnsureRetentionPolicies(ctx context.Context, flowLogDays int) error {
	existing, err := s.retentionPoliciesByTable(ctx)
	if err != nil {
		return err
	}

	missing := make([]string, 0, len(retentionTables))
	for table := range retentionTables {
		if _, follows := retentionFollowers[table]; follows {
			continue // governed by its leader's policy, never seeded
		}
		if _, ok := existing[table]; !ok {
			missing = append(missing, table)
		}
	}

	var live map[string]uint32
	if len(missing) > 0 {
		live, err = s.liveTTLDays(ctx)
		if err != nil {
			// Non-fatal: fall back to migration defaults rather than refusing to seed.
			log.Printf("retention: could not read live TTLs, seeding from defaults: %v", err)
			live = map[string]uint32{}
		}
	}

	now := time.Now().UTC()
	for _, table := range missing {
		rt := retentionTables[table]
		days := rt.DefaultDays
		if l, ok := live[table]; ok && l > 0 {
			days = l
		}
		// After migrations 000017 / 000018 the new flows_log / traffic_by_port
		// only carry the migration default; the retention the operator
		// actually ran with is on the renamed legacy table.
		for _, f := range followersOf(table) {
			if l, ok := live[f]; ok && l > 0 {
				days = l
			}
		}
		// An explicit FLOW_LOG_RETENTION_DAYS is an operator decision and wins.
		if table == "flows_log" && flowLogDays > 0 {
			days = uint32(flowLogDays)
		}
		if err := s.conn.Exec(ctx, `
			INSERT INTO retention_policies (table_name, ttl_column, ttl_days, enabled, updated_at)
			VALUES (@table_name, @ttl_column, @ttl_days, 1, @updated_at)
		`,
			clickhouse.Named("table_name", table),
			clickhouse.Named("ttl_column", rt.TTLColumn),
			clickhouse.Named("ttl_days", days),
			clickhouse.Named("updated_at", now),
		); err != nil {
			return fmt.Errorf("seed retention policy %q: %w", table, err)
		}
	}
	if len(missing) > 0 {
		log.Printf("retention: seeded %d retention policy/policies", len(missing))
	}

	s.warnFlowLogRetentionEnvIgnored(existing, flowLogDays)
	return nil
}

// warnFlowLogRetentionEnvIgnored surfaces a FLOW_LOG_RETENTION_DAYS that no
// longer has any effect.
//
// retention_policies is the source of truth once seeded; the env var only
// supplies the initial value for flows_log. Re-applying it on every startup was
// considered and rejected: it would silently revert a retention an operator set
// from the admin UI — on an unattended restart, with data loss on the next merge
// — which is exactly the invariant the retention subsystem promises not to
// break. So the value is not applied, but it is no longer silently swallowed
// either: the operator is told which knob actually governs the table.
func (s *ClickHouseStore) warnFlowLogRetentionEnvIgnored(existing map[string]model.RetentionPolicy, flowLogDays int) {
	if flowLogDays <= 0 {
		return
	}
	cur, ok := existing["flows_log"]
	if !ok || cur.TTLDays == uint32(flowLogDays) {
		return // freshly seeded from this value, or already in agreement
	}
	log.Printf("retention: FLOW_LOG_RETENTION_DAYS=%d is IGNORED — flows_log is already managed by retention_policies at %d day(s); change it via PUT /admin/retention/flows_log (the admin UI), not the environment",
		flowLogDays, cur.TTLDays)
}

// retentionPoliciesByTable returns the stored policies keyed by table name.
func (s *ClickHouseStore) retentionPoliciesByTable(ctx context.Context) (map[string]model.RetentionPolicy, error) {
	policies, err := s.ListRetentionPolicies(ctx)
	if err != nil {
		return nil, err
	}
	byTable := make(map[string]model.RetentionPolicy, len(policies))
	for _, p := range policies {
		byTable[p.TableName] = p
	}
	return byTable, nil
}

// ListRetentionPolicies returns the current retention policy for every managed
// table, ordered by table name. Only tables in the whitelist are returned.
func (s *ClickHouseStore) ListRetentionPolicies(ctx context.Context) ([]model.RetentionPolicy, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table_name, ttl_column, ttl_days, cold_after_days, enabled, updated_at
		FROM retention_policies FINAL
		ORDER BY table_name
	`)
	if err != nil {
		return nil, fmt.Errorf("list retention policies: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.RetentionPolicy
	for rows.Next() {
		var p model.RetentionPolicy
		var enabled uint8
		if err := rows.Scan(&p.TableName, &p.TTLColumn, &p.TTLDays, &p.ColdAfterDays, &enabled, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		results = append(results, p)
	}
	return results, nil
}

// SetRetentionPolicy upserts the retention policy for a table. The table must be
// in the retentionTables whitelist; unknown tables are rejected so the API can
// never write a policy for an arbitrary name. The TTL is not applied here — the
// reconciler picks it up on its next cycle.
//
// coldAfterDays is optional: nil keeps the table's current cold tier (so a
// client that only sends ttl_days / enabled cannot wipe it), a value replaces
// it (0 removes the cold tier). The resulting pair is checked by
// validateRetention. Returns the policy as stored.
func (s *ClickHouseStore) SetRetentionPolicy(ctx context.Context, table string, days uint32, enabled bool, coldAfterDays *uint32) (model.RetentionPolicy, error) {
	rt, ok := retentionTables[table]
	if !ok {
		return model.RetentionPolicy{}, fmt.Errorf("unknown table %q", table)
	}
	if leader, follows := retentionFollowers[table]; follows {
		return model.RetentionPolicy{}, fmt.Errorf("table %q follows the %s retention policy; change %s instead", table, leader, leader)
	}
	if days < 1 {
		return model.RetentionPolicy{}, fmt.Errorf("ttl_days must be >= 1")
	}
	var cold uint32
	if coldAfterDays != nil {
		cold = *coldAfterDays
	} else {
		existing, err := s.retentionPoliciesByTable(ctx)
		if err != nil {
			return model.RetentionPolicy{}, err
		}
		cold = existing[table].ColdAfterDays
	}
	if err := validateRetention(days, cold); err != nil {
		return model.RetentionPolicy{}, err
	}
	en := uint8(0)
	if enabled {
		en = 1
	}
	now := time.Now().UTC()
	if err := s.conn.Exec(ctx, `
		INSERT INTO retention_policies (table_name, ttl_column, ttl_days, cold_after_days, enabled, updated_at)
		VALUES (@table_name, @ttl_column, @ttl_days, @cold_after_days, @enabled, @updated_at)
	`,
		clickhouse.Named("table_name", table),
		clickhouse.Named("ttl_column", rt.TTLColumn),
		clickhouse.Named("ttl_days", days),
		clickhouse.Named("cold_after_days", cold),
		clickhouse.Named("enabled", en),
		clickhouse.Named("updated_at", now),
	); err != nil {
		return model.RetentionPolicy{}, err
	}
	return model.RetentionPolicy{
		TableName:     table,
		TTLColumn:     rt.TTLColumn,
		TTLDays:       days,
		ColdAfterDays: cold,
		Enabled:       enabled,
		UpdatedAt:     now,
	}, nil
}

// liveTTLDays returns, for each whitelisted table that exists, the retention in
// days currently encoded in its definition — the DELETE interval, never the
// `TO VOLUME 'cold'` move interval of a cold tier (see parseTableTTL). Tables
// with no TTL, an unparsable one, or a non-day interval are simply absent from
// the map; callers fall back to the migration default.
func (s *ClickHouseStore) liveTTLDays(ctx context.Context) (map[string]uint32, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT name, create_table_query FROM system.tables
		WHERE database = currentDatabase()
	`)
	if err != nil {
		return nil, fmt.Errorf("read live TTLs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]uint32)
	for rows.Next() {
		var name, createQuery string
		if err := rows.Scan(&name, &createQuery); err != nil {
			return nil, err
		}
		if _, ok := retentionTables[name]; !ok {
			continue
		}
		ttl, ok := parseTableTTL(createQuery)
		if !ok || ttl.Deletes != 1 || ttl.DeleteDays == 0 {
			continue
		}
		out[name] = ttl.DeleteDays
	}
	return out, rows.Err()
}

// tableTTL is the table-level TTL of a create_table_query, reduced to what the
// reconciler manages: the DELETE interval and the optional move-to-cold one.
type tableTTL struct {
	DeleteDays uint32 // N of `<expr> + toIntervalDay(N)` with no action (or DELETE)
	ColdDays   uint32 // N of `<expr> + toIntervalDay(N) TO VOLUME 'cold'`
	Deletes    int    // number of DELETE elements
	Colds      int    // number of TO VOLUME 'cold' elements
	Others     int    // anything else: TO DISK, another volume, RECOMPRESS, WHERE, GROUP BY, non-day interval
}

// matches reports whether the live TTL is exactly the one buildModifyTTLStatement
// produces for (days, coldDays): a single DELETE at `days`, a single move to
// the cold volume at coldDays (none when coldDays = 0), and nothing else. Both
// intervals and the TO VOLUME clause are compared, so the reconciler neither
// re-issues the ALTER every cycle nor misses a change to either value.
func (t tableTTL) matches(days, coldDays uint32) bool {
	if t.Others != 0 || t.Deletes != 1 || t.DeleteDays != days {
		return false
	}
	if coldDays == 0 {
		return t.Colds == 0
	}
	return t.Colds == 1 && t.ColdDays == coldDays
}

// ttlElementPattern splits one normalised TTL element into its day interval
// and trailing action. ClickHouse 24.3 stores
//
//	MODIFY TTL ts + INTERVAL 14 DAY TO VOLUME 'cold', ts + INTERVAL 180 DAY
//
// in system.tables.create_table_query as
//
//	... TTL ts + toIntervalDay(14) TO VOLUME 'cold', ts + toIntervalDay(180) SETTINGS ...
//
// (captured from a real 24.3 server; pinned by TestParseTableTTL).
var ttlElementPattern = regexp.MustCompile(`^.+ \+ toIntervalDay\((\d+)\)(.*)$`)

// parseTableTTL extracts the table-level TTL from a create_table_query.
// ok=false when the query has no table TTL. Only the text after ` ENGINE = `
// is searched, so a column-level TTL is never mistaken for the table's, and
// the clause ends at ` SETTINGS ` (ClickHouse always prints SETTINGS last).
func parseTableTTL(createQuery string) (tableTTL, bool) {
	var out tableTTL
	eng := strings.Index(createQuery, " ENGINE = ")
	if eng < 0 {
		return out, false
	}
	rest := createQuery[eng:]
	start := strings.Index(rest, " TTL ")
	if start < 0 {
		return out, false
	}
	clause := rest[start+len(" TTL "):]
	if end := strings.Index(clause, " SETTINGS "); end >= 0 {
		clause = clause[:end]
	}
	for _, el := range splitTopLevel(clause) {
		m := ttlElementPattern.FindStringSubmatch(strings.TrimSpace(el))
		if m == nil {
			out.Others++
			continue
		}
		n, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil {
			out.Others++
			continue
		}
		switch strings.TrimSpace(m[2]) {
		case "", "DELETE":
			out.Deletes++
			out.DeleteDays = uint32(n)
		case "TO VOLUME '" + coldVolumeName + "'":
			out.Colds++
			out.ColdDays = uint32(n)
		default:
			out.Others++
		}
	}
	return out, true
}

// splitTopLevel splits s on the commas that are outside parentheses and quotes.
func splitTopLevel(s string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// presentTables returns the set of tables that actually exist in the current
// database, so the reconciler / stats can skip feature-gated tables that were
// never created.
func (s *ClickHouseStore) presentTables(ctx context.Context) (map[string]bool, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT name FROM system.tables WHERE database = currentDatabase()
	`)
	if err != nil {
		return nil, fmt.Errorf("list system.tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	present := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present[name] = true
	}
	return present, nil
}

// ReconcileRetention applies each enabled policy's desired TTL to its table when
// it diverges from the live TTL. The ALTER is metadata-only and idempotent
// *because* buildModifyTTLStatement pins materialize_ttl_after_modify = 0 —
// without that setting this loop rewrites whole tables (see its docstring).
// Tables absent from system.tables (e.g. feature-gated ones that were never
// created) are skipped. Applied changes are logged.
//
// Cold tier: a policy with cold_after_days > 0 only takes effect on a table
// whose live storage policy has a volume named `cold` (otherwise it is logged
// once per process and the table is reconciled exactly as without a cold
// tier). There, the TTL gains a `TO VOLUME 'cold'` clause and
// sweepColdPartitions moves the partitions that predate it. Every per-table
// failure is logged and the loop moves on to the next table.
//
// A table with cold_after_days > 0 still on ClickHouse's `default` storage
// policy is first switched to the server's hot/cold policy when exactly one
// qualifies (adoptColdPolicy) — that is how the flows_log created by migration
// 000017 inherits the policy the old table had been moved to by hand.
//
// Tables in retentionFollowers get their leader's policy, as long as they
// exist and still hold at least one active part.
func (s *ClickHouseStore) ReconcileRetention(ctx context.Context) error {
	policies, err := s.ListRetentionPolicies(ctx)
	if err != nil {
		return err
	}
	present, err := s.presentTables(ctx)
	if err != nil {
		return err
	}
	tablePolicies, err := s.tableStoragePolicies(ctx)
	if err != nil {
		// Non-fatal: without the policy map no cold tier is applied, which is
		// exactly the pre-cold-tier behaviour.
		log.Printf("retention: could not read storage policies, cold tier skipped this cycle: %v", err)
		tablePolicies = map[string]storagePolicyInfo{}
	}
	var withParts map[string]bool // lazily loaded: only needed when a follower exists

	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		if _, ok := retentionTables[p.TableName]; !ok {
			continue // ignore policies for tables outside the whitelist
		}
		if _, follows := retentionFollowers[p.TableName]; follows {
			continue // a stray row for a follower is never authoritative
		}
		targets := []string{p.TableName}
		for _, f := range followersOf(p.TableName) {
			if !present[f] {
				continue
			}
			if withParts == nil {
				if withParts, err = s.tablesWithActiveParts(ctx); err != nil {
					log.Printf("retention: could not list active parts, followers skipped this cycle: %v", err)
					withParts = map[string]bool{}
				}
			}
			if withParts[f] {
				targets = append(targets, f)
			}
		}
		for _, table := range targets {
			if !present[table] {
				continue // feature-gated table not created — nothing to do
			}
			s.reconcileTable(ctx, table, p, tablePolicies)
		}
	}
	return nil
}

// reconcileTable applies policy p (possibly a leader's policy, for a follower)
// to one existing table: storage-policy adoption, TTL, expired-partition sweep,
// cold sweep. Failures are logged; the caller moves on to the next table.
func (s *ClickHouseStore) reconcileTable(ctx context.Context, table string, p model.RetentionPolicy, tablePolicies map[string]storagePolicyInfo) {
	pol := tablePolicies[table]
	if p.ColdAfterDays > 0 && p.ColdAfterDays < p.TTLDays && len(pol.ColdDisks) == 0 {
		if adopted, ok := s.adoptColdPolicy(ctx, table, pol.Policy); ok {
			pol = adopted
			tablePolicies[table] = adopted
		}
	}

	tp := p
	tp.TableName = table
	cold := s.effectiveColdDays(tp, pol.ColdDisks)

	applied, err := s.applyTTLIfDiverged(ctx, table, p.TTLDays, cold)
	if err != nil {
		log.Printf("retention: reconcile %s failed: %v", table, err)
		return
	}
	if applied {
		if cold > 0 {
			log.Printf("retention: applied TTL %d day(s), move to volume '%s' after %d day(s), to %s", p.TTLDays, coldVolumeName, cold, table)
		} else {
			log.Printf("retention: applied TTL %d day(s) to %s", p.TTLDays, table)
		}
	}

	if err := s.sweepExpiredPartitions(ctx, table, p.TTLDays); err != nil {
		log.Printf("retention: partition sweep %s failed: %v", table, err)
	}
	if cold > 0 {
		if err := s.sweepColdPartitions(ctx, table, cold, p.TTLDays, pol.ColdDisks); err != nil {
			log.Printf("retention: cold move %s failed: %v", table, err)
		}
	}
}

// tablesWithActiveParts returns the tables of the current database that have
// at least one active, non-empty part (26.8 leaves an empty part behind each
// TTLDropMerge until cleanup; an emptied legacy table must stay untouched).
func (s *ClickHouseStore) tablesWithActiveParts(ctx context.Context) (map[string]bool, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT DISTINCT table FROM system.parts
		WHERE database = currentDatabase() AND active AND rows > 0
	`)
	if err != nil {
		return nil, fmt.Errorf("list tables with parts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// defaultStoragePolicy is ClickHouse's built-in storage policy (one volume
// `default` on disk `default`), the one every table gets unless its CREATE
// names another.
const defaultStoragePolicy = "default"

// policyVolume is one volume of a storage policy (system.storage_policies).
type policyVolume struct {
	Name     string
	Priority uint64
	Disks    []string
}

// storagePolicyNamePattern bounds the policy names adoptColdPolicy will
// interpolate into MODIFY SETTING (it cannot take a bound parameter).
var storagePolicyNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// coldPolicyCandidates returns, sorted, the storage policies a table on the
// default policy can be switched to for a cold tier: first volume named
// `default` on exactly the `default` disk (ClickHouse refuses a switch to a
// policy that lacks the old policy's volumes), second volume named `cold`.
// Policies whose name is not a plain identifier are ignored.
func coldPolicyCandidates(policies map[string][]policyVolume) []string {
	var out []string
	for name, vols := range policies {
		if !storagePolicyNamePattern.MatchString(name) || len(vols) < 2 {
			continue
		}
		v := slices.Clone(vols)
		slices.SortFunc(v, func(a, b policyVolume) int { return cmp.Compare(a.Priority, b.Priority) })
		if v[0].Name == "default" && slices.Equal(v[0].Disks, []string{"default"}) && v[1].Name == coldVolumeName {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// buildAdoptStoragePolicyStatement returns the MODIFY SETTING storage_policy
// statement for a whitelisted table, or ok=false for an unknown table or a
// policy name that is not a plain identifier.
func buildAdoptStoragePolicyStatement(table, policy string) (string, bool) {
	if _, ok := retentionTables[table]; !ok {
		return "", false
	}
	if !storagePolicyNamePattern.MatchString(policy) {
		return "", false
	}
	return fmt.Sprintf("ALTER TABLE %s MODIFY SETTING storage_policy = '%s'", table, policy), true
}

// adoptColdPolicy switches a table that wants a cold tier but sits on the
// default storage policy to the server's hot/cold policy — only when exactly
// one candidate exists (coldPolicyCandidates). Zero or several candidates, or
// a table on a non-default policy, are logged once and left alone: picking a
// disk layout is the operator's call. The switch is metadata-only (the
// existing parts stay on the `default` disk, which the new policy contains);
// it cannot be undone to a policy without the cold volume, hence the strict
// conditions. Returns the table's new policy info and ok=true on a switch.
func (s *ClickHouseStore) adoptColdPolicy(ctx context.Context, table, current string) (storagePolicyInfo, bool) {
	if current != defaultStoragePolicy {
		return storagePolicyInfo{}, false // effectiveColdDays logs the missing cold volume
	}
	policies, err := s.storagePolicyVolumes(ctx)
	if err != nil {
		log.Printf("retention: %s: could not read storage policies for cold-tier adoption: %v", table, err)
		return storagePolicyInfo{}, false
	}
	candidates := coldPolicyCandidates(policies)
	if len(candidates) != 1 {
		s.warnColdOnce("adopt:"+table, fmt.Sprintf(
			"retention: %s wants a cold tier but is on the default storage policy and %d storage policies qualify (volume 'default' on disk 'default', then volume '%s'): %v — switch it by hand with ALTER TABLE %s MODIFY SETTING storage_policy = '…'",
			table, len(candidates), coldVolumeName, candidates, table))
		return storagePolicyInfo{}, false
	}
	stmt, ok := buildAdoptStoragePolicyStatement(table, candidates[0])
	if !ok {
		return storagePolicyInfo{}, false
	}
	if err := s.conn.Exec(ctx, stmt); err != nil {
		log.Printf("retention: switching %s to storage policy %q failed: %v", table, candidates[0], err)
		return storagePolicyInfo{}, false
	}
	var coldDisks []string
	for _, v := range policies[candidates[0]] {
		if v.Name == coldVolumeName {
			coldDisks = v.Disks
		}
	}
	log.Printf("retention: switched %s from storage policy %q to %q (the only policy with a '%s' volume) for its cold tier",
		table, current, candidates[0], coldVolumeName)
	return storagePolicyInfo{Policy: candidates[0], ColdDisks: coldDisks}, true
}

// storagePolicyVolumes returns every storage policy's volumes.
func (s *ClickHouseStore) storagePolicyVolumes(ctx context.Context) (map[string][]policyVolume, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT policy_name, volume_name, volume_priority, disks
		FROM system.storage_policies
		ORDER BY policy_name, volume_priority
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.storage_policies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string][]policyVolume)
	for rows.Next() {
		var policy string
		var v policyVolume
		if err := rows.Scan(&policy, &v.Name, &v.Priority, &v.Disks); err != nil {
			return nil, err
		}
		out[policy] = append(out[policy], v)
	}
	return out, rows.Err()
}

// effectiveColdDays returns the cold tier the reconciler actually applies to a
// table: the policy's cold_after_days when the table's storage policy has a
// `cold` volume (coldDisks non-empty) and the value is consistent, 0 otherwise.
// A configured-but-ineffective cold tier is logged once per table per process.
func (s *ClickHouseStore) effectiveColdDays(p model.RetentionPolicy, coldDisks []string) uint32 {
	if p.ColdAfterDays == 0 {
		return 0
	}
	if len(coldDisks) == 0 {
		s.warnColdOnce(p.TableName, fmt.Sprintf(
			"retention: %s has cold_after_days=%d but its storage policy has no volume named '%s' — cold tier ignored (see README, \"Cold storage on a second disk\")",
			p.TableName, p.ColdAfterDays, coldVolumeName))
		return 0
	}
	if p.ColdAfterDays >= p.TTLDays {
		s.warnColdOnce(p.TableName, fmt.Sprintf(
			"retention: %s has cold_after_days=%d >= ttl_days=%d — cold tier ignored",
			p.TableName, p.ColdAfterDays, p.TTLDays))
		return 0
	}
	return p.ColdAfterDays
}

// warnColdOnce logs msg the first time it is called for table in this process.
// The reconciler runs every RETENTION_RECONCILE_INTERVAL; repeating the same
// configuration warning every cycle would only bury the log.
func (s *ClickHouseStore) warnColdOnce(table, msg string) {
	s.coldWarnMu.Lock()
	defer s.coldWarnMu.Unlock()
	if s.coldWarned[table] {
		return
	}
	if s.coldWarned == nil {
		s.coldWarned = make(map[string]bool)
	}
	s.coldWarned[table] = true
	log.Print(msg)
}

// storagePolicyInfo is the live storage policy of one table.
type storagePolicyInfo struct {
	Policy    string
	ColdDisks []string // disks of the policy's `cold` volume; empty = no cold volume
}

// tableStoragePolicies returns, for every table of the current database, its
// storage policy and the disks of that policy's `cold` volume (if any), from
// system.tables.storage_policy + system.storage_policies.
func (s *ClickHouseStore) tableStoragePolicies(ctx context.Context) (map[string]storagePolicyInfo, error) {
	coldByPolicy := make(map[string][]string)
	prow, err := s.conn.Query(ctx, `
		SELECT policy_name, disks FROM system.storage_policies
		WHERE volume_name = @volume
	`, clickhouse.Named("volume", coldVolumeName))
	if err != nil {
		return nil, fmt.Errorf("query system.storage_policies: %w", err)
	}
	for prow.Next() {
		var policy string
		var disks []string
		if err := prow.Scan(&policy, &disks); err != nil {
			_ = prow.Close()
			return nil, err
		}
		coldByPolicy[policy] = disks
	}
	if err := prow.Err(); err != nil {
		_ = prow.Close()
		return nil, err
	}
	_ = prow.Close()

	trow, err := s.conn.Query(ctx, `
		SELECT name, storage_policy FROM system.tables
		WHERE database = currentDatabase()
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.tables: %w", err)
	}
	defer func() { _ = trow.Close() }()
	out := make(map[string]storagePolicyInfo)
	for trow.Next() {
		var name, policy string
		if err := trow.Scan(&name, &policy); err != nil {
			return nil, err
		}
		out[name] = storagePolicyInfo{Policy: policy, ColdDisks: coldByPolicy[policy]}
	}
	return out, trow.Err()
}

// coldDisksByTable returns the cold-volume disks of every table whose storage
// policy has a `cold` volume; tables without one are absent.
func (s *ClickHouseStore) coldDisksByTable(ctx context.Context) (map[string][]string, error) {
	pol, err := s.tableStoragePolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for t, p := range pol {
		if len(p.ColdDisks) > 0 {
			out[t] = p.ColdDisks
		}
	}
	return out, nil
}

// buildMovePartitionToColdStatement returns the MOVE PARTITION ID ... TO VOLUME
// 'cold' statement for a whitelisted table, or ok=false if the table is
// unknown or the ID malformed. Same partition-id whitelist as
// buildDropPartitionStatement: MOVE PARTITION ID cannot take a bound parameter.
func buildMovePartitionToColdStatement(table, partitionID string) (string, bool) {
	if _, ok := retentionTables[table]; !ok {
		return "", false
	}
	if !partitionIDPattern.MatchString(partitionID) {
		return "", false
	}
	return fmt.Sprintf("ALTER TABLE %s MOVE PARTITION ID '%s' TO VOLUME '%s'", table, partitionID, coldVolumeName), true
}

// maxColdMovesPerCycle bounds how many partitions sweepColdPartitions moves per
// table per reconcile cycle. A MOVE copies the whole partition onto the cold
// disk — an HDD — so enabling a cold tier on a table with months of history
// would otherwise write all of it in one burst (flows_log: ~1 GiB per daily
// partition). Three per table per cycle (default every 15 min) catches up on
// ~290 daily partitions a day while keeping each burst to a few GiB.
const maxColdMovesPerCycle = 3

// sweepColdPartitions moves to the `cold` volume the whole partitions that are
// entirely older than coldDays but still have active parts on a non-cold disk.
//
// The TTL move clause alone is not enough, for the same reason a lowered TTL is
// not (bug #13): buildModifyTTLStatement pins materialize_ttl_after_modify = 0,
// so parts written or merged before the clause existed carry no move TTL info
// and ClickHouse never moves them — and closed daily/monthly partitions are
// never merged again. Partitions about to be dropped by sweepExpiredPartitions
// (past ttl_days + grace) are skipped: copying them to the HDD first would be
// wasted I/O. Oldest first, at most maxColdMovesPerCycle per call; the first
// failure (e.g. not enough space on the cold disk) stops this table's sweep
// and is returned for the caller to log.
func (s *ClickHouseStore) sweepColdPartitions(ctx context.Context, table string, coldDays, ttlDays uint32, coldDisks []string) error {
	if _, ok := retentionTables[table]; !ok {
		return fmt.Errorf("unknown table %q", table)
	}
	if coldDays == 0 || len(coldDisks) == 0 {
		return nil
	}

	rows, err := s.conn.Query(ctx, `
		SELECT partition_id, sumIf(bytes_on_disk, NOT has(@cold_disks, disk_name)) AS hot_bytes
		FROM system.parts
		WHERE database = currentDatabase() AND table = @table AND active
		  AND min_time > toDateTime(0)
		GROUP BY partition_id
		HAVING max(max_time) < now() - toIntervalDay(@cold_days)
		   AND max(max_time) >= now() - toIntervalDay(@drop_days)
		   AND countIf(NOT has(@cold_disks, disk_name)) > 0
		ORDER BY max(max_time) ASC
		LIMIT @max_moves
	`,
		clickhouse.Named("table", table),
		clickhouse.Named("cold_disks", coldDisks),
		clickhouse.Named("cold_days", uint64(coldDays)),
		clickhouse.Named("drop_days", uint64(ttlDays)+sweepGraceDays),
		clickhouse.Named("max_moves", uint64(maxColdMovesPerCycle)),
	)
	if err != nil {
		return fmt.Errorf("list partitions to move: %w", err)
	}
	type candidate struct {
		id    string
		bytes uint64
	}
	var moves []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.bytes); err != nil {
			_ = rows.Close()
			return err
		}
		moves = append(moves, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, m := range moves {
		stmt, ok := buildMovePartitionToColdStatement(table, m.id)
		if !ok {
			log.Printf("retention: refusing to move %s partition %q: unexpected partition id", table, m.id)
			continue
		}
		start := time.Now()
		if err := s.conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("move partition %s (%d bytes) to volume '%s': %w", m.id, m.bytes, coldVolumeName, err)
		}
		log.Printf("retention: moved %s partition %s (%d bytes) to volume '%s' in %s — older than %d day(s)",
			table, m.id, m.bytes, coldVolumeName, time.Since(start).Round(time.Millisecond), coldDays)
	}
	return nil
}

// partitionIDPattern matches the partition IDs our tables produce: every
// whitelisted table is PARTITION BY toYYYYMM(col) or toYYYYMMDD(col), whose IDs
// are plain digits. DROP PARTITION ID cannot take a bound parameter, so the ID is
// validated against this before being interpolated.
var partitionIDPattern = regexp.MustCompile(`^[0-9]{6,8}$`)

// buildDropPartitionStatement returns the DROP PARTITION ID statement for a
// whitelisted table, or ok=false if the table is unknown or the ID malformed.
func buildDropPartitionStatement(table, partitionID string) (string, bool) {
	if _, ok := retentionTables[table]; !ok {
		return "", false
	}
	if !partitionIDPattern.MatchString(partitionID) {
		return "", false
	}
	return fmt.Sprintf("ALTER TABLE %s DROP PARTITION ID '%s'", table, partitionID), true
}

// sweepGraceDays keeps a partition until it is a full day past its retention
// cutoff, so a partition is never dropped while TTL could still be keeping
// some of its rows.
const sweepGraceDays = 1

// sweepExpiredPartitions drops whole partitions whose newest row is already
// past the table's retention.
//
// TTL alone is not enough. Because buildModifyTTLStatement pins
// materialize_ttl_after_modify = 0, lowering a retention only changes the TTL
// metadata of parts written or merged afterwards. Parts that were already fully
// merged — which is every closed daily/monthly partition — are never merged
// again, so they keep the expiry computed under the OLD retention. In
// production, cutting flows_log from 180 to 60 days left ~30 daily partitions
// (~38 GB) scheduled to expire months later while the disk filled up.
//
// Every whitelisted table partitions on a function of its TTL column, so
// system.parts.max_time is the newest TTL-column value in the partition. A
// partition whose max_time is past the cutoff holds no row that TTL would keep,
// and DROP PARTITION removes it by unlinking files: no rewrite, and it works even
// when the disk is too full for a merge to run.
func (s *ClickHouseStore) sweepExpiredPartitions(ctx context.Context, table string, days uint32) error {
	if _, ok := retentionTables[table]; !ok {
		return fmt.Errorf("unknown table %q", table)
	}
	if days == 0 {
		return nil
	}

	rows, err := s.conn.Query(ctx, `
		SELECT partition_id, sum(bytes_on_disk)
		FROM system.parts
		WHERE database = currentDatabase() AND table = @table AND active
		  AND min_time > toDateTime(0)
		GROUP BY partition_id
		HAVING max(max_time) < now() - toIntervalDay(@days)
	`,
		clickhouse.Named("table", table),
		clickhouse.Named("days", uint64(days)+sweepGraceDays),
	)
	if err != nil {
		return fmt.Errorf("list expired partitions: %w", err)
	}
	type expired struct {
		id    string
		bytes uint64
	}
	var victims []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.bytes); err != nil {
			_ = rows.Close()
			return err
		}
		victims = append(victims, e)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, v := range victims {
		stmt, ok := buildDropPartitionStatement(table, v.id)
		if !ok {
			log.Printf("retention: refusing to drop %s partition %q: unexpected partition id", table, v.id)
			continue
		}
		if err := s.conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("drop partition %s: %w", v.id, err)
		}
		log.Printf("retention: dropped %s partition %s (%d bytes) — entirely past its %d-day retention", table, v.id, v.bytes, days)
	}
	return nil
}

// applyTTLIfDiverged issues the MODIFY TTL only if the live table definition
// does not already encode the desired retention and cold tier. The normalised
// TTL in create_table_query is parsed (parseTableTTL) and compared on both
// intervals and the TO VOLUME clause (tableTTL.matches), so a matching table is
// skipped — no log noise every cycle — while a change to either value, or a
// cold tier being switched off, is applied. Returns true when an ALTER was issued.
func (s *ClickHouseStore) applyTTLIfDiverged(ctx context.Context, table string, days, coldDays uint32) (bool, error) {
	stmt, ok := buildModifyTTLStatement(table, days, coldDays)
	if !ok {
		return false, fmt.Errorf("cannot build TTL for %q (ttl_days=%d, cold_after_days=%d)", table, days, coldDays)
	}

	var createQuery string
	err := s.conn.QueryRow(ctx, `
		SELECT create_table_query FROM system.tables
		WHERE database = currentDatabase() AND name = @name
	`, clickhouse.Named("name", table)).Scan(&createQuery)
	if err != nil {
		return false, fmt.Errorf("read create query: %w", err)
	}

	if live, ok := parseTableTTL(createQuery); ok && live.matches(days, coldDays) {
		return false, nil // already at desired retention / cold tier
	}

	if err := s.conn.Exec(ctx, stmt); err != nil {
		return false, fmt.Errorf("modify ttl: %w", err)
	}
	return true, nil
}

// StorageStats returns per-table storage observability (size, parts, rows, data
// window, configured retention, pending mutations) plus per-disk usage. All
// interpolated values are bound parameters; the database is the connection's
// currentDatabase().
func (s *ClickHouseStore) StorageStats(ctx context.Context) (model.StorageStats, error) {
	var out model.StorageStats

	// Configured retention, keyed by table.
	policies, err := s.ListRetentionPolicies(ctx)
	if err != nil {
		return out, err
	}
	policyByTable := make(map[string]model.RetentionPolicy, len(policies))
	for _, p := range policies {
		policyByTable[p.TableName] = p
	}

	// Pending (not-done) mutations per table.
	pending, err := s.pendingMutationsByTable(ctx)
	if err != nil {
		return out, err
	}

	// Per-table aggregates from active parts.
	rows, err := s.conn.Query(ctx, `
		SELECT
			table,
			sum(data_compressed_bytes)   AS compressed,
			sum(data_uncompressed_bytes) AS uncompressed,
			count()                      AS parts,
			sum(rows)                    AS row_count,
			-- Empty parts carry min_time = max_time = 1970: ClickHouse 26.8
			-- writes one per TTLDropMerge (an expired part replaced by an
			-- empty one until cleanup), which would blank "oldest data".
			minIf(min_time, rows > 0)    AS oldest,
			maxIf(max_time, rows > 0)    AS newest
		FROM system.parts
		WHERE database = currentDatabase() AND active
		GROUP BY table
		ORDER BY compressed DESC
	`)
	if err != nil {
		return out, fmt.Errorf("query system.parts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var t model.TableStorageStats
		var oldest, newest time.Time
		if err := rows.Scan(
			&t.Table, &t.CompressedBytes, &t.UncompressedBytes,
			&t.Parts, &t.Rows, &oldest, &newest,
		); err != nil {
			return out, err
		}
		if !oldest.IsZero() && oldest.Unix() > 0 {
			o := oldest
			t.OldestData = &o
		}
		if !newest.IsZero() && newest.Unix() > 0 {
			n := newest
			t.NewestData = &n
		}
		policyTable := t.Table
		if leader, follows := retentionFollowers[t.Table]; follows {
			// Read-only in the UI: shows the leader's policy, which is the
			// one the reconciler applies to it.
			policyTable = leader
			t.RetentionFollows = leader
			t.Legacy = true
		}
		if p, ok := policyByTable[policyTable]; ok {
			t.TTLDays = p.TTLDays
			t.TTLEnabled = p.Enabled
			t.ColdAfterDays = p.ColdAfterDays
		}
		t.PendingMutations = pending[t.Table]
		out.Tables = append(out.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	_ = rows.Close()

	// Hot / cold split: live storage policy per table and bytes per disk.
	storagePolicies, err := s.tableStoragePolicies(ctx)
	if err != nil {
		return out, err
	}
	perDisk, err := s.tableDiskBytes(ctx)
	if err != nil {
		return out, err
	}
	for i := range out.Tables {
		t := &out.Tables[i]
		pol := storagePolicies[t.Table]
		t.StoragePolicy = pol.Policy
		t.HasColdVolume = len(pol.ColdDisks) > 0
		t.Disks = perDisk[t.Table]
		for j := range t.Disks {
			d := &t.Disks[j]
			d.Cold = slices.Contains(pol.ColdDisks, d.Disk)
			t.BytesOnDisk += d.Bytes
			if d.Cold {
				t.ColdBytes += d.Bytes
			} else {
				t.HotBytes += d.Bytes
			}
		}
		if t.Disks == nil {
			t.Disks = []model.TableDiskBytes{}
		}
	}

	disks, err := s.diskStats(ctx)
	if err != nil {
		return out, err
	}
	out.Disks = disks
	return out, nil
}

// tableDiskBytes returns, per table, the active-part bytes and part count on
// each disk (system.parts.disk_name), largest first.
func (s *ClickHouseStore) tableDiskBytes(ctx context.Context) (map[string][]model.TableDiskBytes, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table, disk_name, sum(bytes_on_disk) AS bytes, count() AS parts
		FROM system.parts
		WHERE database = currentDatabase() AND active
		GROUP BY table, disk_name
		ORDER BY table, bytes DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.parts per disk: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string][]model.TableDiskBytes)
	for rows.Next() {
		var table string
		var d model.TableDiskBytes
		if err := rows.Scan(&table, &d.Disk, &d.Bytes, &d.Parts); err != nil {
			return nil, err
		}
		out[table] = append(out[table], d)
	}
	return out, rows.Err()
}

func (s *ClickHouseStore) pendingMutationsByTable(ctx context.Context) (map[string]uint64, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT table, count() AS cnt
		FROM system.mutations
		WHERE database = currentDatabase() AND is_done = 0
		GROUP BY table
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.mutations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]uint64)
	for rows.Next() {
		var table string
		var cnt uint64
		if err := rows.Scan(&table, &cnt); err != nil {
			return nil, err
		}
		out[table] = cnt
	}
	return out, nil
}

func (s *ClickHouseStore) diskStats(ctx context.Context) ([]model.DiskStats, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT name, path, free_space, total_space
		FROM system.disks
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query system.disks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var disks []model.DiskStats
	for rows.Next() {
		var d model.DiskStats
		if err := rows.Scan(&d.Name, &d.Path, &d.FreeBytes, &d.TotalBytes); err != nil {
			return nil, err
		}
		if d.TotalBytes >= d.FreeBytes {
			d.UsedBytes = d.TotalBytes - d.FreeBytes
		}
		if d.TotalBytes > 0 {
			d.UsedPercent = float64(d.UsedBytes) / float64(d.TotalBytes) * 100
		}
		disks = append(disks, d)
	}
	return disks, rows.Err()
}

// fullestDisk returns the disk with the highest used percentage among those
// reporting a size. Every disk in system.disks counts — with a hot/cold
// storage policy that is both the fast `default` disk and the HDD — so the
// disk_usage alert and /status/live report whichever is closest to full.
// ok=false when no disk reports a size.
func fullestDisk(disks []model.DiskStats) (model.DiskStats, bool) {
	var worst model.DiskStats
	found := false
	for _, d := range disks {
		if d.TotalBytes == 0 {
			continue
		}
		if !found || d.UsedPercent > worst.UsedPercent {
			worst = d
			found = true
		}
	}
	return worst, found
}

// softDeleteTables lists the ReplacingMergeTree config tables that carry a
// `deleted` tombstone column and can therefore be physically purged.
var softDeleteTables = []string{"alert_rules", "webhook_configs", "hostgroups", "report_schedules"}

// PurgeSoftDeleted physically removes tombstoned (deleted = 1) rows older than
// `days` from the config tables via lightweight ALTER ... DELETE mutations.
// OPTIMIZE FINAL is intentionally not forced (expensive). Tables absent from the
// database (feature-gated) are skipped. Affected row counts are logged.
func (s *ClickHouseStore) PurgeSoftDeleted(ctx context.Context, days int) error {
	if days < 1 {
		return fmt.Errorf("days must be >= 1")
	}
	present, err := s.presentTables(ctx)
	if err != nil {
		return err
	}

	for _, table := range softDeleteTables {
		if !present[table] {
			continue
		}

		// Count matching tombstones first (FINAL so superseded versions don't
		// inflate the count) for an informative log line.
		var n uint64
		countQ := fmt.Sprintf(`
			SELECT count() FROM %s FINAL
			WHERE deleted = 1 AND updated_at < now() - INTERVAL %d DAY
		`, table, days)
		if err := s.conn.QueryRow(ctx, countQ).Scan(&n); err != nil {
			log.Printf("retention: purge %s count failed: %v", table, err)
			continue
		}
		if n == 0 {
			continue
		}

		delQ := fmt.Sprintf(`
			ALTER TABLE %s DELETE
			WHERE deleted = 1 AND updated_at < now() - INTERVAL %d DAY
		`, table, days)
		if err := s.conn.Exec(ctx, delQ); err != nil {
			log.Printf("retention: purge %s failed: %v", table, err)
			continue
		}
		log.Printf("retention: purged %d tombstoned row(s) from %s (older than %d day(s))", n, table, days)
	}
	return nil
}
