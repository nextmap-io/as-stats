/** Traffic-asymmetry class (F2). "content" = mostly egress (hosting/CDN),
 *  "eyeball" = mostly ingress (access/broadband), "balanced" otherwise. */
export type AsymmetryClass = "eyeball" | "content" | "balanced"

export interface ASTraffic {
  as_number: number
  as_name: string
  /** ISO 3166-1 alpha-2 code from as_names.country. Optional — empty when the
   *  source AS has no country populated or the backend build predates Module C. */
  country?: string
  bytes: number
  packets: number
  flows: number
  pct: number
  /** Mean packet size = sum(bytes)/max(sum(packets),1) (F1). Present on the
   *  Top-N paths; 0 on peer/link paths that don't compute it. */
  avg_pkt_size: number
  /** In/out asymmetry (F2). Populated by /top/as and /as/{asn}; omitted
   *  (undefined) on peer/link responses. `ratio` = bytes_out / max(bytes_in,1). */
  bytes_in?: number
  bytes_out?: number
  ratio?: number
  class?: AsymmetryClass
}

/** Traffic aggregated to a single country (AS-level geo, no per-IP lookup).
 *  Mirrors internal/model.CountryTraffic. `country` is the ISO 3166-1 alpha-2
 *  code (or "Unknown"); `name` is the resolved human-readable name (optional). */
export interface CountryTraffic {
  country: string
  name?: string
  bytes: number
  packets: number
  flows: number
  pct: number
}

// Traffic heatmap (U8). Mirrors internal/model.HeatmapCell / HeatmapData. The
// backend always returns a dense 7×24 grid (day 1=Monday..7=Sunday, hour 0-23),
// zero-filling absent slots, so the frontend never handles gaps. Both rates are
// bits per second.
export interface HeatmapCell {
  day: number
  hour: number
  mean_bps: number
  peak_bps: number
}

export interface HeatmapData {
  cells: HeatmapCell[]
}

export interface IPTraffic {
  ip: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
  /** Mean packet size = sum(bytes)/max(sum(packets),1) (F1). */
  avg_pkt_size: number
}

export interface PrefixTraffic {
  prefix: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
  /** Mean packet size = sum(bytes)/max(sum(packets),1) (F1). */
  avg_pkt_size: number
}

/** One bidirectional top-talker row (F3). Mirrors internal/model.Conversation.
 *  A→B and B→A are folded into a single row via a canonical endpoint ordering;
 *  forward is the A→B direction, reverse is B→A. For the dst_port_proto
 *  dimension endpoint_a is the protocol number and endpoint_b the dst port,
 *  both as strings, and everything counts as forward (no reverse). */
export interface Conversation {
  endpoint_a: string
  endpoint_b: string
  total_bytes: number
  total_packets: number
  forward_bytes: number
  forward_packets: number
  reverse_bytes: number
  reverse_packets: number
  flows: number
}

export interface TrafficPoint {
  t: string
  bytes_in: number
  bytes_out: number
  packets_in?: number
  packets_out?: number
}

export interface LinkTraffic {
  tag: string
  description: string
  capacity_mbps?: number
  bytes_in: number
  bytes_out: number
}

export interface ASInfo {
  number: number
  name: string
  country: string
}

export interface Overview {
  total_bytes_in: number
  total_bytes_out: number
  total_flows: number
  active_as_count: number
  top_as: ASTraffic[]
  top_ip: IPTraffic[]
  links: LinkTraffic[]
}

export interface ApiResponse<T> {
  data: T
  meta?: {
    from: string
    to: string
    total_bytes?: number
    limit?: number
    offset?: number
  }
  error?: string
}

