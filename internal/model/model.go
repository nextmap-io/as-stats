package model

import (
	"net"
	"time"
)

// FlowRecord represents a single parsed flow record from any source (NetFlow/sFlow/IPFIX).
type FlowRecord struct {
	Timestamp    time.Time
	RouterIP     net.IP
	LinkTag      string
	SrcIP        net.IP
	DstIP        net.IP
	IPVersion    uint8
	SrcAS        uint32
	DstAS        uint32
	SrcPrefix    string
	DstPrefix    string
	Protocol     uint8
	SrcPort      uint16
	DstPort      uint16
	TCPFlags     uint8
	Bytes        uint64
	Packets      uint64
	SamplingRate uint32
	InInterface  uint32
	OutInterface uint32
	Direction    uint8  // 0=unknown, 1=inbound, 2=outbound
	FlowType     string // "netflow5", "netflow9", "ipfix", "sflow"
}

// Direction constants.
const (
	DirectionUnknown  uint8 = 0
	DirectionInbound  uint8 = 1
	DirectionOutbound uint8 = 2
)

// Link represents a known network link (router interface).
type Link struct {
	Tag          string `json:"tag"`
	RouterIP     net.IP `json:"router_ip"`
	SNMPIndex    uint32 `json:"snmp_index"`
	Description  string `json:"description"`
	CapacityMbps uint32 `json:"capacity_mbps"`
	Color        string `json:"color,omitempty"`
}

// ASInfo holds AS number to name mapping.
type ASInfo struct {
	Number  uint32 `json:"number"`
	Name    string `json:"name"`
	Country string `json:"country"`
}

// TrafficPoint represents a single time-series data point.
type TrafficPoint struct {
	Timestamp  time.Time `json:"t"`
	BytesIn    uint64    `json:"bytes_in"`
	BytesOut   uint64    `json:"bytes_out"`
	PacketsIn  uint64    `json:"packets_in,omitempty"`
	PacketsOut uint64    `json:"packets_out,omitempty"`
}

// ASTraffic represents traffic statistics for a single AS.
type ASTraffic struct {
	ASNumber uint32  `json:"as_number"`
	ASName   string  `json:"as_name"`
	Country  string  `json:"country,omitempty"` // ISO 3166-1 alpha-2, from as_names.country (may be empty)
	Bytes    uint64  `json:"bytes"`
	Packets  uint64  `json:"packets"`
	Flows    uint64  `json:"flows"`
	Percent  float64 `json:"pct"`
	// AvgPktSize is sum(bytes)/max(sum(packets),1) — mean packet size in bytes.
	AvgPktSize float64 `json:"avg_pkt_size"`
	// In/out asymmetry (F2). BytesIn/BytesOut are sumIf on the direction column;
	// Ratio is BytesOut/max(BytesIn,1); Class is one of eyeball/content/balanced.
	// Populated only by TopAS and ASAsymmetry — omitempty on the peer/link paths.
	BytesIn  uint64  `json:"bytes_in,omitempty"`
	BytesOut uint64  `json:"bytes_out,omitempty"`
	Ratio    float64 `json:"ratio,omitempty"`
	Class    string  `json:"class,omitempty"`
}

// CountryTraffic represents traffic aggregated to a single country, derived by
// joining the AS traffic tables to as_names.country (AS-level geo, no per-IP
// lookup). Country is the ISO 3166-1 alpha-2 code, or "Unknown" when the source
// AS has no country populated. Name is the resolved human-readable country name
// (optional — empty if the code is not recognised).
type CountryTraffic struct {
	Country string  `json:"country"`
	Name    string  `json:"name,omitempty"`
	Bytes   uint64  `json:"bytes"`
	Packets uint64  `json:"packets"`
	Flows   uint64  `json:"flows"`
	Percent float64 `json:"pct"`
}

// IPTraffic represents traffic statistics for a single IP.
type IPTraffic struct {
	IP         string  `json:"ip"`
	ASNumber   uint32  `json:"as_number"`
	ASName     string  `json:"as_name"`
	Bytes      uint64  `json:"bytes"`
	Packets    uint64  `json:"packets"`
	Flows      uint64  `json:"flows"`
	AvgPktSize float64 `json:"avg_pkt_size"`
}

