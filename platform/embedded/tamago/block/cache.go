// SPDX-License-Identifier: GPL-3.0-or-later

package block

import (
	"errors"
	"fmt"
	"sync"
)

const (
	defaultMaxPopulateSectors = 16
	maxPrefetchChunk          = 16
	maxCacheCapacitySectors   = 16384 // 8 MiB ceiling for bare-metal RAM safety
)

// CacheConfig configures a bounded LRU write-through sector cache.
type CacheConfig struct {
	// Capacity is the maximum number of 512-byte sectors held in the cache.
	// Must be in [1, 16384].
	Capacity int

	// MaxPopulateSectors caps the size of a single read or write request that
	// populates new cache entries. Larger sequential transfers bypass
	// allocation into the cache so streaming reads do not evict hot FAT32
	// directory/FAT sectors (overlapping cached sectors on writes are still
	// updated in place). When <= 0, defaults to min(Capacity, 16).
	MaxPopulateSectors int
}

// CacheStats reports cumulative sector-cache counters.
type CacheStats struct {
	Capacity      int
	Entries       int
	Hits          uint64
	Misses        uint64
	Evictions     uint64
	ReadOps       uint64
	WriteOps      uint64
	FlushOps      uint64
	ReadErrors    uint64
	WriteErrors   uint64
	FlushErrors   uint64
	ReadBypasses  uint64
	WriteBypasses uint64
	WriteUpdates  uint64
	Invalidations uint64
}

type cacheSlot struct {
	lba  uint64
	prev int
	next int
	data [SectorSize]byte
}

// Cache wraps a Device with a fixed-capacity, zero-allocation LRU read and
// write-through sector cache. Every write is sent synchronously to the
// underlying Device before updating cached entries; any write error invalidates
// all cached sectors in the attempted range so stale data is never served.
type Cache struct {
	mu           sync.Mutex
	dev          Device
	capacity     int
	maxPopulate  int
	slots        []cacheSlot
	freeList     []int
	index        map[uint64]int
	head         int // MRU slot index (-1 when empty)
	tail         int // LRU slot index (-1 when empty)
	lastMissEnd  uint64
	seqMisses    uint64
	lastWriteEnd uint64
	seqWrites    uint64
	prefetchBuf  [maxPrefetchChunk * SectorSize]byte
	stats        CacheStats
}

// NewCache creates a bounded write-through sector cache around dev.
func NewCache(dev Device, cfg CacheConfig) (*Cache, error) {
	if dev == nil {
		return nil, errors.New("block: nil device")
	}
	if dev.SectorCount() == 0 {
		return nil, ErrBounds
	}
	if cfg.Capacity <= 0 || cfg.Capacity > maxCacheCapacitySectors {
		return nil, fmt.Errorf("block: cache capacity %d out of range [1,%d]", cfg.Capacity, maxCacheCapacitySectors)
	}
	maxPop := cfg.MaxPopulateSectors
	if maxPop <= 0 {
		maxPop = min(cfg.Capacity, defaultMaxPopulateSectors)
	} else if maxPop > cfg.Capacity {
		maxPop = cfg.Capacity
	}
	slots := make([]cacheSlot, cfg.Capacity)
	freeList := make([]int, cfg.Capacity)
	for i := range cfg.Capacity {
		slots[i].prev = -1
		slots[i].next = -1
		freeList[i] = cfg.Capacity - 1 - i
	}
	return &Cache{
		dev:         dev,
		capacity:    cfg.Capacity,
		maxPopulate: maxPop,
		slots:       slots,
		freeList:    freeList,
		index:       make(map[uint64]int, cfg.Capacity),
		head:        -1,
		tail:        -1,
		stats:       CacheStats{Capacity: cfg.Capacity},
	}, nil
}

// SectorCount delegates to the underlying device.
func (c *Cache) SectorCount() uint64 {
	return c.dev.SectorCount()
}

