package input

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"testing"
)

type mouseWrite struct {
	x, y int16
	b    uint8
	g    uint64
}

func TestMouseSourcesAndUncertainMotion(t *testing.T) {
	var writes []mouseWrite
	fail := false
	s := &mouseSink{poster: func(_ context.Context, id string, g uint64, x, y int16, b uint8) error {
		writes = append(writes, mouseWrite{x, y, b, g})
		if fail {
			return errors.New("lost ACK")
		}
		return nil
	}}
	s.bindContext(context.Background(), &MouseBinding{"pkg", 7})
	frame := func(x, y int16, b uint8) protocol.InputFrame {
		e := remoteinput.MouseEvent(x, y, b)
		return protocol.InputFrame{Device: uint8(e.Device), Kind: uint8(e.Kind), Action: uint8(e.Action), Code: uint16(e.Code), Value: e.Value}
	}
	if err := s.applyFrom(context.Background(), sourceRemote, frame(5, -2, 1)); err != nil {
		t.Fatal(err)
	}
	s.applyFrom(context.Background(), sourceLocal, frame(0, 0, 2))
	if writes[1].b != 3 {
		t.Fatal(writes)
	}
	s.releaseSource(sourceRemote)
	if writes[2].b != 2 || writes[2].x != 0 {
		t.Fatal(writes)
	}
	fail = true
	if err := s.applyFrom(context.Background(), sourceLocal, frame(9, 3, 2)); err == nil {
		t.Fatal("uncertain accepted")
	}
	fail = false
	s.releaseSource(sourceLocal)
	if len(writes) != 5 || writes[4].x != 0 || writes[4].y != 0 || writes[4].b != 0 {
		t.Fatalf("motion replayed %+v", writes)
	}
	s.bindContext(context.Background(), &MouseBinding{"pkg", 8})
	s.applyFrom(context.Background(), sourceRemote, frame(0, 0, 0))
	if len(writes) != 5 {
		t.Fatal("old state replayed")
	}
	s.bindContext(context.Background(), nil)
	s.applyFrom(context.Background(), sourceRemote, frame(1, 1, 1))
	if len(writes) != 5 {
		t.Fatal("inactive core received mouse")
	}
}

func TestMouseDisplayNeutralKeepsGeneration(t *testing.T) {
	var writes []mouseWrite
	s := &mouseSink{binding: &MouseBinding{"pkg", 7}, buttons: [sourceCount]uint8{1, 2}, poster: func(_ context.Context, _ string, g uint64, x, y int16, b uint8) error {
		writes = append(writes, mouseWrite{x, y, b, g})
		return nil
	}}
	if err := s.neutral(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.binding == nil || len(writes) != 1 || writes[0].b != 0 || writes[0].g != 7 {
		t.Fatal(writes)
	}
}
