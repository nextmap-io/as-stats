package udpstat

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func TestParseDrops(t *testing.T) {
	const table = `   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  123: 00000000:0807 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 4242 2 0000000000000000 0
  124: 00000000:18CB 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 31337 2 0000000000000000 1729
`
	drops, found, err := parseDrops(bufio.NewScanner(strings.NewReader(table)), 31337)
	if err != nil || !found || drops != 1729 {
		t.Fatalf("parseDrops = %d, %v, %v; want 1729, true, nil", drops, found, err)
	}
	if _, found, _ := parseDrops(bufio.NewScanner(strings.NewReader(table)), 1); found {
		t.Fatal("parseDrops found a socket that is not in the table")
	}
}

func TestDropsOnLiveSocket(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := Drops(conn); err != nil {
		t.Fatalf("Drops: %v", err)
	}
	if got := SetReadBuffer(conn, 64*1024, "test"); got <= 0 {
		t.Fatalf("SetReadBuffer effective size = %d", got)
	}
}