// PrefixTraffic represents traffic statistics for a single prefix.
type PrefixTraffic struct {
	Prefix     string  `json:"prefix"`
	ASNumber   uint32  `json:"as_number"`
	ASName     string  `json:"as_name"`
	Bytes      uint64  `json:"bytes"`
	Packets    uint64  `json:"packets"`
	Flows      uint64  `json:"flows"`
	AvgPktSize float64 `json:"avg_pkt_size"`
}

// Conversation is one bidirectional top-talker row (F3). A→B and B→A flows are
// folded into a single row via a canonical (least/greatest) ordering of the two
// endpoints, so a pair appears once regardless of who initiated. Forward is the
// A→B direction (the canonically-lower endpoint as source); Reverse is B→A.
type Conversation struct {
	EndpointA      string `json:"endpoint_a"`
	EndpointB      string `json:"endpoint_b"`
	TotalBytes     uint64 `json:"total_bytes"`
	TotalPackets   uint64 `json:"total_packets"`
	ForwardBytes   uint64 `json:"forward_bytes"`
	ForwardPackets uint64 `json:"forward_packets"`
	ReverseBytes   uint64 `json:"reverse_bytes"`
	ReversePackets uint64 `json:"reverse_packets"`
	Flows          uint64 `json:"flows"`
}

// HeatmapCell is one cell of the day-of-week × hour-of-day throughput heatmap
// (U8). Day follows the ClickHouse toDayOfWeek convention (1=Monday .. 7=Sunday)
// and Hour is 0-23. MeanBps is the average throughput across all hourly buckets
// that fell in this (Day, Hour) slot over the selected window; PeakBps is the
// maximum across those buckets. Both are bits per second.
type HeatmapCell struct {
	Day     uint8   `json:"day"`
	Hour    uint8   `json:"hour"`
	MeanBps float64 `json:"mean_bps"`
	PeakBps float64 `json:"peak_bps"`
}

// HeatmapData is the full 7×24 grid returned by GET /traffic/heatmap. Every
// (Day, Hour) slot is always present — slots with no data are zero-filled — so
// the frontend can render a dense grid without gap handling.
type HeatmapData struct {
	Cells []HeatmapCell `json:"cells"`
}

// LinkTraffic represents traffic statistics for a link.
type LinkTraffic struct {
	Tag          string `json:"tag"`
	Description  string `json:"description"`
	CapacityMbps uint32 `json:"capacity_mbps,omitempty"`
	BytesIn      uint64 `json:"bytes_in"`
	BytesOut     uint64 `json:"bytes_out"`
}

// LinkTimeSeries represents traffic time series for a single link.
type LinkTimeSeries struct {
	Tag         string         `json:"link_tag"`
	Description string         `json:"description"`
	Points      []TrafficPoint `json:"points"`
}

// ASTrafficDetail represents an AS with time series split by link and direction.
type ASTrafficDetail struct {
	ASNumber uint32           `json:"as_number"`
	ASName   string           `json:"as_name"`
	Bytes    uint64           `json:"bytes"`
	P95In    uint64           `json:"p95_in,omitempty"`
	P95Out   uint64           `json:"p95_out,omitempty"`
	Series   []LinkTimeSeries `json:"series"`
}

// Overview represents the dashboard overview data.
type Overview struct {
	TotalBytesIn  uint64        `json:"total_bytes_in"`
	TotalBytesOut uint64        `json:"total_bytes_out"`
	TotalFlows    uint64        `json:"total_flows"`
	ActiveASCount uint64        `json:"active_as_count"`
	Links         []LinkTraffic `json:"links"`
}

