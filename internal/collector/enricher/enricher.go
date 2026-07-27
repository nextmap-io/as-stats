package enricher

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nextmap-io/as-stats/internal/metrics"
	"github.com/nextmap-io/as-stats/internal/model"
)

type linkKey struct {
	RouterIP  [16]byte
	SNMPIndex uint32
}

type linkInfo struct {
	Tag       string
	Direction uint8 // model.DirectionInbound or model.DirectionOutbound
}

// Enricher maps flows to known links and determines traffic direction.
type Enricher struct {
	mu        sync.RWMutex
	links     map[linkKey]linkInfo
	asNames   map[uint32]string
	localAS   uint32
	localNets []net.IPNet

	// clampedTS counts flows whose exporter timestamp was rejected as
	// out-of-range and replaced with receive time. Atomic: Enrich runs without
	// holding the write lock.
	clampedTS atomic.Uint64
}

// New creates a new Enricher.
func New() *Enricher {
	return &Enricher{
		links:   make(map[linkKey]linkInfo),
		asNames: make(map[uint32]string),
	}
}

// LoadLinks replaces the link map with the provided links.
// For each link, traffic arriving on the link's SNMP interface is inbound,
// and traffic leaving on that interface is outbound.
func (e *Enricher) LoadLinks(links []model.Link) {
	newLinks := make(map[linkKey]linkInfo, len(links))
	for _, l := range links {
		ip := normalizeIP(l.RouterIP)
		key := linkKey{RouterIP: ip, SNMPIndex: l.SNMPIndex}
		newLinks[key] = linkInfo{Tag: l.Tag}
	}

	e.mu.Lock()
	e.links = newLinks
	e.mu.Unlock()

	log.Printf("enricher: loaded %d link mappings", len(newLinks))
}

// LoadASNames replaces the AS name map.
func (e *Enricher) LoadASNames(names []model.ASInfo) {
	newNames := make(map[uint32]string, len(names))
	for _, n := range names {
		newNames[n.Number] = n.Name
	}

	e.mu.Lock()
	e.asNames = newNames
	e.mu.Unlock()

	log.Printf("enricher: loaded %d AS names", len(newNames))
}

// SetLocalAS sets the local AS number and its announced prefixes.
// Flows with src/dst IPs in these prefixes get their AS overridden.
func (e *Enricher) SetLocalAS(asn uint32, prefixes []net.IPNet) {
	e.mu.Lock()
	e.localAS = asn
	e.localNets = prefixes
	e.mu.Unlock()
	log.Printf("enricher: local AS%d with %d prefixes", asn, len(prefixes))
}

// timestampSkew bounds how far a router-supplied flow timestamp may sit from
// local time before we distrust it. flows_raw is PARTITION BY
// toYYYYMMDD(timestamp) with a TTL keyed on the same column, so a bogus
// timestamp creates a partition that either never expires (far future) or is
// dropped immediately (far past). One exporter with a broken clock — or a
// spoofed packet, since the UDP listeners accept any source — is enough to
// accumulate permanent partitions. Clamping keeps retention enforceable.
const (
	timestampSkewFuture = 5 * time.Minute
	timestampSkewPast   = 24 * time.Hour
)

// ClampedTimestamps counts flows whose exporter timestamp was out of range and
// rewritten to receive time. Exposed for metrics/diagnostics.
func (e *Enricher) ClampedTimestamps() uint64 { return e.clampedTS.Load() }

// Enrich sets the LinkTag and Direction fields on a flow based on known links.
// If the input interface matches a known link, the flow is inbound on that link.
// If the output interface matches, the flow is outbound on that link. It also
// clamps an out-of-range exporter timestamp to receive time.
func (e *Enricher) Enrich(flow *model.FlowRecord) {
	routerIP := normalizeIP(flow.RouterIP)

	// Clamp before anything downstream keys on it.
	now := time.Now()
	if flow.Timestamp.IsZero() ||
		flow.Timestamp.After(now.Add(timestampSkewFuture)) ||
		flow.Timestamp.Before(now.Add(-timestampSkewPast)) {
		flow.Timestamp = now
		e.clampedTS.Add(1)
		metrics.TimestampsClamped.Inc()
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check input interface -> inbound traffic
	inKey := linkKey{RouterIP: routerIP, SNMPIndex: flow.InInterface}
	if info, ok := e.links[inKey]; ok {
		flow.LinkTag = info.Tag
		flow.Direction = model.DirectionInbound
	}

	// Check output interface -> outbound traffic
	if flow.LinkTag == "" {
		outKey := linkKey{RouterIP: routerIP, SNMPIndex: flow.OutInterface}
		if info, ok := e.links[outKey]; ok {
			flow.LinkTag = info.Tag
			flow.Direction = model.DirectionOutbound
		}
	}

	// Override private/missing AS for IPs in local prefixes
	if e.localAS > 0 && len(e.localNets) > 0 {
		if e.isLocalIP(flow.SrcIP) && (flow.SrcAS == 0 || isPrivateAS(flow.SrcAS)) {
			flow.SrcAS = e.localAS
		}
		if e.isLocalIP(flow.DstIP) && (flow.DstAS == 0 || isPrivateAS(flow.DstAS)) {
			flow.DstAS = e.localAS
		}
	}
}

// GetASName returns the AS name for the given AS number, or empty string.
func (e *Enricher) GetASName(asn uint32) string {
	e.mu.RLock()
	name := e.asNames[asn]
	e.mu.RUnlock()
	return name
}

func (e *Enricher) isLocalIP(ip net.IP) bool {
	for i := range e.localNets {
		if e.localNets[i].Contains(ip) {
			return true
		}
	}
	return false
}

func isPrivateAS(asn uint32) bool {
	return (asn >= 64512 && asn <= 65534) || (asn >= 4200000000 && asn <= 4294967294)
}

func normalizeIP(ip net.IP) [16]byte {
	var key [16]byte
	if v4 := ip.To4(); v4 != nil {
		key[10] = 0xFF
		key[11] = 0xFF
		copy(key[12:], v4)
	} else if len(ip) == net.IPv6len {
		copy(key[:], ip)
	}
	return key
}
