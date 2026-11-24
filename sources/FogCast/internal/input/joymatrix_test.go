package input

import (
	"context"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/internal/localhid"
	"testing"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalJoystickMatrixAndSourceMerge(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, value uint64) error { matrix = value; return nil })
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
	if matrix != zx81keys.Neutral {
		t.Fatalf("disconnected held button reasserted: %x", matrix)
	}
	press(remoteinput.ButtonDPadDown)
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('X'): true}) {
		t.Fatalf("unrelated fresh button unavailable: %x", matrix)
	}
}

func TestAmbiguousMatrixPressFailureReleasesOnNextFrame(t *testing.T) {
	keys := NewKeyboardSink()
	var posts []uint64
	runtime := zx81keys.Neutral
	fail := true
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, value uint64) error {
		// A timed-out set_keyboard may already hold this matrix.
		runtime = value
		posts = append(posts, value)
		if fail {
			return errors.New("timeout")
		}
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	press := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	release := gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)
	held := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true})
	if err := sink.apply(sourceLocal, press); err == nil {
		t.Fatal("ambiguous press failure hidden")
	}
	if !sink.localMatrixUnconfirmed || len(sink.localKeys) != 0 || len(posts) != 1 || posts[0] != held || runtime != held {
		t.Fatalf("failed press: unconfirmed=%v local=%v posts=%x runtime=%x", sink.localMatrixUnconfirmed, sink.localKeys, posts, runtime)
	}
	fail = false
	if err := sink.apply(sourceLocal, release); err != nil {
		t.Fatal(err)
	}
	if sink.localMatrixUnconfirmed || len(posts) != 2 || posts[1] != zx81keys.Neutral || runtime != zx81keys.Neutral {
		t.Fatalf("release after ambiguous press: unconfirmed=%v posts=%x runtime=%x", sink.localMatrixUnconfirmed, posts, runtime)
	}
}

func TestLocalJoystickMatrixAxisAndOtherCores(t *testing.T) {
	for _, core := range []string{"fes.coleco", "fes.sg1000"} {
		keys := NewKeyboardSink()
		var matrix uint64
		keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, value uint64) error { matrix = value; return nil })
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
	keys.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error { keyboardPosts++; return nil })
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
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, value uint64) error { matrix = value; return nil })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true}) {
		t.Fatalf("press: %x", matrix)
	}
	// The runtime retires the old matrix during replacement. Retain physical
	// contributors for release bookkeeping without posting into ZX81.
	matrix = zx81keys.Neutral
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.zx81"})
	if matrix != zx81keys.Matrix(nil) || !sink.localKeys[zx81keys.Letter('V')] {
		t.Fatalf("core change posted input or lost physical hold: %x %v", matrix, sink.localKeys)
	}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonDPadUp, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.KeyShift: true}) {
		t.Fatalf("stale A after core change: %x", matrix)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.KeyShift: true, zx81keys.Letter('V'): true}) {
		t.Fatalf("fresh A after physical release: %x", matrix)
	}
}

func TestAmbiguousJoystickNeutralPreservesRemoteAndHeldAliases(t *testing.T) {
	keys, binding := matrixFixture(t)
	var posts []uint64
	var witnesses []localhid.MatrixPost
	var fail int
	keys.SetPoster(func(_ context.Context, post localhid.MatrixPost, matrix uint64) error {
		posts = append(posts, matrix)
		witnesses = append(witnesses, post)
		if fail > 0 {
			fail--
			return errors.New("ambiguous")
		}
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation})
	if err := sink.apply(sourceRemote, keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	fail = 2 // ambiguous local press and first exact neutral
	press0 := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	press1 := gamepad(1, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := sink.apply(sourceLocal, press0); err == nil {
		t.Fatal("ambiguous press accepted")
	}
	if err := sink.apply(sourceLocal, press1); err == nil {
		t.Fatal("failed neutral accepted")
	}
	if err := sink.apply(sourceLocal, press1); err != nil {
		t.Fatal(err)
	}
	remoteOnly := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true})
	if got := posts[len(posts)-1]; got != remoteOnly {
		t.Fatalf("neutral replayed local or lost remote: %#x", got)
	}
	if witnesses[len(witnesses)-1].NeutralOf == nil {
		t.Fatal("neutral lacked failed-operation witness")
	}
	before := len(posts)
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, press1); err != nil {
		t.Fatal(err)
	}
	if len(posts) != before {
		t.Fatal("remaining held alias reasserted")
	}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, press0); err != nil {
		t.Fatal(err)
	}
	want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true, zx81keys.Letter('V'): true})
	if got := posts[len(posts)-1]; got != want {
		t.Fatalf("fresh press not rearmed: %#x", got)
	}
}

