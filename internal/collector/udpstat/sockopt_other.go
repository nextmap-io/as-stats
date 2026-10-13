//go:build !unix

package udpstat

import "net"

func readBufferSize(*net.UDPConn) (int, error) { return 0, ErrUnsupported }
