package netflow

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	v9HeaderLen   = 20
	v9Version     = 9
	v9TemplateSet = 0
	v9OptionsSet  = 1
)

var globalTemplateCache = NewTemplateCache(30 * time.Minute)

// V9Header represents a NetFlow v9 packet header.
// DecodeV9 parses a NetFlow v9 packet.
func DecodeV9(data []byte, routerIP net.IP) ([]*model.FlowRecord, error) {
	if len(data) < v9HeaderLen {
		return nil, fmt.Errorf("packet too short for v9 header: %d bytes", len(data))
	}

	version := binary.BigEndian.Uint16(data[0:2])
	if version != v9Version {
		return nil, fmt.Errorf("expected version %d, got %d", v9Version, version)
	}

	unixSecs := binary.BigEndian.Uint32(data[8:12])
	sourceID := binary.BigEndian.Uint32(data[16:20])

	routerKey := ipToKey(routerIP)
	ts := time.Unix(int64(unixSecs), 0).UTC()

	var flows []*model.FlowRecord
	offset := v9HeaderLen

	for offset+4 <= len(data) {
		setID := binary.BigEndian.Uint16(data[offset : offset+2])
		setLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))

		if setLen < 4 || offset+setLen > len(data) {
			break
		}

		setData := data[offset+4 : offset+setLen]

		switch {
		case setID == v9TemplateSet:
			parseV9Templates(setData, routerKey, sourceID)
		case setID == v9OptionsSet:
			// Options templates - skip for now
		case setID >= 256:
			decoded := decodeDataSet(setData, routerKey, sourceID, setID, routerIP, ts)
			flows = append(flows, decoded...)
		}

		offset += setLen
		// Pad to 4-byte boundary
		if offset%4 != 0 {
			offset += 4 - (offset % 4)
		}
	}

	return flows, nil
}

func parseV9Templates(data []byte, routerKey [16]byte, sourceID uint32) {
	offset := 0
	for offset+4 <= len(data) {
		templateID := binary.BigEndian.Uint16(data[offset : offset+2])
		fieldCount := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		offset += 4

		if fieldCount == 0 || fieldCount > 256 || offset+fieldCount*4 > len(data) {
			break
		}

		tmpl := &Template{
			ID:         templateID,
			Fields:     make([]TemplateField, fieldCount),
			ReceivedAt: time.Now(),
		}

		totalLen := 0
		for i := 0; i < fieldCount; i++ {
			fType := binary.BigEndian.Uint16(data[offset : offset+2])
			fLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
			tmpl.Fields[i] = TemplateField{Type: fType, Length: fLen}
			totalLen += int(fLen)
			offset += 4
		}
		tmpl.TotalLen = totalLen

		globalTemplateCache.Set(routerKey, sourceID, tmpl)
		log.Printf("v9: cached template %d from source %d (%d fields, %d bytes/record)",
			templateID, sourceID, fieldCount, totalLen)
	}
}

func decodeDataSet(data []byte, routerKey [16]byte, sourceID uint32, setID uint16, routerIP net.IP, ts time.Time) []*model.FlowRecord {
	tmpl := globalTemplateCache.Get(routerKey, sourceID, setID)
	if tmpl == nil {
		return nil // template not yet received
	}

	var flows []*model.FlowRecord
	offset := 0

	for offset+tmpl.TotalLen <= len(data) {
		flow := decodeRecord(data[offset:offset+tmpl.TotalLen], tmpl, routerIP, ts)
		if flow != nil {
			flows = append(flows, flow)
		}
		offset += tmpl.TotalLen
	}

	return flows
}

func decodeRecord(data []byte, tmpl *Template, routerIP net.IP, ts time.Time) *model.FlowRecord {
	flow := &model.FlowRecord{
		Timestamp:    ts,
		RouterIP:     routerIP,
		IPVersion:    4,
		SamplingRate: 1,
		FlowType:     "netflow9",
	}

	offset := 0
	for _, field := range tmpl.Fields {
		if offset+int(field.Length) > len(data) {
			return nil
		}

		fieldData := data[offset : offset+int(field.Length)]
		applyField(flow, field.Type, fieldData)
		offset += int(field.Length)
	}

	return flow
}

