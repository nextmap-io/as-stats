package netflow

import (
	"encoding/binary"
	"math"
	"net"
	"testing"

	"github.com/nextmap-io/as-stats/internal/model"
)

// ── packet builders ────────────────────────────────────────────────────────

type fieldSpec struct{ typ, length uint16 }

// set wraps body in a set/FlowSet header, padding v9 sets to 4 bytes.
func set(id uint16, body []byte, pad bool) []byte {
	if pad {
		for (len(body)+4)%4 != 0 {
			body = append(body, 0)
		}
	}
	b := be16(be16(nil, id), uint16(len(body)+4))
	return append(b, body...)
}

func v9Packet(sourceID uint32, sets ...[]byte) []byte {
	b := be16(nil, 9)
	b = be16(b, uint16(len(sets)))
	b = be32(b, 0)          // sysUptime
	b = be32(b, 1700000000) // unix secs
	b = be32(b, 0)          // sequence
	b = be32(b, sourceID)
	for _, s := range sets {
		b = append(b, s...)
	}
	return b
}

func ipfixPacket(domainID uint32, sets ...[]byte) []byte {
	b := be16(nil, 10)
	b = be16(b, 0) // length, patched below
	b = be32(b, 1700000000)
	b = be32(b, 0) // sequence
	b = be32(b, domainID)
	for _, s := range sets {
		b = append(b, s...)
	}
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	return b
}

func v9Template(id uint16, fields ...fieldSpec) []byte {
	b := be16(be16(nil, id), uint16(len(fields)))
	for _, f := range fields {
		b = be16(be16(b, f.typ), f.length)
	}
	return set(v9TemplateSet, b, true)
}

func v9OptionsTemplate(id uint16, scope, options []fieldSpec) []byte {
	b := be16(nil, id)
	b = be16(b, uint16(len(scope)*4))
	b = be16(b, uint16(len(options)*4))
	for _, f := range append(append([]fieldSpec{}, scope...), options...) {
		b = be16(be16(b, f.typ), f.length)
	}
	return set(v9OptionsSet, b, true)
}

func ipfixTemplate(id uint16, fields ...fieldSpec) []byte {
	b := be16(be16(nil, id), uint16(len(fields)))
	for _, f := range fields {
		b = be16(be16(b, f.typ), f.length)
	}
	return set(ipfixTemplateSet, b, false)
}

func ipfixOptionsTemplate(id uint16, scopeCount int, fields ...fieldSpec) []byte {
	b := be16(nil, id)
	b = be16(b, uint16(len(fields)))
	b = be16(b, uint16(scopeCount))
	for _, f := range fields {
		b = be16(be16(b, f.typ), f.length)
	}
	return set(ipfixOptionsSet, b, false)
}

// simpleFlowFields: src, dst, bytes, packets — no sampling information.
var simpleFlowFields = []fieldSpec{{FieldIPv4SrcAddr, 4}, {FieldIPv4DstAddr, 4}, {FieldInBytes, 4}, {FieldInPkts, 4}}

func simpleFlowRecord() []byte {
	b := []byte{10, 0, 0, 1, 10, 0, 0, 2}
	return be32(be32(b, 1500), 3)
}

// ── tests ──────────────────────────────────────────────────────────────────

