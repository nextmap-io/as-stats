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

		// A template whose fields are all zero-length yields a zero-byte record.
		// decodeDataSet advances by TotalLen, so caching this would wedge the
		// decoder goroutine in an infinite allocating loop on the next data set
		// referencing it. Drop it instead of trusting the exporter.
		if totalLen <= 0 {
			log.Printf("v9: ignoring template %d from source %d: zero-length record", templateID, sourceID)
			continue
		}

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
	// Defence in depth: parseV9Templates refuses to cache these, but a zero
	// record length would make the loop below never advance.
	if tmpl.TotalLen <= 0 {
		return nil
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
	return decodeTemplateRecord(data, tmpl, routerIP, ts, "netflow9")
}

// recordState holds the fields of one data record that cannot be applied in
// template order, because their meaning depends on another field that may
// come later in the same template.
type recordState struct {
	inBytes, outBytes uint64
	inPkts, outPkts   uint64
	hasInBytes        bool
	hasInPkts         bool
	srcMask, dstMask  int // -1 when absent
}

// decodeTemplateRecord decodes one v9 / IPFIX data record. v9 and IPFIX share
// the IANA field IDs used here.
func decodeTemplateRecord(data []byte, tmpl *Template, routerIP net.IP, ts time.Time, flowType string) *model.FlowRecord {
	flow := &model.FlowRecord{
		Timestamp:    ts,
		RouterIP:     routerIP,
		IPVersion:    4,
		SamplingRate: 1,
		FlowType:     flowType,
	}
	st := recordState{srcMask: -1, dstMask: -1}

	offset := 0
	for _, field := range tmpl.Fields {
		if offset+int(field.Length) > len(data) {
			return nil
		}

		fieldData := data[offset : offset+int(field.Length)]
		applyField(flow, &st, field.Type, fieldData)
		offset += int(field.Length)
	}

	st.finish(flow)
	return flow
}

// finish applies the order-dependent fields once the whole record is read.
func (st *recordState) finish(flow *model.FlowRecord) {
	// IN_BYTES/IN_PKTS (1/2) are the counters of the record. OUT_BYTES/OUT_PKTS
	// (23/24) are only a fallback: an exporter sending both commonly leaves
	// OUT_* at 0, which used to overwrite the real value depending on field
	// order.
	if st.hasInBytes {
		flow.Bytes = st.inBytes
	} else {
		flow.Bytes = st.outBytes
	}
	if st.hasInPkts {
		flow.Packets = st.inPkts
	} else {
		flow.Packets = st.outPkts
	}

	// Masks are resolved here rather than in field order: a template is free
	// to list the mask before the address it applies to.
	flow.SrcPrefix = prefixString(flow.SrcIP, st.srcMask)
	flow.DstPrefix = prefixString(flow.DstIP, st.dstMask)
}

// prefixString returns "addr/len" for ip masked to maskLen, or "" when there
// is no address, no mask, or a mask longer than the address family allows
// (an exporter-supplied 200 used to produce "<nil>/200").
func prefixString(ip net.IP, maskLen int) string {
	if ip == nil || maskLen <= 0 {
		return ""
	}
	bits := 128
	if ip.To4() != nil {
		bits = 32
	}
	if maskLen > bits {
		return ""
	}
	return fmt.Sprintf("%s/%d", maskIP(ip, uint8(maskLen)), maskLen)
}

func applyField(flow *model.FlowRecord, st *recordState, fieldType uint16, data []byte) {
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
	case FieldInBytes:
		st.inBytes, st.hasInBytes = readUint64(data), true
	case FieldOutBytes:
		st.outBytes = readUint64(data)
	case FieldInPkts:
		st.inPkts, st.hasInPkts = readUint64(data), true
	case FieldOutPkts:
		st.outPkts = readUint64(data)
	case FieldInputSNMP:
		flow.InInterface = readUint32(data)
	case FieldOutputSNMP:
		flow.OutInterface = readUint32(data)
	case FieldSrcMask, FieldIPv6SrcMask:
		if len(data) >= 1 {
			st.srcMask = int(data[0])
		}
	case FieldDstMask, FieldIPv6DstMask:
		if len(data) >= 1 {
			st.dstMask = int(data[0])
		}
	case FieldDirection:
		// flowDirection: 0 = ingress, 1 = egress, relative to the interface
		// where the record was observed. The enricher reads it as a hint and
		// then overwrites Direction with the link-relative direction.
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
