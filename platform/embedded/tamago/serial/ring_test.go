// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import (
	"sync"
	"testing"
)

func TestRingBasicAndWraparound(t *testing.T) {
	var r Ring
	if b, ok := r.Pop(); ok || b != 0 || r.Len() != 0 {
		t.Fatalf("empty Pop = (%d, %v), len=%d", b, ok, r.Len())
	}

	// Fill to capacity and verify overflow drops are counted without overwriting.
	for i := 0; i < ringSize; i++ {
		if !r.Push(byte(i)) {
			t.Fatalf("Push(%d) failed before capacity", i)
		}
	}
	if r.Len() != ringSize {
		t.Fatalf("Len = %d, want %d", r.Len(), ringSize)
	}
	if r.Cap() != ringSize {
		t.Fatalf("Cap = %d, want %d", r.Cap(), ringSize)
	}
	if b, ok := r.Peek(); !ok || b != 0 {
		t.Fatalf("Peek = (%d, %v), want (0, true)", b, ok)
	}
	if r.Push(0xff) || r.Push(0xfe) {
		t.Fatal("Push succeeded on full ring")
	}
	if got := r.Dropped(); got != 2 {
		t.Fatalf("Dropped = %d, want 2", got)
	}

	// Drain half and push again to exercise wraparound across ringMask.
	for i := 0; i < ringSize/2; i++ {
		b, ok := r.Pop()
		if !ok || b != byte(i) {
			t.Fatalf("Pop #%d = (%d, %v), want (%d, true)", i, b, ok, byte(i))
		}
	}
	for i := 0; i < ringSize/2; i++ {
		if !r.Push(byte(0x80 ^ i)) {
			t.Fatalf("wrapped Push(%d) failed", i)
		}
	}
	for i := ringSize / 2; i < ringSize; i++ {
		b, ok := r.Pop()
		if !ok || b != byte(i) {
			t.Fatalf("Pop second half #%d = (%d, %v), want (%d, true)", i, b, ok, byte(i))
		}
	}
	for i := 0; i < ringSize/2; i++ {
		b, ok := r.Pop()
		if !ok || b != byte(0x80^i) {
			t.Fatalf("Pop wrapped #%d = (%d, %v), want (%d, true)", i, b, ok, byte(0x80^i))
		}
	}
	if _, ok := r.Pop(); ok || r.Len() != 0 {
		t.Fatalf("ring not empty after full drain: len=%d", r.Len())
	}

	r.Push(0x42)
	r.Reset()
	if r.Len() != 0 || r.Dropped() != 0 {
		t.Fatalf("after Reset: len=%d dropped=%d", r.Len(), r.Dropped())
	}
}

func TestRingUint32Wrap(t *testing.T) {
	var r Ring
	r.head.Store(^uint32(0) - 3)
	r.tail.Store(^uint32(0) - 3)
	for i := 0; i < 16; i++ {
		if !r.Push(byte(i + 1)) {
			t.Fatalf("Push(%d) across uint32 wrap failed", i)
		}
	}
	if r.Len() != 16 {
		t.Fatalf("Len across uint32 wrap = %d, want 16", r.Len())
	}
	for i := 0; i < 16; i++ {
		b, ok := r.Pop()
		if !ok || b != byte(i+1) {
			t.Fatalf("Pop(%d) across uint32 wrap = (%d, %v)", i, b, ok)
		}
	}
}

func TestRingConcurrentSPSC(t *testing.T) {
	var r Ring
	const total = 10000
	var retries uint32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total; i++ {
			for !r.Push(byte(i)) {
				retries++
			}
		}
	}()
	for i := 0; i < total; i++ {
		var b byte
		var ok bool
		for {
			b, ok = r.Pop()
			if ok {
				break
			}
		}
		if b != byte(i) {
			t.Fatalf("item %d = %d, want %d", i, b, byte(i))
		}
	}
	wg.Wait()
	if r.Len() != 0 || r.Dropped() != retries {
		t.Fatalf("after SPSC: len=%d dropped=%d want=%d", r.Len(), r.Dropped(), retries)
	}
}
