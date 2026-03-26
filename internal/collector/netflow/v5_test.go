package netflow

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildV5Packet creates a synthetic NetFlow v5 packet for testing.
func buildV5Packet(count int, samplingRate uint16) []byte {
	data := make([]byte, v5HeaderLen+count*v5RecordLen)

	// Header
	binary.BigEndian.PutUint16(data[0:2], 5)         // version
	binary.BigEndian.PutUint16(data[2:4], uint16(count))
	binary.BigEndian.PutUint32(data[4:8], 123456)     // sysUptime
	binary.BigEndian.PutUint32(data[8:12], 1700000000) // unix_secs
	binary.BigEndian.PutUint32(data[12:16], 0)         // unix_nsecs
	binary.BigEndian.PutUint32(data[16:20], 1)         // flow_sequence
	data[20] = 1                                        // engine_type
	data[21] = 0                                        // engine_id
	binary.BigEndian.PutUint16(data[22:24], samplingRate) // sampling (mode=0)

	for i := 0; i < count; i++ {
		offset := v5HeaderLen + i*v5RecordLen

		// srcaddr: 10.0.0.1
		data[offset+0] = 10
		data[offset+1] = 0
		data[offset+2] = 0
		data[offset+3] = byte(i + 1)

		// dstaddr: 192.168.1.1
		data[offset+4] = 192
		data[offset+5] = 168
		data[offset+6] = 1
		data[offset+7] = byte(i + 1)

		// nexthop: 0.0.0.0 (offset 8-11)

		// input interface
		binary.BigEndian.PutUint16(data[offset+12:offset+14], 1)
		// output interface
		binary.BigEndian.PutUint16(data[offset+14:offset+16], 2)

		// packets
		binary.BigEndian.PutUint32(data[offset+16:offset+20], 100)
		// bytes
		binary.BigEndian.PutUint32(data[offset+20:offset+24], 15000)

		// srcport
		binary.BigEndian.PutUint16(data[offset+32:offset+34], 12345)
		// dstport
		binary.BigEndian.PutUint16(data[offset+34:offset+36], 80)

		// tcp_flags
		data[offset+37] = 0x02 // SYN

		// protocol (TCP)
		data[offset+38] = 6

		// src_as
		binary.BigEndian.PutUint16(data[offset+40:offset+42], 64496)
		// dst_as
		binary.BigEndian.PutUint16(data[offset+42:offset+44], 13335)

		// src_mask
		data[offset+44] = 24
		// dst_mask
		data[offset+45] = 24
	}

	return data
}

func TestDecodeV5Basic(t *testing.T) {
	routerIP := net.ParseIP("10.0.0.254")
	data := buildV5Packet(5, 100)

	flows, err := DecodeV5(data, routerIP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(flows) != 5 {
		t.Fatalf("expected 5 flows, got %d", len(flows))
	}

	f := flows[0]
	if f.SrcIP.String() != "10.0.0.1" {
		t.Errorf("expected src IP 10.0.0.1, got %s", f.SrcIP)
	}
	if f.DstIP.String() != "192.168.1.1" {
		t.Errorf("expected dst IP 192.168.1.1, got %s", f.DstIP)
	}
	if f.SrcAS != 64496 {
		t.Errorf("expected src AS 64496, got %d", f.SrcAS)
	}
	if f.DstAS != 13335 {
		t.Errorf("expected dst AS 13335, got %d", f.DstAS)
	}
	if f.Bytes != 15000 {
		t.Errorf("expected 15000 bytes, got %d", f.Bytes)
	}
	if f.Packets != 100 {
		t.Errorf("expected 100 packets, got %d", f.Packets)
	}
	if f.SamplingRate != 100 {
		t.Errorf("expected sampling rate 100, got %d", f.SamplingRate)
	}
	if f.Protocol != 6 {
		t.Errorf("expected protocol 6 (TCP), got %d", f.Protocol)
	}
	if f.SrcPort != 12345 {
		t.Errorf("expected src port 12345, got %d", f.SrcPort)
	}
	if f.DstPort != 80 {
		t.Errorf("expected dst port 80, got %d", f.DstPort)
	}
	if f.InInterface != 1 {
		t.Errorf("expected in_interface 1, got %d", f.InInterface)
	}
	if f.OutInterface != 2 {
		t.Errorf("expected out_interface 2, got %d", f.OutInterface)
	}
	if f.FlowType != "netflow5" {
		t.Errorf("expected flow type netflow5, got %s", f.FlowType)
	}
	if f.SrcPrefix != "10.0.0.0/24" {
		t.Errorf("expected src prefix 10.0.0.0/24, got %s", f.SrcPrefix)
	}
	if f.DstPrefix != "192.168.1.0/24" {
		t.Errorf("expected dst prefix 192.168.1.0/24, got %s", f.DstPrefix)
	}
}

func TestDecodeV5NoSampling(t *testing.T) {
	routerIP := net.ParseIP("10.0.0.254")
	data := buildV5Packet(1, 0)

	flows, err := DecodeV5(data, routerIP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if flows[0].SamplingRate != 1 {
		t.Errorf("expected sampling rate 1 when 0, got %d", flows[0].SamplingRate)
	}
}

func TestDecodeV5TooShort(t *testing.T) {
	_, err := DecodeV5([]byte{0, 5}, net.ParseIP("10.0.0.1"))
	if err == nil {
		t.Error("expected error for short packet")
	}
}

func TestDecodeV5WrongVersion(t *testing.T) {
	data := make([]byte, v5HeaderLen)
	binary.BigEndian.PutUint16(data[0:2], 9) // version 9
	binary.BigEndian.PutUint16(data[2:4], 1)

	_, err := DecodeV5(data, net.ParseIP("10.0.0.1"))
	if err == nil {
		t.Error("expected error for wrong version")
	}
}

func TestDecodeV5TruncatedRecords(t *testing.T) {
	data := buildV5Packet(3, 1)
	// Truncate to only include 2 records worth
	truncated := data[:v5HeaderLen+2*v5RecordLen]

	_, err := DecodeV5(truncated, net.ParseIP("10.0.0.1"))
	if err == nil {
		t.Error("expected error for truncated records")
	}
}

func BenchmarkDecodeV5(b *testing.B) {
	routerIP := net.ParseIP("10.0.0.254")
	data := buildV5Packet(30, 100) // max records per packet

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DecodeV5(data, routerIP)
	}
}
