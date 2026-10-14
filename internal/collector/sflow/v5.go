package sflow

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	sflowVersion  = 5
	headerLen     = 28
	minSampleLen  = 12
	ethHeaderLen  = 14
	ipv4HeaderLen = 20

	// Sample types
	sampleTypeFlow        = 1
	sampleTypeCounter     = 2
	sampleTypeExpandFlow  = 3

	// Record types within a flow sample
	recordRawPacket       = 1
	recordExtendedRouter  = 1002
	recordExtendedGateway = 1003
)

// Listener receives sFlow packets and decodes them.
type Listener struct {
	addr    string
	conn    *net.UDPConn
	workers int
	packets chan packet
	// decoders tracks the decoder goroutines, which are the only senders on the
	// flows channel — see Wait.
	decoders sync.WaitGroup
}

// NewListener creates a new sFlow UDP listener.
func NewListener(addr string, workers int) *Listener {
	if workers <= 0 {
		workers = 4
	}
	return &Listener{
		addr:    addr,
		workers: workers,
	}
}

// DecodeDatagram parses an sFlow v5 datagram.
func DecodeDatagram(data []byte, routerIP net.IP) ([]*model.FlowRecord, error) {
	if len(data) < headerLen {
		return nil, fmt.Errorf("sflow datagram too short: %d bytes", len(data))
	}

	version := binary.BigEndian.Uint32(data[0:4])
	if version != sflowVersion {
		return nil, fmt.Errorf("unsupported sflow version: %d", version)
	}

	addrType := binary.BigEndian.Uint32(data[4:8])
	var agentIP net.IP
	offset := 8

	switch addrType {
	case 1: // IPv4
		if offset+4 > len(data) {
			return nil, fmt.Errorf("truncated agent address")
		}
		agentIP = net.IP(make([]byte, 4))
		copy(agentIP, data[offset:offset+4])
		offset += 4
	case 2: // IPv6
		if offset+16 > len(data) {
			return nil, fmt.Errorf("truncated agent address")
		}
		agentIP = net.IP(make([]byte, 16))
		copy(agentIP, data[offset:offset+16])
		offset += 16
	default:
		return nil, fmt.Errorf("unknown agent address type: %d", addrType)
	}

	if routerIP == nil {
		routerIP = agentIP
	}

	if offset+16 > len(data) {
		return nil, fmt.Errorf("truncated sflow header")
	}

	// Skip sub-agent ID (4), sequence number (4), uptime (4)
	numSamples := binary.BigEndian.Uint32(data[offset+12 : offset+16])
	offset += 16
	if numSamples > 1000 {
		numSamples = 1000
	}

	ts := time.Now().UTC()
	var flows []*model.FlowRecord

	for i := uint32(0); i < numSamples && offset+8 <= len(data); i++ {
		// Enterprise + format packed in 4 bytes
		sampleTypeRaw := binary.BigEndian.Uint32(data[offset : offset+4])
		sampleLen := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8

		if sampleLen < 0 || offset+sampleLen > len(data) {
			break
		}

		sampleData := data[offset : offset+sampleLen]
		enterprise := sampleTypeRaw >> 12
		format := sampleTypeRaw & 0xFFF

		if enterprise == 0 {
			switch format {
			case sampleTypeFlow:
				decoded := decodeFlowSample(sampleData, routerIP, ts, false)
				flows = append(flows, decoded...)
			case sampleTypeExpandFlow:
				decoded := decodeFlowSample(sampleData, routerIP, ts, true)
				flows = append(flows, decoded...)
			}
		}

		offset += sampleLen
	}

	return flows, nil
}

// sFlow interface encodings (sFlow v5 §5.1 "interface"): the top two bits of
// the compact form select the meaning of the low 30. Only format 0 is an
// ifIndex; 1 means the packet was discarded (value = reason) and 2 that it
// left on several interfaces (value = count). The raw words used to be
// matched against link ifIndexes as-is.
const (
	ifFormatIndex = 0
	ifUnknown     = 0x3FFFFFFF
)

func compactIfIndex(v uint32) uint32 {
	return expandedIfIndex(v>>30, v&0x3FFFFFFF)
}

// expandedIfIndex returns the ifIndex for a (format, value) pair, or 0 when
// the value is not a single known interface.
func expandedIfIndex(format, value uint32) uint32 {
	if format != ifFormatIndex || value == ifUnknown {
		return 0
	}
	return value
}