func TestJoystickHeldContributorsRequireReleaseAcrossCoreChange(t *testing.T) {
	for _, nextGeneration := range []uint64{1, 2} {
		t.Run(fmt.Sprintf("generation-%d", nextGeneration), func(t *testing.T) {
			keys, binding := matrixFixture(t)
			var matrix uint64
			keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
			sink := &controllerPortsSink{keys: keys}
			obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: 1}
			if err := sink.setObservationContext(context.Background(), obs); err != nil {
				t.Fatal(err)
			}
			event := func(player uint8, code remoteinput.Code, action remoteinput.Action, value int32) {
				t.Helper()
				if err := sink.apply(sourceLocal, gamepad(player, code, action, value)); err != nil {
					t.Fatal(err)
				}
			}
			event(0, remoteinput.ButtonDPadRight, remoteinput.ActionPress, 0)
			event(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 10000)
			event(1, remoteinput.ButtonDPadRight, remoteinput.ActionPress, 0)
			if err := sink.ReleaseAll(); err != nil {
				t.Fatal(err)
			}
			obs.Generation = nextGeneration
			if nextGeneration == 2 {
				obs.CoreID = "fes.coleco"
			}
			if err := sink.setObservationContext(context.Background(), obs); err != nil {
				t.Fatal(err)
			}
			event(0, remoteinput.ButtonDPadRight, remoteinput.ActionPress, 0)
			if matrix != zx81keys.Neutral {
				t.Fatal("held button reasserted after replacement")
			}
			event(0, remoteinput.ButtonDPadRight, remoteinput.ActionRelease, 0)
			event(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0)
			if matrix != zx81keys.Neutral {
				t.Fatal("another player's alias reasserted")
			}
			event(1, remoteinput.ButtonDPadRight, remoteinput.ActionRelease, 0)
			event(0, remoteinput.ButtonDPadRight, remoteinput.ActionPress, 0)
			if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('Z'): true}) {
				t.Fatalf("fresh press after all releases: %#x", matrix)
			}
		})
	}
}

func TestJoystickDisconnectQuarantinesOnlyHeldMappings(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("held-%v", held), func(t *testing.T) {
			keys := NewKeyboardSink()
			var matrix uint64
			keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
			sink := &controllerPortsSink{keys: keys}
			sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
			press := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
			release := gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)
			if held {
				if err := sink.apply(sourceLocal, press); err != nil {
					t.Fatal(err)
				}
			}
			if err := sink.releaseSource(sourceLocal); err != nil {
				t.Fatal(err)
			}
			if err := sink.apply(sourceLocal, release); err != nil {
				t.Fatal(err)
			}
			if err := sink.apply(sourceLocal, press); err != nil {
				t.Fatal(err)
			}
			if held && matrix != zx81keys.Neutral {
				t.Fatal("unrelated reader release cleared held-source quarantine")
			}
			if !held && matrix == zx81keys.Neutral {
				t.Fatal("no-held disconnect unnecessarily quarantined input")
			}
		})
	}
}

func TestJoystickReleaseOnCoreWithoutMappingRearmsOriginalCore(t *testing.T) {
	keys, binding := matrixFixture(t)
	var matrix uint64
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
	sink := &controllerPortsSink{keys: keys}
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.coleco", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: 1}
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	press := gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0)
	release := gamepad(0, remoteinput.ButtonB, remoteinput.ActionRelease, 0)
	if err := sink.apply(sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	obs.CoreID, obs.Generation = "fes.sms", 2
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, release); err != nil {
		t.Fatal(err)
	}
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	obs.CoreID, obs.Generation = "fes.coleco", 3
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('Q'): true}); matrix != want {
		t.Fatalf("released mapping did not rearm: %#x", matrix)
	}
}

func TestAmbiguousJoystickDoesNotNeutralizeOtherSource(t *testing.T) {
	keys, binding := matrixFixture(t)
	var posts int
	keys.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error { posts++; return errors.New("ambiguous") })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation})
	if err := sink.apply(sourceRemote, keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err == nil {
		t.Fatal("ambiguous remote post accepted")
	}
	sink.localMatrixUnconfirmed = true
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err == nil {
		t.Fatal("different source uncertainty cleared")
	}
	if posts != 1 {
		t.Fatalf("unexpected post through foreign uncertainty: %d", posts)
	}
}

func TestJoystickPreDispatchFailureRetriesWithoutAmbiguousNeutral(t *testing.T) {
	keys, binding := matrixFixture(t)
	var posts []uint64
	var witnesses []localhid.MatrixPost
	keys.SetPoster(func(_ context.Context, post localhid.MatrixPost, v uint64) error {
		posts = append(posts, v)
		witnesses = append(witnesses, post)
		if len(posts) == 1 {
			return localhid.ErrMatrixNotDispatched
		}
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation})
	press := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := sink.apply(sourceLocal, press); err == nil {
		t.Fatal("rejection hidden")
	}
	if err := sink.apply(sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || witnesses[1].NeutralOf != nil || posts[1] != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true}) {
		t.Fatalf("pre-dispatch retry = %#v", posts)
	}
}

