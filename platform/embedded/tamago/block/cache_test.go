// SPDX-License-Identifier: GPL-3.0-or-later

package block

import (
	"bytes"
	"errors"
	"math"
	"sync"
	"testing"
)

type memDevice struct {
	mu       sync.Mutex
	sectors  uint64
	data     []byte
	reads    int
	writes   int
	flushes  int
	readErr  error
	writeErr error
	flushErr error
	readOnly bool
}

func newMemDevice(sectors uint64) *memDevice {
	data := make([]byte, sectors*SectorSize)
	for s := uint64(0); s < sectors; s++ {
		for b := 0; b < SectorSize; b++ {
			data[s*SectorSize+uint64(b)] = byte((s*31 + uint64(b)) & 0xff)
		}
	}
	return &memDevice{sectors: sectors, data: data}
}

func (m *memDevice) SectorCount() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sectors
}

func (m *memDevice) ReadSectors(lba uint64, dst []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := CheckRange(m.sectors, lba, len(dst)); err != nil {
		return err
	}
	m.reads++
	if m.readErr != nil {
		return m.readErr
	}
	copy(dst, m.data[lba*SectorSize:lba*SectorSize+uint64(len(dst))])
	return nil
}

func (m *memDevice) WriteSectors(lba uint64, src []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readOnly {
		return ErrReadOnly
	}
	if err := CheckRange(m.sectors, lba, len(src)); err != nil {
		return err
	}
	m.writes++
	if m.writeErr != nil {
		// Simulate a partial write landing on disk before the error.
		copy(m.data[lba*SectorSize:lba*SectorSize+SectorSize], src[:SectorSize])
		return m.writeErr
	}
	copy(m.data[lba*SectorSize:lba*SectorSize+uint64(len(src))], src)
	return nil
}

func (m *memDevice) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushes++
	return m.flushErr
}

func TestCacheValidationAndEmptyDevice(t *testing.T) {
	if _, err := NewCache(nil, CacheConfig{Capacity: 8}); err == nil {
		t.Fatal("nil device accepted")
	}
	if _, err := NewCache(&memDevice{sectors: 0}, CacheConfig{Capacity: 8}); !errors.Is(err, ErrBounds) {
		t.Fatalf("empty device err=%v, want ErrBounds", err)
	}
	dev := newMemDevice(32)
	if _, err := NewCache(dev, CacheConfig{Capacity: 0}); err == nil {
		t.Fatal("zero capacity accepted")
	}
	if _, err := NewCache(dev, CacheConfig{Capacity: maxCacheCapacitySectors + 1}); err == nil {
		t.Fatal("oversize capacity accepted")
	}
	c, err := NewCache(dev, CacheConfig{Capacity: 4, MaxPopulateSectors: 100})
	if err != nil {
		t.Fatal(err)
	}
	if c.SectorCount() != 32 || c.maxPopulate != 4 {
		t.Fatalf("sectors=%d maxPop=%d", c.SectorCount(), c.maxPopulate)
	}
	if err := c.ReadSectors(0, make([]byte, 10)); !errors.Is(err, ErrAlignment) {
		t.Fatalf("unaligned read err=%v", err)
	}
	if err := c.ReadSectors(32, make([]byte, SectorSize)); !errors.Is(err, ErrBounds) {
		t.Fatalf("oob read err=%v", err)
	}
	if err := c.WriteSectors(0, make([]byte, 10)); !errors.Is(err, ErrAlignment) {
		t.Fatalf("unaligned write err=%v", err)
	}
	if err := c.ReadSectors(0, nil); err != nil {
		t.Fatalf("empty read err=%v", err)
	}
	if err := c.WriteSectors(0, nil); err != nil {
		t.Fatalf("empty write err=%v", err)
	}
}

