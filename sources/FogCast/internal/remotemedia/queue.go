package remotemedia

import (
	"context"
	"sync"
)

type CapturedFrame struct {
	Sequence      uint64
	CaptureMonoNS int64
	Data          []byte
}

type OfferResult uint8

const (
	OfferAccepted OfferResult = iota
	OfferReplaced
	OfferClosed
)

func (r OfferResult) String() string {
	switch r {
	case OfferAccepted:
		return "accepted"
	case OfferReplaced:
		return "replaced"
	case OfferClosed:
		return "closed"
	default:
		return "unknown"
	}
}

type FrameQueue struct {
	mu        sync.Mutex
	capacity  int
	items     []CapturedFrame
	closed    bool
	drops     uint64
	highWater int
	notify    chan struct{}
}

func NewFrameQueue(capacity int) *FrameQueue {
	if capacity < 1 {
		capacity = 1
	}
	return &FrameQueue{capacity: capacity, notify: make(chan struct{}, 1)}
}

func (q *FrameQueue) Offer(frame CapturedFrame) OfferResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return OfferClosed
	}
	result := OfferAccepted
	if len(q.items) >= q.capacity {
		copy(q.items, q.items[1:])
		q.items = q.items[:len(q.items)-1]
		q.drops++
		result = OfferReplaced
	}
	q.items = append(q.items, frame)
	if len(q.items) > q.highWater {
		q.highWater = len(q.items)
	}
	q.signal()
	return result
}

func (q *FrameQueue) Pop(ctx context.Context) (CapturedFrame, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			frame := q.items[0]
			copy(q.items, q.items[1:])
			q.items = q.items[:len(q.items)-1]
			q.mu.Unlock()
			return frame, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return CapturedFrame{}, false
		}
		select {
		case <-ctx.Done():
			return CapturedFrame{}, false
		case <-q.notify:
		}
	}
}

func (q *FrameQueue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.signal()
	}
	q.mu.Unlock()
}

func (q *FrameQueue) Drops() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drops
}

func (q *FrameQueue) HighWaterMark() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.highWater
}

func (q *FrameQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *FrameQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

type EncodedFrame struct {
	CaptureMonoNS    int64
	EncodeDurationNS int64
	AVCC             []byte
	SPS              []byte
	PPS              []byte
	NALLengthSize    int
	Keyframe         bool
	Width            int
	Height           int
}

type EncodedFrameQueue struct {
	mu        sync.Mutex
	frame     EncodedFrame
	hasFrame  bool
	closed    bool
	drops     uint64
	highWater int
	notify    chan struct{}
}

func NewEncodedFrameQueue() *EncodedFrameQueue {
	return &EncodedFrameQueue{notify: make(chan struct{}, 1)}
}

func (q *EncodedFrameQueue) Offer(frame EncodedFrame) OfferResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return OfferClosed
	}
	result := OfferAccepted
	if q.hasFrame {
		q.drops++
		result = OfferReplaced
	}
	q.frame = frame
	q.hasFrame = true
	q.highWater = 1
	q.signal()
	return result
}

func (q *EncodedFrameQueue) Pop(ctx context.Context) (EncodedFrame, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.hasFrame {
			frame := q.frame
			q.frame = EncodedFrame{}
			q.hasFrame = false
			q.mu.Unlock()
			return frame, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return EncodedFrame{}, false
		}
		select {
		case <-ctx.Done():
			return EncodedFrame{}, false
		case <-q.notify:
		}
	}
}

func (q *EncodedFrameQueue) Close() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.signal()
	}
	q.mu.Unlock()
}

func (q *EncodedFrameQueue) Drops() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.drops
}

func (q *EncodedFrameQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.hasFrame {
		return 1
	}
	return 0
}

func (q *EncodedFrameQueue) HighWaterMark() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.highWater
}

func (q *EncodedFrameQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