func TestJoystickDepartureDoesNotClearAmbiguousSamePlayerContributors(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	apply := func(frame protocol.InputFrame) {
		t.Helper()
		if err := sink.apply(sourceLocal, frame); err != nil {
			t.Fatal(err)
		}
	}
	// The raw route cannot distinguish these same-player pad and shaped-keyboard contributors.
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	apply(gamepad(0, remoteinput.ButtonDPadUp, remoteinput.ActionPress, 0))
	apply(localPlayerDeparture(0))
	if matrix != zx81keys.Neutral {
		t.Fatalf("ambiguous departure did not neutralize: %#x", matrix)
	}
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0))
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	if matrix != zx81keys.Neutral {
		t.Fatal("ambiguous release rearmed an orphaned mapping")
	}
	apply(gamepad(0, remoteinput.ButtonDPadDown, remoteinput.ActionPress, 0))
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('X'): true}) {
		t.Fatalf("unrelated control unavailable: %#x", matrix)
	}
}

func TestJoystickDepartureAfterReleasesDoesNotQuarantine(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	for _, frame := range []protocol.InputFrame{
		gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0),
		gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0),
		localPlayerDeparture(0),
		gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0),
	} {
		if err := sink.apply(sourceLocal, frame); err != nil {
			t.Fatal(err)
		}
	}
	if matrix != zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('V'): true}) {
		t.Fatalf("confirmed departure quarantined fresh input: %#x", matrix)
	}
}

func TestJoystickDeparturePreservesOtherPlayerDistinctHold(t *testing.T) {
	keys := NewKeyboardSink()
	var matrix uint64
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error { matrix = v; return nil })
	sink := &controllerPortsSink{keys: keys}
	sink.setObservation(CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms"})
	apply := func(frame protocol.InputFrame) {
		t.Helper()
		if err := sink.apply(sourceLocal, frame); err != nil {
			t.Fatal(err)
		}
	}
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	apply(gamepad(1, remoteinput.ButtonDPadDown, remoteinput.ActionPress, 0))
	apply(localPlayerDeparture(0))
	want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('X'): true})
	if matrix != want {
		t.Fatalf("departure dropped other player: %#x", matrix)
	}
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0))
	apply(gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	if matrix != want {
		t.Fatalf("orphaned mapping resumed: %#x", matrix)
	}
	apply(gamepad(1, remoteinput.ButtonDPadDown, remoteinput.ActionRelease, 0))
	if matrix != zx81keys.Neutral {
		t.Fatalf("other player release lost: %#x", matrix)
	}
}

func TestDepartureRetryDoesNotCrossCoreReplacement(t *testing.T) {
	keys, binding := matrixFixture(t)
	var count int
	fail := false
	keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, _ uint64) error {
		count++
		if fail {
			return localhid.ErrMatrixNotDispatched
		}
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation}
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err == nil {
		t.Fatal("departure failure hidden")
	}
	before := count
	fail = false
	obs.Generation++
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err == nil {
		t.Fatal("old departure admitted to replacement")
	}
	if count != before {
		t.Fatal("old departure posted to replacement")
	}
}

func TestDepartureRetryCancellationDoesNotDispatch(t *testing.T) {
	c := &TargetController{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.retryLocalDeparture(ctx, localPlayerDeparture(0), nil) {
		t.Fatal("canceled retry confirmed")
	}
}

func TestDepartureNeutralRetryPreservesOtherContributorsWithoutNewInput(t *testing.T) {
	keys, binding := matrixFixture(t)
	var writes []struct {
		post   localhid.MatrixPost
		matrix uint64
	}
	failNext := false
	keys.SetPoster(func(_ context.Context, post localhid.MatrixPost, matrix uint64) error {
		writes = append(writes, struct {
			post   localhid.MatrixPost
			matrix uint64
		}{post, matrix})
		if failNext {
			failNext = false
			return errors.New("ambiguous departure")
		}
		return nil
	})
	sink := &controllerPortsSink{keys: keys}
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation}
	if err := sink.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonDPadDown, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	failNext = true
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err == nil {
		t.Fatal("ambiguous departure succeeded")
	}
	failed := writes[len(writes)-1].post.Witness
	// Retry the same neutral decision; no further physical input is delivered.
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err != nil {
		t.Fatal(err)
	}
	last := writes[len(writes)-1]
	if last.post.NeutralOf == nil || *last.post.NeutralOf != failed {
		t.Fatal("neutral did not retain the exact failed witness")
	}
	want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true, zx81keys.Letter('X'): true})
	if last.matrix != want {
		t.Fatalf("neutral lost unaffected contributors or replayed departed input: %#x", last.matrix)
	}
}
