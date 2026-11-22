package input

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func tapFrame(usage uint16, generation uint64) protocol.InputFrame {
	return protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeTap, Session: 1}, Seq: 1, ServerMonoNS: generation, Action: 1, Code: 0x1000 + usage, Value: protocol.InputTapHoldMS}
}

func TestTargetChordPreservesLocalModifierAndReleasesOwnedKeys(t *testing.T) {
	var posts []hidkeys.Rows
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		posts = append(posts, rows)
		return nil
	})
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	shift, _ := hidkeys.Code(0xe1)
	if err := hid.applyFrom(context.Background(), sourceLocal, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	f := tapFrame(4, 7)
	f.Header.Type = protocol.InputTypeTapChord
	f.Reserved = 2
	if err := hid.tap(context.Background(), f, nil); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 3 || posts[1][0] != 1<<4 || posts[1][8] != 2 || posts[2][0] != 0 || posts[2][8] != 2 {
		t.Fatalf("local modifier was disturbed: %+v", posts)
	}
	if !hid.pressed[sourceLocal][0xe1] || hid.pressed[sourceRemote][0xe1] || hid.pressed[sourceRemote][4] {
		t.Fatal("incorrect source cleanup")
	}
}

func TestTargetChordCancellationReleasesKeyAndModifiers(t *testing.T) {
	calls := make(chan hidkeys.Rows, 4)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { calls <- rows; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	f := tapFrame(4, 7)
	f.Header.Type = protocol.InputTypeTapChord
	f.Reserved = 2
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- hid.tap(ctx, f, nil) }()
	select {
	case rows := <-calls:
		if rows[0] != 1<<4 || rows[8] != 2 {
			t.Fatal(rows)
		}
	case <-time.After(time.Second):
		t.Fatal("missing press")
	}
	cancel()
	select {
	case rows := <-calls:
		if rows != (hidkeys.Rows{}) {
			t.Fatal(rows)
		}
	case <-time.After(time.Second):
		t.Fatal("missing release")
	}
	if err := <-done; err == nil {
		t.Fatal("cancellation accepted")
	}
}

func TestTargetTapPreservesLocalHoldAndReleasesOnTarget(t *testing.T) {
	var mu sync.Mutex
	var posts []hidkeys.Rows
	var times []time.Time
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		mu.Lock()
		defer mu.Unlock()
		posts = append(posts, rows)
		times = append(times, time.Now())
		return nil
	})
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	code, _ := hidkeys.Code(4)
	if err := hid.applyFrom(context.Background(), sourceLocal, keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if err := hid.tap(context.Background(), tapFrame(5, 7), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 3 || posts[0][0] != 1<<4 || posts[1][0] != 1<<4|1<<5 || posts[2][0] != 1<<4 {
		t.Fatalf("local hold lost: %+v", posts)
	}
	if times[2].Sub(times[1]) < 55*time.Millisecond {
		t.Fatalf("tap ended too soon: %v", times[2].Sub(times[1]))
	}
	if !hid.pressed[sourceLocal][4] || hid.pressed[sourceRemote][5] {
		t.Fatal("source contributions incorrect")
	}
}

func TestTargetTapReleasesOnCancelAndUncertainPress(t *testing.T) {
	for _, failPress := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "press-fails"}[failPress], func(t *testing.T) {
			calls := make(chan hidkeys.Rows, 4)
			hid := &keyboardHIDSink{}
			hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
				calls <- rows
				if failPress && rows[0]&(1<<4) != 0 {
					return errors.New("uncertain press")
				}
				return nil
			})
			hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- hid.tap(ctx, tapFrame(4, 7), nil) }()
			select {
			case rows := <-calls:
				if rows[0] != 1<<4 {
					t.Fatalf("press %v", rows)
				}
			case <-time.After(time.Second):
				t.Fatal("no press")
			}
			if !failPress {
				cancel()
			}
			select {
			case rows := <-calls:
				if rows != (hidkeys.Rows{}) {
					t.Fatalf("release %v", rows)
				}
			case <-time.After(time.Second):
				t.Fatal("no release")
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("failure accepted")
				}
			case <-time.After(time.Second):
				t.Fatal("tap stuck")
			}
		})
	}
}

func TestTargetTapNeverReleasesIntoReplacementGeneration(t *testing.T) {
	calls := make(chan hidPost, 4)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, id string, g uint64, rows hidkeys.Rows) error {
		calls <- hidPost{id, g, rows}
		return nil
	})
	hid.bind(&KeyboardHIDBinding{PackageID: "old", Generation: 7})
	done := make(chan error, 1)
	go func() { done <- hid.tap(context.Background(), tapFrame(4, 7), nil) }()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("no press")
	}
	hid.bind(&KeyboardHIDBinding{PackageID: "new", Generation: 8})
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("replacement accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("tap stuck")
	}
	select {
	case p := <-calls:
		t.Fatalf("old tap wrote to replacement: %+v", p)
	default:
	}
	if err := hid.tap(context.Background(), tapFrame(4, 7), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("stale generation: %v", err)
	}
}

