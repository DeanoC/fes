package input

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestDisplayFocusRequiresReleaseAndFreshPressAcrossSources(t *testing.T) {
	keys := NewKeyboardSink()
	posts := 0
	var matrix uint64
	keys.SetPoster(func(_ context.Context, value uint64) error { posts++; matrix = value; return nil })
	frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey),
		Action: uint8(remoteinput.ActionPress), Code: uint16(zx81keys.Letter('J'))}
	apply := func(source inputSource) {
		t.Helper()
		if err := keys.ApplyFrom(context.Background(), source, frame); err != nil {
			t.Fatal(err)
		}
	}
	apply(sourceRemote)
	keys.setDisplayFocus(true)
	apply(sourceRemote) // held repeat
	apply(sourceLocal)  // new UI key
	if posts != 1 {
		t.Fatalf("UI keys reached machine: %d posts", posts)
	}
	keys.setDisplayFocus(false)
	apply(sourceRemote)
	apply(sourceLocal)
	if posts != 1 {
		t.Fatal("held UI keys replayed on close")
	}
	frame.Action = uint8(remoteinput.ActionRelease)
	apply(sourceRemote)
	if matrix != zx81keys.Neutral {
		t.Fatal("release restored a held UI key")
	}
	frame.Action = uint8(remoteinput.ActionPress)
	apply(sourceRemote)
	if matrix == zx81keys.Neutral {
		t.Fatal("fresh press did not reach machine")
	}
	before := posts
	apply(sourceLocal)
	if posts != before {
		t.Fatal("another source's blocked key resumed")
	}
	_ = keys.ReleaseAll()
	apply(sourceLocal)
	if matrix == zx81keys.Neutral {
		t.Fatal("replacement retained stale focus")
	}
}

func TestSessionDisplayFenceKeepsLifecycleAndSuppressesNavigation(t *testing.T) {
	keys := NewKeyboardSink()
	pads := &recordingSink{}
	ports := &controllerPortsSink{keys: keys, fallback: muxSink{keys: keys, pads: pads}}
	c := &TargetController{ports: ports, keyboard: keys}
	finish, err := c.BeginSessionDisplay(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.lifecycle.TryLock() {
		c.lifecycle.Unlock()
		t.Fatal("physical transition did not fence local input")
	}
	frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := ports.Apply(frame); err != nil || len(pads.frames) != 0 {
		t.Fatal("UI navigation reached machine")
	}
	finish(true) // unconfirmed close retains focus
	if !c.lifecycle.TryLock() {
		t.Fatal("completed transition retained lifecycle lock")
	}
	c.lifecycle.Unlock()
	if err := ports.Apply(frame); err != nil || len(pads.frames) != 0 {
		t.Fatal("failed close restored machine input")
	}
	finish, err = c.BeginSessionDisplay(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if err := ports.Apply(frame); err != nil || len(pads.frames) != 1 {
		t.Fatal("confirmed close did not restore delivery")
	}
}

func TestSourceDisconnectRetiresOnlyItsSuppressedKeys(t *testing.T) {
	keys := NewKeyboardSink()
	posts := 0
	keys.SetPoster(func(context.Context, uint64) error { posts++; return nil })
	frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(remoteinput.ActionPress), Code: uint16(zx81keys.Letter('J'))}
	keys.setDisplayFocus(true)
	_ = keys.ApplyFrom(context.Background(), sourceLocal, frame)
	_ = keys.ApplyFrom(context.Background(), sourceRemote, frame)
	keys.setDisplayFocus(false)
	_ = keys.ReleaseSource(sourceLocal)
	before := posts
	_ = keys.ApplyFrom(context.Background(), sourceLocal, frame)
	if posts != before+1 {
		t.Fatal("disconnected source retained suppressed key")
	}
	_ = keys.ApplyFrom(context.Background(), sourceRemote, frame)
	if posts != before+1 {
		t.Fatal("disconnect unsuppressed another source")
	}
}

func TestControllerPublicationCannotReleaseDisplayFocus(t *testing.T) {
	ports := &controllerPortsSink{keys: NewKeyboardSink(), binding: &ControllerBinding{PackageID: "package", Generation: 1},
		poster: func(context.Context, string, uint64, uint8, uint8, uint16) error { return nil }}
	ports.setDisplayFocus(true)
	if err := ports.releaseSource(sourceRemote); err != nil {
		t.Fatal(err)
	}
	if !ports.displayFocused {
		t.Fatal("source cleanup released display focus")
	}
	if err := ports.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if ports.displayFocused {
		t.Fatal("replacement retained display focus")
	}
}