// FlowLogEntry represents one row from the flows_log table.
type FlowLogEntry struct {
	Timestamp    time.Time `json:"ts"`
	LinkTag      string    `json:"link_tag"`
	SrcIP        string    `json:"src_ip"`
	DstIP        string    `json:"dst_ip"`
	SrcAS        uint32    `json:"src_as"`
	DstAS        uint32    `json:"dst_as"`
	Protocol     uint8     `json:"protocol"`
	ProtocolName string    `json:"protocol_name,omitempty"`
	SrcPort      uint16    `json:"src_port"`
	DstPort      uint16    `json:"dst_port"`
	Service      string    `json:"service,omitempty"`
	TCPFlags     uint8     `json:"tcp_flags,omitempty"`
	IPVersion    uint8     `json:"ip_version"`
	Bytes        uint64    `json:"bytes"`
	Packets      uint64    `json:"packets"`
	FlowCount    uint64    `json:"flow_count"`
}

// FlowSearchFilters holds all filters for a flow search query.
type FlowSearchFilters struct {
	From      time.Time
	To        time.Time
	SrcIP     string // single IP or CIDR
	DstIP     string
	SrcAS     uint32
	DstAS     uint32
	Protocol  uint8  // 0 = any
	SrcPort   uint16 // 0 = any
	DstPort   uint16 // 0 = any
	Port      uint16 // 0 = any; matches either side (Top Ports drill-down: service port)
	LinkTag   string
	MinBytes  uint64
	IPVersion uint8 // 0 = any
	Limit     int
	Offset    int
	OrderBy   string // 'ts', 'bytes' (default 'bytes')
}

// ProtocolTraffic represents aggregated traffic for one protocol.
type ProtocolTraffic struct {
	Protocol     uint8   `json:"protocol"`
	ProtocolName string  `json:"protocol_name"`
	Direction    string  `json:"direction"`
	Bytes        uint64  `json:"bytes"`
	Packets      uint64  `json:"packets"`
	Flows        uint64  `json:"flows"`
	Percent      float64 `json:"pct"`
}

// PortTraffic represents aggregated traffic for one (protocol, port) tuple.
type PortTraffic struct {
	Protocol     uint8   `json:"protocol"`
	ProtocolName string  `json:"protocol_name"`
	Port         uint16  `json:"port"`
	Service      string  `json:"service,omitempty"`
	Direction    string  `json:"direction"`
	Bytes        uint64  `json:"bytes"`
	Packets      uint64  `json:"packets"`
	Flows        uint64  `json:"flows"`
	Percent      float64 `json:"pct"`
}

// Hostgroup is a named collection of CIDR prefixes. Rules can be scoped to
// a hostgroup instead of the global LOCAL_AS prefixes, enabling per-segment
// thresholds (e.g. "CDN: 10 Gbps" vs "mail: 100 Mbps").
type Hostgroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CIDRs       []string  `json:"cidrs"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AlertRule is a configurable DDoS detection rule.
type AlertRule struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	RuleType        string    `json:"rule_type"` // volume_in, volume_out, syn_flood, amplification, port_scan, icmp_flood, udp_flood, connection_flood, subnet_flood, smtp_abuse, disk_usage, link_capacity, anomaly, custom
	Enabled         bool      `json:"enabled"`
	ThresholdBps    uint64    `json:"threshold_bps,omitempty"`
	ThresholdPps    uint64    `json:"threshold_pps,omitempty"`
	ThresholdCount  uint64    `json:"threshold_count,omitempty"`
	WindowSeconds   uint32    `json:"window_seconds"`
	CooldownSeconds uint32    `json:"cooldown_seconds"`
	Severity        string    `json:"severity"` // info, warning, critical
	TargetFilter    string    `json:"target_filter,omitempty"`
	CustomSQL       string    `json:"custom_sql,omitempty"`
	Action          string    `json:"action"` // notify, ack_required, auto_block
	WebhookIDs      []string  `json:"webhook_ids,omitempty"`
	HostgroupID     string    `json:"hostgroup_id,omitempty"`      // zero UUID or empty = use global LOCAL_AS
	SubnetPrefixLen uint8     `json:"subnet_prefix_len,omitempty"` // only for subnet_flood: IPv4 prefix length (e.g. 24)
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Alert represents a triggered alert instance.
type Alert struct {
	ID             string     `json:"id"`
	RuleID         string     `json:"rule_id"`
	RuleName       string     `json:"rule_name"`
	Severity       string     `json:"severity"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	TargetIP       string     `json:"target_ip"`
	TargetAS       uint32     `json:"target_as,omitempty"`
	Protocol       uint8      `json:"protocol,omitempty"`
	MetricValue    float64    `json:"metric_value"`
	Threshold      float64    `json:"threshold"`
	MetricType     string     `json:"metric_type"` // bps, pps, count
	Details        string     `json:"details,omitempty"`
	Status         string     `json:"status"` // active, acknowledged, resolved, muted
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ActionTaken    string     `json:"action_taken,omitempty"`
	ActionBy       string     `json:"action_by,omitempty"`
	ActionAt       *time.Time `json:"action_at,omitempty"`
}

