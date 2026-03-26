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
	ipfixHeaderLen   = 16
	ipfixVersion     = 10
	ipfixTemplateSet = 2
	ipfixOptionsSet  = 3
)

// DecodeIPFIX parses an IPFIX (NetFlow v10) packet.
func DecodeIPFIX(data []byte, routerIP net.IP) ([]*model.FlowRecord, error) {
	if len(data) < ipfixHeaderLen {
		return nil, fmt.Errorf("packet too short for IPFIX header: %d bytes", len(data))
	}

	version := binary.BigEndian.Uint16(data[0:2])
	if version != ipfixVersion {
		return nil, fmt.Errorf("expected IPFIX version %d, got %d", ipfixVersion, version)
	}

	msgLen := int(binary.BigEndian.Uint16(data[2:4]))
	if msgLen > len(data) {
		msgLen = len(data)
	}

	exportTime := binary.BigEndian.Uint32(data[4:8])
	domainID := binary.BigEndian.Uint32(data[12:16])

	routerKey := ipToKey(routerIP)
	ts := time.Unix(int64(exportTime), 0).UTC()

	var flows []*model.FlowRecord
	offset := ipfixHeaderLen

	for offset+4 <= msgLen {
		setID := binary.BigEndian.Uint16(data[offset : offset+2])
		setLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))

		if setLen < 4 || offset+setLen > msgLen {
			break
		}

		setData := data[offset+4 : offset+setLen]

		switch {
		case setID == ipfixTemplateSet:
			parseIPFIXTemplates(setData, routerKey, domainID)
		case setID == ipfixOptionsSet:
			parseIPFIXOptionsTemplate(setData, routerKey, domainID)
		case setID >= 256:
			decoded := decodeIPFIXDataSet(setData, routerKey, domainID, setID, routerIP, ts)
			flows = append(flows, decoded...)
		}

		offset += setLen
	}

	return flows, nil
}

func parseIPFIXTemplates(data []byte, routerKey [16]byte, domainID uint32) {
	offset := 0
	for offset+4 <= len(data) {
		templateID := binary.BigEndian.Uint16(data[offset : offset+2])
		fieldCount := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		offset += 4

		if fieldCount == 0 || fieldCount > 256 {
			break
		}

		tmpl := &Template{
			ID:         templateID,
			Fields:     make([]TemplateField, 0, fieldCount),
			ReceivedAt: time.Now(),
		}

		totalLen := 0
		valid := true
		for i := 0; i < fieldCount; i++ {
			if offset+4 > len(data) {
				valid = false
				break
			}

			fType := binary.BigEndian.Uint16(data[offset : offset+2])
			fLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
			offset += 4

			// Enterprise bit: if high bit of type is set, skip 4-byte enterprise number
			if fType&0x8000 != 0 {
				fType &= 0x7FFF
				if offset+4 > len(data) {
					valid = false
					break
				}
				offset += 4 // skip enterprise number
			}

			tmpl.Fields = append(tmpl.Fields, TemplateField{Type: fType, Length: fLen})
			totalLen += int(fLen)
		}

		if !valid {
			break
		}

		tmpl.TotalLen = totalLen
		globalTemplateCache.Set(routerKey, domainID, tmpl)
		log.Printf("ipfix: cached template %d from domain %d (%d fields, %d bytes/record)",
			templateID, domainID, fieldCount, totalLen)
	}
}

func parseIPFIXOptionsTemplate(data []byte, routerKey [16]byte, domainID uint32) {
	if len(data) < 6 {
		return
	}

	templateID := binary.BigEndian.Uint16(data[0:2])
	totalFieldCount := int(binary.BigEndian.Uint16(data[2:4]))
	// data[4:6] = scope field count (unused, all fields parsed uniformly)
	offset := 6

	tmpl := &Template{
		ID:         templateID,
		Fields:     make([]TemplateField, 0, totalFieldCount),
		ReceivedAt: time.Now(),
	}

	totalLen := 0
	for i := 0; i < totalFieldCount; i++ {
		if offset+4 > len(data) {
			return
		}

		fType := binary.BigEndian.Uint16(data[offset : offset+2])
		fLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		offset += 4

		if fType&0x8000 != 0 {
			fType &= 0x7FFF
			if offset+4 > len(data) {
				return
			}
			offset += 4
		}

		tmpl.Fields = append(tmpl.Fields, TemplateField{Type: fType, Length: fLen})
		totalLen += int(fLen)
	}

	tmpl.TotalLen = totalLen
	globalTemplateCache.Set(routerKey, domainID, tmpl)
}

func decodeIPFIXDataSet(data []byte, routerKey [16]byte, domainID uint32, setID uint16, routerIP net.IP, ts time.Time) []*model.FlowRecord {
	tmpl := globalTemplateCache.Get(routerKey, domainID, setID)
	if tmpl == nil {
		return nil
	}

	var flows []*model.FlowRecord
	offset := 0

	for offset+tmpl.TotalLen <= len(data) {
		flow := decodeIPFIXRecord(data[offset:offset+tmpl.TotalLen], tmpl, routerIP, ts)
		if flow != nil {
			flows = append(flows, flow)
		}
		offset += tmpl.TotalLen
	}

	return flows
}

func decodeIPFIXRecord(data []byte, tmpl *Template, routerIP net.IP, ts time.Time) *model.FlowRecord {
	flow := &model.FlowRecord{
		Timestamp:    ts,
		RouterIP:     routerIP,
		IPVersion:    4,
		SamplingRate: 1,
		FlowType:     "ipfix",
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
