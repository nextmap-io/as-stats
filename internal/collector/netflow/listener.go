package netflow

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	maxPacketSize = 65535
	readBufSize   = 8 * 1024 * 1024 // 8MB socket buffer
)

// Listener receives NetFlow packets over UDP and decodes them.
type Listener struct {
	addr    string
	conn    *net.UDPConn
	bufPool sync.Pool
	workers int
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

	if err := conn.SetReadBuffer(readBufSize); err != nil {
		log.Printf("warning: failed to set read buffer to %d: %v", readBufSize, err)
	}

	type packet struct {
		data     *[]byte
		n        int
		routerIP net.IP
	}

	packets := make(chan packet, l.workers*64)

	// Reader goroutine: reads from UDP socket
	go func() {
		defer close(packets)
		for {
			bufPtr := l.bufPool.Get().(*[]byte)
			n, remoteAddr, err := conn.ReadFromUDP(*bufPtr)
			if err != nil {
				l.bufPool.Put(bufPtr)
				if ctx.Err() != nil {
					return
				}
				log.Printf("netflow read error: %v", err)
				continue
			}

			routerIP := make(net.IP, len(remoteAddr.IP))
			copy(routerIP, remoteAddr.IP)

			select {
			case packets <- packet{data: bufPtr, n: n, routerIP: routerIP}:
			case <-ctx.Done():
				l.bufPool.Put(bufPtr)
				return
			}
		}
	}()

	// Decoder goroutines
	var wg sync.WaitGroup
	for i := 0; i < l.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pkt := range packets {
				data := (*pkt.data)[:pkt.n]
				decoded, err := l.decode(data, pkt.routerIP)
				l.bufPool.Put(pkt.data)

				if err != nil {
					log.Printf("netflow decode error from %s: %v", pkt.routerIP, err)
					continue
				}

				for _, f := range decoded {
					select {
					case flows <- f:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	go func() {
		wg.Wait()
	}()

	return nil
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