// WebhookConfig is a notification webhook (Slack, Teams, generic).
type WebhookConfig struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	WebhookType string    `json:"webhook_type"` // slack, teams, discord, generic
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	MinSeverity string    `json:"min_severity"`      // info, warning, critical
	Headers     string    `json:"headers,omitempty"` // JSON
	Template    string    `json:"template,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AuditLogEntry represents one audit trail row.
type AuditLogEntry struct {
	Timestamp    time.Time `json:"ts"`
	UserSub      string    `json:"user_sub,omitempty"`
	UserEmail    string    `json:"user_email,omitempty"`
	UserRole     string    `json:"user_role,omitempty"`
	Action       string    `json:"action"`
	Resource     string    `json:"resource,omitempty"`
	Params       string    `json:"params,omitempty"`
	ClientIP     string    `json:"client_ip"`
	UserAgent    string    `json:"user_agent,omitempty"`
	Result       string    `json:"result"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// BGPBlock represents a BGP blackhole block record (active or withdrawn).
type BGPBlock struct {
	ID              string     `json:"id"`
	IP              string     `json:"ip"`
	PrefixLen       uint8      `json:"prefix_len"`
	Community       string     `json:"community"`
	NextHop         string     `json:"next_hop,omitempty"`
	Reason          string     `json:"reason"` // "auto_block" or "manual"
	Description     string     `json:"description"`
	Status          string     `json:"status"` // "active" or "withdrawn"
	BlockedBy       string     `json:"blocked_by"`
	BlockedAt       time.Time  `json:"blocked_at"`
	UnblockedBy     string     `json:"unblocked_by,omitempty"`
	UnblockedAt     *time.Time `json:"unblocked_at,omitempty"`
	UnblockReason   string     `json:"unblock_reason,omitempty"`
	AlertID         string     `json:"alert_id,omitempty"`
	RuleName        string     `json:"rule_name,omitempty"`
	MetricValue     float64    `json:"metric_value,omitempty"`
	MetricType      string     `json:"metric_type,omitempty"`
	Threshold       float64    `json:"threshold,omitempty"`
	TopSources      []string   `json:"top_sources,omitempty"`
	DurationSeconds uint32     `json:"duration_seconds,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

// Live Threats (GET /threats/live): a snapshot of the hot 1-minute tables,
// ranked by how far each local host is from its own usual traffic rather
// than by raw volume. CLAUDE.md "Live Threats" has the definitions.

// Live Threats levels. "normal" rows are counted but only listed with all=1.
const (
	ThreatLevelNormal   = "normal"
	ThreatLevelWatch    = "watch"
	ThreatLevelIncident = "incident"
)

// LiveThreats is the payload of GET /threats/live.
type LiveThreats struct {
	Direction     string      `json:"direction"` // "in" (local destinations) | "out" (local sources)
	WindowSeconds uint32      `json:"window_seconds"`
	Checked       int         `json:"checked"` // candidates evaluated
	Normal        int         `json:"normal"`  // of which level == "normal"
	Rows          []ThreatRow `json:"rows"`
}

// ThreatRow is one local host in the Live Threats table.
type ThreatRow struct {
	IP          string            `json:"ip"` // IPv4 without ::ffff:
	Level       string            `json:"level"`
	Score       float64           `json:"score"`
	Now         ThreatNow         `json:"now"`
	Baseline    ThreatBaseline    `json:"baseline"`
	Ratio       float64           `json:"ratio"`
	Since       string            `json:"since"` // RFC 3339 minute; "" when not above its usual now
	Series      []ThreatPoint     `json:"series"`
	Signatures  []ThreatSignature `json:"signatures"`
	ClosestRule string            `json:"closest_rule"`
	RulePct     float64           `json:"rule_pct"`
}

// ThreatNow holds a host's window-averaged rates. Peers are unique source
// IPs (in) or destination IPs (out) over the window; peer ports likewise.
type ThreatNow struct {
	BPS            uint64  `json:"bps"`
	PPS            uint64  `json:"pps"`
	FlowsPerSec    float64 `json:"flows_per_sec"`
	TCPPPS         uint64  `json:"tcp_pps"`
	UDPPPS         uint64  `json:"udp_pps"`
	ICMPPPS        uint64  `json:"icmp_pps"`
	SynPPS         uint64  `json:"syn_pps"`
	AvgPacketBytes uint64  `json:"avg_packet_bytes"`
	Peers          uint64  `json:"peers"`
	PeerPorts      uint64  `json:"peer_ports"`
}

// ThreatBaseline is a host's usual per-minute traffic: the same hour of day
// over the previous 7 days. Samples counts the minutes found (0 = new host).
type ThreatBaseline struct {
	Samples uint64 `json:"samples"`
	BPSP50  uint64 `json:"bps_p50"`
	BPSP95  uint64 `json:"bps_p95"`
	PPSP50  uint64 `json:"pps_p50"`
	PPSP95  uint64 `json:"pps_p95"`
}

// ThreatPoint is one complete minute of a host's traffic.
type ThreatPoint struct {
	T   time.Time `json:"t"`
	BPS uint64    `json:"bps"`
	PPS uint64    `json:"pps"`
}

// ThreatSignature names the attack shape a row matches.
type ThreatSignature struct {
	Kind     string       `json:"kind"`
	TopPorts []ThreatPort `json:"top_ports,omitempty"`
}

// ThreatPort is one port's share of a host's traffic in the lookup window
// (bytes for amplification, flows for smtp_burst).
type ThreatPort struct {
	Port     uint16  `json:"port"`
	Protocol uint8   `json:"protocol"`
	Pct      float64 `json:"pct"`
}

// AnomalyContributorAS is one source AS contributing to a link's traffic during
// an anomaly window, ranked by bytes (Module E explainability).
type AnomalyContributorAS struct {
	ASNumber uint32  `json:"as_number"`
	ASName   string  `json:"as_name,omitempty"`
	Bytes    uint64  `json:"bytes"`
	Packets  uint64  `json:"packets"`
	Percent  float64 `json:"pct"`
}

// AnomalyContributorIP is one source IP contributing to a link's traffic during
// an anomaly window, ranked by bytes (Module E explainability).
type AnomalyContributorIP struct {
	IP      string  `json:"ip"`
	Bytes   uint64  `json:"bytes"`
	Packets uint64  `json:"packets"`
	Percent float64 `json:"pct"`
}

// AnomalyContributorPort is one destination (protocol, port) contributing to a
// link's traffic during an anomaly window, ranked by bytes (Module E).
type AnomalyContributorPort struct {
	Protocol     uint8   `json:"protocol"`
	ProtocolName string  `json:"protocol_name,omitempty"`
	Port         uint16  `json:"port"`
	Service      string  `json:"service,omitempty"`
	Bytes        uint64  `json:"bytes"`
	Packets      uint64  `json:"packets"`
	Percent      float64 `json:"pct"`
}

// AnomalyExplanation decomposes the traffic on a link during a window into its
// top contributing source ASes, source IPs, and destination ports by bytes
// (Module E). It answers "why did this link's throughput spike" without a full
// flow search. Returned by GET /anomaly/explain and also attached to an alert's
// details when an anomaly rule fires.
type AnomalyExplanation struct {
	Target     string                   `json:"target"` // link tag
	From       time.Time                `json:"from"`
	To         time.Time                `json:"to"`
	TopASes    []AnomalyContributorAS   `json:"top_ases"`
	TopSources []AnomalyContributorIP   `json:"top_sources"`
	TopPorts   []AnomalyContributorPort `json:"top_ports"`
}

// RetentionPolicy is the desired retention for one TTL-bearing table, stored in
// the retention_policies table and applied by the reconciler goroutine.
type RetentionPolicy struct {
	TableName string `json:"table_name"`
	TTLColumn string `json:"ttl_column"`
	TTLDays   uint32 `json:"ttl_days"`
	// ColdAfterDays moves data older than this to the `cold` volume of the
	// table's storage policy (0 = no cold tier). Only effective on tables whose
	// storage policy has a volume named `cold`; always < TTLDays when set.
	ColdAfterDays uint32    `json:"cold_after_days"`
	Enabled       bool      `json:"enabled"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// APIToken is a read-only programmatic access token (Module G). The plaintext
// token and its SHA-256 hash are NEVER exposed in this struct — token_hash is
// deliberately absent so it can never be serialized to an API response or log.
// The token grants viewer-role, GET/HEAD-only access via a Bearer header.
type APIToken struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TokenPrefix string    `json:"token_prefix"` // first ~10 chars, for display only
	Owner       string    `json:"owner"`
	CreatedAt   time.Time `json:"created_at"`
	LastUsedAt  time.Time `json:"last_used_at"`
	ExpiresAt   time.Time `json:"expires_at"` // Unix <= 0 means "never expires"
	Revoked     bool      `json:"revoked"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// APITokenCreated wraps a freshly minted APIToken together with its one-time
// plaintext value. The plaintext is returned to the caller exactly once (on
// creation) and is never persisted or retrievable again.
type APITokenCreated struct {
	APIToken
	Token string `json:"token"` // plaintext — shown once, never stored
}

// TableStorageStats holds per-table storage observability metrics derived from
// system.parts / system.mutations and joined with the configured retention.
type TableStorageStats struct {
	Table             string     `json:"table"`
	CompressedBytes   uint64     `json:"compressed_bytes"`
	UncompressedBytes uint64     `json:"uncompressed_bytes"`
	Parts             uint64     `json:"parts"`
	Rows              uint64     `json:"rows"`
	OldestData        *time.Time `json:"oldest_data,omitempty"`
	NewestData        *time.Time `json:"newest_data,omitempty"`
	TTLDays           uint32     `json:"ttl_days"`          // configured retention (0 = unmanaged)
	TTLEnabled        bool       `json:"ttl_enabled"`       // whether the reconciler manages this table
	PendingMutations  uint64     `json:"pending_mutations"` // not-yet-done mutations (incl. TTL materialization)

	// RetentionFollows names the table whose retention policy the reconciler
	// applies to this one (TTLDays / TTLEnabled / ColdAfterDays above are that
	// policy); it cannot be edited on its own. Legacy marks a pre-upgrade copy
	// kept read-only until it ages out (flows_log_legacy, migration 000017;
	// traffic_by_port_legacy, migration 000018 — the latter is never read).
	RetentionFollows string `json:"retention_follows,omitempty"`
	Legacy           bool   `json:"legacy,omitempty"`

	// Hot / cold tier. StoragePolicy is the table's live storage policy;
	// HasColdVolume is true when that policy has a volume named `cold` (the
	// only case where ColdAfterDays has any effect). BytesOnDisk is split by
	// disk in Disks and summarised as HotBytes (any non-cold volume) / ColdBytes.
	StoragePolicy string           `json:"storage_policy"`
	HasColdVolume bool             `json:"has_cold_volume"`
	ColdAfterDays uint32           `json:"cold_after_days"` // configured (0 = no cold tier)
	BytesOnDisk   uint64           `json:"bytes_on_disk"`
	HotBytes      uint64           `json:"hot_bytes"`
	ColdBytes     uint64           `json:"cold_bytes"`
	Disks         []TableDiskBytes `json:"disks"`
}

// TableDiskBytes is one table's active-part footprint on one disk.
type TableDiskBytes struct {
	Disk  string `json:"disk"`
	Bytes uint64 `json:"bytes"`
	Parts uint64 `json:"parts"`
	Cold  bool   `json:"cold"` // disk belongs to the policy's `cold` volume
}

// DiskStats holds usage figures for one ClickHouse disk (system.disks).
type DiskStats struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	FreeBytes   uint64  `json:"free_bytes"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// StorageStats is the response payload for GET /admin/storage.
type StorageStats struct {
	Tables []TableStorageStats `json:"tables"`
	Disks  []DiskStats         `json:"disks"`
}

// LinkCapacity summarizes utilization and a linear-regression saturation
// forecast for one link over a selected window. It is the row type for
// GET /links/capacity.
//
// UtilizationPct is nil when the link has no configured capacity (capacity_mbps
// == 0), because a percentage against an unknown ceiling is meaningless.
//
// ForecastDaysNN estimate how many days until the daily p95 trend (linear
// regression over ~90 days of traffic_by_link_daily) crosses NN% of capacity:
//   - nil  when capacity is unset OR the trend is flat/declining (slope <= 0)
//     OR there are too few daily samples to fit a line
//   - 0    when the projected value already meets/exceeds the level
//   - >0   estimated days from now otherwise
type LinkCapacity struct {
	Tag             string   `json:"tag"`
	Description     string   `json:"description"`
	CapacityMbps    uint32   `json:"capacity_mbps"`
	// A link is full duplex: every *_bps without a direction is the BUSIER
	// direction, max(in, out) — never in+out.
	CurrentInBps       uint64   `json:"current_in_bps"`  // last complete bucket
	CurrentOutBps      uint64   `json:"current_out_bps"` // last complete bucket
	CurrentBps         uint64   `json:"current_bps"`     // max(current_in_bps, current_out_bps)
	PeakInBps          uint64   `json:"peak_in_bps"`     // busiest bucket over the window
	PeakOutBps         uint64   `json:"peak_out_bps"`
	PeakUtilizationPct *float64 `json:"peak_utilization_pct"` // max(peak_in, peak_out) / capacity; nil without capacity
	P95InBps           uint64   `json:"p95_in_bps"`           // p95 of per-bucket throughput
	P95OutBps          uint64   `json:"p95_out_bps"`
	P95Bps             uint64   `json:"p95_bps"`         // max(p95_in_bps, p95_out_bps)
	UtilizationPct     *float64 `json:"utilization_pct"` // p95_bps / capacity; nil without capacity
	// Trend and forecasts are fitted on the daily p95 of max(in, out).
	TrendBpsPerDay  float64  `json:"trend_bps_per_day,omitempty"`
	ForecastDays80  *float64 `json:"forecast_days_80,omitempty"`
	ForecastDays95  *float64 `json:"forecast_days_95,omitempty"`
	ForecastDays100 *float64 `json:"forecast_days_100,omitempty"`
}

// LoadCurveQuantiles holds the standard load-duration quantiles, in bps.
type LoadCurveQuantiles struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	P100 float64 `json:"p100"`
}

