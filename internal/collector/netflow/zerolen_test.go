package netflow

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// TestZeroLengthTemplateDoesNotWedgeDecoder feeds a v9 template whose fields are
// all zero-length — a malformed/hostile exporter can send this. Before the guard
// the template was cached with TotalLen==0 and decodeDataSet looped forever
// appending records, hanging a decoder goroutine and exhausting memory.
func TestZeroLengthTemplateDoesNotWedgeDecoder(t *testing.T) {
	var routerKey [16]byte
	const sourceID = 42
	const templateID = 999

	// template: id, fieldCount=2, then 2 fields each (type, length=0)
	tpl := make([]byte, 4+2*4)
	binary.BigEndian.PutUint16(tpl[0:2], templateID)
	binary.BigEndian.PutUint16(tpl[2:4], 2)
	binary.BigEndian.PutUint16(tpl[4:6], 8)  // IPV4_SRC_ADDR
	binary.BigEndian.PutUint16(tpl[6:8], 0)  // length 0
	binary.BigEndian.PutUint16(tpl[8:10], 12) // IPV4_DST_ADDR
	binary.BigEndian.PutUint16(tpl[10:12], 0) // length 0

	parseV9Templates(tpl, routerKey, sourceID)

	if tmpl := globalTemplateCache.Get(routerKey, sourceID, templateID); tmpl != nil {
		t.Fatalf("zero-length template must not be cached, got TotalLen=%d", tmpl.TotalLen)
	}

	done := make(chan int, 1)
	go func() {
		flows := decodeDataSet(make([]byte, 64), routerKey, sourceID, templateID, net.ParseIP("192.0.2.1"), time.Now())
		done <- len(flows)
	}()
	select {
	case n := <-done:
		if n != 0 {
			t.Fatalf("expected no flows from an uncached zero-length template, got %d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("decodeDataSet did not terminate: infinite loop regression")
	}
}
