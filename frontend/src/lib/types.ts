export interface ASTraffic {
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
  pct: number
}

export interface IPTraffic {
  ip: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
  flows: number
}

export interface PrefixTraffic {
  prefix: string
  as_number: number
  as_name: string
  bytes: number
  packets: number
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
}

export interface ASDetailData {
  as_number: number
  as_name: string
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
  local_as?: number
  auth: boolean
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
