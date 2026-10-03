package input

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func gamepad(player uint8, code remoteinput.Code, action remoteinput.Action, value int32) protocol.InputFrame {
	kind := remoteinput.KindButton
	if action == remoteinput.ActionAbsolute {
		kind = remoteinput.KindAxis
	}
	return protocol.InputFrame{
		Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1},
		Seq:    1,
		Player: player,
		Device: uint8(remoteinput.DeviceGamepad),
		Kind:   uint8(kind),
		Action: uint8(action),
		Code:   uint16(code),
		Value:  value,
	}
}

func localSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "li")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func keyboardFrameFor(code remoteinput.Code, action remoteinput.Action) protocol.InputFrame {
	return protocol.InputFrame{
		Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1},
		Seq:    1,
		Device: uint8(remoteinput.DeviceKeyboard),
		Kind:   uint8(remoteinput.KindKey),
		Action: uint8(action),
		Code:   uint16(code),
	}
}

func newPortsFixture(t *testing.T, keypad bool) (*controllerPortsSink, *[]portWrite) {
	t.Helper()
	var mu sync.Mutex
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypadMask uint16) error {
		mu.Lock()
		writes = append(writes, portWrite{id, generation, port, buttons, keypadMask})
		mu.Unlock()
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 4, Keypad: keypad}); err != nil {
		t.Fatal(err)
	}
	return sink, &writes
}

func TestMergeAxisPrefersLargerDeflectionAndLocalTie(t *testing.T) {
	if got := mergeAxis(20000, 0); got != 20000 {
		t.Fatalf("centred remote cleared local stick: %d", got)
	}
	if got := mergeAxis(0, -16000); got != -16000 {
		t.Fatalf("centred local hid remote stick: %d", got)
	}
	if got := mergeAxis(-1000, 1000); got != -1000 {
		t.Fatalf("tie should keep the local deflection, got %d", got)
	}
	local := remoteinput.Snapshot{Pressed: []remoteinput.Code{remoteinput.ButtonA}, Axes: map[remoteinput.Code]int16{remoteinput.AxisLeftX: 20000}}
	remote := remoteinput.Snapshot{Axes: map[remoteinput.Code]int16{remoteinput.AxisLeftX: 0}}
	merged := mergeSnapshots(local, remote)
	buttons, _ := controllerSnapshot(merged, false)
	if buttons&16 == 0 || buttons&(1<<3) == 0 {
		t.Fatalf("merged buttons = %d, want A and right", buttons)
	}
}

func TestSamePlayerOrMergeKeepsLocalPress(t *testing.T) {
	sink, writes := newPortsFixture(t, true)
	pressA := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := sink.sources[sourceLocal].Apply(frameEvent(pressA)); err != nil {
		t.Fatal(err)
	}
	if err := sink.sources[sourceRemote].Apply(frameEvent(pressA)); err != nil {
		t.Fatal(err)
	}
	if err := sink.sources[sourceRemote].Apply(frameEvent(gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0))); err != nil {
		t.Fatal(err)
	}
	if err := sink.sources[sourceRemote].Apply(frameEvent(gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0))); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	err := sink.publishPortLocked(context.Background(), 0, true)
	sink.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].buttons&16 == 0 {
		t.Fatalf("remote release and centred stick cleared local A: %+v", *writes)
	}
	if err := sink.sources[sourceLocal].Apply(frameEvent(gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0))); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	err = sink.publishPortLocked(context.Background(), 0, true)
	sink.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	last := (*writes)[len(*writes)-1]
	if last.buttons&128 == 0 || last.keypad&(1<<1) == 0 {
		t.Fatalf("shared-port Start lost keypad 1 alias: %+v", last)
	}
}

func TestLegacyPadMergeSurvivesRemoteRelease(t *testing.T) {
	inner := &recordingSink{}
	pads := newPadMerge(inner)
	sink := &controllerPortsSink{fallback: &recordingSink{}, pads: pads}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0)); err != nil {
		t.Fatal(err)
	}
	releases := 0
	for _, frame := range inner.frames {
		if frame.Code == uint16(remoteinput.ButtonA) && frame.Action == uint8(remoteinput.ActionRelease) {
			releases++
		}
	}
	if releases != 0 || len(inner.frames) != 1 || inner.frames[0].Action != uint8(remoteinput.ActionPress) {
		t.Fatalf("legacy merge dropped the local press: %+v", inner.frames)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if len(inner.frames) != 2 || inner.frames[1].Action != uint8(remoteinput.ActionRelease) {
		t.Fatalf("local release did not reach the pad: %+v", inner.frames)
	}
}

