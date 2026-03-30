package handler

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type ptrEntry struct {
	name string
	at   time.Time
}

var (
	ptrCache   = make(map[string]ptrEntry)
	ptrCacheMu sync.RWMutex
	ptrTTL     = 1 * time.Hour
)

// DNSPtr handles GET /api/v1/dns/ptr?ip=x.x.x.x
func (h *Handler) DNSPtr(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		writeError(w, http.StatusBadRequest, "missing ip parameter")
		return
	}

	// Check cache
	ptrCacheMu.RLock()
	entry, ok := ptrCache[ip]
	ptrCacheMu.RUnlock()

	if ok && time.Since(entry.at) < ptrTTL {
		writeJSON(w, http.StatusOK, Response{Data: map[string]string{"ip": ip, "ptr": entry.name}})
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
	ptrCacheMu.Lock()
	ptrCache[ip] = ptrEntry{name: ptr, at: time.Now()}
	ptrCacheMu.Unlock()

	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"ip": ip, "ptr": ptr}})
}
