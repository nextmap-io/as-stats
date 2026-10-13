package enricher

import (
	"net"
	"testing"

	"github.com/nextmap-io/as-stats/internal/model"
)

func TestEnrichInbound(t *testing.T) {
	e := New()
	e.LoadLinks([]model.Link{
		{Tag: "transit-a", RouterIP: net.ParseIP("10.0.0.1"), SNMPIndex: 5},
	})

	flow := &model.FlowRecord{
		RouterIP:     net.ParseIP("10.0.0.1"),
		InInterface:  5,
		OutInterface: 10,
	}

	e.Enrich(flow)

	if flow.LinkTag != "transit-a" {
		t.Errorf("expected link tag 'transit-a', got '%s'", flow.LinkTag)
	}
	if flow.Direction != model.DirectionInbound {
		t.Errorf("expected direction inbound, got %d", flow.Direction)
	}
}

func TestEnrichOutbound(t *testing.T) {
	e := New()
	e.LoadLinks([]model.Link{
		{Tag: "peering-b", RouterIP: net.ParseIP("10.0.0.1"), SNMPIndex: 5},
	})

	flow := &model.FlowRecord{
		RouterIP:     net.ParseIP("10.0.0.1"),
		InInterface:  10,
		OutInterface: 5,
	}

	e.Enrich(flow)

	if flow.LinkTag != "peering-b" {
		t.Errorf("expected link tag 'peering-b', got '%s'", flow.LinkTag)
	}
	if flow.Direction != model.DirectionOutbound {
		t.Errorf("expected direction outbound, got %d", flow.Direction)
	}
}

func TestEnrichNoMatch(t *testing.T) {
	e := New()
	e.LoadLinks([]model.Link{
		{Tag: "transit-a", RouterIP: net.ParseIP("10.0.0.1"), SNMPIndex: 5},
	})

	flow := &model.FlowRecord{
		RouterIP:     net.ParseIP("10.0.0.2"), // different router
		InInterface:  5,
		OutInterface: 10,
	}

	e.Enrich(flow)

	if flow.LinkTag != "" {
		t.Errorf("expected empty link tag, got '%s'", flow.LinkTag)
	}
	if flow.Direction != model.DirectionUnknown {
		t.Errorf("expected direction unknown, got %d", flow.Direction)
	}
}

// With ingress and egress monitors on both uplinks, a flow A→B is exported
// twice with the same in/out interfaces. Each copy must be credited to the
// link where it was observed — not both to A as inbound.
func TestEnrichFlowDirectionDisambiguatesTransit(t *testing.T) {
	e := New()
	router := net.ParseIP("2001:db8::1")
	e.LoadLinks([]model.Link{
		{Tag: "a", RouterIP: router, SNMPIndex: 1},
		{Tag: "b", RouterIP: router, SNMPIndex: 2},
	})

	cases := []struct {
		name    string
		hint    uint8
		wantTag string
		wantDir uint8
	}{
		{"ingress record", model.DirectionInbound, "a", model.DirectionInbound},
		{"egress record", model.DirectionOutbound, "b", model.DirectionOutbound},
		{"no flowDirection", model.DirectionUnknown, "a", model.DirectionInbound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &model.FlowRecord{RouterIP: router, InInterface: 1, OutInterface: 2, Direction: tc.hint}
			e.Enrich(f)
			if f.LinkTag != tc.wantTag || f.Direction != tc.wantDir {
				t.Errorf("got (%q, %d), want (%q, %d)", f.LinkTag, f.Direction, tc.wantTag, tc.wantDir)
			}
		})
	}
	if got := e.BothInterfacesLinked(); got != uint64(len(cases)) {
		t.Errorf("BothInterfacesLinked = %d, want %d", got, len(cases))
	}
}

// The observed side is preferred, but a record is never dropped when only the
// other side is a link: with ingress-only monitoring everywhere, traffic
// leaving via an uplink only exists as an ingress record on an internal port.
func TestEnrichFlowDirectionFallsBackToOtherSide(t *testing.T) {
	e := New()
	router := net.ParseIP("10.0.0.1")
	e.LoadLinks([]model.Link{{Tag: "uplink", RouterIP: router, SNMPIndex: 7}})

	ingressOut := &model.FlowRecord{RouterIP: router, InInterface: 3, OutInterface: 7, Direction: model.DirectionInbound}
	e.Enrich(ingressOut)
	if ingressOut.LinkTag != "uplink" || ingressOut.Direction != model.DirectionOutbound {
		t.Errorf("ingress record leaving via uplink: got (%q, %d)", ingressOut.LinkTag, ingressOut.Direction)
	}

	egressIn := &model.FlowRecord{RouterIP: router, InInterface: 7, OutInterface: 3, Direction: model.DirectionOutbound}
	e.Enrich(egressIn)
	if egressIn.LinkTag != "uplink" || egressIn.Direction != model.DirectionInbound {
		t.Errorf("egress record entering via uplink: got (%q, %d)", egressIn.LinkTag, egressIn.Direction)
	}
	if got := e.BothInterfacesLinked(); got != 0 {
		t.Errorf("BothInterfacesLinked = %d, want 0", got)
	}
}

func TestASNames(t *testing.T) {
	e := New()
	e.LoadASNames([]model.ASInfo{
		{Number: 13335, Name: "CLOUDFLARENET"},
		{Number: 15169, Name: "GOOGLE"},
	})

	if name := e.GetASName(13335); name != "CLOUDFLARENET" {
		t.Errorf("expected CLOUDFLARENET, got '%s'", name)
	}
	if name := e.GetASName(99999); name != "" {
		t.Errorf("expected empty name for unknown AS, got '%s'", name)
	}
}