func TestLocalPadsTakeLowPortsAndRemoteUsesTheNextFreePort(t *testing.T) {
	sink, writes := newPortsFixture(t, false)
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 2 || (*writes)[0].port != 0 || (*writes)[0].buttons != 16 || (*writes)[1].port != 1 || (*writes)[1].buttons != 32 {
		t.Fatalf("assignment %+v", *writes)
	}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceRemote, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err == nil {
		t.Fatal("remote pad exceeded Coleco's two ports")
	}
	if err := sink.apply(sourceLocal, gamepad(2, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err == nil {
		t.Fatal("local player 2 accepted")
	}
	if len(*writes) != 3 {
		t.Fatalf("rejected pads wrote state: %+v", *writes)
	}
}

func TestExcludedRemotePlayerReleaseDoesNotReappear(t *testing.T) {
	sink, _ := newPortsFixture(t, true)
	for _, frame := range []protocol.InputFrame{
		gamepad(1, remoteinput.ButtonB, remoteinput.ActionPress, 0),
		gamepad(1, remoteinput.KeypadHash, remoteinput.ActionPress, 0),
		gamepad(1, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 25000),
	} {
		if err := sink.Apply(frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	for _, frame := range []protocol.InputFrame{
		gamepad(1, remoteinput.ButtonB, remoteinput.ActionRelease, 0),
		gamepad(1, remoteinput.KeypadHash, remoteinput.ActionRelease, 0),
		gamepad(1, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0),
	} {
		if err := sink.Apply(frame); err == nil {
			t.Fatal("excluded remote player was accepted")
		}
	}
	if err := sink.Apply(gamepad(1, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err == nil {
		t.Fatal("excluded player's new press was accepted")
	}
	if err := sink.releaseSource(sourceLocal); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	buttons, keypad := controllerSnapshot(sink.mergedSnapshotLocked(1), true)
	sink.mu.Unlock()
	if buttons != 0 || keypad != 0 {
		t.Fatalf("excluded remote releases reappeared as buttons=%d keypad=%d", buttons, keypad)
	}
}

func localPlayerDeparture(player uint8) protocol.InputFrame {
	event := remoteinput.LocalPlayerDeparture(player)
	return protocol.InputFrame{
		Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1},
		Seq:    1,
		Player: event.Player,
		Device: uint8(event.Device),
		Kind:   uint8(event.Kind),
		Action: uint8(event.Action),
		Code:   uint16(event.Code),
		Value:  event.Value,
	}
}

func TestIndividualLocalDepartureReopensOnlyItsPort(t *testing.T) {
	sink, _ := newPortsFixture(t, false)
	for player := uint8(0); player < 2; player++ {
		if err := sink.Apply(gamepad(player, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
			t.Fatal(err)
		}
	}
	for player := uint8(0); player < 2; player++ {
		if err := sink.apply(sourceLocal, gamepad(player, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err != nil {
		t.Fatal(err)
	}
	if sink.localClaim != [2]bool{false, true} {
		t.Fatalf("local claims=%v", sink.localClaim)
	}
	sink.mu.Lock()
	buttons0, _ := controllerSnapshot(sink.mergedSnapshotLocked(0), false)
	buttons1, _ := controllerSnapshot(sink.mergedSnapshotLocked(1), false)
	sink.mu.Unlock()
	if buttons0 != 128 || buttons1 != 16 {
		t.Fatalf("departure lost remote P1 or surviving local P2: %d/%d", buttons0, buttons1)
	}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, localPlayerDeparture(1)); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	buttons1, _ = controllerSnapshot(sink.mergedSnapshotLocked(1), false)
	sink.mu.Unlock()
	if sink.localClaim != [2]bool{} || buttons1 != 128 {
		t.Fatalf("second departure claims=%v buttons=%d", sink.localClaim, buttons1)
	}
}

func TestLocalPadDeparturePreservesShapedKeyboardHold(t *testing.T) {
	sink, _ := newPortsFixture(t, false)
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, keyboardFrameFor(remoteinput.KeyUp, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, localPlayerDeparture(0)); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	buttons, _ := controllerSnapshot(sink.mergedSnapshotLocked(0), false)
	sink.mu.Unlock()
	if buttons != 1 || sink.localClaim[0] {
		t.Fatalf("departure buttons=%d claim=%v, want held keyboard Up and free slot", buttons, sink.localClaim[0])
	}
}

func TestPlayerDepartureRequiresLocalRoute(t *testing.T) {
	fallback := &recordingSink{}
	sink := &controllerPortsSink{fallback: fallback}
	if err := sink.Apply(localPlayerDeparture(0)); err == nil {
		t.Fatal("host departure marker reached unbound fallback")
	}
	if len(fallback.frames) != 0 {
		t.Fatal("host marker was delivered")
	}
	bound, _ := newPortsFixture(t, false)
	if err := bound.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := bound.Apply(localPlayerDeparture(0)); err == nil {
		t.Fatal("host departure marker was accepted")
	}
	if !bound.localClaim[0] {
		t.Fatal("host marker cleared the local claim")
	}
}

func TestLocalIdleObservationIsCached(t *testing.T) {
	sink := &controllerPortsSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	observations := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		observations++
		return CoreObservation{}, nil
	})
	for range 10 {
		if err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); !errors.Is(err, errNoCore) {
			t.Fatalf("idle frame: %v", err)
		}
	}
	if observations != 1 {
		t.Fatalf("idle frames observed runtime %d times, want 1", observations)
	}
}

func expireLocalObservation(sink *controllerPortsSink) {
	sink.mu.Lock()
	sink.observedAt = time.Now().Add(-localCoreObserveTTL - time.Millisecond)
	sink.mu.Unlock()
}

func TestLocalIdleObservationExpiresWhenCoreStarts(t *testing.T) {
	sink, writes := newPortsFixture(t, false)
	if err := sink.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	active := false
	observations := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		observations++
		if !active {
			return CoreObservation{}, nil
		}
		return CoreObservation{Active: true, Binding: &ControllerBinding{PackageID: "coleco", Generation: 5}}, nil
	})
	frame := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := controller.deliverLocal(context.Background(), frame); !errors.Is(err, errNoCore) {
		t.Fatal(err)
	}
	active = true
	if err := controller.deliverLocal(context.Background(), frame); !errors.Is(err, errNoCore) {
		t.Fatalf("fresh idle observation: %v", err)
	}
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if observations != 2 || len(*writes) != 1 || (*writes)[0].generation != 5 {
		t.Fatalf("observations=%d writes=%+v", observations, *writes)
	}
}

func TestLocalActiveObservationExpiresAfterCoreStops(t *testing.T) {
	fallback := &recordingSink{}
	sink := &controllerPortsSink{fallback: fallback}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	active := true
	observations := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		observations++
		return CoreObservation{Active: active}, nil
	})
	frame := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := controller.deliverLocal(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	active = false
	if err := controller.deliverLocal(context.Background(), frame); err != nil {
		t.Fatalf("fresh active observation: %v", err)
	}
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), frame); !errors.Is(err, errNoCore) {
		t.Fatalf("expired active observation: %v", err)
	}
	if observations != 2 || len(fallback.frames) != 2 {
		t.Fatalf("observations=%d delivered=%d", observations, len(fallback.frames))
	}
}