export interface QueryFilters {
  from?: string
  to?: string
  period?: string
  link?: string
  direction?: string
  ip_version?: number
  scope?: string
  limit?: number
  offset?: number
  q?: string
  /** Multi-metric Top-N sort key (F1): bytes | packets | flows. */
  metric?: string
  /** Conversation grouping dimension (F3): src_dst_ip | src_dst_as | dst_port_proto. */
  dim?: string
  /** Changes dimension (Module D): as | prefix | port | country (movers) or
   *  as | ip | prefix (talkers). */
  dimension?: string
  /** Comparison window preset/duration for the /changes endpoints (optional —
   *  from/to/period already carry the window otherwise). */
  window?: string
  /** Comparison overlay toggle (Module D). "prev" enables the previous-period
   *  overlay on time-series charts. */
  compare?: string
  /** Anomaly explainability target (Module E): the link tag to decompose. */
  target?: string
}

export interface ASDetailData {
  as_number: number
  as_name: string
  /** ISO 3166-1 alpha-2 code (optional — see ASTraffic.country). */
  country?: string
  time_series: TrafficPoint[]
  v4_series?: LinkTimeSeries[]
  v6_series?: LinkTimeSeries[]
  v4_bytes_in?: number
  v4_bytes_out?: number
  v6_bytes_in?: number
  v6_bytes_out?: number
  p95_v4_in?: number
  p95_v4_out?: number
  p95_v6_in?: number
  p95_v6_out?: number
  /** In/out asymmetry (F2). bytes_in/bytes_out are direction totals over the
   *  window; ratio = bytes_out / max(bytes_in,1); class classifies the mix. */
  bytes_in?: number
  bytes_out?: number
  ratio?: number
  class?: AsymmetryClass
}

export interface IPDetailData {
  ip: string
  as_number?: number
  as_name?: string
  prefix?: string
  ptr?: string
  time_series: TrafficPoint[]
  top_as: ASTraffic[]
  peer_ips?: IPTraffic[]
}

export interface LinkDetailData {
  tag: string
  time_series: TrafficPoint[]
  top_as: ASTraffic[]
  as_series?: ASTrafficDetail[]
  p95_in?: number
  p95_out?: number
  // p50/p95/p99 of per-bucket throughput (bytes per bucket), per direction
  // (Module D). Surfaced alongside p95_in/p95_out on the link detail response.
  p50_in?: number
  p50_out?: number
  p99_in?: number
  p99_out?: number
  capacity_mbps?: number
  // p95 of per-bucket (in+out) throughput as a fraction of capacity, %.
  // null when capacity is unset.
  utilization_pct?: number | null
}

// =============================================================================
// Comparison — movers / talkers (Module D). Mirrors internal/model.Mover,
// internal/model.TalkerChange, and the handler's MoversResponse/TalkersResponse.
// =============================================================================

/** One entity's change in total bytes between the current window and the
 *  immediately-prior equal-length window. `key` is the entity identity within
 *  the dimension (ASN as string for "as", the prefix for "prefix",
 *  "<protocol>/<port>" for "port", ISO country code for "country"); `label` is
 *  an optional human name. `delta` = current − previous (signed); `pct` is the
 *  relative change vs. previous (0 when previous is 0). */
export interface Mover {
  dimension: string
  key: string
  label?: string
  current: number
  previous: number
  delta: number
  pct: number
}

/** An entity that appeared ("new" — no prior traffic, traffic now) or
 *  disappeared ("gone" — prior traffic, none now) between the current and prior
 *  equal-length windows. `bytes` is the non-zero volume (current for "new",
 *  previous for "gone"). */
export interface TalkerChange {
  dimension: string
  key: string
  label?: string
  bytes: number
  status: "new" | "gone"
}

/** Payload of GET /changes/movers. `gainers` (delta > 0) and `losers`
 *  (delta < 0) are each ranked by |delta| desc. */
export interface MoversResponse {
  dimension: string
  gainers: Mover[]
  losers: Mover[]
}

/** Payload of GET /changes/talkers. */
export interface TalkersResponse {
  dimension: string
  new: TalkerChange[]
  gone: TalkerChange[]
}

// =============================================================================
// Capacity planning (Module B) — mirrors internal/model.LinkCapacity / LoadCurve
// =============================================================================

