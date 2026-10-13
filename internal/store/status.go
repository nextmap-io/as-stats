package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	// liveWindow bounds the /status/live scan. flows_raw is ordered by
	// timestamp, so ten minutes is a handful of granules regardless of how
	// much history the table holds — the old Status endpoint scanned 24h.
	liveWindow = 10 * time.Minute
	// liveStaleAfter: an exporter that has been quiet this long is "stale".
	// Active-flow timeouts are typically 60 s, so two of them is a real gap.
	liveStaleAfter = 120 * time.Second
)

// liveRouterRow is one row of the per-router aggregate over liveWindow.
type liveRouterRow struct {
	ip       string
	lastSeen time.Time
	ageSecs  int64
	flows    uint64
}

// LiveStatus reports per-router liveness over the last 10 minutes, merged with
// the routers referenced by configured links (so an exporter that went fully
// silent still shows up, as "missing"), plus the fullest ClickHouse disk.
func (s *ClickHouseStore) LiveStatus(ctx context.Context) (model.LiveStatus, error) {
	// Age is computed by ClickHouse against its own now() so API-host clock
	// skew cannot flip a router between ok and stale.
	rows, err := s.conn.Query(ctx, fmt.Sprintf(`
		SELECT
			toString(f.router_ip) AS router,
			max(f.timestamp) AS last_seen,
			dateDiff('second', max(f.timestamp), now64(3)) AS age_secs,
			count() AS flows
		FROM flows_raw f
		WHERE f.timestamp >= now() - INTERVAL %d SECOND
		GROUP BY f.router_ip
	`, int(liveWindow/time.Second)))
	if err != nil {
		return model.LiveStatus{}, fmt.Errorf("live status: routers: %w", err)
	}
	var seen []liveRouterRow
	for rows.Next() {
		var r liveRouterRow
		if err := rows.Scan(&r.ip, &r.lastSeen, &r.ageSecs, &r.flows); err != nil {
			_ = rows.Close()
			return model.LiveStatus{}, err
		}
		r.ip = cleanIPv4Mapped(r.ip)
		seen = append(seen, r)
	}
	if err := rows.Close(); err != nil {
		return model.LiveStatus{}, err
	}

	// Expected exporters: every router that a configured link points at.
	expected, err := s.configuredRouters(ctx)
	if err != nil {
		return model.LiveStatus{}, err
	}

	out := model.LiveStatus{Routers: buildLiveRouters(seen, expected)}
	out.Healthy = len(out.Routers) > 0
	for _, r := range out.Routers {
		if r.State != model.RouterStateOK {
			out.Healthy = false
		}
	}

	// Disk usage is best-effort: a failure leaves it nil ("unknown") rather
	// than failing the whole health probe.
	if disks, err := s.diskStats(ctx); err == nil && len(disks) > 0 {
		worst := 0.0
		for _, d := range disks {
			if d.TotalBytes > 0 && d.UsedPercent > worst {
				worst = d.UsedPercent
			}
		}
		out.DiskUsedPct = &worst
	}
	return out, nil
}

// configuredRouters returns the distinct router IPs referenced by links
// (FINAL: an edited link must not count twice), IPv4 without ::ffff:.
func (s *ClickHouseStore) configuredRouters(ctx context.Context) ([]string, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT DISTINCT toString(l.router_ip) FROM links AS l FINAL
		WHERE l.router_ip != toIPv6('::')
	`)
	if err != nil {
		return nil, fmt.Errorf("live status: configured routers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		out = append(out, cleanIPv4Mapped(ip))
	}
	return out, rows.Err()
}

// buildLiveRouters merges the routers seen in the window with the configured
// ones and assigns a state to each. Seen routers sort first by flow volume,
// missing ones after them by IP, so the list is stable between polls.
func buildLiveRouters(seen []liveRouterRow, expected []string) []model.RouterLiveStatus {
	out := make([]model.RouterLiveStatus, 0, len(seen)+len(expected))
	have := make(map[string]bool, len(seen))
	sort.SliceStable(seen, func(i, j int) bool {
		if seen[i].flows != seen[j].flows {
			return seen[i].flows > seen[j].flows
		}
		return seen[i].ip < seen[j].ip
	})
	for _, r := range seen {
		have[r.ip] = true
		state := model.RouterStateOK
		if time.Duration(r.ageSecs)*time.Second >= liveStaleAfter {
			state = model.RouterStateStale
		}
		out = append(out, model.RouterLiveStatus{
			RouterIP: r.ip,
			LastSeen: r.lastSeen.UTC().Format(time.RFC3339),
			Flows10m: r.flows,
			State:    state,
		})
	}
	var missing []string
	for _, ip := range expected {
		if !have[ip] {
			have[ip] = true
			missing = append(missing, ip)
		}
	}
	sort.Strings(missing)
	for _, ip := range missing {
		out = append(out, model.RouterLiveStatus{RouterIP: ip, State: model.RouterStateMissing})
	}
	return out
}
