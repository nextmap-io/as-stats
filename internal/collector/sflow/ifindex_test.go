package sflow

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestCompactIfIndex(t *testing.T) {
	cases := map[uint32]uint32{
		5:                   5,
		0x3FFFFFFF:          0, // unknown
		1<<30 | 241:         0, // discarded, reason 241
		2<<30 | 3:           0, // multiple interfaces
		0x80000000 | 0x0007: 0,
	}
	for raw, want := range cases {
		if got := compactIfIndex(raw); got != want {
			t.Errorf("compactIfIndex(%#x) = %d, want %d", raw, got, want)
		}
	}
}

// buildExpandedDatagram reuses the raw-packet record of buildSFlowDatagram in
// an expanded flow sample (sample type 3).
func buildExpandedDatagram(rate, inFormat, inValue, outFormat, outValue uint32) []byte {
	compact := buildSFlowDatagram(rate)
	rec := compact[28+8+32:] // datagram header, sample header, compact sample header

	sample := make([]byte, 44)
	binary.BigEndian.PutUint32(sample[0:4], 1)      // sequence
	binary.BigEndian.PutUint32(sample[4:8], 0)      // source id type
	binary.BigEndian.PutUint32(sample[8:12], 7)     // source id index
	binary.BigEndian.PutUint32(sample[12:16], rate) // sampling rate
	binary.BigEndian.PutUint32(sample[16:20], 1000) // sample pool
	binary.BigEndian.PutUint32(sample[20:24], 0)    // drops
	binary.BigEndian.PutUint32(sample[24:28], inFormat)
	binary.BigEndian.PutUint32(sample[28:32], inValue)
	binary.BigEndian.PutUint32(sample[32:36], outFormat)
	binary.BigEndian.PutUint32(sample[36:40], outValue)
	binary.BigEndian.PutUint32(sample[40:44], 1) // num records
	sample = append(sample, rec...)

	dg := append([]byte{}, compact[:28]...)
	dg = binary.BigEndian.AppendUint32(dg, sampleTypeExpandFlow)
	dg = binary.BigEndian.AppendUint32(dg, uint32(len(sample)))
	return append(dg, sample...)
}

func TestDecodeExpandedFlowSample(t *testing.T) {
	flows, err := DecodeDatagram(buildExpandedDatagram(256, 0, 70000, 0, 70001), net.ParseIP("10.0.0.254"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 {
		t.Fatalf("expanded sample decoded %d flows, want 1", len(flows))
	}
	f := flows[0]
	if f.InInterface != 70000 || f.OutInterface != 70001 || f.SamplingRate != 256 {
		t.Errorf("in=%d out=%d rate=%d, want 70000/70001/256", f.InInterface, f.OutInterface, f.SamplingRate)
	}
	if f.SrcIP.String() != "10.1.2.3" || f.DstPort != 443 {
		t.Errorf("packet header mis-parsed: src=%s dport=%d", f.SrcIP, f.DstPort)
	}

	// Discarded packet: output format 1 is a reason code, not an interface.
	flows, err = DecodeDatagram(buildExpandedDatagram(256, 0, 12, 1, 241), net.ParseIP("10.0.0.254"))
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].InInterface != 12 || flows[0].OutInterface != 0 {
		t.Errorf("discarded packet: in=%d out=%d, want 12/0", flows[0].InInterface, flows[0].OutInterface)
	}
}