func applyField(flow *model.FlowRecord, fieldType uint16, data []byte) {
	switch fieldType {
	case FieldIPv4SrcAddr:
		if len(data) >= 4 {
			flow.SrcIP = net.IP(make([]byte, 4))
			copy(flow.SrcIP, data[:4])
		}
	case FieldIPv4DstAddr:
		if len(data) >= 4 {
			flow.DstIP = net.IP(make([]byte, 4))
			copy(flow.DstIP, data[:4])
		}
	case FieldIPv6SrcAddr:
		if len(data) >= 16 {
			flow.SrcIP = net.IP(make([]byte, 16))
			copy(flow.SrcIP, data[:16])
			flow.IPVersion = 6
		}
	case FieldIPv6DstAddr:
		if len(data) >= 16 {
			flow.DstIP = net.IP(make([]byte, 16))
			copy(flow.DstIP, data[:16])
			flow.IPVersion = 6
		}
	case FieldSrcAS:
		flow.SrcAS = readUint32(data)
	case FieldDstAS:
		flow.DstAS = readUint32(data)
	case FieldL4SrcPort:
		if len(data) >= 2 {
			flow.SrcPort = binary.BigEndian.Uint16(data[:2])
		}
	case FieldL4DstPort:
		if len(data) >= 2 {
			flow.DstPort = binary.BigEndian.Uint16(data[:2])
		}
	case FieldProtocol:
		if len(data) >= 1 {
			flow.Protocol = data[0]
		}
	case FieldTCPFlags:
		if len(data) >= 1 {
			flow.TCPFlags = data[0]
		}
	case FieldInBytes, FieldOutBytes:
		flow.Bytes = readUint64(data)
	case FieldInPkts, FieldOutPkts:
		flow.Packets = readUint64(data)
	case FieldInputSNMP:
		flow.InInterface = readUint32(data)
	case FieldOutputSNMP:
		flow.OutInterface = readUint32(data)
	case FieldSrcMask:
		if len(data) >= 1 && data[0] > 0 && flow.SrcIP != nil {
			flow.SrcPrefix = fmt.Sprintf("%s/%d", maskIP(flow.SrcIP, data[0]), data[0])
		}
	case FieldDstMask:
		if len(data) >= 1 && data[0] > 0 && flow.DstIP != nil {
			flow.DstPrefix = fmt.Sprintf("%s/%d", maskIP(flow.DstIP, data[0]), data[0])
		}
	case FieldDirection:
		if len(data) >= 1 {
			switch data[0] {
			case 0:
				flow.Direction = model.DirectionInbound
			case 1:
				flow.Direction = model.DirectionOutbound
			}
		}
	case FieldSamplingRate:
		flow.SamplingRate = readUint32(data)
	case FieldIPVersion:
		if len(data) >= 1 {
			flow.IPVersion = data[0]
		}
	}
}

// readUint32 reads a variable-length unsigned integer as uint32.
func readUint32(data []byte) uint32 {
	switch len(data) {
	case 1:
		return uint32(data[0])
	case 2:
		return uint32(binary.BigEndian.Uint16(data))
	case 4:
		return binary.BigEndian.Uint32(data)
	default:
		if len(data) >= 4 {
			return binary.BigEndian.Uint32(data[:4])
		}
		return 0
	}
}

// readUint64 reads a variable-length unsigned integer as uint64.
func readUint64(data []byte) uint64 {
	switch len(data) {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(binary.BigEndian.Uint16(data))
	case 4:
		return uint64(binary.BigEndian.Uint32(data))
	case 8:
		return binary.BigEndian.Uint64(data)
	default:
		if len(data) >= 8 {
			return binary.BigEndian.Uint64(data[:8])
		}
		return 0
	}
}
