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
}

// ASInfo holds AS number to name mapping.
type ASInfo struct {
	Number  uint32
	Name    string
	Country string
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
	ASNumber uint32         `json:"as_number"`
	ASName   string         `json:"as_name"`
	Bytes    uint64         `json:"bytes"`
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
