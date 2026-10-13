//go:build !linux

package udpstat

import "net"

// Drops is only implemented on Linux, which exposes per-socket drop counts in
// /proc/net/udp.
func Drops(*net.UDPConn) (uint64, error) { return 0, ErrUnsupported }