// LinkCapacity mirrors internal/model.LinkCapacity — one row of GET /links/capacity.
// current_bps / p95_bps are bits-per-second (already scaled server-side).
// utilization_pct is null when the link has no configured capacity.
// forecast_days_* estimate days until the daily p95 trend crosses NN% of capacity:
// absent/null when capacity is unset or the trend is flat/declining; 0 when
// already at/over the level; >0 otherwise.
export interface LinkCapacity {
  tag: string
  description: string
  capacity_mbps: number
  current_bps: number
  p95_bps: number
  utilization_pct: number | null
  trend_bps_per_day?: number
  forecast_days_80?: number | null
  forecast_days_95?: number | null
  forecast_days_100?: number | null
}

// LoadCurveQuantiles mirrors internal/model.LoadCurveQuantiles — bps.
export interface LoadCurveQuantiles {
  p50: number
  p90: number
  p95: number
  p99: number
  p100: number
}

// HistogramBin mirrors internal/model.HistogramBin — one throughput bucket.
export interface HistogramBin {
  lower_bps: number
  upper_bps: number
  count: number
}

// LoadCurve mirrors internal/model.LoadCurve — payload of GET /link/{tag}/load-curve.
// points are bits-per-second, sorted descending, downsampled to <=500 points.
export interface LoadCurve {
  tag: string
  sample_count: number
  points: number[]
  quantiles: LoadCurveQuantiles
  histogram: HistogramBin[]
}

export interface LinkTimeSeries {
  link_tag: string
  description: string
  points: TrafficPoint[]
}

export interface ASTrafficDetail {
  as_number: number
  as_name: string
  bytes: number
  p95_in?: number
  p95_out?: number
  series: LinkTimeSeries[]
}

export interface LinkConfig {
  tag: string
  router_ip: string
  snmp_index: number
  description: string
  capacity_mbps: number
  color: string
}

export interface UserInfo {
  sub: string
  name: string
  email: string
  role: string
}

// =============================================================================
// Feature flags
// =============================================================================

export interface Features {
  flow_search: boolean
  port_stats: boolean
  alerts: boolean
  bgp: boolean
  reports: boolean
  local_as?: number
  auth: boolean
}

// =============================================================================
// Scheduled reports (Admin → Reports, Module D)
// =============================================================================

// ReportSchedule mirrors internal/model.ReportSchedule exactly. A cron
// goroutine in the collector renders an HTML summary + CSV over a
// frequency-derived window (daily → 24h, weekly → 7d, monthly → 30d) and
// delivers it via SMTP. `hour` is the UTC hour (0-23) the report fires;
// `day_of_week` (0-6, 0 = Sunday) applies to weekly; `day_of_month` (1-28)
// to monthly. `recipients` and `sections` are comma-separated.
export type ReportFrequency = "daily" | "weekly" | "monthly"
export type ReportFormat = "html" | "csv" | "both"
export type ReportSection = "overview" | "top_as" | "top_country" | "capacity" | "alerts"

export interface ReportSchedule {
  id: string
  name: string
  frequency: ReportFrequency
  hour: number
  day_of_week: number
  day_of_month: number
  recipients: string
  sections: string
  format: ReportFormat
  enabled: boolean
  last_run_at: string
  created_at: string
  updated_at: string
}

// =============================================================================
// Read-only API tokens (Module G, Admin → Tokens)
// =============================================================================

// APIToken mirrors internal/model.APIToken exactly. The plaintext token and its
// SHA-256 hash are NEVER exposed here — only the short display prefix is. Grants
// viewer-role, GET/HEAD-only programmatic access via a Bearer header.
// `expires_at` / `last_used_at` are epoch "1970-01-01T00:00:00Z" when unset
// ("never expires" / "never used").
export interface APIToken {
  id: string
  name: string
  token_prefix: string
  owner: string
  created_at: string
  last_used_at: string
  expires_at: string
  revoked: boolean
  updated_at: string
}

// APITokenCreated mirrors internal/model.APITokenCreated — an APIToken plus the
// one-time plaintext `token`, returned only on creation and never retrievable
// again.
export interface APITokenCreated extends APIToken {
  token: string
}