func TestLocalObservationFailureIsCachedAndCounted(t *testing.T) {
	sink := &controllerPortsSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	observations := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		observations++
		return CoreObservation{}, errors.New("runtime unavailable")
	})
	for range 10 {
		if err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); !errors.Is(err, errNoCore) {
			t.Fatal(err)
		}
	}
	if observations != 1 || controller.LocalInputDrops() != 10 {
		t.Fatalf("observations=%d drops=%d", observations, controller.LocalInputDrops())
	}
	expireLocalObservation(sink)
	if err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); !errors.Is(err, errNoCore) {
		t.Fatal(err)
	}
	if observations != 2 || controller.LocalInputDrops() != 11 {
		t.Fatalf("expired failure observations=%d drops=%d", observations, controller.LocalInputDrops())
	}
}

func TestLocalDropsCountReplacementAndInvalidPlayer(t *testing.T) {
	sink, _ := newPortsFixture(t, false)
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.lifecycle.Lock()
	err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	controller.lifecycle.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.deliverLocal(context.Background(), gamepad(2, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err == nil {
		t.Fatal("invalid player was accepted")
	}
	if err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if controller.LocalInputDrops() != 2 {
		t.Fatalf("drops=%d, want 2", controller.LocalInputDrops())
	}
}

func TestLocalFailedBindingDoesNotCacheAnActiveFallback(t *testing.T) {
	fallback := &recordingSink{}
	sink := &controllerPortsSink{fallback: fallback}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	observations := 0
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		observations++
		return CoreObservation{Active: true, Binding: &ControllerBinding{PackageID: "pkg", Generation: 1}}, nil
	})
	for range 2 {
		if err := controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err == nil {
			t.Fatal("binding without a controller poster fell back to legacy input")
		}
	}
	if observations != 2 || len(fallback.frames) != 0 {
		t.Fatalf("observations=%d legacy frames=%d", observations, len(fallback.frames))
	}
}