func TestSamplingInfoRate(t *testing.T) {
	cases := []struct {
		name string
		s    samplingInfo
		want uint32
	}{
		{"none", samplingInfo{}, 0},
		{"samplingInterval", samplingInfo{interval: 512}, 512},
		{"random interval", samplingInfo{randomInterval: 100}, 100},
		{"packet interval+space", samplingInfo{pktInterval: 1, pktSpace: 999, hasPktSpace: true}, 1000},
		{"packet interval, space 0", samplingInfo{pktInterval: 1000, hasPktSpace: true}, 1},
		{"packet interval alone", samplingInfo{pktInterval: 2048}, 2048},
		{"size/population", samplingInfo{size: 1, population: 4000}, 4000},
		{"probability", samplingInfo{probability: 0.001}, 1000},
		{"probability out of range", samplingInfo{probability: 2}, 0},
		{"probability NaN", samplingInfo{probability: math.NaN()}, 0},
	}
	for _, tc := range cases {
		if got := tc.s.rate(); got != tc.want {
			t.Errorf("%s: rate() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The review reproducer: an IPFIX sampling options record used to come out as
// a flow with :: addresses and 0 bytes.
func TestIPFIXOptionsDataIsNotAFlowAndSetsSampling(t *testing.T) {
	ip := net.IP{203, 0, 113, 9}
	const domain = 7

	opts := ipfixOptionsTemplate(300, 1, fieldSpec{149, 4}, fieldSpec{FieldSamplingPacketInterval, 4})
	optData := set(300, be32(be32(nil, domain), 1000), false)
	tmpl := ipfixTemplate(256, simpleFlowFields...)
	data := set(256, simpleFlowRecord(), false)

	flows, err := DecodeIPFIX(ipfixPacket(domain, opts, optData, tmpl, data), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 {
		t.Fatalf("got %d flows, want 1 (options data must not be emitted)", len(flows))
	}
	if flows[0].SamplingRate != 1000 || flows[0].Bytes != 1500 {
		t.Errorf("flow sampling=%d bytes=%d, want 1000/1500", flows[0].SamplingRate, flows[0].Bytes)
	}
}

func TestV9ExporterWideSampling(t *testing.T) {
	ip := net.ParseIP("2001:db8::9")
	const source = 3

	// Juniper-style: scope System, option SAMPLING_INTERVAL + ALGORITHM.
	opts := v9OptionsTemplate(257,
		[]fieldSpec{{1, 4}},
		[]fieldSpec{{FieldSamplingRate, 4}, {FieldSamplingAlgorithm, 1}})
	optData := set(257, append(be32(be32(nil, 0), 512), 1), true)
	tmpl := v9Template(258, simpleFlowFields...)
	data := set(258, simpleFlowRecord(), true)

	flows, err := DecodeV9(v9Packet(source, opts, optData, tmpl, data), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 512 {
		t.Fatalf("flows=%d rate=%v, want 1 flow at 512", len(flows), rates(flows))
	}

	// A different source ID on the same router is a different exporter.
	flows, err = DecodeV9(v9Packet(source+1, tmpl, data), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 1 {
		t.Errorf("other source id: rate=%v, want 1", rates(flows))
	}
}

func TestV9SamplerSpecificSampling(t *testing.T) {
	ip := net.IP{198, 51, 100, 7}
	const source = 1

	// IOS-XR style: scope System, FLOW_SAMPLER_ID + MODE + RANDOM_INTERVAL.
	opts := v9OptionsTemplate(260,
		[]fieldSpec{{1, 4}},
		[]fieldSpec{{FieldSamplerID, 1}, {FieldSamplerMode, 1}, {FieldSamplerRandomInterval, 4}})
	optData := set(260, be32([]byte{0, 0, 0, 0, 5, 2}, 2000), true)

	withSampler := v9Template(261, append(append([]fieldSpec{}, simpleFlowFields...), fieldSpec{FieldSamplerID, 1})...)
	without := v9Template(262, simpleFlowFields...)
	pkt := v9Packet(source, opts, optData, withSampler, without,
		set(261, append(simpleFlowRecord(), 5), true),
		set(261, append(simpleFlowRecord(), 9), true), // unknown sampler
		set(262, simpleFlowRecord(), true))

	flows, err := DecodeV9(pkt, ip)
	if err != nil {
		t.Fatal(err)
	}
	got := rates(flows)
	want := []uint32{2000, 1, 1}
	if len(got) != len(want) {
		t.Fatalf("rates=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rates=%v, want %v (a sampler's rate must not leak to records that do not name it)", got, want)
			break
		}
	}
}

func TestPerRecordSamplingWins(t *testing.T) {
	ip := net.IP{198, 51, 100, 8}
	opts := v9OptionsTemplate(270, []fieldSpec{{1, 4}}, []fieldSpec{{FieldSamplingRate, 4}})
	optData := set(270, be32(be32(nil, 0), 100), true)
	tmpl := v9Template(271, append(append([]fieldSpec{}, simpleFlowFields...), fieldSpec{FieldSamplingRate, 4})...)
	data := set(271, be32(simpleFlowRecord(), 64), true)

	flows, err := DecodeV9(v9Packet(1, opts, optData, tmpl, data), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 64 {
		t.Errorf("rate=%v, want 64 from the record", rates(flows))
	}
}

func TestUnsampledExporterUnchanged(t *testing.T) {
	ip := net.IP{198, 51, 100, 9}
	tmpl := v9Template(280, simpleFlowFields...)
	flows, err := DecodeV9(v9Packet(1, tmpl, set(280, simpleFlowRecord(), true)), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 1 {
		t.Errorf("rate=%v, want 1", rates(flows))
	}

	// A per-record rate of 0 means "not configured", never "multiply by 0".
	tmpl0 := v9Template(281, append(append([]fieldSpec{}, simpleFlowFields...), fieldSpec{FieldSamplingRate, 4})...)
	flows, err = DecodeV9(v9Packet(1, tmpl0, set(281, be32(simpleFlowRecord(), 0), true)), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 1 {
		t.Errorf("rate field 0: rate=%v, want 1", rates(flows))
	}
}

func TestSamplingOverrideIsLastResort(t *testing.T) {
	t.Cleanup(func() { SetSamplingOverride(nil) })
	overridden := net.ParseIP("2001:db8::77")
	announced := net.IP{192, 0, 2, 77}
	SetSamplingOverride(map[string]uint32{"2001:db8::77": 1000, "192.0.2.77": 1000})

	tmpl := v9Template(290, simpleFlowFields...)
	flows, err := DecodeV9(v9Packet(1, tmpl, set(290, simpleFlowRecord(), true)), overridden)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 1000 {
		t.Errorf("override: rate=%v, want 1000", rates(flows))
	}

	opts := v9OptionsTemplate(291, []fieldSpec{{1, 4}}, []fieldSpec{{FieldSamplingRate, 4}})
	flows, err = DecodeV9(v9Packet(1, opts, set(291, be32(be32(nil, 0), 256), true), tmpl, set(290, simpleFlowRecord(), true)), announced)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 256 {
		t.Errorf("announced rate must beat the override: rate=%v, want 256", rates(flows))
	}

	// NetFlow v5 with a zero sampling header falls back to the override too.
	v5, err := DecodeV5(buildV5Packet(1, 0), overridden)
	if err != nil {
		t.Fatal(err)
	}
	if v5[0].SamplingRate != 1000 {
		t.Errorf("v5 override: rate=%d, want 1000", v5[0].SamplingRate)
	}
}

func TestIPFIXVariableLengthOptions(t *testing.T) {
	ip := net.IP{203, 0, 113, 10}
	const domain = 9
	// scope selectorId, then samplerName (variable length), samplingInterval.
	opts := ipfixOptionsTemplate(310, 1,
		fieldSpec{FieldSelectorID, 8}, fieldSpec{84, varLenField}, fieldSpec{FieldSamplingRate, 4})
	name := []byte("sampler-1")
	rec := binary.BigEndian.AppendUint64(nil, 42)
	rec = append(rec, byte(len(name)))
	rec = append(rec, name...)
	rec = be32(rec, 4096)
	optData := set(310, rec, false)

	tmpl := ipfixTemplate(311, append(append([]fieldSpec{}, simpleFlowFields...), fieldSpec{FieldSelectorID, 8})...)
	data := set(311, binary.BigEndian.AppendUint64(simpleFlowRecord(), 42), false)

	flows, err := DecodeIPFIX(ipfixPacket(domain, opts, optData, tmpl, data), ip)
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].SamplingRate != 4096 {
		t.Errorf("rate=%v, want 4096 via selectorId 42", rates(flows))
	}
}

func TestV9ZeroLengthOptionsTemplateRejected(t *testing.T) {
	ip := net.IP{203, 0, 113, 11}
	opts := v9OptionsTemplate(320, []fieldSpec{{1, 0}}, []fieldSpec{{FieldSamplingRate, 0}})
	if _, err := DecodeV9(v9Packet(1, opts), ip); err != nil {
		t.Fatal(err)
	}
	if tmpl := globalTemplateCache.Get(ipToKey(ip), 1, 320); tmpl != nil {
		t.Fatal("zero-length options template must not be cached")
	}
}

func rates(flows []*model.FlowRecord) []uint32 {
	out := make([]uint32, len(flows))
	for i, f := range flows {
		out[i] = f.SamplingRate
	}
	return out
}
