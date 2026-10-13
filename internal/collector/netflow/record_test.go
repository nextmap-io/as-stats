package netflow

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

func be16(b []byte, v uint16) []byte { return binary.BigEndian.AppendUint16(b, v) }
func be32(b []byte, v uint32) []byte { return binary.BigEndian.AppendUint32(b, v) }

var testTS = time.Unix(1700000000, 0).UTC()

func tmplOf(fields ...TemplateField) *Template {
	t := &Template{Fields: fields}
	for _, f := range fields {
		t.TotalLen += int(f.Length)
	}
	return t
}

func TestCountersPreferInOverOut(t *testing.T) {
	cases := []struct {
		name        string
		tmpl        *Template
		rec         []byte
		bytes, pkts uint64
	}{
		{
			name: "IN then OUT=0",
			tmpl: tmplOf(TemplateField{FieldInBytes, 4}, TemplateField{FieldOutBytes, 4},
				TemplateField{FieldInPkts, 4}, TemplateField{FieldOutPkts, 4}),
			rec:   be32(be32(be32(be32(nil, 5000), 0), 7), 0),
			bytes: 5000, pkts: 7,
		},
		{
			name: "OUT=0 then IN",
			tmpl: tmplOf(TemplateField{FieldOutBytes, 4}, TemplateField{FieldInBytes, 4},
				TemplateField{FieldOutPkts, 4}, TemplateField{FieldInPkts, 4}),
			rec:   be32(be32(be32(be32(nil, 0), 5000), 0), 7),
			bytes: 5000, pkts: 7,
		},
		{
			name:  "OUT only",
			tmpl:  tmplOf(TemplateField{FieldOutBytes, 8}, TemplateField{FieldOutPkts, 4}),
			rec:   be32(binary.BigEndian.AppendUint64(nil, 1234), 3),
			bytes: 1234, pkts: 3,
		},
		{
			// IN present and genuinely 0 is not replaced by OUT.
			name:  "IN=0 with OUT",
			tmpl:  tmplOf(TemplateField{FieldInBytes, 4}, TemplateField{FieldOutBytes, 4}),
			rec:   be32(be32(nil, 0), 900),
			bytes: 0, pkts: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, ft := range []string{"netflow9", "ipfix"} {
				f := decodeTemplateRecord(tc.rec, tc.tmpl, net.IP{192, 0, 2, 1}, testTS, ft)
				if f.Bytes != tc.bytes || f.Packets != tc.pkts {
					t.Errorf("%s: bytes=%d packets=%d, want %d/%d", ft, f.Bytes, f.Packets, tc.bytes, tc.pkts)
				}
			}
		})
	}
}

func TestPrefixIndependentOfFieldOrder(t *testing.T) {
	cases := []struct {
		name string
		tmpl *Template
		rec  []byte
		src  string
		dst  string
	}{
		{
			name: "mask before address",
			tmpl: tmplOf(TemplateField{FieldSrcMask, 1}, TemplateField{FieldIPv4SrcAddr, 4},
				TemplateField{FieldDstMask, 1}, TemplateField{FieldIPv4DstAddr, 4}),
			rec: []byte{24, 10, 1, 2, 3, 16, 172, 16, 5, 6},
			src: "10.1.2.0/24", dst: "172.16.0.0/16",
		},
		{
			name: "address before mask",
			tmpl: tmplOf(TemplateField{FieldIPv4SrcAddr, 4}, TemplateField{FieldSrcMask, 1}),
			rec:  []byte{10, 1, 2, 3, 24},
			src:  "10.1.2.0/24",
		},
		{
			name: "IPv6 mask before address",
			tmpl: tmplOf(TemplateField{FieldIPv6SrcMask, 1}, TemplateField{FieldIPv6SrcAddr, 16}),
			rec:  append([]byte{48}, net.ParseIP("2001:db8:1:2::1")...),
			src:  "2001:db8:1::/48",
		},
		{
			name: "IPv4 mask out of range",
			tmpl: tmplOf(TemplateField{FieldIPv4SrcAddr, 4}, TemplateField{FieldSrcMask, 1}),
			rec:  []byte{10, 1, 2, 3, 200},
		},
		{
			name: "IPv4 mask 33",
			tmpl: tmplOf(TemplateField{FieldIPv4SrcAddr, 4}, TemplateField{FieldSrcMask, 1}),
			rec:  []byte{10, 1, 2, 3, 33},
		},
		{
			name: "IPv6 mask 129",
			tmpl: tmplOf(TemplateField{FieldIPv6SrcAddr, 16}, TemplateField{FieldIPv6SrcMask, 1}),
			rec:  append(append([]byte{}, net.ParseIP("2001:db8::1")...), 129),
		},
		{
			name: "IPv6 mask 128",
			tmpl: tmplOf(TemplateField{FieldIPv6SrcAddr, 16}, TemplateField{FieldIPv6SrcMask, 1}),
			rec:  append(append([]byte{}, net.ParseIP("2001:db8::1")...), 128),
			src:  "2001:db8::1/128",
		},
		{
			name: "mask without address",
			tmpl: tmplOf(TemplateField{FieldSrcMask, 1}),
			rec:  []byte{24},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := decodeTemplateRecord(tc.rec, tc.tmpl, net.IP{192, 0, 2, 1}, testTS, "netflow9")
			if f.SrcPrefix != tc.src || f.DstPrefix != tc.dst {
				t.Errorf("prefixes = (%q, %q), want (%q, %q)", f.SrcPrefix, f.DstPrefix, tc.src, tc.dst)
			}
		})
	}
}

func TestFlowDirectionDecoded(t *testing.T) {
	tmpl := tmplOf(TemplateField{FieldDirection, 1})
	for raw, want := range map[byte]uint8{0: model.DirectionInbound, 1: model.DirectionOutbound, 7: model.DirectionUnknown} {
		f := decodeTemplateRecord([]byte{raw}, tmpl, net.IP{192, 0, 2, 1}, testTS, "ipfix")
		if f.Direction != want {
			t.Errorf("flowDirection %d decoded as %d, want %d", raw, f.Direction, want)
		}
	}
}
