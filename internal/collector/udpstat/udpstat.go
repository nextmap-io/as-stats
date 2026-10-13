// Package udpstat inspects the kernel side of the collector's UDP sockets:
// the receive buffer actually granted, and packets the kernel dropped before
// the reader could get them. Neither is visible from Go's net package.
package udpstat

import (
	"errors"
	"log"
	"net"
	"runtime"
)

// ErrUnsupported is returned where the platform offers no cheap way to read
// the statistic.
var ErrUnsupported = errors.New("udpstat: not supported on this platform")

// SetReadBuffer requests a receive buffer of size bytes and logs what the
// kernel granted. SetReadBuffer does not fail when Linux silently caps the
// request at net.core.rmem_max (often 208 KiB), and a buffer that small
// overflows on any burst of flow packets — so the effective size has to be
// read back to be known. It returns the effective size, or 0 if unknown.
func SetReadBuffer(conn *net.UDPConn, size int, name string) int {
	if err := conn.SetReadBuffer(size); err != nil {
		log.Printf("warning: %s: failed to set UDP read buffer to %d: %v", name, size, err)
	}
	got, err := readBufferSize(conn)
	if err != nil {
		log.Printf("%s: requested UDP read buffer %d bytes (effective size unknown: %v)", name, size, err)
		return 0
	}
	// Linux stores and reports twice the granted size (half is reserved for
	// bookkeeping): a fully granted request reads back as 2×size.
	granted := got
	if runtime.GOOS == "linux" {
		granted = got / 2
	}
	if granted < size {
		log.Printf("warning: %s: UDP read buffer capped at %d bytes, %d requested — raise net.core.rmem_max (sysctl) or expect drops on bursts",
			name, granted, size)
	} else {
		log.Printf("%s: UDP read buffer %d bytes", name, granted)
	}
	return granted
}
