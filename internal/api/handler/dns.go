package handler

import (
	"container/list"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// ptrCacheMax bounds the PTR cache. Its key is a client-supplied address, so
	// an unbounded map is an OOM primitive: walking a /64 is enough to insert
	// millions of entries that would then live for the process lifetime. Past
	// the cap the least-recently-used entry is dropped.
	ptrCacheMax = 20_000
	// ptrTTL is how long a resolved (or negative) PTR answer stays valid.
	ptrTTL = 1 * time.Hour
)

type ptrEntry struct {
	key  string
	name string
	at   time.Time
}

var (
	ptrCacheMu sync.Mutex
	ptrCache   = make(map[string]*list.Element, 1024)
	// ptrLRU orders entries most- to least-recently-used (front = newest).
	ptrLRU = list.New()
)

// ptrCacheGet returns a live cached answer, dropping it if the TTL has passed.
func ptrCacheGet(key string) (string, bool) {
	ptrCacheMu.Lock()
	defer ptrCacheMu.Unlock()

	el, ok := ptrCache[key]
	if !ok {
		return "", false
	}
	e := el.Value.(*ptrEntry)
	if time.Since(e.at) >= ptrTTL {
		ptrLRU.Remove(el)
		delete(ptrCache, key)
		return "", false
	}
	ptrLRU.MoveToFront(el)
	return e.name, true
}

// ptrCachePut stores an answer, evicting the oldest entries once at capacity.
func ptrCachePut(key, name string) {
	ptrCacheMu.Lock()
	defer ptrCacheMu.Unlock()

	if el, ok := ptrCache[key]; ok {
		e := el.Value.(*ptrEntry)
		e.name, e.at = name, time.Now()
		ptrLRU.MoveToFront(el)
		return
	}
	for ptrLRU.Len() >= ptrCacheMax {
		oldest := ptrLRU.Back()
		if oldest == nil {
			break
		}
		ptrLRU.Remove(oldest)
		delete(ptrCache, oldest.Value.(*ptrEntry).key)
	}
	ptrCache[key] = ptrLRU.PushFront(&ptrEntry{key: key, name: name, at: time.Now()})
}

// DNSPtr handles GET /api/v1/dns/ptr?ip=x.x.x.x
func (h *Handler) DNSPtr(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ip")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "missing ip parameter")
		return
	}

	// Only address literals are looked up (and cached). Anything else can never
	// resolve to a PTR, and forwarding it verbatim would turn this endpoint into
	// an open resolver for arbitrary attacker-chosen names.
	parsed := net.ParseIP(raw)
	if parsed == nil {
		writeError(w, http.StatusBadRequest, "invalid ip parameter")
		return
	}
	// Canonical form, so equivalent spellings of one address (::ffff:1.2.3.4,
	// 2001:db8::1 vs 2001:0db8::1) share a single cache slot.
	ip := parsed.String()

	if name, ok := ptrCacheGet(ip); ok {
		writeJSON(w, http.StatusOK, Response{Data: map[string]string{"ip": ip, "ptr": name}})
		return
	}

	// Lookup
	names, err := net.LookupAddr(ip)
	ptr := ""
	if err == nil && len(names) > 0 {
		ptr = names[0]
		// Remove trailing dot
		if len(ptr) > 0 && ptr[len(ptr)-1] == '.' {
			ptr = ptr[:len(ptr)-1]
		}
	}

	// Cache result (even empty)
	ptrCachePut(ip, ptr)

	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"ip": ip, "ptr": ptr}})
}