func TestLocalObservationExpiryBoundsMatrixRetirement(t *testing.T) {
	keys := NewKeyboardSink()
	stall := false
	keys.SetPoster(func(ctx context.Context, _ uint64) error {
		if !stall {
			return nil
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("matrix retirement has no local write deadline")
			return errors.New("missing deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	})
	sink := &controllerPortsSink{keys: keys}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	active := true
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: active, Keyboard: active, CoreID: "fes.coleco"}, nil
	})
	frame := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	if err := controller.deliverLocal(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if len(sink.localKeys) == 0 {
		t.Fatal("local pad did not establish a matrix hold")
	}
	active, stall = false, true
	expireLocalObservation(sink)
	started := time.Now()
	if err := controller.deliverLocal(context.Background(), frame); !errors.Is(err, errNoCore) {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("expired matrix retirement exceeded local write budget")
	}
	if !controller.lifecycle.TryLock() {
		t.Fatal("expired matrix retirement retained lifecycle lock")
	}
	controller.lifecycle.Unlock()
}

func TestLocalObservationRefreshDoesNotWaitForRemoteHIDPost(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprint("stopped=", stopped), func(t *testing.T) {
			sink := &controllerPortsSink{fallback: &recordingSink{}, hid: &keyboardHIDSink{}}
			controller := newTargetControllerWithSink("127.0.0.1:0", sink)
			controller.ports = sink
			obs := CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "apple2", Generation: 7}}
			controller.ObserveCore(func(context.Context) (CoreObservation, error) { return obs, nil })
			if err := controller.ensureLocalCore(context.Background()); err != nil {
				t.Fatal(err)
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			sink.hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error {
				close(entered)
				<-release
				return nil
			})
			remoteDone := make(chan error, 1)
			code, _ := hidkeys.Code(0x04)
			go func() { remoteDone <- sink.Apply(keyboardFrameFor(code, remoteinput.ActionPress)) }()
			<-entered
			if stopped {
				obs = CoreObservation{}
			}
			expireLocalObservation(sink)
			localDone := make(chan error, 1)
			go func() {
				localDone <- controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
			}()
			select {
			case err := <-localDone:
				if !stopped && err != nil {
					t.Fatal(err)
				}
				if stopped && err == nil {
					t.Fatal("idle refresh accepted local input")
				}
			case <-time.After(2 * localCoreWriteTimeout):
				t.Fatal("local observation waited for the stalled remote HID post")
			}
			if !controller.lifecycle.TryLock() {
				t.Fatal("HID refresh retained lifecycle lock")
			}
			controller.lifecycle.Unlock()
			unblock()
			if err := <-remoteDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLocalSourceAppliesColecoStartSelectAliases(t *testing.T) {
	sink, writes := newPortsFixture(t, true)
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(1, remoteinput.ButtonSelect, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if (*writes)[0].buttons != 128 || (*writes)[0].keypad != 1<<1 || (*writes)[0].port != 0 {
		t.Fatalf("local Start = %+v", (*writes)[0])
	}
	if (*writes)[1].buttons != 64 || (*writes)[1].keypad != 1<<10 || (*writes)[1].port != 1 {
		t.Fatalf("local Select = %+v", (*writes)[1])
	}
}

func TestLocalRawKeyboardIsShapedOntoThePortsCore(t *testing.T) {
	sink, writes := newPortsFixture(t, true)
	if err := sink.apply(sourceLocal, keyboardFrameFor(remoteinput.KeyUp, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0].buttons != 1 {
		t.Fatalf("shaped up = %+v", *writes)
	}
	fallback := &recordingSink{}
	plain := &controllerPortsSink{fallback: fallback}
	if err := plain.apply(sourceRemote, keyboardFrameFor(remoteinput.KeyUp, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if len(fallback.frames) != 1 || fallback.frames[0].Device != uint8(remoteinput.DeviceKeyboard) {
		t.Fatalf("unobserved host frame was reshaped: %+v", fallback.frames)
	}
}

func TestKeyboardSourcesMerge(t *testing.T) {
	var matrix uint64
	keys := NewKeyboardSink()
	keys.SetPoster(func(_ context.Context, value uint64) error {
		matrix = value
		return nil
	})
	press := keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)
	if err := keys.ApplyFrom(context.Background(), sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	held := matrix
	if held == zx81keys.Neutral {
		t.Fatal("local key did not reach the matrix")
	}
	if err := keys.ApplyFrom(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if err := keys.ReleaseSource(sourceRemote); err != nil {
		t.Fatal(err)
	}
	if matrix != held {
		t.Fatalf("remote release cleared local key: %#x want %#x", matrix, held)
	}
}

func TestLocalFeedLoopbackSocketMapsStartWithoutALease(t *testing.T) {
	path := localSocketPath(t, "local-input.sock")
	listener, err := listenLocalInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if listener.Addr().Network() != "unix" {
		t.Fatalf("network = %s", listener.Addr().Network())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := net.DialTimeout("tcp", listener.Addr().String(), 20*time.Millisecond); err == nil {
		t.Fatal("unix socket accepted a tcp dial")
	}
	_ = listener.Close()
	_ = os.Remove(path)

	var writes []portWrite
	var mu sync.Mutex
	ready := make(chan struct{}, 1)
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		mu.Lock()
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		mu.Unlock()
		select {
		case ready <- struct{}{}:
		default:
		}
		return nil
	}}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Binding: &ControllerBinding{PackageID: "coleco", Generation: 9, Keypad: true}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- controller.ServeLocalInput(ctx, path) }()
	t.Cleanup(func() {
		cancel()
		_ = controller.Close()
	})
	var conn net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err = net.Dial("unix", path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer conn.Close()
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("served socket mode = %o", info.Mode().Perm())
	}
	frame := gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0)
	if err := bridge.WriteFrame(conn, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("local frame was not delivered")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 1 || writes[0].port != 0 || writes[0].buttons != 128 || writes[0].keypad != 1<<1 || writes[0].generation != 9 {
		t.Fatalf("local feed Start = %+v", writes)
	}
}

func TestLocalFeedDropsFramesWhenNoCoreIsBound(t *testing.T) {
	path := localSocketPath(t, "local-input.sock")
	writes := 0
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(context.Context, string, uint64, uint8, uint8, uint16) error {
		writes++
		return nil
	}}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = controller.ServeLocalInput(ctx, path) }()
	t.Cleanup(func() { _ = controller.Close() })
	var conn net.Conn
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err = net.Dial("unix", path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer conn.Close()
	if err := bridge.WriteFrame(conn, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if writes != 0 {
		t.Fatalf("unbound core accepted %d writes", writes)
	}
}

func TestHostDetachKeepsLocalHoldAndReplacementClearsIt(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		return nil
	}}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ConfigureControllerPorts(func(context.Context) (*ControllerBinding, error) {
		return &ControllerBinding{PackageID: "coleco", Generation: 3, Keypad: true}, nil
	}, sink.poster)
	spec := Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.coleco"}
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Close() })
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := controller.Detach(context.Background(), spec.Session); err != nil {
		t.Fatal(err)
	}
	buttons, keypad := sink.mergedButtons(0)
	if buttons&16 == 0 || keypad != 0 {
		t.Fatalf("host detach cleared the local press: buttons=%d keypad=%d", buttons, keypad)
	}
	finish, err := controller.BeginCoreReplacement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if sink.hasBinding() {
		t.Fatal("core replacement left the old binding")
	}
	last := writes[len(writes)-1]
	if last.buttons != 0 || last.keypad != 0 || last.generation != 3 {
		t.Fatalf("replacement did not zero the local source: %+v", last)
	}
}

func (s *controllerPortsSink) mergedButtons(port uint8) (uint8, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keypad := s.binding != nil && s.binding.Keypad
	return controllerSnapshot(s.mergedSnapshotLocked(port), keypad)
}

func TestLocalFrameDuringCoreReplacementDoesNotRebindRetiredGeneration(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		return nil
	}}
	generation := uint64(4)
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Binding: &ControllerBinding{PackageID: "coleco", Generation: generation, Keypad: true}}, nil
	})
	ctx := context.Background()
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if !sink.hasBinding() {
		t.Fatal("local frame did not bind the active generation")
	}
	t.Cleanup(func() { _ = controller.Close() })

	finish, err := controller.BeginCoreReplacement(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// BeginCoreReplacement still holds the lifecycle lock. The runtime has
	// released the old binding and still reports that generation. A frame
	// here must return without waiting, observing, or writing.
	released := len(writes)
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	generation = 9
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if sink.hasBinding() {
		t.Fatal("local frame rebound a generation while replacement held the lifecycle lock")
	}
	if len(writes) != released {
		t.Fatalf("local frame wrote during replacement: %+v", writes[released:])
	}
	if err := finish(ctx, false); err != nil {
		t.Fatal(err)
	}
	if sink.hasBinding() {
		t.Fatal("replacement finish left a binding")
	}
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	last := writes[len(writes)-1]
	if last.generation != 9 || last.id != "coleco" || last.buttons&16 == 0 {
		t.Fatalf("next local frame did not bind the new generation: %+v", last)
	}
	for _, write := range writes[released:] {
		if write.generation == 4 {
			t.Fatalf("local delivery wrote the retired generation after replacement: %+v", writes[released:])
		}
	}
	if err := controller.Attach(ctx, Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.coleco"}); err != nil {
		t.Fatalf("host attach after the new local binding: %v", err)
	}
}