// ReadSectors serves cached sectors from RAM and coalesces contiguous misses
// into a single underlying ReadSectors call.
func (c *Cache) ReadSectors(lba uint64, dst []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := CheckRange(c.dev.SectorCount(), lba, len(dst)); err != nil {
		return err
	}
	if len(dst) == 0 {
		return nil
	}
	count := uint64(len(dst) / SectorSize)
	for i := uint64(0); i < count; {
		if slot, ok := c.index[lba+i]; ok {
			copy(dst[i*SectorSize:(i+1)*SectorSize], c.slots[slot].data[:])
			c.moveToFront(slot)
			c.stats.Hits++
			i++
			continue
		}
		end := i + 1
		for end < count {
			if _, ok := c.index[lba+end]; ok {
				break
			}
			end++
		}
		missStart := lba + i
		missEnd := lba + end
		if err := c.dev.ReadSectors(missStart, dst[i*SectorSize:end*SectorSize]); err != nil {
			c.stats.ReadOps++
			c.stats.ReadErrors++
			return err
		}
		c.stats.ReadOps++
		runLen := end - i
		c.stats.Misses += runLen
		if c.seqMisses > 0 && missStart == c.lastMissEnd {
			c.seqMisses += runLen
		} else {
			c.seqMisses = runLen
		}
		c.lastMissEnd = missEnd
		if int(count) <= c.maxPopulate && c.seqMisses <= uint64(c.maxPopulate) {
			for j := uint64(0); j < runLen; j++ {
				c.put(missStart+j, dst[(i+j)*SectorSize:(i+j+1)*SectorSize])
			}
		} else {
			c.stats.ReadBypasses += runLen
		}
		i = end
	}
	return nil
}

// WriteSectors writes synchronously to the underlying device. On error, all
// cached sectors overlapping [lba, lba+count) are invalidated before returning.
// On success, overlapping cached entries are updated in place, and requests
// within MaxPopulateSectors (including across consecutive sequential calls)
// also populate newly written sectors.
func (c *Cache) WriteSectors(lba uint64, src []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := CheckRange(c.dev.SectorCount(), lba, len(src)); err != nil {
		return err
	}
	if len(src) == 0 {
		return nil
	}
	count := uint64(len(src) / SectorSize)
	c.stats.WriteOps++
	if err := c.dev.WriteSectors(lba, src); err != nil {
		c.stats.WriteErrors++
		c.invalidateRangeLocked(lba, count)
		return err
	}
	if c.seqWrites > 0 && lba == c.lastWriteEnd {
		c.seqWrites += count
	} else {
		c.seqWrites = count
	}
	c.lastWriteEnd = lba + count
	if int(count) <= c.maxPopulate && c.seqWrites <= uint64(c.maxPopulate) {
		for i := uint64(0); i < count; i++ {
			if _, exists := c.index[lba+i]; exists {
				c.stats.WriteUpdates++
			}
			c.put(lba+i, src[i*SectorSize:(i+1)*SectorSize])
		}
		return nil
	}
	// Streaming write: update any already-cached overlapping sectors without
	// evicting unrelated metadata.
	var updated uint64
	if count <= uint64(len(c.index)) {
		for i := uint64(0); i < count; i++ {
			if slot, ok := c.index[lba+i]; ok {
				copy(c.slots[slot].data[:], src[i*SectorSize:(i+1)*SectorSize])
				c.moveToFront(slot)
				updated++
			}
		}
	} else {
		for cachedLBA, slot := range c.index {
			if cachedLBA >= lba && cachedLBA-lba < count {
				off := (cachedLBA - lba) * SectorSize
				copy(c.slots[slot].data[:], src[off:off+SectorSize])
				c.moveToFront(slot)
				updated++
			}
		}
	}
	c.stats.WriteUpdates += updated
	c.stats.WriteBypasses += count - updated
	return nil
}

// Flush forwards Flush directly to the underlying device. Because Cache is
// strictly write-through, no dirty sectors are buffered in RAM.
func (c *Cache) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.FlushOps++
	err := c.dev.Flush()
	if err != nil {
		c.stats.FlushErrors++
	}
	return err
}

// Contains reports whether lba is currently resident in the cache without
// altering LRU order or statistics.
func (c *Cache) Contains(lba uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.index[lba]
	return ok
}

