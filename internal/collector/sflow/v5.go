package sflow

import (
	"encoding/binary"
	"fmt"
	"net"
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

	// subAgentID := binary.BigEndian.Uint32(data[offset : offset+4])
	// seqNo := binary.BigEndian.Uint32(data[offset+4 : offset+8])
	// sysUptime := binary.BigEndian.Uint32(data[offset+8 : offset+12])
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

func decodeFlowSample(data []byte, routerIP net.IP, ts time.Time, expanded bool) []*model.FlowRecord {
	var samplingRate uint32
	var inputIf, outputIf uint32
	var numRecords uint32
	var offset int

	if expanded {
		// Expanded flow sample has 4-byte interface indexes
		if len(data) < 40 {
			return nil
		}
		// seqNo = binary.BigEndian.Uint32(data[0:4])
		// dsClass = binary.BigEndian.Uint32(data[4:8])
		// dsIndex = binary.BigEndian.Uint32(data[8:12])
		samplingRate = binary.BigEndian.Uint32(data[12:16])
		// samplePool = binary.BigEndian.Uint32(data[16:20])
		// drops = binary.BigEndian.Uint32(data[20:24])
		inputIf = binary.BigEndian.Uint32(data[24:28])
		outputIf = binary.BigEndian.Uint32(data[28:32])
		numRecords = binary.BigEndian.Uint32(data[32:36])
		offset = 36
	} else {
		if len(data) < 32 {
			return nil
		}
		// seqNo = binary.BigEndian.Uint32(data[0:4])
		srcIDType := binary.BigEndian.Uint32(data[4:8])
		_ = srcIDType
		samplingRate = binary.BigEndian.Uint32(data[8:12])
		// samplePool = binary.BigEndian.Uint32(data[12:16])
		// drops = binary.BigEndian.Uint32(data[16:20])
		inputIf = binary.BigEndian.Uint32(data[20:24])
		outputIf = binary.BigEndian.Uint32(data[24:28])
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

	protocol := binary.BigEndian.Uint32(data[0:4])
	frameLen := binary.BigEndian.Uint32(data[4:8])
	// stripped := binary.BigEndian.Uint32(data[8:12])
	headerLen := int(binary.BigEndian.Uint32(data[12:16]))

	if 16+headerLen > len(data) {
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

	_ = protocol

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
