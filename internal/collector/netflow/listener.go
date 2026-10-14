package netflow

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
	readBufSize   = 8 * 1024 * 1024 // 8MB socket buffer
)

// Per-packet log lines are rate-limited: any UDP source can trigger them, one
// packet at a time. Template lines get a burst so the normal startup sequence
// (a few templates per exporter) is still logged in full.
var (
	decodeErrLog = ratelog.New(1, 10*time.Second)
	readErrLog   = ratelog.New(1, 10*time.Second)
	templateLog  = ratelog.New(50, time.Minute)
)

type packet struct {
	data     *[]byte
	n        int
	routerIP net.IP
}

// Listener receives NetFlow packets over UDP and decodes them.
type Listener struct {
	addr    string
	conn    *net.UDPConn
	bufPool sync.Pool
	workers int
	packets chan packet
	// decoders tracks the decoder goroutines, which are the only senders on the
	// flows channel — see Wait.
	decoders sync.WaitGroup
}

// NewListener creates a new NetFlow UDP listener.
func NewListener(addr string, workers int) *Listener {
	if workers <= 0 {
		workers = 4
	}
	return &Listener{
		addr:    addr,
		workers: workers,
		bufPool: sync.Pool{
			New: func() any {
				buf := make([]byte, maxPacketSize)
				return &buf
			},
		},
	}
}

// Start begins listening for NetFlow packets and sending decoded flows to the output channel.
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

	metrics.UDPReceiveBuffer.WithLabelValues("netflow").Set(
		float64(udpstat.SetReadBuffer(conn, readBufSize, "netflow")))

	packets := make(chan packet, l.workers*64)
	l.packets = packets

	// Reader goroutine: reads from UDP socket
	go func() {
		defer close(packets)
		for {
			bufPtr := l.bufPool.Get().(*[]byte)
			n, remoteAddr, err := conn.ReadFromUDP(*bufPtr)
			if err != nil {
				l.bufPool.Put(bufPtr)
				// A closed socket never recovers: returning also releases the
				// decoders, which Wait relies on to know all senders are done.
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return
				}
				readErrLog.Printf("netflow read error: %v", err)
				continue
			}

			routerIP := make(net.IP, len(remoteAddr.IP))
			copy(routerIP, remoteAddr.IP)

			// No ctx escape here: a packet already read is handed to the
			// decoders, which drain the channel until the socket is closed.
			packets <- packet{data: bufPtr, n: n, routerIP: routerIP}
		}
	}()

	// Decoder goroutines
	for i := 0; i < l.workers; i++ {
		l.decoders.Add(1)
		go func() {
			defer l.decoders.Done()
			for pkt := range packets {
				data := (*pkt.data)[:pkt.n]
				proto := protocolLabel(data)
				decoded, err := l.decode(data, pkt.routerIP)
				l.bufPool.Put(pkt.data)

				if err != nil {
					metrics.DecodeErrors.WithLabelValues(proto).Inc()
					decodeErrLog.Printf("netflow decode error from %s: %v", pkt.routerIP, err)
					continue
				}
				if len(decoded) == 0 {
					continue // template / options-only packet
				}
				metrics.FlowsReceived.WithLabelValues(proto).Add(float64(len(decoded)))
				metrics.SamplingRate.WithLabelValues(metrics.RouterLabel(pkt.routerIP)).
					Set(float64(decoded[len(decoded)-1].SamplingRate))

				// Blocking on purpose, even after ctx is cancelled: the
				// enricher keeps consuming until this channel is closed, which
				// only happens once every decoder has returned. Bailing out on
				// ctx here would drop flows that were already decoded.
				for _, f := range decoded {
					flows <- f
				}
			}
		}()
	}

	return nil
}

// protocolLabel maps the export version to the "protocol" metric label, so
// v5, v9 and IPFIX exporters can be told apart on the dashboards.
func protocolLabel(data []byte) string {
	if len(data) < 2 {
		return "netflow"
	}
	switch uint16(data[0])<<8 | uint16(data[1]) {
	case 5:
		return "netflow5"
	case 9:
		return "netflow9"
	case 10:
		return "ipfix"
	default:
		return "netflow"
	}
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

// decode detects the NetFlow version and dispatches to the correct parser.
func (l *Listener) decode(data []byte, routerIP net.IP) ([]*model.FlowRecord, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	version := uint16(data[0])<<8 | uint16(data[1])
	switch version {
	case 5:
		return DecodeV5(data, routerIP)
	case 9:
		return DecodeV9(data, routerIP)
	case 10:
		return DecodeIPFIX(data, routerIP)
	default:
		return nil, fmt.Errorf("unsupported NetFlow version: %d", version)
	}
}

// Close stops the listener.
func (l *Listener) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