// blockingInstalledControl accepts load_core and holds it until release is
// closed. It does not inspect a package directory.
type blockingInstalledControl struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingInstalledControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (c *blockingInstalledControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (c *blockingInstalledControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected raw load")
}
func (c *blockingInstalledControl) LoadCore(context.Context, string, string) (misterruntime.Protocol2Response, error) {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	generation := uint64(9)
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test",
		Generation: &generation,
	}, nil
}

func TestLocalFrameDuringLocalLaunchDoesNotRebindRetiredGeneration(t *testing.T) {
	var mu sync.Mutex
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		mu.Lock()
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		mu.Unlock()
		return nil
	}}
	generation := uint64(4)
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Binding: &ControllerBinding{PackageID: "coleco", Generation: generation, Keypad: true}}, nil
	})
	ctx := context.Background()
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if !sink.hasBinding() {
		t.Fatal("local frame did not bind the active generation")
	}
	t.Cleanup(func() { _ = controller.Close() })

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseLoad := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseLoad)
	control := &blockingInstalledControl{entered: make(chan struct{}), release: release}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithCoreReplacementBarrier(controller))
	loadDone := make(chan error, 1)
	go func() {
		response, err := runtime.LoadInstalledCoreOwned(ctx, ctx, "/tmp/fes-pong", "abababababababababababababababababababababababababababababababab")
		if err == nil && !response.OK {
			err = errors.New("installed load was not ok")
		}
		loadDone <- err
	}()
	select {
	case <-control.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("local launch did not reach load_core")
	}
	// The replacement barrier still holds the lifecycle lock. ReleaseAll has
	// cleared the old binding. A pad frame here must return without observing
	// or writing, including after the runtime generation changes.
	mu.Lock()
	released := len(writes)
	mu.Unlock()
	if sink.hasBinding() {
		t.Fatal("local launch left the old binding in place")
	}
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	generation = 9
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if sink.hasBinding() {
		t.Fatal("local frame rebound a generation during local launch")
	}
	mu.Lock()
	wrote := len(writes) != released
	during := append([]portWrite(nil), writes[released:]...)
	mu.Unlock()
	if wrote {
		t.Fatalf("local frame wrote during local launch: %+v", during)
	}
	releaseLoad()
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("local launch did not finish")
	}
	if sink.hasBinding() {
		t.Fatal("local launch left the old binding")
	}
	if err := controller.deliverLocal(ctx, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	last := writes[len(writes)-1]
	mu.Unlock()
	if last.generation != 9 || last.id != "coleco" || last.buttons&16 == 0 {
		t.Fatalf("next local frame did not bind the new generation: %+v", last)
	}
}

