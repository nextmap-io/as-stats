package sflow

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
				if ctx.Err() != nil {
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
	var wg sync.WaitGroup
	for i := 0; i < l.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pkt := range packets {
				decoded, err := DecodeDatagram(pkt.data, pkt.routerIP)
				if err != nil {
					log.Printf("sflow decode error from %s: %v", pkt.routerIP, err)
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

	go func() { wg.Wait() }()

	return nil
}

// Close stops the listener.
func (l *Listener) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