// =============================================================================
// Retention & storage observability (Admin → Storage)
// =============================================================================

// RetentionPolicy mirrors internal/model.RetentionPolicy — the desired
// retention for one TTL-bearing table, returned by PUT /admin/retention/{table}.
export interface RetentionPolicy {
  table_name: string
  ttl_column?: string
  ttl_days: number
  enabled: boolean
  updated_at?: string
}

// TableStorageStats mirrors internal/model.TableStorageStats — per-table
// observability derived from system.parts / system.mutations.
export interface TableStorageStats {
  table: string
  compressed_bytes: number
  uncompressed_bytes: number
  parts: number
  rows: number
  oldest_data?: string
  newest_data?: string
  ttl_days: number
  ttl_enabled: boolean
  pending_mutations: number
}

// DiskStats mirrors internal/model.DiskStats — usage for one ClickHouse disk.
export interface DiskStats {
  name: string
  free_bytes: number
  total_bytes: number
  used_bytes: number
  used_percent: number
}

// StorageStats mirrors internal/model.StorageStats — payload of GET /admin/storage.
export interface StorageStats {
  tables: TableStorageStats[]
  disks: DiskStats[]
}

// =============================================================================
// Flow search
// =============================================================================

export interface FlowLogEntry {
  ts: string
  link_tag: string
  src_ip: string
  dst_ip: string
  src_as: number
  dst_as: number
  protocol: number
  protocol_name?: string
  src_port: number
  dst_port: number
  service?: string
  tcp_flags?: number
  ip_version: number
  bytes: number
  packets: number
  flow_count: number
}

export interface FlowSearchFilters {
  from?: string
  to?: string
  period?: string
  src_ip?: string
  dst_ip?: string
  src_as?: number
  dst_as?: number
  protocol?: number
  src_port?: number
  dst_port?: number
  link?: string
  min_bytes?: number
  ip_version?: 4 | 6
  limit?: number
  offset?: number
  order_by?: "ts" | "bytes"
}

// =============================================================================
// Port / protocol stats
// =============================================================================

export interface ProtocolTraffic {
  protocol: number
  protocol_name: string
  direction: string
  bytes: number
  packets: number
  flows: number
  pct: number
}

export interface PortTraffic {
  protocol: number
  protocol_name: string
  port: number
  service?: string
  direction: string
  bytes: number
  packets: number
  flows: number
  pct: number
}

// =============================================================================
// Alerts
// =============================================================================

export type AlertSeverity = "info" | "warning" | "critical"
export type AlertStatus = "active" | "acknowledged" | "resolved" | "muted"

export interface Alert {
  id: string
  rule_id: string
  rule_name: string
  severity: AlertSeverity
  triggered_at: string
  last_seen_at: string
  resolved_at?: string
  target_ip: string
  target_as?: number
  protocol?: number
  metric_value: number
  threshold: number
  metric_type: "bps" | "pps" | "count"
  details?: string
  status: AlertStatus
  acknowledged_by?: string
  acknowledged_at?: string
  action_taken?: string
  action_by?: string
  action_at?: string
}

export interface AlertRule {
  id: string
  name: string
  description?: string
  rule_type:
    | "volume_in"
    | "volume_out"
    | "syn_flood"
    | "amplification"
    | "port_scan"
    | "icmp_flood"
    | "udp_flood"
    | "connection_flood"
    | "subnet_flood"
    | "smtp_abuse"
    | "disk_usage"
    | "link_capacity"
    | "anomaly"
    | "custom"
    | ""
  enabled: boolean
  threshold_bps?: number
  threshold_pps?: number
  threshold_count?: number
  window_seconds: number
  cooldown_seconds: number
  severity: AlertSeverity
  target_filter?: string
  custom_sql?: string
  action: "notify" | "ack_required" | "auto_block"
  webhook_ids?: string[]
  hostgroup_id?: string
  subnet_prefix_len?: number
  created_at: string
  updated_at: string
}

