package input

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestKeyboardSinkPostsJMatrix(t *testing.T) {
	var got uint64
	sink := NewKeyboardSink()
	sink.SetPoster(func(_ context.Context, matrix uint64) error {
		got = matrix
		return nil
	})
	frame := protocol.InputFrame{
		Device: uint8(remoteinput.DeviceKeyboard),
		Kind:   uint8(remoteinput.KindKey),
		Action: uint8(remoteinput.ActionPress),
		Code:   uint16(zx81keys.Letter('J')),
	}
	if err := sink.Apply(frame); err != nil {
		t.Fatal(err)
	}
	if got&(1<<(6*5+3)) != 0 {
		t.Fatalf("J not down: %#x", got)
	}
	frame.Action = uint8(remoteinput.ActionRelease)
	if err := sink.Apply(frame); err != nil {
		t.Fatal(err)
	}
	if got != zx81keys.Neutral {
		t.Fatalf("released matrix=%#x", got)
	}
}

func TestKeyboardSinkReleaseAllSucceedsWhenPosterFails(t *testing.T) {
	sink := NewKeyboardSink()
	sink.SetPoster(func(context.Context, uint64) error {
		return errors.New("FES computer is not active")
	})
	if err := sink.ReleaseAll(); err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
}

type recordingSink struct {
	frames []protocol.InputFrame
}

func (s *recordingSink) Apply(f protocol.InputFrame) error {
	s.frames = append(s.frames, f)
	return nil
}
func (s *recordingSink) ReleaseAll() error { return nil }
func (s *recordingSink) Close() error      { return nil }

func TestMuxSinkRoutesZX81ToKeyboardAndGamepadToPads(t *testing.T) {
	t.Parallel()
	var matrix uint64
	keys := NewKeyboardSink()
	keys.SetPoster(func(_ context.Context, m uint64) error {
		matrix = m
		return nil
	})
	pads := &recordingSink{}
	mux := muxSink{keys: keys, pads: pads}

	zx := protocol.InputFrame{
		Device: uint8(remoteinput.DeviceKeyboard),
		Kind:   uint8(remoteinput.KindKey),
		Action: uint8(remoteinput.ActionPress),
		Code:   uint16(zx81keys.Letter('J')),
	}
	if err := mux.Apply(zx); err != nil {
		t.Fatal(err)
	}
	if matrix&(1<<(6*5+3)) != 0 {
		t.Fatalf("ZX81 J missed set_keyboard: %#x", matrix)
	}

	low := zx
	low.Code = uint16(remoteinput.KeyA)
	if err := mux.Apply(low); err != nil {
		t.Fatal(err)
	}
	if len(pads.frames) != 0 {
		t.Fatalf("non-ZX81 keyboard leaked to pads: %#v", pads.frames)
	}

	pad := protocol.InputFrame{
		Device: uint8(remoteinput.DeviceGamepad),
		Kind:   uint8(remoteinput.KindButton),
		Action: uint8(remoteinput.ActionPress),
		Code:   uint16(remoteinput.ButtonA),
	}
	if err := mux.Apply(pad); err != nil {
		t.Fatal(err)
	}
	if len(pads.frames) != 1 || pads.frames[0].Code != uint16(remoteinput.ButtonA) {
		t.Fatalf("native pad = %#v", pads.frames)
	}
}
