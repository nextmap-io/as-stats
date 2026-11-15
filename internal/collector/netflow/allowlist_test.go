package netflow

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/collector/exporters"
	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

// sendV5 starts a listener with the given allow-list, sends one v5 packet of
// perPacket flows from 127.0.0.1 and returns how many flows came out.
func sendV5(t *testing.T, allow *exporters.AllowList, perPacket int) int {
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
	if _, err := sender.Write(buildV5Packet(perPacket, 1)); err != nil {
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
	rejected := metrics.DatagramsRejected.WithLabelValues("netflow")

	deny, err := exporters.Parse("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	before := counterValue(t, rejected)
	if n := sendV5(t, deny, 5); n != 0 {
		t.Errorf("a source outside the allow-list produced %d flows, want 0", n)
	}
	if got := counterValue(t, rejected) - before; got != 1 {
		t.Errorf("rejected counter rose by %v, want 1", got)
	}

	allow, err := exporters.Parse("192.0.2.0/24, 127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	before = counterValue(t, rejected)
	if n := sendV5(t, allow, 5); n != 5 {
		t.Errorf("an allowed source produced %d flows, want 5", n)
	}
	if n := sendV5(t, nil, 5); n != 5 {
		t.Errorf("without an allow-list, got %d flows, want 5", n)
	}
	if got := counterValue(t, rejected) - before; got != 0 {
		t.Errorf("rejected counter rose by %v for allowed sources", got)
	}
}
