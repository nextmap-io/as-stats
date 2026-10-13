package netflow

import (
	"encoding/binary"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Sampling-related field IDs. v9 and IPFIX share them; the "v9 name" is the
// RFC 3954 one, the IPFIX information element is given where it differs.
const (
	FieldSamplingAlgorithm      = 35  // SAMPLING_ALGORITHM / samplingAlgorithm
	FieldSamplerID              = 48  // FLOW_SAMPLER_ID / samplerId
	FieldSamplerMode            = 49  // FLOW_SAMPLER_MODE / samplerMode
	FieldSamplerRandomInterval  = 50  // FLOW_SAMPLER_RANDOM_INTERVAL / samplerRandomInterval
	FieldSelectorID             = 302 // selectorId
	FieldSamplingPacketInterval = 305 // samplingPacketInterval
	FieldSamplingPacketSpace    = 306 // samplingPacketSpace
	FieldSamplingSize           = 309 // samplingSize
	FieldSamplingPopulation     = 310 // samplingPopulation
	FieldSamplingProbability    = 311 // samplingProbability
)

// samplingInfo collects the sampling fields of one record, either a data
// record carrying its own rate or an options record describing a sampler.
type samplingInfo struct {
	interval       uint64 // 34
	randomInterval uint64 // 50
	pktInterval    uint64 // 305
	pktSpace       uint64 // 306
	hasPktSpace    bool
	size           uint64 // 309
	population     uint64 // 310
	probability    float64
	samplerID      uint64 // 48 or 302; 0 means "no sampler"
}

// apply records fieldType if it is a sampling field and reports whether it was.
func (s *samplingInfo) apply(fieldType uint16, data []byte) bool {
	switch fieldType {
	case FieldSamplingRate:
		s.interval = readUint64(data)
	case FieldSamplerRandomInterval:
		s.randomInterval = readUint64(data)
	case FieldSamplingPacketInterval:
		s.pktInterval = readUint64(data)
	case FieldSamplingPacketSpace:
		s.pktSpace, s.hasPktSpace = readUint64(data), true
	case FieldSamplingSize:
		s.size = readUint64(data)
	case FieldSamplingPopulation:
		s.population = readUint64(data)
	case FieldSamplingProbability:
		switch len(data) {
		case 4:
			s.probability = float64(math.Float32frombits(binary.BigEndian.Uint32(data)))
		case 8:
			s.probability = math.Float64frombits(binary.BigEndian.Uint64(data))
		}
	case FieldSamplerID, FieldSelectorID:
		s.samplerID = readUint64(data)
	default:
		return false
	}
	return true
}

// rate returns the 1-in-N packet sampling rate the fields describe, or 0 when
// they describe none. A rate of 0 is never returned as "sampled": exporters
// use 0 for "not configured", and storing it would zero the traffic.
func (s *samplingInfo) rate() uint32 {
	var r float64
	switch {
	case s.pktInterval > 0 && s.hasPktSpace:
		// RFC 5476 systematic count-based: pktInterval packets selected, then
		// pktSpace skipped.
		r = float64(s.pktInterval+s.pktSpace) / float64(s.pktInterval)
	case s.pktInterval > 0:
		// Without a space, exporters in the wild send the 1-in-N rate here.
		r = float64(s.pktInterval)
	case s.interval > 0:
		r = float64(s.interval)
	case s.randomInterval > 0:
		r = float64(s.randomInterval)
	case s.size > 0 && s.population >= s.size:
		r = float64(s.population) / float64(s.size)
	case s.probability > 0 && s.probability <= 1:
		r = 1 / s.probability
	default:
		return 0
	}
	r = math.Round(r)
	switch {
	case r < 1:
		return 1
	case r > math.MaxUint32:
		return math.MaxUint32
	}
	return uint32(r)
}

type samplingKey struct {
	router  [16]byte
	source  uint32 // v9 source ID / IPFIX observation domain
	sampler uint64 // 0 = exporter-wide
}

type samplingEntry struct {
	rate    uint32
	updated time.Time
}

// samplingTable holds the sampling rates exporters announce in options data.
// Entries are replaced on re-send and never expire: an exporter that stops
// sampling announces rate 1 (or the collector restarts). It is bounded like
// the template cache because any UDP source can populate it.
type samplingTable struct {
	mu    sync.RWMutex
	rates map[samplingKey]samplingEntry
}

const maxSamplingEntries = 10000

var globalSampling = &samplingTable{rates: make(map[samplingKey]samplingEntry)}

// set stores rate and reports whether it differs from the previous value.
func (t *samplingTable) set(k samplingKey, rate uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	prev, ok := t.rates[k]
	if !ok && len(t.rates) >= maxSamplingEntries {
		var oldest samplingKey
		var oldestAt time.Time
		for key, e := range t.rates {
			if oldestAt.IsZero() || e.updated.Before(oldestAt) {
				oldest, oldestAt = key, e.updated
			}
		}
		delete(t.rates, oldest)
	}
	t.rates[k] = samplingEntry{rate: rate, updated: time.Now()}
	return !ok || prev.rate != rate
}

func (t *samplingTable) get(k samplingKey) (uint32, bool) {
	t.mu.RLock()
	e, ok := t.rates[k]
	t.mu.RUnlock()
	return e.rate, ok
}

// samplingOverride maps router (IPv4-mapped key) → rate, from
// COLLECTOR_SAMPLING_OVERRIDE. Read on every record without a known rate,
// written once at startup.
var samplingOverride atomic.Pointer[map[[16]byte]uint32]

// SetSamplingOverride installs per-router sampling rates used as a last
// resort, for exporters that sample without saying so in their records or
// options data. Keys must be valid IP addresses; rates of 0 are ignored.
func SetSamplingOverride(rates map[string]uint32) {
	m := make(map[[16]byte]uint32, len(rates))
	for ipStr, rate := range rates {
		ip := net.ParseIP(ipStr)
		if ip == nil || rate == 0 {
			continue
		}
		m[ipToKey(ip)] = rate
	}
	samplingOverride.Store(&m)
}

func overrideRate(routerKey [16]byte) (uint32, bool) {
	m := samplingOverride.Load()
	if m == nil {
		return 0, false
	}
	r, ok := (*m)[routerKey]
	return r, ok
}

// resolveSamplingRate picks the rate for a data record, most specific first:
// a rate carried in the record itself, the rate announced in options data for
// the record's sampler, the exporter-wide announced rate, the operator
// override, and finally 1 (unsampled).
//
// A sampler-specific announcement is deliberately never applied to records
// that do not name that sampler: a router may announce a sampler while also
// exporting unsampled monitors, and scaling those by 1000 would be far worse
// than not scaling.
func resolveSamplingRate(info *samplingInfo, routerKey [16]byte, sourceID uint32) uint32 {
	if r := info.rate(); r > 0 {
		return r
	}
	if info.samplerID != 0 {
		if r, ok := globalSampling.get(samplingKey{routerKey, sourceID, info.samplerID}); ok {
			return r
		}
	}
	if r, ok := globalSampling.get(samplingKey{routerKey, sourceID, 0}); ok {
		return r
	}
	if r, ok := overrideRate(routerKey); ok {
		return r
	}
	return 1
}

// learnSampling extracts a sampling rate from one options data record and
// stores it. interpretScope is false for v9, whose scope fields use their own
// type space (1 = System, 2 = Interface, …) that collides with flow field IDs.
func learnSampling(rec []byte, tmpl *Template, routerKey [16]byte, sourceID uint32, interpretScope bool, proto string) {
	var info samplingInfo
	if n := walkRecord(rec, tmpl, func(i int, f TemplateField, v []byte) {
		if i < tmpl.ScopeFields && !interpretScope {
			return
		}
		info.apply(f.Type, v)
	}); n == 0 {
		return
	}
	r := info.rate()
	if r == 0 {
		return // options data about something else (interface names, …)
	}
	k := samplingKey{routerKey, sourceID, info.samplerID}
	if globalSampling.set(k, r) {
		templateLog.Printf("%s: router %s source %d sampler %d announces sampling 1:%d",
			proto, net.IP(routerKey[:]), sourceID, info.samplerID, r)
	}
}
