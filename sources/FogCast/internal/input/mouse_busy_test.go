package input

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func busyMouseFrame(x, y int16, b uint8) protocol.InputFrame {
	e := remoteinput.MouseEvent(x, y, b)
	return protocol.InputFrame{Device: uint8(e.Device), Kind: uint8(e.Kind), Action: uint8(e.Action), Code: uint16(e.Code), Value: e.Value}
}
func mouseAttempt(t *testing.T, ch <-chan mouseWrite) mouseWrite {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(time.Second):
		t.Fatal("mouse report did not arrive")
		return mouseWrite{}
	}
}
func TestMouseBusyReleaseReconcilesWithoutNewEventOrMotionReplay(t *testing.T) {
	var busy atomic.Bool
	var actualButtons atomic.Uint32
	var actualMotion atomic.Int32
	attempts := make(chan mouseWrite, 32)
	accepted := make(chan mouseWrite, 32)
	s := &mouseSink{poster: func(_ context.Context, _ string, g uint64, x, y int16, b uint8) error {
		w := mouseWrite{x, y, b, g}
		attempts <- w
		if busy.Load() {
			return &protocol.APIError{Code: protocol.CodeBusy}
		}
		actualButtons.Store(uint32(b))
		actualMotion.Add(int32(x))
		accepted <- w
		return nil
	}}
	defer s.releaseAll()
	s.bindContext(context.Background(), &MouseBinding{"pkg", 7})
	if err := s.applyFrom(context.Background(), sourceRemote, busyMouseFrame(5, 0, 1)); err != nil {
		t.Fatal(err)
	}
	mouseAttempt(t, attempts)
	mouseAttempt(t, accepted)
	busy.Store(true)
	if err := s.applyFrom(context.Background(), sourceRemote, busyMouseFrame(17, -3, 0)); !mouseBusy(err) {
		t.Fatal(err)
	}
	if w := mouseAttempt(t, attempts); w.x != 17 || w.b != 0 {
		t.Fatal(w)
	}
	if w := mouseAttempt(t, attempts); w.x != 0 || w.y != 0 || w.b != 0 || w.g != 7 {
		t.Fatal(w)
	}
	busy.Store(false)
	// No further input event follows capture completion.
	if w := mouseAttempt(t, accepted); w.x != 0 || w.y != 0 || w.b != 0 || w.g != 7 {
		t.Fatal(w)
	}
	if actualButtons.Load() != 0 || actualMotion.Load() != 5 {
		t.Fatal("release lost or motion duplicated", actualButtons.Load(), actualMotion.Load())
	}
	for len(attempts) > 0 {
		w := <-attempts
		if w.x != 0 || w.y != 0 {
			t.Fatal("relative motion replayed", w)
		}
	}
}
func TestMouseBusySourceDetachKeepsOtherHoldAndCancelsOldGeneration(t *testing.T) {
	var busy atomic.Bool
	attempts := make(chan mouseWrite, 32)
	accepted := make(chan mouseWrite, 32)
	s := &mouseSink{poster: func(_ context.Context, _ string, g uint64, x, y int16, b uint8) error {
		w := mouseWrite{x, y, b, g}
		attempts <- w
		if busy.Load() {
			return &protocol.APIError{Code: protocol.CodeBusy}
		}
		accepted <- w
		return nil
	}}
	defer s.releaseAll()
	s.bindContext(context.Background(), &MouseBinding{"pkg", 7})
	s.applyFrom(context.Background(), sourceLocal, busyMouseFrame(0, 0, 2))
	s.applyFrom(context.Background(), sourceRemote, busyMouseFrame(0, 0, 1))
	mouseAttempt(t, attempts)
	mouseAttempt(t, attempts)
	mouseAttempt(t, accepted)
	mouseAttempt(t, accepted)
	busy.Store(true)
	if err := s.releaseSource(sourceRemote); !mouseBusy(err) {
		t.Fatal(err)
	}
	if w := mouseAttempt(t, attempts); w.b != 2 {
		t.Fatal(w)
	}
	busy.Store(false)
	if w := mouseAttempt(t, accepted); w.b != 2 || w.x != 0 || w.y != 0 {
		t.Fatal("detached source replayed", w)
	}
	mouseAttempt(t, attempts)
	busy.Store(true)
	s.applyFrom(context.Background(), sourceLocal, busyMouseFrame(9, 3, 0))
	mouseAttempt(t, attempts)
	s.bindContext(context.Background(), &MouseBinding{"pkg", 8})
	busy.Store(false)
	select {
	case w := <-attempts:
		t.Fatal("stale generation reconciled", w)
	case <-time.After(220 * time.Millisecond):
	}
	s.applyFrom(context.Background(), sourceRemote, busyMouseFrame(2, 0, 1))
	if w := mouseAttempt(t, accepted); w.g != 8 || w.x != 2 || w.b != 1 {
		t.Fatal(w)
	}
}
func TestMouseAmbiguousFailureDoesNotScheduleRetryAndUnbindCancelsBusy(t *testing.T) {
	for _, ambiguous := range []bool{true, false} {
		t.Run(map[bool]string{true: "lost-ack", false: "busy-unbind"}[ambiguous], func(t *testing.T) {
			attempts := make(chan mouseWrite, 8)
			s := &mouseSink{poster: func(_ context.Context, _ string, g uint64, x, y int16, b uint8) error {
				attempts <- mouseWrite{x, y, b, g}
				if ambiguous {
					return errors.New("lost ACK")
				}
				return &protocol.APIError{Code: protocol.CodeBusy}
			}}
			s.bindContext(context.Background(), &MouseBinding{"pkg", 7})
			if err := s.applyFrom(context.Background(), sourceRemote, busyMouseFrame(9, 3, 1)); err == nil {
				t.Fatal("failure accepted")
			}
			mouseAttempt(t, attempts)
			if !ambiguous {
				s.bindContext(context.Background(), nil)
			}
			select {
			case w := <-attempts:
				t.Fatal("uncertain/stale report retried", w)
			case <-time.After(220 * time.Millisecond):
			}
			s.bindContext(context.Background(), nil)
		})
	}
}
