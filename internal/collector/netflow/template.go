package netflow

import (
	"container/list"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/collector/ratelog"
	"github.com/nextmap-io/as-stats/internal/metrics"
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
//
// Templates never expire by age (unless a ttl is given): a re-send replaces
// the entry. Two bounds keep the cache from growing without limit or being
// taken over by one exporter:
//
//   - maxTemplatesPerRouter: a router at its cap may still replace its own
//     templates, but a new template ID from it is refused and counted
//     (asstats_templates_refused_total);
//   - maxTemplateCacheSize overall: when full, the least recently set entry
//     is evicted. Entries sit in a list ordered by last Set, so eviction is
//     O(1) — never a scan of the whole cache under the write lock.
type TemplateCache struct {
	mu        sync.RWMutex
	templates map[templateKey]*list.Element // Value: *cacheEntry
	order     *list.List                    // front = least recently set
	perRouter map[[16]byte]int
	ttl       time.Duration
	maxTotal  int
	maxRouter int
}

type cacheEntry struct {
	key  templateKey
	tmpl *Template
}

// NewTemplateCache creates a template cache with the given TTL (0 = never
// expire by age).
func NewTemplateCache(ttl time.Duration) *TemplateCache {
	return newTemplateCache(ttl, maxTemplateCacheSize, maxTemplatesPerRouter)
}

func newTemplateCache(ttl time.Duration, maxTotal, maxRouter int) *TemplateCache {
	return &TemplateCache{
		templates: make(map[templateKey]*list.Element),
		order:     list.New(),
		perRouter: make(map[[16]byte]int),
		ttl:       ttl,
		maxTotal:  maxTotal,
		maxRouter: maxRouter,
	}
}

func makeKey(routerIP [16]byte, sourceID uint32, templateID uint16) templateKey {
	return templateKey{
		RouterIP: routerIP,
		SourceID: sourceID,
		TemplID:  templateID,
	}
}

const (
	maxTemplateCacheSize = 10000
	// maxTemplatesPerRouter bounds one exporter's share of the cache. Real
	// exporters announce tens of templates (per source ID / observation
	// domain), far below it.
	maxTemplatesPerRouter = 1000
)

// templateCapLog has its own budget so a flood of refused templates cannot
// use up the one templateLog keeps for real template announcements.
var templateCapLog = ratelog.New(1, time.Minute)

// Set stores a template in the cache and reports whether it is new or differs
// from the cached one (exporters re-send unchanged templates periodically).
// A new template from a router already at its cap is refused: Set returns
// false and the template is not cached.
func (tc *TemplateCache) Set(routerIP [16]byte, sourceID uint32, tmpl *Template) bool {
	key := makeKey(routerIP, sourceID, tmpl.ID)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if el, exists := tc.templates[key]; exists {
		e := el.Value.(*cacheEntry)
		prev := e.tmpl
		e.tmpl = tmpl
		tc.order.MoveToBack(el)
		return !sameLayout(prev, tmpl)
	}
	if tc.perRouter[routerIP] >= tc.maxRouter {
		metrics.TemplatesRefused.WithLabelValues(metrics.RouterLabel(net.IP(routerIP[:]))).Inc()
		templateCapLog.Printf("netflow: router %s is at its %d-template cap; refusing template %d (source %d)",
			net.IP(routerIP[:]), tc.maxRouter, tmpl.ID, sourceID)
		return false
	}
	if tc.order.Len() >= tc.maxTotal {
		tc.removeLocked(tc.order.Front())
	}
	tc.templates[key] = tc.order.PushBack(&cacheEntry{key: key, tmpl: tmpl})
	tc.perRouter[routerIP]++
	return true
}

// removeLocked drops one entry; tc.mu must be held for writing.
func (tc *TemplateCache) removeLocked(el *list.Element) {
	e := tc.order.Remove(el).(*cacheEntry)
	delete(tc.templates, e.key)
	if n := tc.perRouter[e.key.RouterIP] - 1; n > 0 {
		tc.perRouter[e.key.RouterIP] = n
	} else {
		delete(tc.perRouter, e.key.RouterIP)
	}
}

// Len returns the number of cached templates.
func (tc *TemplateCache) Len() int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.order.Len()
}

func sameLayout(a, b *Template) bool {
	return a.Options == b.Options && a.ScopeFields == b.ScopeFields && slices.Equal(a.Fields, b.Fields)
}

// Get retrieves a template from the cache. Returns nil if not found or expired.
func (tc *TemplateCache) Get(routerIP [16]byte, sourceID uint32, templateID uint16) *Template {
	key := makeKey(routerIP, sourceID, templateID)
	tc.mu.RLock()
	var tmpl *Template
	el, ok := tc.templates[key]
	if ok {
		tmpl = el.Value.(*cacheEntry).tmpl
	}
	tc.mu.RUnlock()

	if !ok {
		return nil
	}
	if tc.ttl > 0 && time.Since(tmpl.ReceivedAt) > tc.ttl {
		tc.mu.Lock()
		// Only drop the entry we judged stale, not one re-sent meanwhile.
		if el, ok := tc.templates[key]; ok && el.Value.(*cacheEntry).tmpl == tmpl {
			tc.removeLocked(el)
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
