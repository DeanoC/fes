// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestPoolInvalidConfigs(t *testing.T) {
	hw := newMockCoherency(64)
	mem := make([]byte, 4096)

	if _, err := NewPool(0x50000000, mem, Uncached, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil hw: got %v", err)
	}
	if _, err := NewPool(0x50000000, nil, Uncached, hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("empty backing: got %v", err)
	}
	if _, err := NewPool(0x50000000, mem, MemoryMode(99), hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad mode: got %v", err)
	}
	if _, err := NewPool(0x50000004, mem, Uncached, hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unaligned uncached base: got %v", err)
	}
	if _, err := NewPool(0x50000020, mem, Cached, hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unaligned cached base: got %v", err)
	}
	if _, err := NewPool(0x50000000, make([]byte, 100), Cached, hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unaligned cached size: got %v", err)
	}
	if _, err := NewPool(^uint64(0)&^uint64(63), make([]byte, 128), Cached, hw); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("overflow: got %v", err)
	}
	if _, err := NewPool(0x50000000, mem, Cached, newMockCoherency(8)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad cache line: got %v", err)
	}
}

func TestPoolAllocAlignZeroCoalesce(t *testing.T) {
	hw := newMockCoherency(64)
	backing := make([]byte, 4096)
	for i := range backing {
		backing[i] = 0xa5
	}
	pool, err := NewPool(0x50000000, backing, Cached, hw)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Base() != 0x50000000 || pool.Size() != 4096 || pool.Mode() != Cached || pool.Coherency() != hw {
		t.Fatalf("unexpected pool metadata: base=%#x size=%d mode=%v", pool.Base(), pool.Size(), pool.Mode())
	}

	// Small allocations in Cached mode are padded to cache line (64B).
	a, err := pool.Alloc(13, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Mode() != Cached || a.Released() {
		t.Fatalf("unexpected buf a state: mode=%v released=%v", a.Mode(), a.Released())
	}
	if a.PhysAddr() != 0x50000000 || a.Len() != 13 || a.Cap() != 64 {
		t.Fatalf("buf a: phys=%#x len=%d cap=%d", a.PhysAddr(), a.Len(), a.Cap())
	}
	for i, v := range a.Bytes() {
		if v != 0 {
			t.Fatalf("buf a not zeroed at %d: %#x", i, v)
		}
	}

	// Higher alignment splits leading gap cleanly without losing it.
	b, err := pool.Alloc(100, 512)
	if err != nil {
		t.Fatal(err)
	}
	if b.PhysAddr() != 0x50000200 || b.Len() != 100 || b.Cap() != 128 {
		t.Fatalf("buf b: phys=%#x len=%d cap=%d", b.PhysAddr(), b.Len(), b.Cap())
	}

	// Next small allocation should fit in the gap [0x40, 0x200).
	c, err := pool.Alloc(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if c.PhysAddr() != 0x50000040 {
		t.Fatalf("expected gap reuse at 0x50000040, got %#x", c.PhysAddr())
	}

	// Write non-zero data into a, free in arbitrary order, confirm coalescing and zeroing.
	copy(a.Bytes(), []byte("stale-secret!"))
	if err := b.Free(); err != nil {
		t.Fatal(err)
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
	if err := c.Free(); err != nil {
		t.Fatal(err)
	}
	st := pool.Stats()
	if st.FreeBytes != 4096 || st.AllocatedBytes != 0 || st.LiveBuffers != 0 || st.FreeSpans != 1 {
		t.Fatalf("expected full coalesce to 1 span, got %+v", st)
	}

	// Allocate full arena after coalesce and verify zeroed.
	full, err := pool.Alloc(4096, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(full.Bytes(), make([]byte, 4096)) {
		t.Fatal("re-allocated span was not zeroed")
	}
	if _, err := pool.Alloc(64, 64); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory, got %v", err)
	}
	if err := full.Free(); err != nil {
		t.Fatal(err)
	}
	if !full.Released() || full.Bytes() != nil {
		t.Fatalf("expected released buffer to report Released()=true and nil Bytes()")
	}
	if err := full.Free(); !errors.Is(err, ErrReleased) {
		t.Fatalf("double free: got %v", err)
	}
}

func TestBufferReadWriteSliceAndSync(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x94800000, make([]byte, 1024), Cached, hw)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := pool.Alloc(128, 64)
	if err != nil {
		t.Fatal(err)
	}

	if err := buf.Write32(0, 0x11223344); err != nil {
		t.Fatal(err)
	}
	if v, err := buf.Read32(0); err != nil || v != 0x11223344 {
		t.Fatalf("Read32: %#x err=%v", v, err)
	}
	if err := buf.Write64(8, 0x0102030405060708); err != nil {
		t.Fatal(err)
	}
	if v, err := buf.Read64(8); err != nil || v != 0x0102030405060708 {
		t.Fatalf("Read64: %#x err=%v", v, err)
	}
	// Alignment and bounds checks.
	if err := buf.Write32(2, 1); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unaligned Write32: %v", err)
	}
	if err := buf.Write64(4, 1); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unaligned Write64: %v", err)
	}
	if _, err := buf.Read32(128); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("oob Read32: %v", err)
	}
	if phys, sub, err := buf.Slice(16, 32); err != nil || phys != 0x94800010 || len(sub) != 32 {
		t.Fatalf("Slice: phys=%#x len=%d err=%v", phys, len(sub), err)
	}
	if _, _, err := buf.Slice(120, 16); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("oob Slice: %v", err)
	}

	// Cached mode sync operations.
	if err := buf.SyncForDevice(ToDevice); err != nil {
		t.Fatal(err)
	}
	if err := buf.SyncForDevice(FromDevice); err != nil {
		t.Fatal(err)
	}
	if err := buf.SyncForDevice(Bidirectional); err != nil {
		t.Fatal(err)
	}
	if err := buf.SyncForCPU(ToDevice); err != nil {
		t.Fatal(err)
	}
	if err := buf.SyncForCPU(FromDevice); err != nil {
		t.Fatal(err)
	}
	if err := buf.SyncRangeForCPU(64, 64, Bidirectional); err != nil {
		t.Fatal(err)
	}
	// Partial cached receive ranges that share a cache line with neighbor bytes
	// must reject, while partial ToDevice ranges and full-buffer tail spans work.
	if err := buf.SyncRangeForDevice(8, 32, FromDevice); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unaligned start cached FromDevice should reject: got %v", err)
	}
	if err := buf.SyncRangeForCPU(0, 32, FromDevice); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unaligned mid-buffer cached FromDevice should reject: got %v", err)
	}
	if err := buf.SyncRangeForCPU(0, 32, Bidirectional); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unaligned mid-buffer cached Bidirectional should reject: got %v", err)
	}
	if err := buf.SyncRangeForDevice(8, 32, ToDevice); err != nil {
		t.Fatalf("partial cached ToDevice should succeed: %v", err)
	}
	// A whole-buffer receive sync on a non-line-multiple Len() expands safely
	// to its exclusive Cap().
	shortBuf, err := pool.Alloc(100, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := shortBuf.SyncForDevice(FromDevice); err != nil {
		t.Fatalf("shortBuf SyncForDevice(FromDevice): %v", err)
	}
	if err := shortBuf.SyncForCPU(FromDevice); err != nil {
		t.Fatalf("shortBuf SyncForCPU(FromDevice): %v", err)
	}
	if err := shortBuf.Free(); err != nil {
		t.Fatal(err)
	}
	got := hw.take()
	want := []coherencyOp{
		{"clean", 0x94800000, 128},
		{"clean_inv", 0x94800000, 128},
		{"clean_inv", 0x94800000, 128},
		{"dmb", 0, 0},
		{"inv", 0x94800000, 128},
		{"inv", 0x94800040, 64},
		{"clean", 0x94800008, 32},
		{"clean_inv", 0x94800080, 128},
		{"inv", 0x94800080, 128},
	}
	if len(got) != len(want) {
		t.Fatalf("cached ops count: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cached op[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// Uncached mode issues barriers only.
	ucPool, err := NewPool(0x50000000, make([]byte, 512), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	ucBuf, err := ucPool.Alloc(64, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := ucBuf.SyncForDevice(ToDevice); err != nil {
		t.Fatal(err)
	}
	if err := ucBuf.SyncForCPU(FromDevice); err != nil {
		t.Fatal(err)
	}
	got = hw.take()
	want = []coherencyOp{
		{"dsb", 0, 0},
		{"dmb", 0, 0},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("uncached ops: got %+v, want %+v", got, want)
	}
}

func TestBufferViewsAreCapacityBounded(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x50000000, make([]byte, 256), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := pool.Alloc(13, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Alloc(16, 8); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	if len(data) != 13 || cap(data) != 13 {
		t.Fatalf("Bytes view: len=%d cap=%d, want len=13 cap=13", len(data), cap(data))
	}

	_, sub, err := buf.Slice(4, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 5 || cap(sub) != 5 {
		t.Fatalf("Slice view: len=%d cap=%d, want len=5 cap=5", len(sub), cap(sub))
	}
}

func TestPoolAndBufferPoisonFailClosed(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x50000000, make([]byte, 256), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := pool.Alloc(128, 64)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := pool.Alloc(128, 64)
	if err != nil {
		t.Fatal(err)
	}

	timeoutErr := errors.New("tx dma timeout")
	b1.Poison(timeoutErr)
	if !errors.Is(b1.Err(), timeoutErr) {
		t.Fatalf("expected timeoutErr, got %v", b1.Err())
	}
	if err := b1.Free(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("freeing poisoned buffer should fail: %v", err)
	}
	if err := b1.SyncForDevice(ToDevice); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("syncing poisoned buffer should fail: %v", err)
	}
	if err := b1.Write32(0, 1); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("writing poisoned buffer should fail: %v", err)
	}

	// Healthy sibling buffer can still be freed, but poisoned span remains pinned.
	if err := b2.Free(); err != nil {
		t.Fatal(err)
	}
	st := pool.Stats()
	if st.PinnedBytes != 128 || st.PoisonedBuffers != 1 || st.FreeBytes != 128 || st.AllocatedBytes != 128 {
		t.Fatalf("unexpected stats after b1 poison + b2 free: %+v", st)
	}
	// Poisoned span must never be reallocated.
	b3, err := pool.Alloc(128, 64)
	if err != nil {
		t.Fatal(err)
	}
	if b3.PhysAddr() == b1.PhysAddr() {
		t.Fatalf("poisoned span %#x was reused!", b1.PhysAddr())
	}

	// Poisoning the pool blocks all future Alloc and Free calls.
	pool.Poison(errors.New("smmu fault"))
	if err := b3.Free(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("free after pool poison: %v", err)
	}
	if _, err := pool.Alloc(16, 8); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("alloc after pool poison: %v", err)
	}
	if !pool.Stats().Poisoned {
		t.Fatal("expected pool.Stats().Poisoned == true")
	}
}

func TestPoolConcurrentAllocFree(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x50000000, make([]byte, 64<<10), Cached, hw)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 64; i++ {
				b, err := pool.Alloc(256, 64)
				if err != nil {
					t.Errorf("Alloc: %v", err)
					return
				}
				if err := b.SyncForDevice(ToDevice); err != nil {
					t.Errorf("SyncForDevice: %v", err)
					return
				}
				if err := b.SyncForCPU(FromDevice); err != nil {
					t.Errorf("SyncForCPU: %v", err)
					return
				}
				if err := b.Free(); err != nil {
					t.Errorf("Free: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	st := pool.Stats()
	if st.FreeBytes != 64<<10 || st.FreeSpans != 1 || st.LiveBuffers != 0 {
		t.Fatalf("expected clean pool after concurrent test: %+v", st)
	}
}
