package remotemedia

import (
	"context"
	"testing"
	"time"
)

func TestFrameQueueCapacityOneKeepsNewestFrame(t *testing.T) {
	queue := NewFrameQueue(1)
	if result := queue.Offer(CapturedFrame{Sequence: 1}); result != OfferAccepted {
		t.Fatalf("first offer = %v", result)
	}
	if result := queue.Offer(CapturedFrame{Sequence: 2}); result != OfferReplaced {
		t.Fatalf("second offer = %v, want replacement", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	frame, ok := queue.Pop(ctx)
	if !ok || frame.Sequence != 2 {
		t.Fatalf("pop = %#v, %v; want sequence 2", frame, ok)
	}
	if queue.Drops() != 1 || queue.HighWaterMark() != 1 {
		t.Fatalf("drops/high-water = %d/%d", queue.Drops(), queue.HighWaterMark())
	}
}

func TestFrameQueueCloseUnblocksPop(t *testing.T) {
	queue := NewFrameQueue(1)
	queue.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok := queue.Pop(ctx); ok {
		t.Fatal("closed queue returned a frame")
	}
	if result := queue.Offer(CapturedFrame{Sequence: 1}); result != OfferClosed {
		t.Fatalf("offer after close = %v", result)
	}
}

func TestFrameQueuesTreatNilContextAsBackground(t *testing.T) {
	frameQueue := NewFrameQueue(1)
	if result := frameQueue.Offer(CapturedFrame{Sequence: 1}); result != OfferAccepted {
		t.Fatalf("frame queue offer = %v", result)
	}
	if frame, ok := frameQueue.Pop(nil); !ok || frame.Sequence != 1 {
		t.Fatalf("frame queue pop = %#v, %v", frame, ok)
	}

	encodedQueue := NewEncodedFrameQueue()
	if result := encodedQueue.Offer(EncodedFrame{Width: 640}); result != OfferAccepted {
		t.Fatalf("encoded queue offer = %v", result)
	}
	if frame, ok := encodedQueue.Pop(nil); !ok || frame.Width != 640 {
		t.Fatalf("encoded queue pop = %#v, %v", frame, ok)
	}
}
