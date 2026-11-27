// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"container/list"
	"encoding/hex"
	"slices"
	"sync"
)

const (
	preflightCacheMaxEntries = 8
	preflightCacheMaxBytes   = 64 << 20
)

type preflightCacheKey struct {
	packageID  string
	buildID    string
	coreSHA256 string
	coreSize   int
	chunkBytes int
}

type preflightCacheEntry struct {
	key         preflightCacheKey
	payload     []byte
	coreSHA384  string
	chunkSHA384 []string
	sizeBytes   int64
}

type preflightCacheStats struct {
	hits      uint64
	misses    uint64
	evictions uint64
	extracts  uint64
	entries   int
	usedBytes int64
}

// preflightCache is a mutex-protected bounded LRU cache of immutable preflighted
// payloads and their derived SHA-384 / chunk SHA-384 digests. Callers must never
// mutate returned payload slices.
type preflightCache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int64
	usedBytes  int64
	order      *list.List
	entries    map[preflightCacheKey]*list.Element
	hits       uint64
	misses     uint64
	evictions  uint64
	extracts   uint64
}

func newPreflightCache(maxEntries int, maxBytes int64) *preflightCache {
	return &preflightCache{
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		order:      list.New(),
		entries:    make(map[preflightCacheKey]*list.Element),
	}
}

func (c *preflightCache) get(key preflightCacheKey) (payload []byte, coreSHA384 string, chunkSHA384 []string, ok bool) {
	if c == nil {
		return nil, "", nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, found := c.entries[key]
	if !found {
		c.misses++
		return nil, "", nil, false
	}
	c.order.MoveToFront(elem)
	c.hits++
	entry := elem.Value.(*preflightCacheEntry)
	return entry.payload, entry.coreSHA384, slices.Clone(entry.chunkSHA384), true
}

func (c *preflightCache) recordExtract() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.extracts++
	c.mu.Unlock()
}

func (c *preflightCache) put(key preflightCacheKey, payload []byte, coreSHA384 string, chunkSHA384 []string) {
	if c == nil {
		return
	}
	sizeBytes := int64(len(payload))
	if sizeBytes <= 0 || sizeBytes > c.maxBytes || c.maxEntries <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, found := c.entries[key]; found {
		c.order.MoveToFront(elem)
		return
	}
	for (c.order.Len() >= c.maxEntries) || (c.usedBytes+sizeBytes > c.maxBytes) {
		back := c.order.Back()
		if back == nil {
			break
		}
		evicted := c.order.Remove(back).(*preflightCacheEntry)
		delete(c.entries, evicted.key)
		c.usedBytes -= evicted.sizeBytes
		c.evictions++
	}
	entry := &preflightCacheEntry{
		key:         key,
		payload:     payload,
		coreSHA384:  coreSHA384,
		chunkSHA384: slices.Clone(chunkSHA384),
		sizeBytes:   sizeBytes,
	}
	c.entries[key] = c.order.PushFront(entry)
	c.usedBytes += sizeBytes
}

func (c *preflightCache) stats() preflightCacheStats {
	if c == nil {
		return preflightCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return preflightCacheStats{
		hits:      c.hits,
		misses:    c.misses,
		evictions: c.evictions,
		extracts:  c.extracts,
		entries:   c.order.Len(),
		usedBytes: c.usedBytes,
	}
}

func decodeSHA256Hex(raw string) ([32]byte, bool) {
	var out [32]byte
	if !validHex(raw, 64) {
		return out, false
	}
	n, err := hex.Decode(out[:], []byte(raw))
	return out, err == nil && n == 32
}
