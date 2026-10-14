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
