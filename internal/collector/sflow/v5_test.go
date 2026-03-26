package sflow

import (
	"encoding/binary"
	"net"
	"testing"
)

// buildSFlowDatagram creates a synthetic sFlow v5 datagram with one flow sample
// containing one raw packet record (IPv4 TCP).
func buildSFlowDatagram(samplingRate uint32) []byte {
	// Build a raw IPv4 TCP packet header
	ethFrame := make([]byte, 54) // 14 eth + 20 ipv4 + 20 tcp
	// Ethernet: dst(6) + src(6) + type(2)
	binary.BigEndian.PutUint16(ethFrame[12:14], 0x0800) // IPv4

	// IPv4 header at offset 14
	ip := ethFrame[14:]
	ip[0] = 0x45 // version 4, IHL 5
	ip[9] = 6    // TCP
	// total length
	binary.BigEndian.PutUint16(ip[2:4], 40)
	// src IP: 10.1.2.3
	ip[12] = 10
	ip[13] = 1
	ip[14] = 2
	ip[15] = 3
	// dst IP: 172.16.0.1
	ip[16] = 172
	ip[17] = 16
	ip[18] = 0
	ip[19] = 1

	// TCP header at offset 34
	tcp := ethFrame[34:]
	binary.BigEndian.PutUint16(tcp[0:2], 54321)  // src port
	binary.BigEndian.PutUint16(tcp[2:4], 443)    // dst port
	tcp[13] = 0x18                                 // ACK+PSH flags

	// Build raw_packet_record
	rawPacketRecord := make([]byte, 16+len(ethFrame))
	binary.BigEndian.PutUint32(rawPacketRecord[0:4], 1)                  // protocol: ethernet
	binary.BigEndian.PutUint32(rawPacketRecord[4:8], 1500)               // frame_length
	binary.BigEndian.PutUint32(rawPacketRecord[8:12], 0)                 // stripped
	binary.BigEndian.PutUint32(rawPacketRecord[12:16], uint32(len(ethFrame))) // header_length
	copy(rawPacketRecord[16:], ethFrame)

	// Build flow sample (non-expanded)
	flowSample := make([]byte, 32+8+len(rawPacketRecord))
	binary.BigEndian.PutUint32(flowSample[0:4], 1)              // sequence number
	binary.BigEndian.PutUint32(flowSample[4:8], 0)              // source id
	binary.BigEndian.PutUint32(flowSample[8:12], samplingRate)   // sampling rate
	binary.BigEndian.PutUint32(flowSample[12:16], 1000)          // sample pool
	binary.BigEndian.PutUint32(flowSample[16:20], 0)             // drops
	binary.BigEndian.PutUint32(flowSample[20:24], 5)             // input interface
	binary.BigEndian.PutUint32(flowSample[24:28], 6)             // output interface
	binary.BigEndian.PutUint32(flowSample[28:32], 1)             // num records

	// Record header: enterprise(0) + format(1) = raw_packet
	recOffset := 32
	binary.BigEndian.PutUint32(flowSample[recOffset:recOffset+4], 1) // type: raw_packet
	binary.BigEndian.PutUint32(flowSample[recOffset+4:recOffset+8], uint32(len(rawPacketRecord)))
	copy(flowSample[recOffset+8:], rawPacketRecord)

	// Build sFlow datagram
	// Header: version(4) + addr_type(4) + agent_ip(4) + sub_agent(4) + seq(4) + uptime(4) + num_samples(4) = 28
	datagram := make([]byte, 28+8+len(flowSample))
	binary.BigEndian.PutUint32(datagram[0:4], 5)   // version
	binary.BigEndian.PutUint32(datagram[4:8], 1)   // addr type: IPv4
	datagram[8] = 192                                // agent IP: 192.168.1.1
	datagram[9] = 168
	datagram[10] = 1
	datagram[11] = 1
	binary.BigEndian.PutUint32(datagram[12:16], 0)  // sub-agent ID
	binary.BigEndian.PutUint32(datagram[16:20], 1)  // sequence number
	binary.BigEndian.PutUint32(datagram[20:24], 0)  // uptime
	binary.BigEndian.PutUint32(datagram[24:28], 1)  // num samples

	// Sample header: enterprise_format(4) + length(4)
	sampleOffset := 28
	binary.BigEndian.PutUint32(datagram[sampleOffset:sampleOffset+4], 1) // flow_sample
	binary.BigEndian.PutUint32(datagram[sampleOffset+4:sampleOffset+8], uint32(len(flowSample)))
	copy(datagram[sampleOffset+8:], flowSample)

	return datagram
}

func TestDecodeDatagram(t *testing.T) {
	data := buildSFlowDatagram(512)
	routerIP := net.ParseIP("10.0.0.254")

	flows, err := DecodeDatagram(data, routerIP)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(flows))
	}

	f := flows[0]
	if f.SrcIP.String() != "10.1.2.3" {
		t.Errorf("expected src IP 10.1.2.3, got %s", f.SrcIP)
	}
	if f.DstIP.String() != "172.16.0.1" {
		t.Errorf("expected dst IP 172.16.0.1, got %s", f.DstIP)
	}
	if f.SrcPort != 54321 {
		t.Errorf("expected src port 54321, got %d", f.SrcPort)
	}
	if f.DstPort != 443 {
		t.Errorf("expected dst port 443, got %d", f.DstPort)
	}
	if f.Protocol != 6 {
		t.Errorf("expected protocol 6, got %d", f.Protocol)
	}
	if f.SamplingRate != 512 {
		t.Errorf("expected sampling rate 512, got %d", f.SamplingRate)
	}
	if f.InInterface != 5 {
		t.Errorf("expected in_interface 5, got %d", f.InInterface)
	}
	if f.OutInterface != 6 {
		t.Errorf("expected out_interface 6, got %d", f.OutInterface)
	}
	if f.FlowType != "sflow" {
		t.Errorf("expected flow type sflow, got %s", f.FlowType)
	}
	if f.IPVersion != 4 {
		t.Errorf("expected IP version 4, got %d", f.IPVersion)
	}
	if f.Bytes != 1500 {
		t.Errorf("expected 1500 bytes (frame_length), got %d", f.Bytes)
	}
}

func TestDecodeDatagramTooShort(t *testing.T) {
	_, err := DecodeDatagram([]byte{0, 0, 0, 5}, nil)
	if err == nil {
		t.Error("expected error for truncated datagram")
	}
}

func TestDecodeDatagramWrongVersion(t *testing.T) {
	data := make([]byte, 28)
	binary.BigEndian.PutUint32(data[0:4], 4) // version 4

	_, err := DecodeDatagram(data, nil)
	if err == nil {
		t.Error("expected error for wrong version")
	}
}
