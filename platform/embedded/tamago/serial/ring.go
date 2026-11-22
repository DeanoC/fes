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