func TestCacheLRUEvictionAndContiguousCoalescing(t *testing.T) {
	dev := newMemDevice(64)
	c, err := NewCache(dev, CacheConfig{Capacity: 4, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, SectorSize)

	// Populate non-contiguous LBAs 0, 2, 4, 6 (MRU: 6, 4, 2, 0 :LRU).
	for _, lba := range []uint64{0, 2, 4, 6} {
		if err := c.ReadSectors(lba, buf); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, dev.data[lba*SectorSize:(lba+1)*SectorSize]) {
			t.Fatalf("lba %d mismatch", lba)
		}
	}
	if dev.reads != 4 {
		t.Fatalf("initial reads=%d, want 4", dev.reads)
	}

	// Touch LBA 0 (hit) so LBA 2 becomes LRU.
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != 4 {
		t.Fatalf("cached LBA 0 caused underlying read")
	}

	// Reading LBA 8 must evict LBA 2 (not LBA 0).
	if err := c.ReadSectors(8, buf); err != nil {
		t.Fatal(err)
	}
	if st := c.Stats(); st.Evictions != 1 || st.Entries != 4 || st.Hits != 1 || st.Misses != 5 {
		t.Fatalf("unexpected stats after eviction: %+v", st)
	}
	// Verify LBA 0 is still cached and LBA 2 was evicted.
	readsBefore := dev.reads
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore {
		t.Fatal("LBA 0 was wrongly evicted")
	}
	if err := c.ReadSectors(2, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore+1 {
		t.Fatal("LBA 2 was not evicted")
	}

	// Contiguous miss coalescing: read LBAs [10..13] (4 misses) in one underlying call.
	multi := make([]byte, 4*SectorSize)
	readsBefore = dev.reads
	if err := c.ReadSectors(10, multi); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore+1 {
		t.Fatalf("contiguous miss did not coalesce: reads=%d want %d", dev.reads, readsBefore+1)
	}
	if !bytes.Equal(multi, dev.data[10*SectorSize:14*SectorSize]) {
		t.Fatal("coalesced read data mismatch")
	}

	// Mixed hit+miss read: LBAs [9..14] -> miss(9), hits(10..13), miss(14) = 2 underlying reads.
	big := make([]byte, 6*SectorSize)
	readsBefore = dev.reads
	if err := c.ReadSectors(9, big); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore+2 {
		t.Fatalf("mixed hit/miss reads=%d, want %d", dev.reads, readsBefore+2)
	}
	if !bytes.Equal(big, dev.data[9*SectorSize:15*SectorSize]) {
		t.Fatal("mixed hit/miss data mismatch")
	}
	// Streaming bypass: reading 6 sectors (> MaxPopulateSectors=4) must NOT
	// evict LBAs 10..13!
	readsBefore = dev.reads
	if err := c.ReadSectors(10, multi); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore {
		t.Fatal("streaming read polluted hot cached sectors")
	}
}

func TestCacheSequentialSingleSectorStreamBypass(t *testing.T) {
	// Mirror fat.file.Read: single-sector sequential file reads interleaved
	// with hot FAT/directory hits must stop populating after MaxPopulateSectors
	// so hot metadata sectors are never evicted by large file streams.
	dev := newMemDevice(128)
	c, err := NewCache(dev, CacheConfig{Capacity: 6, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, SectorSize)
	// Warm two hot metadata sectors (FAT LBA 0, directory LBA 1).
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	if err := c.ReadSectors(1, buf); err != nil {
		t.Fatal(err)
	}
	// Stream 32 consecutive single-sector reads at LBAs 32..63, interleaving
	// FAT hits at LBA 0 every 4 sectors (matching FAT32 cluster walks).
	for i := uint64(0); i < 32; i++ {
		if i%4 == 0 {
			if err := c.ReadSectors(0, buf); err != nil {
				t.Fatal(err)
			}
		}
		lba := 32 + i
		if err := c.ReadSectors(lba, buf); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf, dev.data[lba*SectorSize:(lba+1)*SectorSize]) {
			t.Fatalf("stream lba %d mismatch", lba)
		}
	}
	// Both hot metadata sectors (0 and 1) must still be resident in cache.
	readsBefore := dev.reads
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	if err := c.ReadSectors(1, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore {
		t.Fatalf("single-sector read stream evicted hot metadata (evictions=%d)", c.Stats().Evictions)
	}
	if c.Stats().Evictions != 0 {
		t.Fatalf("expected 0 evictions during bypassed stream, got %d", c.Stats().Evictions)
	}

	// Sequential single-sector writes (LBAs 80..95) must also bypass after
	// MaxPopulateSectors without evicting hot metadata, while updating any
	// overlapped cached sector in place.
	for i := uint64(0); i < 16; i++ {
		sector := bytes.Repeat([]byte{byte(i + 1)}, SectorSize)
		if err := c.WriteSectors(80+i, sector); err != nil {
			t.Fatal(err)
		}
	}
	readsBefore = dev.reads
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	if err := c.ReadSectors(1, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore {
		t.Fatal("single-sector write stream evicted hot metadata")
	}
}

func TestCacheWriteThroughFailureInvalidationAndFlush(t *testing.T) {
	dev := newMemDevice(64)
	c, err := NewCache(dev, CacheConfig{Capacity: 8, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, SectorSize)
	// Warm LBA 2 and LBA 3.
	if err := c.ReadSectors(2, buf); err != nil {
		t.Fatal(err)
	}
	if err := c.ReadSectors(3, buf); err != nil {
		t.Fatal(err)
	}

	// Write-through small write updates both backing device and cache immediately.
	pattern := bytes.Repeat([]byte{0x5a}, SectorSize)
	if err := c.WriteSectors(2, pattern); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dev.data[2*SectorSize:3*SectorSize], pattern) {
		t.Fatal("write did not reach backing device immediately")
	}
	readsBefore := dev.reads
	if err := c.ReadSectors(2, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore || !bytes.Equal(buf, pattern) {
		t.Fatal("cached read after write-through missed or returned stale data")
	}

	// Streaming write (> MaxPopulateSectors) updates overlapping cached sectors
	// without evicting unrelated sectors.
	stream := bytes.Repeat([]byte{0xa5}, 6*SectorSize)
	if err := c.WriteSectors(1, stream); err != nil {
		t.Fatal(err)
	}
	readsBefore = dev.reads
	if err := c.ReadSectors(3, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore || !bytes.Equal(buf, bytes.Repeat([]byte{0xa5}, SectorSize)) {
		t.Fatal("streaming write left stale overlapping cache entry")
	}

	// Streaming write larger than len(c.index) exercises map-scan update path.
	huge := bytes.Repeat([]byte{0x3c}, 12*SectorSize)
	if err := c.WriteSectors(0, huge); err != nil {
		t.Fatal(err)
	}
	readsBefore = dev.reads
	if err := c.ReadSectors(2, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore || !bytes.Equal(buf, bytes.Repeat([]byte{0x3c}, SectorSize)) {
		t.Fatal("large streaming write left stale overlapping cache entry")
	}

	// Failed write MUST invalidate overlapping cached sectors so subsequent reads
	// fetch actual disk state (which may have been partially modified).
	dev.writeErr = errors.New("io error")
	partial := bytes.Repeat([]byte{0xee}, 2*SectorSize)
	if err := c.WriteSectors(2, partial); err == nil {
		t.Fatal("write error swallowed")
	}
	dev.writeErr = nil
	readsBefore = dev.reads
	if err := c.ReadSectors(2, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore+1 || !bytes.Equal(buf, bytes.Repeat([]byte{0xee}, SectorSize)) {
		t.Fatal("failed write did not invalidate stale cached sector")
	}

	// Failed read does not populate cache.
	dev.readErr = errors.New("read fault")
	if err := c.ReadSectors(20, buf); err == nil {
		t.Fatal("read error swallowed")
	}
	dev.readErr = nil
	readsBefore = dev.reads
	if err := c.ReadSectors(20, buf); err != nil {
		t.Fatal(err)
	}
	if dev.reads != readsBefore+1 {
		t.Fatal("failed read populated cache")
	}

	// Flush delegates directly and propagates errors.
	if err := c.Flush(); err != nil || dev.flushes != 1 {
		t.Fatalf("Flush err=%v flushes=%d", err, dev.flushes)
	}
	dev.flushErr = errors.New("flush fault")
	if err := c.Flush(); err == nil {
		t.Fatal("Flush error swallowed")
	}
	dev.flushErr = nil
}

func TestCacheInvalidateAndOverflowSafety(t *testing.T) {
	dev := newMemDevice(32)
	c, err := NewCache(dev, CacheConfig{Capacity: 8})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4*SectorSize)
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	// Overflowing InvalidateRange near MaxUint64 must NOT wrap around and drop LBA 0.
	c.InvalidateRange(math.MaxUint64, 4)
	if c.Stats().Entries != 4 {
		t.Fatalf("overflowing InvalidateRange dropped entries: %+v", c.Stats())
	}
	c.InvalidateRange(0, 0)
	if c.Stats().Entries != 4 {
		t.Fatal("zero-count InvalidateRange dropped entries")
	}
	// Range smaller than len(index) and larger than len(index).
	c.InvalidateRange(1, 2)
	if c.Stats().Entries != 2 {
		t.Fatalf("InvalidateRange(1,2) entries=%d, want 2", c.Stats().Entries)
	}
	c.InvalidateRange(0, 100)
	if c.Stats().Entries != 0 {
		t.Fatalf("InvalidateRange(0,100) entries=%d, want 0", c.Stats().Entries)
	}
	// Full Invalidate on empty and non-empty cache.
	c.Invalidate()
	if err := c.ReadSectors(0, buf); err != nil {
		t.Fatal(err)
	}
	c.Invalidate()
	if c.Stats().Entries != 0 {
		t.Fatalf("Invalidate entries=%d, want 0", c.Stats().Entries)
	}
}

func TestCacheConcurrentAccessRace(t *testing.T) {
	dev := newMemDevice(32)
	c, err := NewCache(dev, CacheConfig{Capacity: 8})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			buf := make([]byte, SectorSize)
			for i := range 64 {
				lba := uint64((id*7 + i) % 16)
				if err := c.ReadSectors(lba, buf); err != nil {
					t.Errorf("read %d: %v", lba, err)
					return
				}
				if i%13 == 0 {
					c.InvalidateRange(lba, 1)
				}
				_ = c.Contains(lba)
				_ = c.Stats()
			}
		}(g)
	}
	wg.Wait()
}

func TestCacheExtendedStatsContainsAndPrefetch(t *testing.T) {
	dev := newMemDevice(128)
	c, err := NewCache(dev, CacheConfig{Capacity: 24, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}

	// Prefetch zero count is a no-op; out-of-bounds or exceeding capacity fails cleanly.
	if err := c.Prefetch(0, 0); err != nil {
		t.Fatalf("Prefetch(0,0): %v", err)
	}
	if err := c.Prefetch(120, 10); !errors.Is(err, ErrBounds) {
		t.Fatalf("Prefetch oob err=%v, want ErrBounds", err)
	}
	if err := c.Prefetch(0, 25); err == nil {
		t.Fatal("Prefetch exceeding capacity succeeded")
	}

	// Pre-populate LBA 5 so Prefetch over [0..19] must issue 2 reads: [0..4] and [6..19] (14 <= 16),
	// without changing Hits or Misses.
	one := make([]byte, SectorSize)
	if err := c.ReadSectors(5, one); err != nil {
		t.Fatal(err)
	}
	if !c.Contains(5) || c.Contains(4) {
		t.Fatalf("Contains mismatch before Prefetch")
	}
	hitsBefore, missesBefore := c.Stats().Hits, c.Stats().Misses
	readsBefore := dev.reads
	if err := c.Prefetch(0, 20); err != nil {
		t.Fatalf("Prefetch(0,20): %v", err)
	}
	if dev.reads != readsBefore+2 {
		t.Fatalf("Prefetch reads=%d, want %d", dev.reads-readsBefore, 2)
	}
	if st := c.Stats(); st.Hits != hitsBefore || st.Misses != missesBefore || st.Entries != 20 {
		t.Fatalf("Prefetch disturbed Hits/Misses or Entries: %+v", st)
	}
	for lba := uint64(0); lba < 20; lba++ {
		if !c.Contains(lba) {
			t.Fatalf("LBA %d missing after Prefetch", lba)
		}
	}

	// Large single-run Prefetch (> maxPrefetchChunk=16) splits into 16 + remaining chunks.
	dev2 := newMemDevice(64)
	c2, err := NewCache(dev2, CacheConfig{Capacity: 20, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.Prefetch(10, 20); err != nil {
		t.Fatalf("Prefetch(10,20): %v", err)
	}
	if dev2.reads != 2 || c2.Stats().ReadOps != 2 || c2.Stats().Entries != 20 {
		t.Fatalf("chunked Prefetch reads=%d stats=%+v", dev2.reads, c2.Stats())
	}
	// Repeated Prefetch must allocate zero bytes using the preallocated scratch buffer.
	if allocs := testing.AllocsPerRun(10, func() {
		if err := c2.Prefetch(30, 8); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("Prefetch allocs=%v, want 0", allocs)
	}
	// Faulty Prefetch increments ReadErrors without caching failed sectors.
	dev2.readErr = errors.New("media fault")
	if err := c2.Prefetch(0, 2); err == nil {
		t.Fatal("failed Prefetch succeeded")
	}
	if c2.Contains(0) || c2.Stats().ReadErrors != 1 {
		t.Fatalf("failed Prefetch state: contains0=%t stats=%+v", c2.Contains(0), c2.Stats())
	}
	dev2.readErr = nil

	// Resident LRU sector inside the prefetched range must not be evicted by earlier misses.
	devLRU := newMemDevice(32)
	cLRU, err := NewCache(devLRU, CacheConfig{Capacity: 4, MaxPopulateSectors: 4})
	if err != nil {
		t.Fatal(err)
	}
	_ = cLRU.ReadSectors(3, one)
	_ = cLRU.ReadSectors(10, one)
	_ = cLRU.ReadSectors(11, one)
	_ = cLRU.ReadSectors(12, one) // LBA 3 is now LRU
	readsBefore = devLRU.reads
	if err := cLRU.Prefetch(0, 4); err != nil {
		t.Fatalf("Prefetch(0,4) with LRU LBA 3: %v", err)
	}
	if devLRU.reads != readsBefore+1 {
		t.Fatalf("Prefetch with LRU resident read %d times, want 1 (only [0..2])", devLRU.reads-readsBefore)
	}
	for lba := uint64(0); lba < 4; lba++ {
		if !cLRU.Contains(lba) {
			t.Fatalf("LBA %d missing after LRU-preserving Prefetch", lba)
		}
	}

	// Exercise ReadBypasses, WriteUpdates, WriteBypasses, FlushOps, and Error counters.
	// Read 6 uncached sectors (> MaxPopulateSectors=4) -> ReadBypasses=6.
	six := make([]byte, 6*SectorSize)
	if err := c.ReadSectors(40, six); err != nil {
		t.Fatal(err)
	}
	if st := c.Stats(); st.ReadBypasses != 6 || c.Contains(40) {
		t.Fatalf("oversize ReadBypasses mismatch: %+v contains40=%t", st, c.Contains(40))
	}

	// Sequential small writes: overwrite cached LBA 2 (WriteUpdates=1) and new LBA 30 (WriteUpdates still 1).
	if err := c.WriteSectors(2, bytes.Repeat([]byte{0x11}, SectorSize)); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteSectors(30, bytes.Repeat([]byte{0x22}, SectorSize)); err != nil {
		t.Fatal(err)
	}
	// Streaming write spanning cached LBAs 18..19 + uncached LBAs 20..23 -> +2 WriteUpdates, +4 WriteBypasses.
	if err := c.WriteSectors(18, bytes.Repeat([]byte{0x33}, 6*SectorSize)); err != nil {
		t.Fatal(err)
	}
	// Streaming write with count > len(index): test on smaller cache with 2 cached sectors and 5-sector write.
	cSmall, err := NewCache(dev, CacheConfig{Capacity: 8, MaxPopulateSectors: 2})
	if err != nil {
		t.Fatal(err)
	}
	_ = cSmall.ReadSectors(0, one)
	_ = cSmall.ReadSectors(3, one)
	if err := cSmall.WriteSectors(0, bytes.Repeat([]byte{0x44}, 5*SectorSize)); err != nil {
		t.Fatal(err)
	}
	if st := cSmall.Stats(); st.WriteUpdates != 2 || st.WriteBypasses != 3 {
		t.Fatalf("map-scan stream write stats mismatch: %+v", st)
	}

	// WriteErrors, FlushOps, FlushErrors.
	dev.writeErr = errors.New("write fault")
	_ = c.WriteSectors(2, one)
	dev.writeErr = nil
	_ = c.Flush()
	dev.flushErr = errors.New("flush fault")
	_ = c.Flush()
	dev.flushErr = nil
	dev.readErr = errors.New("read fault")
	_ = c.ReadSectors(60, one)
	dev.readErr = nil

	st := c.Stats()
	if st.ReadBypasses != 6 || st.WriteUpdates != 3 || st.WriteBypasses != 4 ||
		st.WriteErrors != 1 || st.FlushOps != 2 || st.FlushErrors != 1 || st.ReadErrors != 1 {
		t.Fatalf("extended stats mismatch: %+v", st)
	}
}
