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
			parseIPFIXOptionsTemplates(setData, routerKey, domainID)
		case setID >= 256:
			decoded := decodeIPFIXDataSet(setData, routerKey, domainID, setID, routerIP, ts)
			flows = append(flows, decoded...)
		}

		offset += setLen
	}

	return flows, nil
}

// parseIPFIXFields reads count field specifiers starting at data[offset],
// skipping enterprise numbers. It returns the new offset and false when the
// specifiers run past the set.
func parseIPFIXFields(data []byte, offset, count int, tmpl *Template) (int, bool) {
	for i := 0; i < count; i++ {
		if offset+4 > len(data) {
			return offset, false
		}

		fType := binary.BigEndian.Uint16(data[offset : offset+2])
		fLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		offset += 4

		// Enterprise bit: if high bit of type is set, skip 4-byte enterprise number
		if fType&0x8000 != 0 {
			fType &= 0x7FFF
			if offset+4 > len(data) {
				return offset, false
			}
			offset += 4 // skip enterprise number
		}

		f := TemplateField{Type: fType, Length: fLen}
		if fLen == varLenField {
			tmpl.VarLen = true
		}
		tmpl.Fields = append(tmpl.Fields, f)
		tmpl.TotalLen += fieldLen(f, true)
	}
	return offset, true
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

		var ok bool
		if offset, ok = parseIPFIXFields(data, offset, fieldCount, tmpl); !ok {
			break
		}

		// Zero-byte records would make the data set loop spin forever (it
		// advances by the record length). Refuse the template rather than the
		// packets.
		if tmpl.TotalLen <= 0 {
			log.Printf("ipfix: ignoring template %d from domain %d: zero-length record", templateID, domainID)
			continue
		}
		globalTemplateCache.Set(routerKey, domainID, tmpl)
		log.Printf("ipfix: cached template %d from domain %d (%d fields, %d bytes/record)",
			templateID, domainID, fieldCount, tmpl.TotalLen)
	}
}

// parseIPFIXOptionsTemplates parses an options template set (RFC 7011 §3.4.2.2):
// template ID, field count, scope field count, then the field specifiers.
// A set may carry several templates.
func parseIPFIXOptionsTemplates(data []byte, routerKey [16]byte, domainID uint32) {
	offset := 0
	for offset+4 <= len(data) {
		templateID := binary.BigEndian.Uint16(data[offset : offset+2])
		totalFieldCount := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		if totalFieldCount == 0 {
			// Template withdrawal (4 bytes) — or set padding. Nothing to cache.
			offset += 4
			continue
		}
		if offset+6 > len(data) {
			return
		}
		scopeCount := int(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
		offset += 6

		if totalFieldCount > 256 || scopeCount == 0 || scopeCount > totalFieldCount {
			return
		}

		tmpl := &Template{
			ID:          templateID,
			Fields:      make([]TemplateField, 0, totalFieldCount),
			Options:     true,
			ScopeFields: scopeCount,
			ReceivedAt:  time.Now(),
		}
		var ok bool
		if offset, ok = parseIPFIXFields(data, offset, totalFieldCount, tmpl); !ok {
			return
		}

		// Same infinite-loop guard as the regular template path.
		if tmpl.TotalLen <= 0 {
			log.Printf("ipfix: ignoring options template %d from domain %d: zero-length record", templateID, domainID)
			continue
		}
		globalTemplateCache.Set(routerKey, domainID, tmpl)
	}
}

func decodeIPFIXDataSet(data []byte, routerKey [16]byte, domainID uint32, setID uint16, routerIP net.IP, ts time.Time) []*model.FlowRecord {
	tmpl := globalTemplateCache.Get(routerKey, domainID, setID)
	if tmpl == nil {
		return nil
	}
	return decodeRecords(data, tmpl, routerKey, domainID, routerIP, ts, "ipfix")
}