// hungStatusRuntime accepts Protocol2 status requests and never writes a
// reply. ControllerStatus returns only when the caller's deadline closes
// the socket.
func hungStatusRuntime(t *testing.T) *misterruntime.Runtime {
	t.Helper()
	path := localSocketPath(t, "runtime.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go func(conn net.Conn) {
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				<-done
			}(conn)
		}
	}()
	t.Cleanup(func() {
		close(done)
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	return misterruntime.NewRuntime(misterruntime.NewClient(path), "", time.Millisecond, localCoreObserveTimeout)
}

func TestStalledLocalStatusCannotWedgeLifecycle(t *testing.T) {
	runtime := hungStatusRuntime(t)
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(context.Context, string, uint64, uint8, uint8, uint16) error {
		return nil
	}}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	t.Cleanup(func() { _ = controller.Close() })

	started := make(chan time.Duration, 1)
	controller.ObserveCore(func(ctx context.Context) (CoreObservation, error) {
		deadline, ok := ctx.Deadline()
		remaining := time.Duration(-1)
		if ok {
			remaining = time.Until(deadline)
		}
		select {
		case started <- remaining:
		default:
		}
		if _, err := runtime.ControllerStatus(ctx); err != nil {
			return CoreObservation{}, err
		}
		return CoreObservation{Active: true}, nil
	})

	localDone := make(chan error, 1)
	go func() {
		localDone <- controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	}()
	var remaining time.Duration
	select {
	case remaining = <-started:
	case <-time.After(time.Second):
		t.Fatal("local status observation did not start")
	}
	if remaining <= 0 || remaining > localCoreObserveTimeout {
		t.Fatalf("local status deadline remaining = %s, want (0, %s]", remaining, localCoreObserveTimeout)
	}

	attachDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), localCoreObserveTimeout)
		defer cancel()
		attachDone <- controller.Attach(ctx, Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.coleco"})
	}()
	replaceDone := make(chan error, 1)
	go func() {
		finish, err := controller.BeginCoreReplacement(context.Background())
		if err != nil {
			replaceDone <- err
			return
		}
		replaceDone <- finish(context.Background(), false)
	}()

	limit := localCoreObserveTimeout + time.Second
	select {
	case err := <-localDone:
		if !errors.Is(err, errNoCore) {
			t.Fatalf("stalled status delivered the frame: %v", err)
		}
	case <-time.After(limit):
		t.Fatal("stalled local status held the lifecycle lock")
	}
	select {
	case err := <-attachDone:
		if err == nil {
			t.Fatal("host attach succeeded while status never replied")
		}
	case <-time.After(limit):
		t.Fatal("host attach stayed blocked after the local status bound")
	}
	select {
	case err := <-replaceDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(limit):
		t.Fatal("core replacement stayed blocked after the local status bound")
	}
	if sink.hasBinding() {
		t.Fatal("stalled status bound a core")
	}
}

// TestStalledLocalKeyboardCannotWedgeLifecycle posts a fes.keyboard frame
// whose set_keyboard stub accepts the call and never returns until its
// context ends. The write under the lifecycle lock must carry
// localCoreWriteTimeout, so host Attach and BeginCoreReplacement proceed
// after that bound.
func TestStalledLocalKeyboardCannotWedgeLifecycle(t *testing.T) {
	keys := NewKeyboardSink()
	started := make(chan time.Duration, 1)
	keys.SetPoster(func(ctx context.Context, _ uint64) error {
		// Host cleanup still posts with context.Background(). A context
		// with no deadline is not the local write under test.
		if ctx == nil || ctx.Done() == nil {
			return nil
		}
		deadline, ok := ctx.Deadline()
		remaining := time.Duration(-1)
		if ok {
			remaining = time.Until(deadline)
		}
		select {
		case started <- remaining:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	})
	sink := &controllerPortsSink{fallback: &recordingSink{}, keys: keys}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	controller.keyboard = keys
	t.Cleanup(func() { _ = controller.Close() })
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Keyboard: true}, nil
	})

	localDone := make(chan error, 1)
	go func() {
		localDone <- controller.deliverLocal(context.Background(), keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress))
	}()
	var remaining time.Duration
	select {
	case remaining = <-started:
	case <-time.After(time.Second):
		t.Fatal("local keyboard post did not start")
	}
	if remaining <= 0 || remaining > localCoreWriteTimeout {
		t.Fatalf("local keyboard deadline remaining = %s, want (0, %s]", remaining, localCoreWriteTimeout)
	}

	attachDone := make(chan error, 1)
	go func() {
		attachDone <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.zx81"})
	}()
	replaceDone := make(chan error, 1)
	go func() {
		finish, err := controller.BeginCoreReplacement(context.Background())
		if err != nil {
			replaceDone <- err
			return
		}
		replaceDone <- finish(context.Background(), false)
	}()

	limit := localCoreWriteTimeout + time.Second
	select {
	case err := <-localDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stalled keyboard post = %v, want deadline exceeded", err)
		}
	case <-time.After(limit):
		t.Fatal("stalled local keyboard post held the lifecycle lock")
	}
	select {
	case err := <-attachDone:
		if err != nil {
			t.Fatalf("host attach after the keyboard bound: %v", err)
		}
	case <-time.After(limit):
		t.Fatal("host attach stayed blocked after the local keyboard bound")
	}
	select {
	case err := <-replaceDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(limit):
		t.Fatal("core replacement stayed blocked after the local keyboard bound")
	}
}

