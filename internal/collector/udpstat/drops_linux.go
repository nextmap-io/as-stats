package udpstat

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Drops returns how many datagrams the kernel dropped on conn's socket since
// it was opened — receive-buffer overflows the reader never sees. It reads the
// "drops" column of /proc/net/udp{,6} for the socket's inode.
func Drops(conn *net.UDPConn) (uint64, error) {
	inode, err := socketInode(conn)
	if err != nil {
		return 0, err
	}
	for _, path := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		drops, found, err := scanDrops(path, inode)
		if err != nil {
			return 0, err
		}
		if found {
			return drops, nil
		}
	}
	return 0, fmt.Errorf("udpstat: socket inode %d not found in /proc/net/udp*", inode)
}

func socketInode(conn *net.UDPConn) (uint64, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var st syscall.Stat_t
	var statErr error
	if err := raw.Control(func(fd uintptr) {
		statErr = syscall.Fstat(int(fd), &st)
	}); err != nil {
		return 0, err
	}
	return st.Ino, statErr
}

// scanDrops looks for inode in a /proc/net/udp-format file. Columns:
// sl local rem st tx:rx tr:when retrnsmt uid timeout inode ref pointer drops.
func scanDrops(path string, inode uint64) (uint64, bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = f.Close() }()
	return parseDrops(bufio.NewScanner(f), inode)
}

func parseDrops(sc *bufio.Scanner, inode uint64) (uint64, bool, error) {
	want := strconv.FormatUint(inode, 10)
	sc.Scan() // header
	for sc.Scan() {
		cols := strings.Fields(sc.Text())
		if len(cols) < 13 || cols[9] != want {
			continue
		}
		drops, err := strconv.ParseUint(cols[12], 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("udpstat: bad drops column %q: %w", cols[12], err)
		}
		return drops, true, nil
	}
	return 0, false, sc.Err()
}
