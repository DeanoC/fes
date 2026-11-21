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