// TestStalledLocalControllerWriteCannotWedgeLifecycle is the ports-core
// counterpart: set_controller under the same lock uses the write deadline,
// not an unbounded poster. Cleanup after the frame uses a context with no
// deadline and must still return.
func TestStalledLocalControllerWriteCannotWedgeLifecycle(t *testing.T) {
	started := make(chan time.Duration, 1)
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(ctx context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil
		}
		remaining := time.Until(deadline)
		select {
		case started <- remaining:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 4, Keypad: true}); err != nil {
		t.Fatal(err)
	}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	t.Cleanup(func() { _ = controller.Close() })

	localDone := make(chan error, 1)
	go func() {
		localDone <- controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	}()
	var remaining time.Duration
	select {
	case remaining = <-started:
	case <-time.After(time.Second):
		t.Fatal("local controller post did not start")
	}
	if remaining <= 0 || remaining > localCoreWriteTimeout {
		t.Fatalf("local controller deadline remaining = %s, want (0, %s]", remaining, localCoreWriteTimeout)
	}

	attachDone := make(chan error, 1)
	go func() {
		attachDone <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.coleco"})
	}()
	replaceDone := make(chan error, 1)
	go func() {
		finish, err := controller.BeginCoreReplacement(context.Background())
		if err != nil {
			replaceDone <- err
			return
		}
		replaceDone <- finish(context.Background(), false)
	}()

	limit := localCoreWriteTimeout + time.Second
	select {
	case err := <-localDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stalled controller post = %v, want deadline exceeded", err)
		}
	case <-time.After(limit):
		t.Fatal("stalled local controller post held the lifecycle lock")
	}
	select {
	case err := <-attachDone:
		if err != nil {
			t.Fatalf("host attach after the controller bound: %v", err)
		}
	case <-time.After(limit):
		t.Fatal("host attach stayed blocked after the local controller bound")
	}
	select {
	case err := <-replaceDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(limit):
		t.Fatal("core replacement stayed blocked after the local controller bound")
	}
}

func TestLeaseReleaseClearsLocalSource(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, id string, generation uint64, port, buttons uint8, keypad uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypad})
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 2, Keypad: true}); err != nil {
		t.Fatal(err)
	}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := writes[len(writes)-1]
	if last.buttons != 0 || last.keypad != 0 {
		t.Fatalf("lease release left %+v", last)
	}
	if sink.hasBinding() {
		t.Fatal("lease release kept the retired binding")
	}
}

func TestConcurrentSourcesDoNotRace(t *testing.T) {
	sink, _ := newPortsFixture(t, true)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
			_ = sink.apply(sourceLocal, gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 20000))
		}()
		go func() {
			defer wg.Done()
			_ = sink.apply(sourceRemote, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0))
			_ = sink.apply(sourceRemote, gamepad(0, remoteinput.AxisLeftX, remoteinput.ActionAbsolute, 0))
		}()
	}
	wg.Wait()
	buttons, _ := sink.mergedButtons(0)
	if buttons&16 == 0 {
		t.Fatalf("concurrent remote release cleared local A: %d", buttons)
	}
}

