package netflow

import (
	"slices"
	"sync"
	"time"
)

// IANA field type IDs for NetFlow v9 / IPFIX.
const (
	FieldInBytes      = 1
	FieldInPkts       = 2
	FieldProtocol     = 4
	FieldTCPFlags     = 6
	FieldL4SrcPort    = 7
	FieldIPv4SrcAddr  = 8
	FieldSrcMask      = 9
	FieldInputSNMP    = 10
	FieldL4DstPort    = 11
	FieldIPv4DstAddr  = 12
	FieldDstMask      = 13
	FieldOutputSNMP   = 14
	FieldSrcAS        = 16
	FieldDstAS        = 17
	FieldOutBytes     = 23
	FieldOutPkts      = 24
	FieldIPv6SrcAddr  = 27
	FieldIPv6DstAddr  = 28
	FieldIPv6SrcMask  = 29
	FieldIPv6DstMask  = 30
	FieldSamplingRate = 34
	FieldIPVersion    = 60
	FieldDirection    = 61
)

// TemplateField represents a single field in a v9/IPFIX template.
type TemplateField struct {
	Type   uint16
	Length uint16
}

// varLenField is the IPFIX field length that announces a variable-length
// field (RFC 7011 §7): the record then carries the actual length inline.
const varLenField = 0xFFFF

// Template represents a decoded template.
type Template struct {
	ID     uint16
	Fields []TemplateField
	// TotalLen is the record length, or its minimum when VarLen is set
	// (each variable-length field counting for its 1-byte length prefix).
	TotalLen int
	// VarLen is set for IPFIX templates with variable-length fields.
	VarLen bool
	// Options marks an options template: its data records describe the
	// exporter (sampling, interface names, …) and are never flows.
	Options bool
	// ScopeFields is the number of leading scope fields of an options template.
	ScopeFields int
	ReceivedAt  time.Time
}

// fieldLen returns the length a field contributes to Template.TotalLen.
func fieldLen(f TemplateField, ipfix bool) int {
	if ipfix && f.Length == varLenField {
		return 1
	}
	return int(f.Length)
}

// walkRecord calls fn for each field of the record at the start of data and
// returns the number of bytes the record occupies, or 0 when it does not fit.
// The result is never 0 for a record that fits a template with TotalLen > 0,
// so callers advancing by it always make progress.
func walkRecord(data []byte, tmpl *Template, fn func(i int, f TemplateField, v []byte)) int {
	off := 0
	for i, f := range tmpl.Fields {
		l := int(f.Length)
		if tmpl.VarLen && f.Length == varLenField {
			if off >= len(data) {
				return 0
			}
			l = int(data[off])
			off++
			if l == 255 {
				if off+2 > len(data) {
					return 0
				}
				l = int(data[off])<<8 | int(data[off+1])
				off += 2
			}
		}
		if off+l > len(data) {
			return 0
		}
		fn(i, f, data[off:off+l])
		off += l
	}
	return off
}

type templateKey struct {
	RouterIP [16]byte
	SourceID uint32
	TemplID  uint16
}

// TemplateCache stores templates keyed by (router IP, source ID, template ID).
type TemplateCache struct {
	mu        sync.RWMutex
	templates map[templateKey]*Template
	ttl       time.Duration
}

// NewTemplateCache creates a template cache with the given TTL.
func NewTemplateCache(ttl time.Duration) *TemplateCache {
	return &TemplateCache{
		templates: make(map[templateKey]*Template),
		ttl:       ttl,
	}
}

func makeKey(routerIP [16]byte, sourceID uint32, templateID uint16) templateKey {
	return templateKey{
		RouterIP: routerIP,
		SourceID: sourceID,
		TemplID:  templateID,
	}
}

const maxTemplateCacheSize = 10000

// Set stores a template in the cache and reports whether it is new or differs
// from the cached one (exporters re-send unchanged templates periodically).
func (tc *TemplateCache) Set(routerIP [16]byte, sourceID uint32, tmpl *Template) bool {
	key := makeKey(routerIP, sourceID, tmpl.ID)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	prev, exists := tc.templates[key]
	if !exists && len(tc.templates) >= maxTemplateCacheSize {
		// Evict oldest entry to prevent unbounded growth
		var oldestKey templateKey
		var oldestTime time.Time
		for k, v := range tc.templates {
			if oldestTime.IsZero() || v.ReceivedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.ReceivedAt
			}
		}
		delete(tc.templates, oldestKey)
	}
	tc.templates[key] = tmpl
	return !exists || !sameLayout(prev, tmpl)
}

func sameLayout(a, b *Template) bool {
	return a.Options == b.Options && a.ScopeFields == b.ScopeFields && slices.Equal(a.Fields, b.Fields)
}

// Get retrieves a template from the cache. Returns nil if not found or expired.
func (tc *TemplateCache) Get(routerIP [16]byte, sourceID uint32, templateID uint16) *Template {
	key := makeKey(routerIP, sourceID, templateID)
	tc.mu.RLock()
	tmpl, ok := tc.templates[key]
	tc.mu.RUnlock()

	if !ok {
		return nil
	}
	if tc.ttl > 0 && time.Since(tmpl.ReceivedAt) > tc.ttl {
		tc.mu.Lock()
		// Only drop the entry we judged stale, not one re-sent meanwhile.
		if tc.templates[key] == tmpl {
			delete(tc.templates, key)
		}
		tc.mu.Unlock()
		return nil
	}
	return tmpl
}

// ipToKey converts a net.IP to a fixed-size array for use as map key.
func ipToKey(ip []byte) [16]byte {
	var key [16]byte
	if len(ip) == 4 {
		// IPv4-mapped IPv6
		key[10] = 0xFF
		key[11] = 0xFF
		copy(key[12:], ip)
	} else {
		copy(key[:], ip)
	}
	return key
}
