package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCacheIsBounded: the cache key is the caller-controlled request URI, so a
// client walking distinct query strings must not be able to grow the map (or
// the retained bytes) without limit.
func TestCacheIsBounded(t *testing.T) {
	body := make([]byte, 1024)
	h := Cache(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))

	for i := 0; i < maxCacheEntries*3; i++ {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/overview?nonce=%d", i), nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	// The middleware owns its cache, so reach it through a fresh request that
	// must miss: the first URI inserted has long been evicted.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/overview?nonce=0", nil))
	if got := rec.Header().Get("X-Cache"); got == "HIT" {
		t.Fatalf("oldest entry survived %d insertions; LRU eviction is not working", maxCacheEntries*3)
	}
}

// TestResponseCacheEvictsAndAccounts covers the LRU bookkeeping directly:
// byte accounting must not drift and expired entries must be swept.
func TestResponseCacheEvictsAndAccounts(t *testing.T) {
	c := newResponseCache()
	now := time.Now()

	for i := 0; i < maxCacheEntries+10; i++ {
		c.put(&cacheEntry{
			key:       fmt.Sprintf("k%d", i),
			body:      make([]byte, 100),
			status:    200,
			expiresAt: now.Add(time.Minute),
		})
	}
	if c.lru.Len() != maxCacheEntries || len(c.entries) != maxCacheEntries {
		t.Fatalf("entries = (%d list, %d map), want %d", c.lru.Len(), len(c.entries), maxCacheEntries)
	}
	if want := maxCacheEntries * 100; c.bytes != want {
		t.Fatalf("bytes = %d, want %d", c.bytes, want)
	}

	// Re-inserting an existing key must replace, not double-count.
	before := c.bytes
	c.put(&cacheEntry{key: "k20", body: make([]byte, 100), status: 200, expiresAt: now.Add(time.Minute)})
	if c.bytes != before {
		t.Fatalf("bytes = %d after replace, want %d", c.bytes, before)
	}

	c.sweep(now.Add(2 * time.Minute))
	if c.lru.Len() != 0 || len(c.entries) != 0 || c.bytes != 0 {
		t.Fatalf("after sweep: %d list, %d map, %d bytes; want all zero", c.lru.Len(), len(c.entries), c.bytes)
	}
}

// TestCacheSkipsOversizedResponses: a single huge body must not be allowed to
// evict the entire working set.
func TestCacheSkipsOversizedResponses(t *testing.T) {
	c := newResponseCache()
	c.put(&cacheEntry{
		key:       "huge",
		body:      make([]byte, maxCacheEntryBytes+1),
		status:    200,
		expiresAt: time.Now().Add(time.Minute),
	})
	if _, ok := c.get("huge"); ok {
		t.Fatal("oversized response was cached")
	}
}
