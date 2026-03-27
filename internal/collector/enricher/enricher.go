package enricher

import (
	"log"
	"net"
	"sync"

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
	mu           sync.RWMutex
	links        map[linkKey]linkInfo
	asNames      map[uint32]string
	localAS      uint32
	localNets    []net.IPNet
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

// Enrich sets the LinkTag and Direction fields on a flow based on known links.
// If the input interface matches a known link, the flow is inbound on that link.
// If the output interface matches, the flow is outbound on that link.
func (e *Enricher) Enrich(flow *model.FlowRecord) {
	routerIP := normalizeIP(flow.RouterIP)

	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check input interface -> inbound traffic
	inKey := linkKey{RouterIP: routerIP, SNMPIndex: flow.InInterface}
	if info, ok := e.links[inKey]; ok {
		flow.LinkTag = info.Tag
		flow.Direction = model.DirectionInbound
		return
	}

	// Check output interface -> outbound traffic
	outKey := linkKey{RouterIP: routerIP, SNMPIndex: flow.OutInterface}
	if info, ok := e.links[outKey]; ok {
		flow.LinkTag = info.Tag
		flow.Direction = model.DirectionOutbound
		return
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
