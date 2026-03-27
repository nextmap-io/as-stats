package netflow

import (
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

// Template represents a decoded template.
type Template struct {
	ID         uint16
	Fields     []TemplateField
	TotalLen   int
	ReceivedAt time.Time
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

// Set stores a template in the cache.
func (tc *TemplateCache) Set(routerIP [16]byte, sourceID uint32, tmpl *Template) {
	key := makeKey(routerIP, sourceID, tmpl.ID)
	tc.mu.Lock()
	if len(tc.templates) >= maxTemplateCacheSize {
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
	tc.mu.Unlock()
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
		delete(tc.templates, key)
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
