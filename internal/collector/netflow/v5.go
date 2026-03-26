package netflow

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

const (
	v5HeaderLen = 24
	v5RecordLen = 48
	v5Version   = 5
	v5MaxCount  = 30
)

// V5Header represents a NetFlow v5 packet header.
type V5Header struct {
	Version      uint16
	Count        uint16
	SysUptime    uint32
	UnixSecs     uint32
	UnixNsecs    uint32
	FlowSequence uint32
	EngineType   uint8
	EngineID     uint8
	SamplingMode uint8
	SamplingRate uint16
}

// DecodeV5 parses a NetFlow v5 packet and returns flow records.
func DecodeV5(data []byte, routerIP net.IP) ([]*model.FlowRecord, error) {
	if len(data) < v5HeaderLen {
		return nil, fmt.Errorf("packet too short for v5 header: %d bytes", len(data))
	}

	header := parseV5Header(data)
	if header.Version != v5Version {
		return nil, fmt.Errorf("expected version %d, got %d", v5Version, header.Version)
	}
	if header.Count == 0 || header.Count > v5MaxCount {
		return nil, fmt.Errorf("invalid flow count: %d", header.Count)
	}

	expectedLen := v5HeaderLen + int(header.Count)*v5RecordLen
	if len(data) < expectedLen {
		return nil, fmt.Errorf("packet too short: expected %d bytes, got %d", expectedLen, len(data))
	}

	// Extract sampling rate from header (bottom 14 bits)
	samplingRate := uint32(header.SamplingRate)
	if samplingRate == 0 {
		samplingRate = 1
	}

	ts := time.Unix(int64(header.UnixSecs), int64(header.UnixNsecs)).UTC()

	flows := make([]*model.FlowRecord, 0, header.Count)
	for i := 0; i < int(header.Count); i++ {
		offset := v5HeaderLen + i*v5RecordLen
		rec := data[offset : offset+v5RecordLen]

		flow := &model.FlowRecord{
			Timestamp:    ts,
			RouterIP:     routerIP,
			IPVersion:    4,
			SrcIP:        net.IP(make([]byte, 4)),
			DstIP:        net.IP(make([]byte, 4)),
			Protocol:     rec[38],
			TCPFlags:     rec[37],
			SrcPort:      binary.BigEndian.Uint16(rec[32:34]),
			DstPort:      binary.BigEndian.Uint16(rec[34:36]),
			Packets:      uint64(binary.BigEndian.Uint32(rec[16:20])),
			Bytes:        uint64(binary.BigEndian.Uint32(rec[20:24])),
			InInterface:  uint32(binary.BigEndian.Uint16(rec[12:14])),
			OutInterface: uint32(binary.BigEndian.Uint16(rec[14:16])),
			SrcAS:        uint32(binary.BigEndian.Uint16(rec[40:42])),
			DstAS:        uint32(binary.BigEndian.Uint16(rec[42:44])),
			SamplingRate: samplingRate,
			FlowType:     "netflow5",
		}

		copy(flow.SrcIP, rec[0:4])
		copy(flow.DstIP, rec[4:8])

		// Build prefix from IP + mask
		srcMask := rec[44]
		dstMask := rec[45]
		if srcMask > 0 {
			flow.SrcPrefix = fmt.Sprintf("%s/%d", maskIP(flow.SrcIP, srcMask), srcMask)
		}
		if dstMask > 0 {
			flow.DstPrefix = fmt.Sprintf("%s/%d", maskIP(flow.DstIP, dstMask), dstMask)
		}

		flows = append(flows, flow)
	}

	return flows, nil
}

func parseV5Header(data []byte) V5Header {
	samplingField := binary.BigEndian.Uint16(data[22:24])
	return V5Header{
		Version:      binary.BigEndian.Uint16(data[0:2]),
		Count:        binary.BigEndian.Uint16(data[2:4]),
		SysUptime:    binary.BigEndian.Uint32(data[4:8]),
		UnixSecs:     binary.BigEndian.Uint32(data[8:12]),
		UnixNsecs:    binary.BigEndian.Uint32(data[12:16]),
		FlowSequence: binary.BigEndian.Uint32(data[16:20]),
		EngineType:   data[20],
		EngineID:     data[21],
		SamplingMode: uint8(samplingField >> 14),
		SamplingRate: samplingField & 0x3FFF,
	}
}

// maskIP applies a prefix mask to an IPv4 address.
func maskIP(ip net.IP, maskLen uint8) net.IP {
	mask := net.CIDRMask(int(maskLen), 32)
	masked := ip.Mask(mask)
	return masked
}