func decodeFlowSample(data []byte, routerIP net.IP, ts time.Time, expanded bool) []*model.FlowRecord {
	var samplingRate uint32
	var inputIf, outputIf uint32
	var numRecords uint32
	var offset int

	if expanded {
		// Expanded flow sample: interfaces are (format, value) pairs of two
		// 4-byte words each. Reading them as single words used to take the
		// input format as the input ifIndex and the output format (usually
		// 0) as the record count, so expanded samples decoded no flows.
		if len(data) < 44 {
			return nil
		}
		// seqNo = binary.BigEndian.Uint32(data[0:4])
		// dsClass = binary.BigEndian.Uint32(data[4:8])
		// dsIndex = binary.BigEndian.Uint32(data[8:12])
		samplingRate = binary.BigEndian.Uint32(data[12:16])
		// samplePool = binary.BigEndian.Uint32(data[16:20])
		// drops = binary.BigEndian.Uint32(data[20:24])
		inputIf = expandedIfIndex(binary.BigEndian.Uint32(data[24:28]), binary.BigEndian.Uint32(data[28:32]))
		outputIf = expandedIfIndex(binary.BigEndian.Uint32(data[32:36]), binary.BigEndian.Uint32(data[36:40]))
		numRecords = binary.BigEndian.Uint32(data[40:44])
		offset = 44
	} else {
		if len(data) < 32 {
			return nil
		}
		// seqNo = binary.BigEndian.Uint32(data[0:4])
		// data[4:8] = source ID type+index (unused)
		samplingRate = binary.BigEndian.Uint32(data[8:12])
		// samplePool = binary.BigEndian.Uint32(data[12:16])
		// drops = binary.BigEndian.Uint32(data[16:20])
		inputIf = compactIfIndex(binary.BigEndian.Uint32(data[20:24]))
		outputIf = compactIfIndex(binary.BigEndian.Uint32(data[24:28]))
		numRecords = binary.BigEndian.Uint32(data[28:32])
		offset = 32
	}

	if samplingRate == 0 {
		samplingRate = 1
	}
	if numRecords > 1000 {
		numRecords = 1000
	}

	var flows []*model.FlowRecord

	for i := uint32(0); i < numRecords && offset+8 <= len(data); i++ {
		recTypeRaw := binary.BigEndian.Uint32(data[offset : offset+4])
		recLen := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8

		if recLen < 0 || offset+recLen > len(data) {
			break
		}

		recData := data[offset : offset+recLen]
		enterprise := recTypeRaw >> 12
		format := recTypeRaw & 0xFFF

		if enterprise == 0 && format == recordRawPacket {
			flow := decodeRawPacketRecord(recData, routerIP, ts, samplingRate, inputIf, outputIf)
			if flow != nil {
				flows = append(flows, flow)
			}
		}

		offset += recLen
		// Pad to 4-byte boundary
		if offset%4 != 0 {
			offset += 4 - (offset % 4)
		}
	}

	return flows
}

func decodeRawPacketRecord(data []byte, routerIP net.IP, ts time.Time, samplingRate, inputIf, outputIf uint32) *model.FlowRecord {
	if len(data) < 16 {
		return nil
	}

	_ = binary.BigEndian.Uint32(data[0:4]) // protocol
	// frame_length is the original length on the wire, L2 header (and
	// usually FCS) included — roughly 18 bytes per packet more than the L3
	// byte counts NetFlow/IPFIX exporters report, so the two are not exactly
	// comparable.
	frameLen := binary.BigEndian.Uint32(data[4:8])
	// data[8:12] = stripped bytes (unused)
	headerLen := int(binary.BigEndian.Uint32(data[12:16]))

	if headerLen < 0 || headerLen > 65535 || 16+headerLen > len(data) {
		return nil
	}

	packetHeader := data[16 : 16+headerLen]

	flow := &model.FlowRecord{
		Timestamp:    ts,
		RouterIP:     routerIP,
		SamplingRate: samplingRate,
		InInterface:  inputIf,
		OutInterface: outputIf,
		Bytes:        uint64(frameLen),
		Packets:      1,
		FlowType:     "sflow",
	}

	// Parse Ethernet header
	if len(packetHeader) < ethHeaderLen {
		return flow
	}

	etherType := binary.BigEndian.Uint16(packetHeader[12:14])
	ipOffset := ethHeaderLen

	// Handle VLAN tags (802.1Q)
	for etherType == 0x8100 || etherType == 0x88A8 {
		if ipOffset+4 > len(packetHeader) {
			return flow
		}
		etherType = binary.BigEndian.Uint16(packetHeader[ipOffset+2 : ipOffset+4])
		ipOffset += 4
	}

	switch etherType {
	case 0x0800: // IPv4
		parseIPv4Header(packetHeader[ipOffset:], flow)
	case 0x86DD: // IPv6
		parseIPv6Header(packetHeader[ipOffset:], flow)
	}

	return flow
}

func parseIPv4Header(data []byte, flow *model.FlowRecord) {
	if len(data) < ipv4HeaderLen {
		return
	}

	flow.IPVersion = 4
	ihl := int(data[0]&0x0F) * 4
	flow.Protocol = data[9]

	flow.SrcIP = net.IP(make([]byte, 4))
	copy(flow.SrcIP, data[12:16])
	flow.DstIP = net.IP(make([]byte, 4))
	copy(flow.DstIP, data[16:20])

	// Parse transport layer
	if ihl > 0 && len(data) >= ihl+4 {
		parseTransport(data[ihl:], flow)
	}
}

func parseIPv6Header(data []byte, flow *model.FlowRecord) {
	if len(data) < 40 {
		return
	}

	flow.IPVersion = 6
	flow.Protocol = data[6] // Next Header

	flow.SrcIP = net.IP(make([]byte, 16))
	copy(flow.SrcIP, data[8:24])
	flow.DstIP = net.IP(make([]byte, 16))
	copy(flow.DstIP, data[24:40])

	// Parse transport layer (simplified: assumes no extension headers)
	if len(data) >= 44 {
		parseTransport(data[40:], flow)
	}
}

func parseTransport(data []byte, flow *model.FlowRecord) {
	if len(data) < 4 {
		return
	}

	switch flow.Protocol {
	case 6, 17: // TCP, UDP
		flow.SrcPort = binary.BigEndian.Uint16(data[0:2])
		flow.DstPort = binary.BigEndian.Uint16(data[2:4])
		if flow.Protocol == 6 && len(data) >= 14 {
			flow.TCPFlags = data[13]
		}
	}
}
