package input

import (
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestKeyboardSinkPostsJMatrix(t *testing.T) {
	var got uint64
	sink := NewKeyboardSink()
	sink.SetPoster(func(matrix uint64) error {
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
	sink.SetPoster(func(uint64) error {
		return errors.New("FES computer is not active")
	})
	if err := sink.ReleaseAll(); err != nil {
		t.Fatalf("ReleaseAll: %v", err)
	}
}
