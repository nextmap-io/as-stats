package reports

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/config"
)

// blackHoleSMTP accepts TCP connections and then says nothing at all — the
// behaviour of a firewalled or wedged relay. The client must never block on it.
func blackHoleSMTP(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold the connection open without ever sending a banner.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

func TestSendTimesOutOnUnresponsiveServer(t *testing.T) {
	host, port := blackHoleSMTP(t)

	s := NewSender(config.SMTPConfig{Host: host, Port: port, From: "as-stats@example.com"})
	s.timeout = 150 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		done <- s.Send(context.Background(), []string{"x@y.com"}, Rendered{Subject: "s"}, "html")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from an unresponsive server")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send blocked past its timeout — the scheduler goroutine would be stuck forever")
	}
}

func TestSendReturnsOnContextCancel(t *testing.T) {
	host, port := blackHoleSMTP(t)

	s := NewSender(config.SMTPConfig{Host: host, Port: port, From: "as-stats@example.com"})
	// Long enough that only cancellation can unblock the call.
	s.timeout = time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Send(ctx, []string{"x@y.com"}, Rendered{Subject: "s"}, "html")
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after cancellation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send ignored context cancellation — shutdown would hang")
	}
}
