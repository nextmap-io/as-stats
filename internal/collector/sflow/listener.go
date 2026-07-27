package sflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	maxPacketSize = 65535
	readBufSize   = 8 * 1024 * 1024
)

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

	if err := conn.SetReadBuffer(readBufSize); err != nil {
		log.Printf("warning: failed to set sflow read buffer: %v", err)
	}

	type packet struct {
		data     []byte
		routerIP net.IP
	}

	var bufPool = sync.Pool{
		New: func() any {
			buf := make([]byte, maxPacketSize)
			return &buf
		},
	}

	packets := make(chan packet, l.workers*64)

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
				log.Printf("sflow read error: %v", err)
				continue
			}

			// Copy the data to avoid holding the pooled buffer
			dataCopy := make([]byte, n)
			copy(dataCopy, (*bufPtr)[:n])
			bufPool.Put(bufPtr)

			routerIP := make(net.IP, len(remoteAddr.IP))
			copy(routerIP, remoteAddr.IP)

			select {
			case packets <- packet{data: dataCopy, routerIP: routerIP}:
			case <-ctx.Done():
				return
			}
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
					log.Printf("sflow decode error from %s: %v", pkt.routerIP, err)
					continue
				}
				metrics.FlowsReceived.WithLabelValues("sflow").Add(float64(len(decoded)))

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

	return nil
}

// Wait blocks until every decoder goroutine has stopped sending on the flows
// channel. Call it after Close and before closing that channel, otherwise a
// decoder still draining a decoded packet panics with "send on closed channel".
func (l *Listener) Wait() {
	l.decoders.Wait()
}

// Close stops the listener.
func (l *Listener) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
