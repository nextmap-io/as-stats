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