// TestRemoteSetControllerDoesNotStarveLocalSocket is the kit-local sock
// starvation case: remote Apply blocks inside set_controller for longer than
// the kit write budget. Local deliver and the unix peer must still finish
// inside localCoreWriteTimeout, because that post does not hold ports.mu.
func TestRemoteSetControllerDoesNotStarveLocalSocket(t *testing.T) {
	const storm = 8
	stall := localCoreWriteTimeout + localCoreWriteTimeout
	var remotePosts atomic.Int32
	var localPosts atomic.Int32
	var recordMu sync.Mutex
	var last [controllerPortCount]uint8
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(ctx context.Context, _ string, _ uint64, port, buttons uint8, _ uint16) error {
		if _, ok := ctx.Deadline(); !ok {
			n := remotePosts.Add(1)
			if n <= storm {
				time.Sleep(stall)
			}
		} else {
			localPosts.Add(1)
		}
		recordMu.Lock()
		last[port] = buttons
		recordMu.Unlock()
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 4, Keypad: true}); err != nil {
		t.Fatal(err)
	}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.ports = sink
	path := localSocketPath(t, "local-input.sock")
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan struct{})
	go func() {
		_ = controller.ServeLocalInput(ctx, path)
		close(serveDone)
	}()
	var wg sync.WaitGroup
	wg.Add(storm)
	for i := 0; i < storm; i++ {
		go func() {
			defer wg.Done()
			_ = sink.Apply(gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0))
		}()
	}
	t.Cleanup(func() {
		wg.Wait()
		cancel()
		_ = controller.Close()
		<-serveDone
	})

	deadline := time.Now().Add(localCoreWriteTimeout)
	for remotePosts.Load() < storm {
		if time.Now().After(deadline) {
			t.Fatalf("remote set_controller entries = %d, want %d within %s (ports mutex still serializes the poster)", remotePosts.Load(), storm, localCoreWriteTimeout)
		}
		time.Sleep(time.Millisecond)
	}

	localDone := make(chan error, 1)
	go func() {
		localDone <- controller.deliverLocal(context.Background(), gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0))
	}()
	select {
	case err := <-localDone:
		if err != nil {
			t.Fatalf("local deliver: %v", err)
		}
	case <-time.After(localCoreWriteTimeout):
		t.Fatal("stalled remote set_controller blocked local deliver past localCoreWriteTimeout")
	}

	var conn net.Conn
	var err error
	dialDeadline := time.Now().Add(2 * time.Second)
	for {
		conn, err = net.Dial("unix", path)
		if err == nil {
			break
		}
		if time.Now().After(dialDeadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer conn.Close()
	frame := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
	const writes = 32
	seen := localPosts.Load()
	for i := 0; i < writes; i++ {
		if err := conn.SetWriteDeadline(time.Now().Add(localCoreWriteTimeout)); err != nil {
			t.Fatal(err)
		}
		if err := bridge.WriteFrame(conn, frame); err != nil {
			t.Fatalf("local sock write %d while remote set_controller was stalled: %v", i, err)
		}
	}
	wait := time.Now().Add(localCoreWriteTimeout)
	for localPosts.Load() <= seen {
		if time.Now().After(wait) {
			t.Fatalf("agent did not drain the local socket during the remote set_controller stall (posts=%d)", localPosts.Load())
		}
		time.Sleep(time.Millisecond)
	}
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var buf [1]byte
	if _, err := conn.Read(buf[:]); err == nil {
		t.Fatal("local sock returned unexpected data")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("local sock closed under remote set_controller storm: %v", err)
	}

	wg.Wait()
	buttons, _ := sink.mergedButtons(0)
	if buttons&16 == 0 {
		t.Fatalf("remote posts cleared local A: buttons=%d", buttons)
	}
	recordMu.Lock()
	final := last[0]
	recordMu.Unlock()
	if final&16 == 0 {
		t.Fatalf("last port 0 set_controller lost local A: buttons=%d", final)
	}
}

// TestStalePortRepublishesWhileOtherPortInFlight locks the per-port stale
// path: port 0 must republish after a late set_controller while port 1 is
// still inside its poster.
func TestStalePortRepublishesWhileOtherPortInFlight(t *testing.T) {
	holdPort0 := make(chan struct{})
	holdPort1 := make(chan struct{})
	enteredPort0 := make(chan struct{})
	// Buffered so the poster can record entry before the test is receiving.
	// An unbuffered send with a default drops that signal under -race.
	enteredPort1 := make(chan struct{}, 1)
	var releasePort0 sync.Once
	var releasePort1 sync.Once
	release0 := func() { releasePort0.Do(func() { close(holdPort0) }) }
	release1 := func() { releasePort1.Do(func() { close(holdPort1) }) }
	t.Cleanup(func() { release0(); release1() })

	var port0Posts atomic.Int32
	var recordMu sync.Mutex
	var last0 uint8
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(_ context.Context, _ string, _ uint64, port, buttons uint8, _ uint16) error {
		if port == 1 {
			select {
			case enteredPort1 <- struct{}{}:
			default:
			}
			<-holdPort1
		}
		if port == 0 {
			n := port0Posts.Add(1)
			if n == 2 {
				close(enteredPort0)
				<-holdPort0
			}
		}
		recordMu.Lock()
		if port == 0 {
			last0 = buttons
		}
		recordMu.Unlock()
		return nil
	}}
	if err := sink.bind(&ControllerBinding{PackageID: "coleco", Generation: 4}); err != nil {
		t.Fatal(err)
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)); err != nil {
		t.Fatal(err)
	}

	remoteDone := make(chan error, 1)
	go func() {
		remoteDone <- sink.Apply(gamepad(0, remoteinput.ButtonB, remoteinput.ActionPress, 0))
	}()
	select {
	case <-enteredPort1:
	case <-time.After(time.Second):
		t.Fatal("port 1 set_controller did not start")
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonStart, remoteinput.ActionPress, 0))
	}()
	select {
	case <-enteredPort0:
	case <-time.After(time.Second):
		t.Fatal("port 0 set_controller did not block")
	}
	if err := sink.apply(sourceLocal, gamepad(0, remoteinput.ButtonA, remoteinput.ActionRelease, 0)); err != nil {
		t.Fatal(err)
	}
	release0()

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale port 0 apply did not finish")
	}
	recordMu.Lock()
	got := last0
	posts := port0Posts.Load()
	recordMu.Unlock()
	if posts < 4 || got != 128 {
		t.Fatalf("port 0 posts=%d buttons=%d, want a republish of Start only (128) while port 1 was in flight", posts, got)
	}
	select {
	case <-remoteDone:
		t.Fatal("port 1 apply finished before its poster was released")
	default:
	}

	release1()
	select {
	case err := <-remoteDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("port 1 apply did not finish")
	}
}