// HistogramBin is one bucket of a throughput histogram (bps range + count).
type HistogramBin struct {
	LowerBps float64 `json:"lower_bps"`
	UpperBps float64 `json:"upper_bps"`
	Count    uint64  `json:"count"`
}

// LoadCurve is a load-duration curve for a link over a window: per-bucket
// throughput samples sorted descending (downsampled to <=500 points for the
// chart), summary quantiles, and a ~20-bin histogram. It is the payload for
// GET /link/{tag}/load-curve.
type LoadCurve struct {
	Tag         string             `json:"tag"`
	SampleCount int                `json:"sample_count"`
	Points      []float64          `json:"points"` // bps, sorted descending
	Quantiles   LoadCurveQuantiles `json:"quantiles"`
	Histogram   []HistogramBin     `json:"histogram"`
}

// Percentiles holds p50/p95/p99 of per-bucket throughput (bytes per bucket) for
// one direction on a link (Module D). Surfaced alongside the existing p95 on the
// link detail response.
type Percentiles struct {
	P50 uint64 `json:"p50"`
	P95 uint64 `json:"p95"`
	P99 uint64 `json:"p99"`
}

// Mover is one entity's change in traffic volume between the current window and
// the immediately-prior equal-length window (Module D). Key is the entity
// identity within the dimension: the ASN (as string) for "as", the prefix for
// "prefix", "<protocol>/<port>" for "port", or the ISO country code (or
// "Unknown") for "country". Label is an optional human name (AS name). Delta is
// Current - Previous (signed); Pct is the relative change vs. Previous (0 when
// Previous is 0, since the change is then unbounded — such entities surface as
// new/gone talkers instead).
type Mover struct {
	Dimension string  `json:"dimension"`
	Key       string  `json:"key"`
	Label     string  `json:"label,omitempty"`
	Current   uint64  `json:"current"`
	Previous  uint64  `json:"previous"`
	Delta     int64   `json:"delta"`
	Pct       float64 `json:"pct"`
}

