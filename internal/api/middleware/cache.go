package middleware

import (
	"bytes"
	"container/list"
	"net/http"
	"sync"
	"time"
)

const (
	// maxCacheEntries / maxCacheBytes bound the response cache. Its key is the
	// full request URI, which the caller controls: without a cap, varying the
	// query string on a cached route pins an unbounded number of response bodies
	// in memory until the TTL sweep catches up. Past either cap the
	// least-recently-used entries are dropped.
	maxCacheEntries = 1024
	maxCacheBytes   = 64 << 20 // 64 MiB of bodies, total
	// maxCacheEntryBytes skips caching individually huge responses — one of them
	// would otherwise evict most of the working set for a single URL.
	maxCacheEntryBytes = 4 << 20
)

type cacheEntry struct {
	key         string
	body        []byte
	contentType string
	status      int
	expiresAt   time.Time
}

// responseCache is a size- and count-bounded LRU of cached GET responses.
//
// The key is deliberately identity-free: it is only mounted on routes whose
// output depends purely on the query string (overview / top-N / links /
// heatmap / changes). None of them consult the authenticated principal, and
// admin and viewer see byte-identical bodies, so one user's response can never
// be served as another's. Adding an identity-sensitive route to the cached
// group means adding the principal to the key.
type responseCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List // front = most recently used
	bytes   int
}

func newResponseCache() *responseCache {
	return &responseCache{
		entries: make(map[string]*list.Element),
		lru:     list.New(),
	}
}

func (c *responseCache) get(key string) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*cacheEntry)
	if time.Now().After(e.expiresAt) {
		c.removeElement(el)
		return nil, false
	}
	c.lru.MoveToFront(el)
	return e, true
}

func (c *responseCache) put(e *cacheEntry) {
	if len(e.body) > maxCacheEntryBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[e.key]; ok {
		c.removeElement(el)
	}
	c.entries[e.key] = c.lru.PushFront(e)
	c.bytes += len(e.body)

	for c.lru.Len() > maxCacheEntries || c.bytes > maxCacheBytes {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.removeElement(oldest)
	}
}

// sweep drops expired entries so memory is released before the LRU fills up.
func (c *responseCache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for el := c.lru.Back(); el != nil; {
		prev := el.Prev()
		if now.After(el.Value.(*cacheEntry).expiresAt) {
			c.removeElement(el)
		}
		el = prev
	}
}

// removeElement drops an element from both the map and the list. Caller holds mu.
func (c *responseCache) removeElement(el *list.Element) {
	e := el.Value.(*cacheEntry)
	c.lru.Remove(el)
	delete(c.entries, e.key)
	c.bytes -= len(e.body)
}

type responseRecorder struct {
	http.ResponseWriter
	body   bytes.Buffer
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Cache returns a middleware that caches successful GET responses in memory.
func Cache(ttl time.Duration) func(http.Handler) http.Handler {
	cache := newResponseCache()

	// Cleanup goroutine
	go func() {
		for {
			time.Sleep(30 * time.Second)
			cache.sweep(time.Now())
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			// Bypass cache
			if r.Header.Get("Cache-Control") == "no-cache" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.URL.RequestURI()

			if entry, ok := cache.get(key); ok {
				w.Header().Set("Content-Type", entry.contentType)
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(entry.status)
				_, _ = w.Write(entry.body)
				return
			}

			// Record response
			rec := &responseRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)

			// Only cache 200 responses
			if rec.status == 200 {
				cache.put(&cacheEntry{
					key:         key,
					body:        rec.body.Bytes(),
					contentType: w.Header().Get("Content-Type"),
					status:      rec.status,
					expiresAt:   time.Now().Add(ttl),
				})
			}
		})
	}
}
