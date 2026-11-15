package sflow

import (
	"context"
	"net"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/nextmap-io/as-stats/internal/collector/exporters"
	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

func rejectedCount(t *testing.T) float64 {
	t.Helper()
	var m dto.Metric
	if err := metrics.DatagramsRejected.WithLabelValues("sflow").Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetCounter().GetValue()
}

func sendSFlow(t *testing.T, allow *exporters.AllowList) int {
	t.Helper()
	l := NewListener("127.0.0.1:0", 1)
	l.SetAllowedExporters(allow)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flows := make(chan *model.FlowRecord, 64)
	if err := l.Start(ctx, flows); err != nil {
		t.Fatalf("Start: %v", err)
	}
	sender, err := net.DialUDP("udp", nil, l.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = sender.Close() }()
	if _, err := sender.Write(buildSFlowDatagram(1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	cancel()
	_ = l.Close()
	l.Wait()
	close(flows)
	n := 0
	for range flows {
		n++
	}
	return n
}

func TestListenerDropsUnlistedExporters(t *testing.T) {
	deny, err := exporters.Parse("2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	before := rejectedCount(t)
	if n := sendSFlow(t, deny); n != 0 {
		t.Errorf("a source outside the allow-list produced %d flows, want 0", n)
	}
	if got := rejectedCount(t) - before; got != 1 {
		t.Errorf("rejected counter rose by %v, want 1", got)
	}

	allow, err := exporters.Parse("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	if n := sendSFlow(t, allow); n != 1 {
		t.Errorf("an allowed source produced %d flows, want 1", n)
	}
}