// TalkerChange is an entity that either APPEARED (no traffic in the prior
// window, traffic now — Status "new") or DISAPPEARED (traffic in the prior
// window, none now — Status "gone") between the current and prior equal-length
// windows (Module D). Key/Label follow the same convention as Mover for the
// supported dimensions (as/ip/prefix). Bytes is the non-zero volume (current
// for "new", previous for "gone").
type TalkerChange struct {
	Dimension string `json:"dimension"`
	Key       string `json:"key"`
	Label     string `json:"label,omitempty"`
	Bytes     uint64 `json:"bytes"`
	Status    string `json:"status"` // "new" | "gone"
}

// ReportSchedule is a scheduled report definition (Module D). A cron goroutine
// in the collector renders an HTML summary + CSV over a frequency-derived window
// (daily → last 24h, weekly → 7d, monthly → 30d) and delivers it via SMTP.
//
// Frequency is one of "daily" | "weekly" | "monthly". Hour is the UTC hour of
// day (0-23) the report fires. DayOfWeek (0-6, 0 = Sunday) applies to weekly
// schedules; DayOfMonth (1-28) applies to monthly. Recipients and Sections are
// comma-separated (recipients = email addresses; sections = subset of
// overview,top_as,top_country,capacity,alerts). Format is "html" | "csv" |
// "both". LastRunAt is stamped after each successful send and used to dedupe a
// single occurrence.
type ReportSchedule struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Frequency  string    `json:"frequency"`
	Hour       uint8     `json:"hour"`
	DayOfWeek  uint8     `json:"day_of_week"`
	DayOfMonth uint8     `json:"day_of_month"`
	Recipients string    `json:"recipients"`
	Sections   string    `json:"sections"`
	Format     string    `json:"format"`
	Enabled    bool      `json:"enabled"`
	LastRunAt  time.Time `json:"last_run_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Router liveness states reported by GET /status/live.
const (
	RouterStateOK      = "ok"      // last flow less than 120 s ago
	RouterStateStale   = "stale"   // last flow 120 s – 600 s ago
	RouterStateMissing = "missing" // referenced by a configured link, no flow in the last 10 min
)

// RouterLiveStatus is one exporter's liveness over the last 10 minutes.
type RouterLiveStatus struct {
	RouterIP string `json:"router_ip"` // IPv4 without the ::ffff: prefix
	LastSeen string `json:"last_seen"` // RFC 3339, "" when nothing in the window
	Flows10m uint64 `json:"flows_10m"`
	State    string `json:"state"`
}

// LiveStatus is the payload of GET /status/live: a cheap health probe for the
// header dot and the home status strip (reads 10 minutes of flows_raw only).
type LiveStatus struct {
	// Healthy is true iff at least one router is known and every router is "ok".
	Healthy bool               `json:"healthy"`
	Routers []RouterLiveStatus `json:"routers"`
	// DiskUsedPct is the highest used-% across ClickHouse disks; nil if unknown.
	DiskUsedPct *float64 `json:"disk_used_pct"`
}