func TestTargetTapRejectsAlreadyHeldRemoteKey(t *testing.T) {
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	code, _ := hidkeys.Code(4)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if err := hid.tap(context.Background(), tapFrame(4, 7), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("held key: %v", err)
	}
	if !hid.pressed[sourceRemote][4] {
		t.Fatal("tap released another remote hold")
	}
}

func TestTargetTapRequiresFreshActiveObservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		observe func(context.Context) (CoreObservation, error)
	}{
		{"missing", nil},
		{"failed", func(context.Context) (CoreObservation, error) { return CoreObservation{}, errors.New("unavailable") }},
		{"inactive", func(context.Context) (CoreObservation, error) {
			return CoreObservation{KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
		}},
		{"replacement", func(context.Context) (CoreObservation, error) {
			return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 8}}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			hid := &keyboardHIDSink{}
			hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
			hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
			ports := &controllerPortsSink{hid: hid}
			controller := newTargetControllerWithSink("127.0.0.1:0", ports)
			controller.ports = ports
			if tc.observe != nil {
				controller.ObserveCore(tc.observe)
			}
			sink := retainedTapSink{controller: controller, hid: hid}
			if err := sink.Tap(context.Background(), tapFrame(4, 7)); !errors.Is(err, errTapUnavailable) {
				t.Fatalf("tap: %v", err)
			}
			if posts != 0 {
				t.Fatalf("unconfirmed press posted %d times", posts)
			}
		})
	}
}

func TestTargetTapRejectsBusyLifecycle(t *testing.T) {
	hid := &keyboardHIDSink{}
	controller := &TargetController{}
	controller.lifecycle.Lock()
	defer controller.lifecycle.Unlock()
	if err := (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7)); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("tap: %v", err)
	}
}

func TestRejectedTapDoesNotMutateExistingRemoteHold(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	binding := &KeyboardHIDBinding{PackageID: "p", Generation: 7}
	hid.bind(binding)
	code, _ := hidkeys.Code(4)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	ports := &controllerPortsSink{hid: hid, hidBinding: binding}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 8}}, nil
	})
	if err := (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(5, 7)); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("tap: %v", err)
	}
	if posts != 1 || !hid.pressed[sourceRemote][4] || hid.binding.Generation != 7 {
		t.Fatalf("rejected tap changed held input or binding")
	}
}

func TestTargetTapRejectsSameLocalHold(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	code, _ := hidkeys.Code(4)
	if err := hid.applyFrom(context.Background(), sourceLocal, keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if err := hid.tap(context.Background(), tapFrame(4, 7), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("tap: %v", err)
	}
	if posts != 1 || !hid.pressed[sourceLocal][4] || hid.pressed[sourceRemote][4] {
		t.Fatal("rejected tap changed local hold")
	}
}

func TestTargetTapReportsLocalOverlapDuringHold(t *testing.T) {
	posts := make(chan hidkeys.Rows, 8)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ uint64, r hidkeys.Rows) error { posts <- r; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	done := make(chan error, 1)
	go func() { done <- hid.tap(context.Background(), tapFrame(4, 7), nil) }()
	select {
	case <-posts:
	case <-time.After(time.Second):
		t.Fatal("no tap press")
	}
	code, _ := hidkeys.Code(4)
	if err := hid.applyFrom(context.Background(), sourceLocal, keyboardFrameFor(code, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("tap: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tap stuck")
	}
	if !hid.pressed[sourceLocal][4] || hid.pressed[sourceRemote][4] {
		t.Fatal("local hold not preserved")
	}
}

// A local key write and a target-timed remote tap are separate sources. A
// short, bounded overlap must not disconnect the remote stream or lose the
// local hold merely because both need the lifecycle fence.
func TestTargetTapWaitsForLocalKeyboardWrite(t *testing.T) {
	started := make(chan struct{})
	resume := make(chan struct{})
	localDone := make(chan error, 1)
	var resumeOnce sync.Once
	unblock := func() { resumeOnce.Do(func() { close(resume) }) }
	t.Cleanup(unblock)
	var mu sync.Mutex
	var posts []hidkeys.Rows
	hid := &keyboardHIDSink{}
	hid.setPoster(func(ctx context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		mu.Lock()
		posts = append(posts, rows)
		first := len(posts) == 1
		mu.Unlock()
		if first {
			close(started)
			select {
			case <-resume:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	shift, _ := hidkeys.Code(0xe1)
	go func() {
		localDone <- controller.deliverLocal(context.Background(), keyboardFrameFor(shift, remoteinput.ActionPress))
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("local Shift write did not start")
	}
	tapStarted := make(chan struct{})
	tapDone := make(chan error, 1)
	go func() {
		close(tapStarted)
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(0x25, 7))
	}()
	<-tapStarted
	select {
	case err := <-tapDone:
		t.Fatalf("tap did not wait for the in-flight local write: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	unblock()
	if err := <-localDone; err != nil {
		t.Fatalf("local Shift write: %v", err)
	}
	select {
	case err := <-tapDone:
		if err != nil {
			t.Fatalf("remote tap after local write: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote tap did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 3 || posts[0][8] != 2 || posts[1][8] != 2 || posts[1][2] != 1<<5 || posts[2][8] != 2 || posts[2][2] != 0 {
		t.Fatalf("tap did not preserve the local Shift hold: %+v", posts)
	}
}

func TestTargetTapLifecycleWaitHonorsCancellation(t *testing.T) {
	controller := &TargetController{}
	controller.lifecycle.Lock()
	defer controller.lifecycle.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: &keyboardHIDSink{}}).Tap(ctx, tapFrame(4, 7))
	}()
	// Keep the lifecycle fence held until the waiting call has to observe the
	// cancellation; it must not wait for the 500 ms contention budget.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-tapDone:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, errTapUnavailable) {
			t.Fatalf("canceled lifecycle wait: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("canceled tap waited on the lifecycle fence")
	}
}

func TestTargetTapRechecksGenerationAfterLifecycleWait(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	generation := uint64(7)
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: generation}}, nil
	})
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7))
	}()
	select {
	case err := <-tapDone:
		controller.lifecycle.Unlock()
		t.Fatalf("tap did not wait for replacement: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	// The replacement changes the observed generation while holding the same
	// fence. The old tap must re-observe and refuse to touch the new core.
	generation = 8
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("stale-generation tap: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale-generation tap did not finish")
	}
	if posts != 0 {
		t.Fatalf("stale-generation tap posted %d times", posts)
	}
}

