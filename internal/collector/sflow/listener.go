package sflow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/collector/ratelog"
	"github.com/nextmap-io/as-stats/internal/collector/udpstat"
	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	maxPacketSize = 65535
	readBufSize   = 8 * 1024 * 1024
)

// Per-packet failures are logged at most once per interval: any UDP source
// can trigger them, one packet at a time.
var (
	decodeErrLog = ratelog.New(1, 10*time.Second)
	readErrLog   = ratelog.New(1, 10*time.Second)
)

type packet struct {
	data     []byte
	routerIP net.IP
}

// Start begins listening for sFlow packets and sending decoded flows to the output channel.
func (l *Listener) Start(ctx context.Context, flows chan<- *model.FlowRecord) error {
	udpAddr, err := net.ResolveUDPAddr("udp", l.addr)
	if err != nil {
		return fmt.Errorf("resolve addr %s: %w", l.addr, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp %s: %w", l.addr, err)
	}
	l.conn = conn

	metrics.UDPReceiveBuffer.WithLabelValues("sflow").Set(
		float64(udpstat.SetReadBuffer(conn, readBufSize, "sflow")))

	var bufPool = sync.Pool{
		New: func() any {
			buf := make([]byte, maxPacketSize)
			return &buf
		},
	}

	packets := make(chan packet, l.workers*64)
	l.packets = packets

	// Reader goroutine
	go func() {
		defer close(packets)
		for {
			bufPtr := bufPool.Get().(*[]byte)
			n, remoteAddr, err := conn.ReadFromUDP(*bufPtr)
			if err != nil {
				bufPool.Put(bufPtr)
				// A closed socket never recovers: returning also releases the
				// decoders, which Wait relies on to know all senders are done.
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				readErrLog.Printf("sflow read error: %v", err)
				continue
			}

			// Copy the data to avoid holding the pooled buffer
			dataCopy := make([]byte, n)
			copy(dataCopy, (*bufPtr)[:n])
			bufPool.Put(bufPtr)

			routerIP := make(net.IP, len(remoteAddr.IP))
			copy(routerIP, remoteAddr.IP)

			// No ctx escape: the decoders drain this channel until the
			// socket is closed, so a packet already read is not lost.
			packets <- packet{data: dataCopy, routerIP: routerIP}
		}
	}()

	// Decoder goroutines
	for i := 0; i < l.workers; i++ {
		l.decoders.Add(1)
		go func() {
			defer l.decoders.Done()
			for pkt := range packets {
				decoded, err := DecodeDatagram(pkt.data, pkt.routerIP)
				if err != nil {
					metrics.DecodeErrors.WithLabelValues("sflow").Inc()
					decodeErrLog.Printf("sflow decode error from %s: %v", pkt.routerIP, err)
					continue
				}
				if len(decoded) == 0 {
					continue // counter samples only
				}
				metrics.FlowsReceived.WithLabelValues("sflow").Add(float64(len(decoded)))
				metrics.SamplingRate.WithLabelValues(metrics.RouterLabel(pkt.routerIP)).
					Set(float64(decoded[len(decoded)-1].SamplingRate))

				// Blocking on purpose (see the netflow listener): the
				// enricher drains until this channel closes after shutdown.
				for _, f := range decoded {
					flows <- f
				}
			}
		}()
	}

	return nil
}

// Wait blocks until every decoder goroutine has stopped sending on the flows
// channel. Call it after Close and before closing that channel, otherwise a
// decoder still draining a decoded packet panics with "send on closed channel".
func (l *Listener) Wait() {
	l.decoders.Wait()
}

// QueueStats reports the length and capacity of the packet queue between the
// socket reader and the decoders (0, 0 before Start).
func (l *Listener) QueueStats() (length, capacity int) {
	if l.packets == nil {
		return 0, 0
	}
	return len(l.packets), cap(l.packets)
}

// SocketDrops returns the kernel's drop count for the listener socket.
func (l *Listener) SocketDrops() (uint64, error) {
	if l.conn == nil {
		return 0, udpstat.ErrUnsupported
	}
	return udpstat.Drops(l.conn)
}

// Close stops the listener.
func (l *Listener) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
