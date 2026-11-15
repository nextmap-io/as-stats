package netflow

import (
	"net"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/nextmap-io/as-stats/internal/metrics"
)

// A template must stay usable however long ago it was received: exporters
// re-send on a fixed period, and expiring at about that period dropped data
// that arrived just before the refresh.
func TestTemplatesDoNotExpireByAge(t *testing.T) {
	var router [16]byte
	router[15] = 1
	old := &Template{ID: 400, Fields: []TemplateField{{FieldInBytes, 4}}, TotalLen: 4, ReceivedAt: time.Now().Add(-24 * time.Hour)}
	globalTemplateCache.Set(router, 1, old)
	if globalTemplateCache.Get(router, 1, 400) == nil {
		t.Fatal("template expired by age")
	}
}

func TestTemplateSetReportsChanges(t *testing.T) {
	tc := NewTemplateCache(0)
	var router [16]byte
	mk := func(l uint16) *Template {
		return &Template{ID: 1, Fields: []TemplateField{{FieldInBytes, l}}, TotalLen: int(l), ReceivedAt: time.Now()}
	}
	if !tc.Set(router, 1, mk(4)) {
		t.Error("first Set must report a new template")
	}
	if tc.Set(router, 1, mk(4)) {
		t.Error("re-sending an identical template must not report a change")
	}
	if !tc.Set(router, 1, mk(8)) {
		t.Error("a different layout under the same ID must report a change")
	}
	if got := tc.Get(router, 1, 1); got == nil || got.TotalLen != 8 {
		t.Error("re-sent template must replace the cached one")
	}
}

func tmplID(id uint16) *Template {
	return &Template{ID: id, Fields: []TemplateField{{FieldInBytes, 4}}, TotalLen: 4, ReceivedAt: time.Now()}
}

// One exporter sending new template IDs without end must not push the other
// exporters' templates out: it is capped at maxTemplatesPerRouter, may keep
// replacing its own, and every refusal is counted.
func TestTemplateFloodFromOneRouterIsCapped(t *testing.T) {
	tc := NewTemplateCache(0)
	flooder := ipToKey(net.IP{192, 0, 2, 66})
	victim := ipToKey(net.IP{192, 0, 2, 10})
	refused := metrics.TemplatesRefused.WithLabelValues(metrics.RouterLabel(net.IP(flooder[:])))
	before := counterValue(t, refused)

	for id := uint16(256); id < 266; id++ {
		tc.Set(victim, 1, tmplID(id))
	}
	const sent = 3 * maxTemplatesPerRouter
	for i := 0; i < sent; i++ {
		// Spread over source IDs too: the cap is per router, not per domain.
		tc.Set(flooder, uint32(i%7), tmplID(uint16(256+i)))
	}

	for id := uint16(256); id < 266; id++ {
		if tc.Get(victim, 1, id) == nil {
			t.Fatalf("victim template %d evicted by another router's flood", id)
		}
	}
	if got := tc.Len(); got != 10+maxTemplatesPerRouter {
		t.Errorf("cache holds %d templates, want %d", got, 10+maxTemplatesPerRouter)
	}
	if got := counterValue(t, refused) - before; got != sent-maxTemplatesPerRouter {
		t.Errorf("refused counter rose by %v, want %d", got, sent-maxTemplatesPerRouter)
	}

	// At its cap the router can still re-send (replace) a template it has.
	first := &Template{ID: 256, Fields: []TemplateField{{FieldInBytes, 8}}, TotalLen: 8, ReceivedAt: time.Now()}
	if !tc.Set(flooder, 0, first) {
		t.Error("a changed re-send from a router at its cap must be accepted")
	}
	if got := tc.Get(flooder, 0, 256); got == nil || got.TotalLen != 8 {
		t.Error("re-send from a router at its cap did not replace its template")
	}
	// A new one is still refused.
	if tc.Set(flooder, 0, tmplID(60000)) || tc.Get(flooder, 0, 60000) != nil {
		t.Error("a new template from a router at its cap must be refused")
	}
}

// When the whole cache is full, the least recently set entry goes — a
// template that is being refreshed survives one that is not.
func TestTemplateCacheEvictsLeastRecentlySet(t *testing.T) {
	tc := newTemplateCache(0, 4, 100)
	r := ipToKey(net.IP{192, 0, 2, 1})
	for id := uint16(1); id <= 4; id++ {
		tc.Set(r, 1, tmplID(id))
	}
	tc.Set(r, 1, tmplID(1)) // refresh: 2 is now the oldest
	tc.Set(r, 1, tmplID(5))
	if tc.Get(r, 1, 2) != nil {
		t.Error("least recently set template 2 should have been evicted")
	}
	for _, id := range []uint16{1, 3, 4, 5} {
		if tc.Get(r, 1, id) == nil {
			t.Errorf("template %d evicted, want kept", id)
		}
	}
	if tc.Len() != 4 {
		t.Errorf("Len = %d, want 4", tc.Len())
	}
	// Eviction freed a slot of the router's share too.
	if got := tc.perRouter[r]; got != 4 {
		t.Errorf("per-router count = %d, want 4", got)
	}
}

// The TTL path (unused in production, ttl 0) keeps the bookkeeping right.
func TestTemplateTTLExpiryReleasesRouterShare(t *testing.T) {
	tc := newTemplateCache(time.Minute, 10, 1)
	r := ipToKey(net.IP{192, 0, 2, 1})
	old := tmplID(1)
	old.ReceivedAt = time.Now().Add(-time.Hour)
	tc.Set(r, 1, old)
	if tc.Get(r, 1, 1) != nil {
		t.Fatal("expired template returned")
	}
	if !tc.Set(r, 1, tmplID(2)) {
		t.Error("expiry must release the router's share")
	}
}

func BenchmarkTemplateSetFullCache(b *testing.B) {
	tc := NewTemplateCache(0)
	for i := 0; i < maxTemplateCacheSize; i++ {
		r := ipToKey(net.IP{10, 0, byte(i / 1000), 1})
		tc.Set(r, 1, tmplID(uint16(i)))
	}
	tmpl := tmplID(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// New keys from many routers on a full cache: each insert evicts one
		// entry (a full scan per insert before).
		r := ipToKey(net.IP{10, 2, byte(i >> 8), byte(i)})
		tc.Set(r, uint32(i), tmpl)
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

func TestMissingTemplateIsCounted(t *testing.T) {
	ip := net.IP{192, 0, 2, 201}
	c := metrics.FlowSetsNoTemplate.WithLabelValues(metrics.RouterLabel(ip))
	before := counterValue(t, c)

	if _, err := DecodeV9(v9Packet(1, set(999, simpleFlowRecord(), true)), ip); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIPFIX(ipfixPacket(1, set(999, simpleFlowRecord(), false)), ip); err != nil {
		t.Fatal(err)
	}
	if got := counterValue(t, c) - before; got != 2 {
		t.Errorf("no-template counter rose by %v, want 2", got)
	}
}