func TestTargetTapRejectsFocusRoundTripDuringLifecycleWait(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7))
	}()
	select {
	case err := <-tapDone:
		controller.lifecycle.Unlock()
		t.Fatalf("tap did not wait for focus transition: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	// Both focus changes complete while the lifecycle is held. A waiter from
	// before them must not dispatch merely because focus returned to machine.
	if err := ports.setDisplayFocusContext(context.Background(), true); err != nil {
		controller.lifecycle.Unlock()
		t.Fatal(err)
	}
	if err := ports.setDisplayFocusContext(context.Background(), false); err != nil {
		controller.lifecycle.Unlock()
		t.Fatal(err)
	}
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("tap crossed a focus round trip: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("focus-fenced tap did not finish")
	}
	if posts != 0 {
		t.Fatalf("focus-fenced tap posted %d times", posts)
	}
}

func TestTargetTapRejectsLeaseReplacementDuringLifecycleWait(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	controller.mu.Lock()
	controller.active = &lease{session: 1}
	controller.mu.Unlock()
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7))
	}()
	select {
	case err := <-tapDone:
		controller.lifecycle.Unlock()
		t.Fatalf("tap did not wait for lease replacement: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	controller.mu.Lock()
	controller.active = &lease{session: 1}
	controller.mu.Unlock()
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("tap crossed a lease replacement: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lease-fenced tap did not finish")
	}
	if posts != 0 {
		t.Fatalf("lease-fenced tap posted %d times", posts)
	}
}

func awaitTapTransition(t *testing.T, controller *TargetController) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for controller.transitionPending.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("lifecycle transition did not publish its intent")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTargetTapDoesNotOvertakeQueuedDetach(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	controller.mu.Lock()
	controller.active = &lease{session: 1}
	controller.mu.Unlock()
	// A local write owns the lifecycle while the remote tap and Stop arrive.
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7))
	}()
	select {
	case err := <-tapDone:
		controller.lifecycle.Unlock()
		t.Fatalf("tap did not wait for local write: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	detachDone := make(chan error, 1)
	go func() { detachDone <- controller.Detach(context.Background(), 1) }()
	awaitTapTransition(t, controller)
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("tap overtook queued detach: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tap did not finish after queued detach")
	}
	if err := <-detachDone; err != nil {
		t.Fatalf("detach: %v", err)
	}
	if posts != 0 {
		t.Fatalf("tap posted %d times after detach was queued", posts)
	}
}

func TestTargetTapDoesNotOvertakeQueuedFocus(t *testing.T) {
	posts := 0
	hid := &keyboardHIDSink{}
	hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	hid.bind(&KeyboardHIDBinding{PackageID: "p", Generation: 7})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports, controller.keyboard = ports, NewKeyboardSink()
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		tapDone <- (retainedTapSink{controller: controller, hid: hid}).Tap(context.Background(), tapFrame(4, 7))
	}()
	select {
	case err := <-tapDone:
		controller.lifecycle.Unlock()
		t.Fatalf("tap did not wait for local write: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	focusDone := make(chan error, 1)
	go func() {
		finish, err := controller.BeginSessionDisplay(context.Background(), true)
		if err == nil {
			finish(false)
		}
		focusDone <- err
	}()
	awaitTapTransition(t, controller)
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("tap overtook queued focus transition: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tap did not finish after queued focus transition")
	}
	if err := <-focusDone; err != nil {
		t.Fatalf("focus transition: %v", err)
	}
	if posts != 0 {
		t.Fatalf("tap posted %d times after focus was queued", posts)
	}
}