// Prefetch warms the cache with sectors in [lba, lba+count), skipping any
// already-resident sectors and reading uncached contiguous runs in chunks up
// to 16 sectors without affecting Hits, Misses, or sequential bypass tracking.
func (c *Cache) Prefetch(lba, count uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if count == 0 {
		return nil
	}
	sectors := c.dev.SectorCount()
	if lba > sectors || count > sectors-lba {
		return ErrBounds
	}
	if count > uint64(c.capacity) {
		return fmt.Errorf("block: prefetch count %d exceeds cache capacity %d", count, c.capacity)
	}

	for i := uint64(0); i < count; {
		if slot, ok := c.index[lba+i]; ok {
			c.moveToFront(slot)
			i++
			continue
		}
		end := i + 1
		for end < count && end-i < maxPrefetchChunk {
			if _, ok := c.index[lba+end]; ok {
				break
			}
			end++
		}
		runLen := end - i
		chunk := c.prefetchBuf[:runLen*SectorSize]
		c.stats.ReadOps++
		if err := c.dev.ReadSectors(lba+i, chunk); err != nil {
			c.stats.ReadErrors++
			return err
		}
		for j := uint64(0); j < runLen; j++ {
			c.put(lba+i+j, chunk[j*SectorSize:(j+1)*SectorSize])
		}
		i = end
	}
	return nil
}

// Invalidate drops all cached sectors.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seqMisses = 0
	c.seqWrites = 0
	if len(c.index) == 0 {
		return
	}
	c.stats.Invalidations += uint64(len(c.index))
	clear(c.index)
	c.freeList = c.freeList[:0]
	for i := c.capacity - 1; i >= 0; i-- {
		c.slots[i].prev = -1
		c.slots[i].next = -1
		c.freeList = append(c.freeList, i)
	}
	c.head = -1
	c.tail = -1
	c.stats.Entries = 0
}

// InvalidateRange drops any cached sectors within [lba, lba+count).
func (c *Cache) InvalidateRange(lba, count uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidateRangeLocked(lba, count)
}

// Stats returns a snapshot of current cache counters.
func (c *Cache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *Cache) invalidateRangeLocked(lba, count uint64) {
	c.seqMisses = 0
	c.seqWrites = 0
	if count == 0 || len(c.index) == 0 {
		return
	}
	if count <= uint64(len(c.index)) {
		for i := uint64(0); i < count; i++ {
			if i > ^uint64(0)-lba {
				break
			}
			if slot, ok := c.index[lba+i]; ok {
				c.removeSlot(lba+i, slot)
				c.stats.Invalidations++
			}
		}
		return
	}
	for cachedLBA, slot := range c.index {
		if cachedLBA >= lba && cachedLBA-lba < count {
			c.removeSlot(cachedLBA, slot)
			c.stats.Invalidations++
		}
	}
}

func (c *Cache) put(lba uint64, sector []byte) {
	if slot, ok := c.index[lba]; ok {
		copy(c.slots[slot].data[:], sector)
		c.moveToFront(slot)
		return
	}
	var slot int
	if n := len(c.freeList); n > 0 {
		slot = c.freeList[n-1]
		c.freeList = c.freeList[:n-1]
	} else {
		slot = c.tail
		delete(c.index, c.slots[slot].lba)
		c.unlink(slot)
		c.stats.Evictions++
	}
	c.slots[slot].lba = lba
	copy(c.slots[slot].data[:], sector)
	c.pushFront(slot)
	c.index[lba] = slot
	c.stats.Entries = len(c.index)
}

func (c *Cache) removeSlot(lba uint64, slot int) {
	delete(c.index, lba)
	c.unlink(slot)
	c.freeList = append(c.freeList, slot)
	c.stats.Entries = len(c.index)
}

func (c *Cache) pushFront(slot int) {
	c.slots[slot].prev = -1
	c.slots[slot].next = c.head
	if c.head != -1 {
		c.slots[c.head].prev = slot
	} else {
		c.tail = slot
	}
	c.head = slot
}

func (c *Cache) unlink(slot int) {
	prev := c.slots[slot].prev
	next := c.slots[slot].next
	if prev != -1 {
		c.slots[prev].next = next
	} else {
		c.head = next
	}
	if next != -1 {
		c.slots[next].prev = prev
	} else {
		c.tail = prev
	}
	c.slots[slot].prev = -1
	c.slots[slot].next = -1
}

func (c *Cache) moveToFront(slot int) {
	if c.head == slot {
		return
	}
	c.unlink(slot)
	c.pushFront(slot)
}
