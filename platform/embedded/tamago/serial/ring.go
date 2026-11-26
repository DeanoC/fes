// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import "sync/atomic"

const (
	ringSize = 256
	ringMask = ringSize - 1
)

// Ring is a fixed-size single-producer, single-consumer byte ring buffer
// suitable for draining a UART RX FIFO from a GIC handler without heap
// allocations or locks.
type Ring struct {
	buf     [ringSize]byte
	head    atomic.Uint32 // producer write index (monotonic)
	tail    atomic.Uint32 // consumer read index (monotonic)
	dropped atomic.Uint32
}

// Push appends b to the ring. It returns false and increments Dropped when full.
func (r *Ring) Push(b byte) bool {
	head := r.head.Load()
	if head-r.tail.Load() >= ringSize {
		r.dropped.Add(1)
		return false
	}
	r.buf[head&ringMask] = b
	r.head.Store(head + 1)
	return true
}

// PushSlice appends as many bytes from src as fit into the ring with a single
// atomic index update, recording overflow in Dropped.
func (r *Ring) PushSlice(src []byte) (pushed int, dropped uint32) {
	if r == nil || len(src) == 0 {
		return 0, 0
	}
	head := r.head.Load()
	avail := ringSize - int(head-r.tail.Load())
	if avail < 0 {
		avail = 0
	}
	pushed = len(src)
	if pushed > avail {
		pushed = avail
		dropped = uint32(len(src) - avail)
	}
	if pushed > 0 {
		firstIdx := int(head & ringMask)
		first := min(pushed, ringSize-firstIdx)
		copy(r.buf[firstIdx:firstIdx+first], src[:first])
		if first < pushed {
			copy(r.buf[:pushed-first], src[first:pushed])
		}
		r.head.Store(head + uint32(pushed))
	}
	if dropped > 0 {
		r.dropped.Add(dropped)
	}
	return pushed, dropped
}

// PopSlice copies up to len(dst) unread bytes into dst with a single atomic
// index update and returns the count copied.
func (r *Ring) PopSlice(dst []byte) int {
	if r == nil || len(dst) == 0 {
		return 0
	}
	tail := r.tail.Load()
	unread := int(r.head.Load() - tail)
	if unread <= 0 {
		return 0
	}
	n := min(len(dst), unread)
	firstIdx := int(tail & ringMask)
	first := min(n, ringSize-firstIdx)
	copy(dst[:first], r.buf[firstIdx:firstIdx+first])
	if first < n {
		copy(dst[first:n], r.buf[:n-first])
	}
	r.tail.Store(tail + uint32(n))
	return n
}

// Pop removes and returns the oldest buffered byte.
func (r *Ring) Pop() (byte, bool) {
	tail := r.tail.Load()
	if r.head.Load() == tail {
		return 0, false
	}
	b := r.buf[tail&ringMask]
	r.tail.Store(tail + 1)
	return b, true
}

// Peek returns the oldest buffered byte without advancing the consumer tail.
func (r *Ring) Peek() (byte, bool) {
	tail := r.tail.Load()
	if r.head.Load() == tail {
		return 0, false
	}
	return r.buf[tail&ringMask], true
}

// Cap returns the fixed capacity of the ring buffer in bytes.
func (r *Ring) Cap() int {
	return ringSize
}

// Len returns the number of unread bytes currently buffered.
func (r *Ring) Len() int {
	return int(r.head.Load() - r.tail.Load())
}

// Dropped returns the number of bytes dropped because the ring was full.
func (r *Ring) Dropped() uint32 {
	return r.dropped.Load()
}

// Reset discards buffered bytes and zeroes the drop counter.
// Call only when no producer or consumer is accessing the ring concurrently.
func (r *Ring) Reset() {
	r.head.Store(0)
	r.tail.Store(0)
	r.dropped.Store(0)
}
