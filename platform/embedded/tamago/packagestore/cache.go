// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"container/list"
	"errors"
	"io"

	"github.com/DeanoC/FogCast/corepackage"
)

const maxCacheBytes int64 = 128 << 20

// CacheStats reports bounded retention cache capacity, occupancy, and counters.
type CacheStats struct {
	CapacityBytes int64
	UsedBytes     int64
	Entries       int
	Hits          uint64
	Misses        uint64
	Evictions     uint64
	Invalidations uint64
}

type cacheKey struct {
	Path           string
	PackageID      string
	ReceiptSHA256  string
	MetadataPath   string
	MetadataSHA256 string
	SourceCount    uint8
	Sources        [2]Source
	PartCount      uint8
	Parts          [2]Part
	Tape           TapeRequest
}

type cacheEntry struct {
	key        cacheKey
	archive    *corepackage.Archive
	linked     *corepackage.LinkedArchive
	metadata   []byte
	tape       []byte
	tapeFormat string
	sizeBytes  int64
}

type retainCache struct {
	capacityBytes int64
	usedBytes     int64
	entries       map[cacheKey]*list.Element
	order         *list.List
	hits          uint64
	misses        uint64
	evictions     uint64
	invalidations uint64
}

func newRetainCache(maxBytes int64) *retainCache {
	return &retainCache{
		capacityBytes: maxBytes,
		entries:       make(map[cacheKey]*list.Element),
		order:         list.New(),
	}
}

func makeCacheKey(request Request) cacheKey {
	key := cacheKey{
		Path:           request.Path,
		PackageID:      request.PackageID,
		ReceiptSHA256:  request.ReceiptSHA256,
		MetadataPath:   request.MetadataPath,
		MetadataSHA256: request.MetadataSHA256,
		SourceCount:    uint8(len(request.Sources)),
		PartCount:      uint8(len(request.Parts)),
		Tape:           request.Tape,
	}
	for i, src := range request.Sources {
		key.Sources[i] = src
	}
	for i, part := range request.Parts {
		key.Parts[i] = part
	}
	return key
}

func selectionSizeBytes(selection *Selection) int64 {
	if selection == nil || selection.archive == nil {
		return 0
	}
	d := selection.archive.Inspection().Descriptor
	size := d.Payload.Size + int64(len(selection.metadata)) + int64(len(selection.tape))
	if d.ROM != nil {
		size += d.ROM.Size
	}
	if d.ROMMap != nil {
		size += d.ROMMap.Size
	}
	if selection.linked != nil {
		receipt := selection.linked.Receipt()
		if receipt.ROMLink != nil {
			size += receipt.ROMLink.SourceSize + receipt.ROMLink.ProgrammedSize
		}
		if receipt.ROMLinks != nil {
			size += receipt.ROMLinks.ProgrammedSize
			for _, src := range receipt.ROMLinks.Sources {
				size += src.SourceSize
			}
		}
		if receipt.PartsComposition != nil {
			var scratch [4096]byte
			for _, part := range receipt.PartsComposition.Parts {
				if r, err := selection.linked.Part(part.PartID); err == nil {
					if n, err := io.CopyBuffer(io.Discard, r, scratch[:]); err == nil {
						size += n
					}
				}
			}
			if n, err := io.CopyBuffer(io.Discard, selection.linked.Overlay(), scratch[:]); err == nil {
				size += n
			}
		}
	}
	return size
}

func (c *retainCache) get(key cacheKey) (*cacheEntry, bool) {
	elem, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	c.order.MoveToFront(elem)
	return elem.Value.(*cacheEntry), true
}

func (c *retainCache) put(key cacheKey, selection *Selection, sizeBytes int64) {
	if sizeBytes <= 0 || sizeBytes > c.capacityBytes {
		return
	}
	if elem, ok := c.entries[key]; ok {
		old := elem.Value.(*cacheEntry)
		c.usedBytes -= old.sizeBytes
		c.order.Remove(elem)
		delete(c.entries, key)
	}
	c.evictUntil(c.capacityBytes - sizeBytes)
	entry := &cacheEntry{
		key:        key,
		archive:    selection.archive,
		linked:     selection.linked,
		metadata:   selection.metadata,
		tape:       selection.tape,
		tapeFormat: selection.tapeFormat,
		sizeBytes:  sizeBytes,
	}
	c.entries[key] = c.order.PushFront(entry)
	c.usedBytes += sizeBytes
}

func (c *retainCache) evictUntil(limit int64) {
	for c.usedBytes > limit && c.order.Len() > 0 {
		back := c.order.Back()
		evicted := c.order.Remove(back).(*cacheEntry)
		delete(c.entries, evicted.key)
		c.usedBytes -= evicted.sizeBytes
		c.evictions++
	}
}

func (c *retainCache) setCapacity(maxBytes int64) {
	c.capacityBytes = maxBytes
	c.evictUntil(maxBytes)
}

func (c *retainCache) invalidate() {
	if len(c.entries) == 0 {
		return
	}
	c.invalidations += uint64(len(c.entries))
	clear(c.entries)
	c.order.Init()
	c.usedBytes = 0
}

func (c *retainCache) stats() CacheStats {
	return CacheStats{
		CapacityBytes: c.capacityBytes,
		UsedBytes:     c.usedBytes,
		Entries:       len(c.entries),
		Hits:          c.hits,
		Misses:        c.misses,
		Evictions:     c.evictions,
		Invalidations: c.invalidations,
	}
}

// EnableCache installs or resizes a bounded LRU cache for retained selections
// on this store, capped at 128 MiB.
func (s *Store) EnableCache(maxBytes int64) error {
	if s == nil {
		return ErrState
	}
	if maxBytes <= 0 {
		return errors.New("package store: invalid cache capacity")
	}
	if maxBytes > maxCacheBytes {
		maxBytes = maxCacheBytes
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return ErrRetired
	}
	if s.cache == nil {
		s.cache = newRetainCache(maxBytes)
		return nil
	}
	s.cache.setCapacity(maxBytes)
	return nil
}

// DisableCache invalidates and detaches any active retention cache on s.
func (s *Store) DisableCache() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		s.cache.invalidate()
		s.cache = nil
	}
}

// InvalidateCache drops all cached entries while keeping caching enabled.
func (s *Store) InvalidateCache() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidateCacheLocked()
}

// CacheStats returns the active retention cache statistics and true when
// caching is enabled, or zero statistics and false otherwise.
func (s *Store) CacheStats() (CacheStats, bool) {
	if s == nil {
		return CacheStats{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		return CacheStats{}, false
	}
	return s.cache.stats(), true
}

func (s *Store) invalidateCacheLocked() {
	if s.cache != nil {
		s.cache.invalidate()
	}
}
