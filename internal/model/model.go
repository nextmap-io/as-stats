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
	Timestamp time.Time `json:"t"`
	BytesIn   uint64    `json:"bytes_in"`
	BytesOut  uint64    `json:"bytes_out"`
	PacketsIn uint64    `json:"packets_in,omitempty"`
	PacketsOut uint64   `json:"packets_out,omitempty"`
}

// ASTraffic represents traffic statistics for a single AS.
type ASTraffic struct {
	ASNumber uint32  `json:"as_number"`
	ASName   string  `json:"as_name"`
	Bytes    uint64  `json:"bytes"`
	Packets  uint64  `json:"packets"`
	Flows    uint64  `json:"flows"`
	Percent  float64 `json:"pct"`
}

// IPTraffic represents traffic statistics for a single IP.
type IPTraffic struct {
	IP       string `json:"ip"`
	ASNumber uint32 `json:"as_number"`
	ASName   string `json:"as_name"`
	Bytes    uint64 `json:"bytes"`
	Packets  uint64 `json:"packets"`
	Flows    uint64 `json:"flows"`
}

// PrefixTraffic represents traffic statistics for a single prefix.
type PrefixTraffic struct {
	Prefix   string `json:"prefix"`
	ASNumber uint32 `json:"as_number"`
	ASName   string `json:"as_name"`
	Bytes    uint64 `json:"bytes"`
	Packets  uint64 `json:"packets"`
	Flows    uint64 `json:"flows"`
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
	TotalBytesIn  uint64       `json:"total_bytes_in"`
	TotalBytesOut uint64       `json:"total_bytes_out"`
	TotalFlows    uint64       `json:"total_flows"`
	ActiveASCount uint64       `json:"active_as_count"`
	TopAS         []ASTraffic  `json:"top_as"`
	TopIP         []IPTraffic  `json:"top_ip"`
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

// AlertRule is a configurable DDoS detection rule.
type AlertRule struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	RuleType        string    `json:"rule_type"` // volume_in, volume_out, syn_flood, amplification, port_scan, custom
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
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Alert represents a triggered alert instance.
type Alert struct {
	ID              string    `json:"id"`
	RuleID          string    `json:"rule_id"`
	RuleName        string    `json:"rule_name"`
	Severity        string    `json:"severity"`
	TriggeredAt     time.Time `json:"triggered_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	TargetIP        string    `json:"target_ip"`
	TargetAS        uint32    `json:"target_as,omitempty"`
	Protocol        uint8     `json:"protocol,omitempty"`
	MetricValue     float64   `json:"metric_value"`
	Threshold       float64   `json:"threshold"`
	MetricType      string    `json:"metric_type"` // bps, pps, count
	Details         string    `json:"details,omitempty"`
	Status          string    `json:"status"` // active, acknowledged, resolved, muted
	AcknowledgedBy  string    `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	ActionTaken     string    `json:"action_taken,omitempty"`
	ActionBy        string    `json:"action_by,omitempty"`
	ActionAt        *time.Time `json:"action_at,omitempty"`
}

// WebhookConfig is a notification webhook (Slack, Teams, generic).
type WebhookConfig struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	WebhookType string    `json:"webhook_type"` // slack, teams, discord, generic
	URL         string    `json:"url"`
	Enabled     bool      `json:"enabled"`
	MinSeverity string    `json:"min_severity"` // info, warning, critical
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

// LiveThreat is one row in the real-time DDoS detection table.
// It is a snapshot from traffic_by_dst_1min over a short window — not a
// triggered alert. The "status" and "worst_pct" fields express how close
// the row is to firing the closest matching alert rule.
type LiveThreat struct {
	TargetIP        string  `json:"target_ip"`
	BPS             uint64  `json:"bps"`
	PPS             uint64  `json:"pps"`
	SynPPS          uint64  `json:"syn_pps"`
	UniqueSourceIPs uint64  `json:"unique_src_ips"`
	WorstPercent    float64 `json:"worst_pct"`        // % of the closest matching threshold (0..∞)
	WorstRule       string  `json:"worst_rule,omitempty"` // name of the rule the row is closest to
	Status          string  `json:"status"`           // "ok" | "warn" (>50%) | "critical" (>=100%)
}
