// SPDX-License-Identifier: GPL-3.0-or-later

package dmaops

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestRingLifecycleAndWraparound(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x50000000, make([]byte, 16384), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}

	ring, err := NewRing(pool, RingConfig{
		Count:     4,
		DescSize:  16,
		DescAlign: 64,
		BufSize:   1600,
		BufAlign:  64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ring.Count() != 4 || ring.DescSize() != 16 || ring.DescPhysAddr()%64 != 0 || ring.DescBuffer() == nil || ring.Err() != nil {
		t.Fatalf("unexpected ring geometry: count=%d descSize=%d phys=%#x err=%v", ring.Count(), ring.DescSize(), ring.DescPhysAddr(), ring.Err())
	}

	// Run 3 full revolutions (12 transfers) to verify wraparound.
	for i := 0; i < 12; i++ {
		slot, err := ring.Acquire()
		if err != nil {
			t.Fatalf("iter %d Acquire: %v", i, err)
		}
		if slot.Index != uint32(i%4) {
			t.Fatalf("iter %d index=%d want %d", i, slot.Index, i%4)
		}
		binary.LittleEndian.PutUint64(slot.Desc[0:8], slot.Buf.PhysAddr())
		binary.LittleEndian.PutUint32(slot.Desc[8:12], uint32(i+1))
		slot.Buf.Bytes()[0] = byte(i)

		if err := ring.Publish(slot, ToDevice); err != nil {
			t.Fatalf("iter %d Publish: %v", i, err)
		}
		if ring.InFlight() != 1 {
			t.Fatalf("iter %d InFlight=%d", i, ring.InFlight())
		}

		peek, err := ring.PeekComplete()
		if err != nil || peek.Index != slot.Index {
			t.Fatalf("iter %d PeekComplete: slot=%+v err=%v", i, peek, err)
		}
		if err := ring.SyncDescriptorForCPU(peek); err != nil {
			t.Fatalf("iter %d SyncDescriptorForCPU: %v", i, err)
		}
		if err := ring.Retire(peek, ToDevice); err != nil {
			t.Fatalf("iter %d Retire: %v", i, err)
		}
		if ring.InFlight() != 0 {
			t.Fatalf("iter %d InFlight after retire=%d", i, ring.InFlight())
		}
	}

	if err := ring.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := ring.Close(); !errors.Is(err, ErrReleased) {
		t.Fatalf("double Close: %v", err)
	}
	st := pool.Stats()
	if st.LiveBuffers != 0 || st.FreeBytes != 16384 || st.FreeSpans != 1 {
		t.Fatalf("expected pool fully reclaimed after Ring.Close, got %+v", st)
	}

	// Cached pools must reject sub-cache-line descriptors to prevent cache-line
	// sharing between adjacent descriptors, and accept full-line descriptors.
	cachedPool, err := NewPool(0x50000000, make([]byte, 4096), Cached, hw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRing(cachedPool, RingConfig{Count: 4, DescSize: 16}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("cached ring with 16B DescSize should reject: got %v", err)
	}
	if _, err := NewRing(cachedPool, RingConfig{Count: 4, DescSize: 96}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("cached ring with non-multiple DescSize should reject: got %v", err)
	}
	cachedRing, err := NewRing(cachedPool, RingConfig{Count: 4, DescSize: 64, BufSize: 128})
	if err != nil {
		t.Fatalf("cached ring with 64B DescSize: %v", err)
	}
	if err := cachedRing.Close(); err != nil {
		t.Fatalf("cachedRing.Close: %v", err)
	}
}

func TestRingQueueBoundsOrderingAndCancel(t *testing.T) {
	hw := newMockCoherency(64)
	pool, err := NewPool(0x50000000, make([]byte, 4096), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewRing(pool, RingConfig{Count: 2, DescSize: 16})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ring.PeekComplete(); !errors.Is(err, ErrRingEmpty) {
		t.Fatalf("empty PeekComplete: %v", err)
	}

	s0, err := ring.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if s0.Buf != nil {
		t.Fatal("descriptor-only ring should have nil Buf")
	}
	s1, err := ring.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Acquire(); !errors.Is(err, ErrRingFull) {
		t.Fatalf("full Acquire: %v", err)
	}

	// Out-of-order publish must reject.
	if err := ring.Publish(s1, ToDevice); !errors.Is(err, ErrInvalidSlot) {
		t.Fatalf("out-of-order Publish: %v", err)
	}
	// ReleaseAcquired cancels only the most recently acquired slot.
	if err := ring.ReleaseAcquired(s0); !errors.Is(err, ErrInvalidSlot) {
		t.Fatalf("out-of-order ReleaseAcquired: %v", err)
	}
	if err := ring.ReleaseAcquired(s1); err != nil {
		t.Fatalf("ReleaseAcquired s1: %v", err)
	}
	if ring.Acquired() != 1 {
		t.Fatalf("Acquired=%d want 1", ring.Acquired())
	}

	if err := ring.Publish(s0, ToDevice); err != nil {
		t.Fatal(err)
	}
	// Close must refuse while in-flight.
	if err := ring.Close(); !errors.Is(err, ErrInFlight) {
		t.Fatalf("Close while in-flight: %v", err)
	}

	if err := ring.Retire(s0, ToDevice); err != nil {
		t.Fatal(err)
	}
	if err := ring.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRingPoisonAndPartialAllocRollback(t *testing.T) {
	hw := newMockCoherency(64)
	// Pool too small for 4x512B buffers + descriptor table: NewRing must roll back cleanly.
	smallPool, err := NewPool(0x50000000, make([]byte, 1024), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRing(smallPool, RingConfig{Count: 4, DescSize: 16, BufSize: 512}); !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory, got %v", err)
	}
	if st := smallPool.Stats(); st.LiveBuffers != 0 || st.FreeBytes != 1024 || st.FreeSpans != 1 {
		t.Fatalf("partial NewRing leaked buffers: %+v", st)
	}

	// Poison permanently pins descriptor table and all slot buffers.
	pool, err := NewPool(0x50000000, make([]byte, 4096), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewRing(pool, RingConfig{Count: 2, DescSize: 16, BufSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	s0, err := ring.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.Publish(s0, FromDevice); err != nil {
		t.Fatal(err)
	}

	ring.Poison(errors.New("rx ring hung"))
	if ring.Err() == nil {
		t.Fatal("expected non-nil ring.Err() after Poison")
	}
	if err := ring.Close(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("Close after Poison: %v", err)
	}
	if _, err := ring.Acquire(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("Acquire after Poison: %v", err)
	}
	if err := s0.Buf.Free(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("Free slot buf after Poison: %v", err)
	}
	st := pool.Stats()
	if st.PoisonedBuffers != 3 || st.PinnedBytes == 0 {
		t.Fatalf("expected descriptor table + 2 slot buffers poisoned, got %+v", st)
	}

	// Poisoning an individual slot buffer must keep Ring.Close atomic (no partial free).
	pool2, err := NewPool(0x50000000, make([]byte, 4096), Uncached, hw)
	if err != nil {
		t.Fatal(err)
	}
	ring2, err := NewRing(pool2, RingConfig{Count: 2, DescSize: 16, BufSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	slot2, err := ring2.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := ring2.ReleaseAcquired(slot2); err != nil {
		t.Fatal(err)
	}
	liveBefore := pool2.Stats().LiveBuffers
	slot2.Buf.Poison(errors.New("slot-only fault"))
	if err := ring2.Close(); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("Close with poisoned slot buf: got %v", err)
	}
	if pool2.Stats().LiveBuffers != liveBefore {
		t.Fatalf("Close partially freed buffers: before=%d after=%d", liveBefore, pool2.Stats().LiveBuffers)
	}
}