export interface Hostgroup {
  id: string
  name: string
  description?: string
  cidrs: string[]
  created_at: string
  updated_at: string
}

export interface WebhookConfig {
  id: string
  name: string
  webhook_type: "slack" | "teams" | "discord" | "generic"
  url: string
  enabled: boolean
  min_severity: AlertSeverity
  headers?: string
  template?: string
  created_at: string
  updated_at: string
}

export interface AlertsSummary {
  total: number
  by_severity: Record<string, number>
}

// =============================================================================
// Anomaly detection + explainability (Module E)
// =============================================================================

/** One source AS contributing to a link's traffic during an anomaly window.
 *  Mirrors internal/model.AnomalyContributorAS. */
export interface AnomalyContributorAS {
  as_number: number
  as_name?: string
  bytes: number
  packets: number
  pct: number
}

/** One source IP contributing to a link's traffic during an anomaly window.
 *  Mirrors internal/model.AnomalyContributorIP. */
export interface AnomalyContributorIP {
  ip: string
  bytes: number
  packets: number
  pct: number
}

/** One destination (protocol, port) contributing to a link's traffic during an
 *  anomaly window. Mirrors internal/model.AnomalyContributorPort. */
export interface AnomalyContributorPort {
  protocol: number
  protocol_name?: string
  port: number
  service?: string
  bytes: number
  packets: number
  pct: number
}

/** Decomposition of a link's traffic over a window into its top contributing
 *  source ASes, source IPs, and destination ports by bytes. Mirrors
 *  internal/model.AnomalyExplanation — payload of GET /anomaly/explain and also
 *  stored under an anomaly alert's details.extra.explanation. */
export interface AnomalyExplanation {
  target: string
  from: string
  to: string
  top_ases: AnomalyContributorAS[]
  top_sources: AnomalyContributorIP[]
  top_ports: AnomalyContributorPort[]
}

/** Rule-type-specific extras merged into an alert's details.extra by the alert
 *  engine. For anomaly rules this carries the baseline statistics and the stored
 *  contributor explanation; other rule types may add their own keys. */
export interface AlertDetailsExtra {
  target?: string
  baseline?: number
  current?: number
  deviation?: number
  samples_count?: number
  sensitivity_k?: number
  explanation?: AnomalyExplanation
  [key: string]: unknown
}

/** Parsed shape of the JSON blob stored in Alert.details. Mirrors
 *  internal/store.AlertDetails. */
export interface AlertDetailsPayload {
  top_sources?: string[]
  unique_count?: number
  window_seconds?: number
  extra?: AlertDetailsExtra
}

// =============================================================================
// Audit log
// =============================================================================

export interface AuditLogEntry {
  ts: string
  user_sub?: string
  user_email?: string
  user_role?: string
  action: string
  resource?: string
  params?: string
  client_ip: string
  user_agent?: string
  result: "success" | "denied" | "error"
  error_message?: string
}

// =============================================================================
// Live threats — pre-trigger DDoS detection view
// =============================================================================

export type ThreatStatus = "ok" | "warn" | "critical"

export interface LiveThreat {
  target_ip: string
  bps: number
  pps: number
  syn_pps: number
  unique_src_ips: number
  worst_pct: number
  worst_rule?: string
  status: ThreatStatus
}

// =============================================================================
// BGP blackhole management
// =============================================================================

export interface BGPBlock {
  id: string
  ip: string
  prefix_len: number
  community: string
  next_hop?: string
  reason: "auto_block" | "manual"
  description: string
  status: "active" | "withdrawn"
  blocked_by: string
  blocked_at: string
  unblocked_by?: string
  unblocked_at?: string
  unblock_reason?: string
  alert_id?: string
  rule_name?: string
  metric_value?: number
  metric_type?: string
  threshold?: number
  top_sources?: string[]
  duration_seconds?: number
  expires_at?: string
}

export interface BGPSessionStatus {
  enabled: boolean
  peer_address?: string
  peer_as?: number
  local_as?: number
  state: string
  uptime: number
  routes_announced: number
}
