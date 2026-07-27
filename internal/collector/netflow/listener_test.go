package netflow

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// TestListenerWaitReleasesSenders pins the shutdown contract the collector
// relies on: once Close has been called, Wait must return, and only then is it
// safe to close the flows channel. Closing it while a decoder is still draining
// a packet panics with "send on closed channel".
func TestListenerWaitReleasesSenders(t *testing.T) {
	l := NewListener("127.0.0.1:0", 4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Unbuffered: decoders park on the send, which is exactly the window where
	// closing the channel used to be fatal.
	flows := make(chan *model.FlowRecord)
	if err := l.Start(ctx, flows); err != nil {
		t.Fatalf("Start: %v", err)
	}

	target := l.conn.LocalAddr().(*net.UDPAddr)
	sender, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = sender.Close() }()

	pkt := buildV5Packet(10, 1000)
	for i := 0; i < 50; i++ {
		if _, err := sender.Write(pkt); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	time.Sleep(50 * time.Millisecond)

	cancel()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	go func() {
		l.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after Close — the collector would block or close the channel under live senders")
	}

	close(flows)
}
