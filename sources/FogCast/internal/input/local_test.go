package input

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
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
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypadMask uint16) error {
		writes = append(writes, portWrite{id, generation, port, buttons, keypadMask})
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
	err := sink.publishPortLocked(0, true)
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
	err = sink.publishPortLocked(0, true)
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
	keys.SetPoster(func(value uint64) error {
		matrix = value
		return nil
	})
	press := keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)
	if err := keys.ApplyFrom(sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	held := matrix
	if held == zx81keys.Neutral {
		t.Fatal("local key did not reach the matrix")
	}
	if err := keys.ApplyFrom(sourceRemote, press); err != nil {
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
	path := filepath.Join(t.TempDir(), "local-input.sock")
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
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
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
	path := filepath.Join(t.TempDir(), "local-input.sock")
	writes := 0
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(string, uint64, uint8, uint8, uint16) error {
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
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
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
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
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

// hungStatusRuntime accepts Protocol2 status requests and never writes a
// reply. ControllerStatus returns only when the caller's deadline closes
// the socket.
func hungStatusRuntime(t *testing.T) *misterruntime.Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.sock")
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
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(string, uint64, uint8, uint8, uint16) error {
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

func TestLeaseReleaseClearsLocalSource(t *testing.T) {
	var writes []portWrite
	sink := &controllerPortsSink{fallback: &recordingSink{}, poster: func(id string, generation uint64, port, buttons uint8, keypad uint16) error {
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
