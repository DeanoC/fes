package input

import (
	"context"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalJoystickMatrixAndSourceMerge(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, value uint64) error { matrix = value; return nil })
	virtual := &recordingSink{}
	sink := &controllerPortsSink{keys: keys, pads: newPadMerge(virtual)}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	press := func(code remoteinput.Code) {
		t.Helper()
		if err := sink.apply(sourceLocal, gamepad(0, code, remoteinput.ActionPress, 0)); err != nil {
			t.Fatal(err)
		}
	}
	release := func(code remoteinput.Code) {
		t.Helper()
		if err := sink.apply(sourceLocal, gamepad(0, code, remoteinput.ActionRelease, 0)); err != nil {
			t.Fatal(err)
		}
	}
	want := map[remoteinput.Code]bool{}
	for _, step := range []struct{ button, key remoteinput.Code }{
		{remoteinput.ButtonDPadUp, zx81keys.KeyShift},
		{remoteinput.ButtonDPadRight, zx81keys.Letter('Z')},
		{remoteinput.ButtonDPadDown, zx81keys.Letter('X')},
		{remoteinput.ButtonDPadLeft, zx81keys.Letter('C')},
		{remoteinput.ButtonA, zx81keys.Letter('V')},
	} {
		press(step.button)
		want[step.key] = true
		if matrix != zx81keys.Matrix(want) {
			t.Fatalf("press %d: got %x want %x", step.button, matrix, zx81keys.Matrix(want))
		}
	}
	press(remoteinput.ButtonB)
	press(remoteinput.ButtonSelect)
	press(remoteinput.ButtonStart)
	if matrix != zx81keys.Matrix(want) || len(virtual.frames) != 0 {
		t.Fatalf("unmapped buttons or virtual pad: %x %v", matrix, virtual.frames)
	}
	for _, step := range []struct{ button, key remoteinput.Code }{
		{remoteinput.ButtonDPadUp, zx81keys.KeyShift},
		{remoteinput.ButtonDPadRight, zx81keys.Letter('Z')},
		{remoteinput.ButtonDPadDown, zx81keys.Letter('X')},
		{remoteinput.ButtonDPadLeft, zx81keys.Letter('C')},
		{remoteinput.ButtonA, zx81keys.Letter('V')},
	} {
		release(step.button)
		delete(want, step.key)
		if matrix != zx81keys.Matrix(want) {
			t.Fatalf("release %d: %x", step.button, matrix)
		}
	}
	if err := sink.apply(sourceRemote, keyboardFrameFor(zx81keys.KeyShift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	press(remoteinput.ButtonDPadUp)
	release(remoteinput.ButtonDPadUp)
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.KeyShift: true}) {
		t.Fatal("local release cleared remote hold")
	}
	if err := sink.apply(sourceRemote, keyboardFrameFor(zx81keys.KeyShift, remoteinput.ActionRelease)); err != nil {
		t.Fatal(err)
	}
	press(remoteinput.ButtonA)
	if err := sink.releaseSource(sourceLocal); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(nil) {
		t.Fatalf("local socket close left a key held: %x", matrix)
	}
	press(remoteinput.ButtonDPadRight)
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(nil) || len(virtual.frames) != 0 {
		t.Fatalf("release: %x %v", matrix, virtual.frames)
	}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	press(remoteinput.ButtonA)
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true}) {
		t.Fatalf("relaunch: %x", matrix)
	}
}

func TestLocalJoystickMatrixAxisAndOtherCores(t *testing.T) {
	for _, core := range []string{"fes.coleco", "fes.sg1000"} {
		keys := NewKeyboardSink()
		var matrix uint64
		keys.SetPoster(func(_ context.Context, value uint64) error { matrix = value; return nil })
		sink := &controllerPortsSink{keys: keys}
		sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: core})
		for _, value := range []int32{8000, 8001} {
			if err := sink.apply(sourceLocal, gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, value)); err != nil {
				t.Fatal(err)
			}
		}
		if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('Z'): true}) {
			t.Fatalf("%s axis: %x", core, matrix)
		}
		if err := sink.apply(sourceLocal, gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0)); err != nil {
			t.Fatal(err)
		}
		if matrix != zx81keys.Matrix(nil) {
			t.Fatalf("%s axis release: %x", core, matrix)
		}
	}
	for _, obs := range []CoreObservation{{Active: true, Keyboard: true, CoreID: "fes.zx81"}, {Active: true, CoreID: "fes.sms"}} {
		virtual := &recordingSink{}
		sink := &controllerPortsSink{keys: NewKeyboardSink(), pads: newPadMerge(virtual)}
		sink.setObservation(obs)
		if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
			t.Fatal(err)
		}
		if len(virtual.frames) != 1 {
			t.Fatalf("%+v: virtual pad frames %d", obs, len(virtual.frames))
		}
	}
	keys := NewKeyboardSink()
	var keyboardPosts, portPosts int
	keys.SetPoster(func(context.Context, uint64) error { keyboardPosts++; return nil })
	sink := &controllerPortsSink{keys: keys, poster: func(context.Context, string, uint64, uint8, uint8, uint16) error { portPosts++; return nil }}
	if err := sink.bind(&ControllerBinding{PackageID: "package", Generation: 1}); err != nil {
		t.Fatal(err)
	}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", Binding: sink.binding})
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if portPosts != 1 || keyboardPosts != 0 {
		t.Fatalf("controller port path changed: ports=%d keyboard=%d", portPosts, keyboardPosts)
	}
}

func TestLocalJoystickMatrixReleasesOnCoreChange(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, value uint64) error { matrix = value; return nil })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true}) {
		t.Fatalf("press: %x", matrix)
	}
	// The runtime retires the old matrix during replacement. Observation only
	// forgets that contribution; it must not post an old release into ZX81.
	matrix = zx81keys.Neutral
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.zx81"})
	if matrix != zx81keys.Matrix(nil) || len(sink.localKeys) != 0 {
		t.Fatalf("core change kept local matrix keys: %x %v", matrix, sink.localKeys)
	}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonDPadUp, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.KeyShift: true}) {
		t.Fatalf("stale A after core change: %x", matrix)
	}
}
