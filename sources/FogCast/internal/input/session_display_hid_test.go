package input

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestHIDDisplayFocusRetiresKeysPerSource(t *testing.T) {
	for _, source := range []inputSource{sourceLocal, sourceRemote} {
		for _, during := range []bool{false, true} {
			t.Run(map[inputSource]string{sourceLocal: "local", sourceRemote: "remote"}[source]+map[bool]string{false: "-before", true: "-during"}[during], func(t *testing.T) {
				ports, _, _ := newHIDPortsFixture(t, true)
				var machine hidkeys.Rows
				posts := 0
				ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
					posts++
					machine = rows
					return nil
				})
				controller := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
				a, _ := hidkeys.Code(0x04)
				b, _ := hidkeys.Code(0x05)
				apply := func(code remoteinput.Code, action remoteinput.Action) {
					t.Helper()
					if err := ports.applyContext(context.Background(), source, keyboardFrameFor(code, action)); err != nil {
						t.Fatal(err)
					}
				}
				if !during {
					apply(a, remoteinput.ActionPress)
				}
				finish, err := controller.BeginSessionDisplay(context.Background(), true)
				if err != nil {
					t.Fatal(err)
				}
				machine = hidkeys.Rows{} // runtime neutralizes before reporting focus
				finish(true)
				if during {
					apply(a, remoteinput.ActionPress)
				}
				before := posts
				apply(a, remoteinput.ActionPress) // repeated key cannot reach machine
				if posts != before {
					t.Fatal("launcher-focused key posted to machine")
				}
				finish, err = controller.BeginSessionDisplay(context.Background(), false)
				if err != nil {
					t.Fatal(err)
				}
				finish(false)
				apply(b, remoteinput.ActionPress)
				if want := hidkeys.Encode(map[uint8]bool{0x05: true}); machine != want {
					t.Fatalf("retired A replayed: got %v want %v", machine, want)
				}
				before = posts
				apply(a, remoteinput.ActionPress)
				if posts != before {
					t.Fatal("held A repeated before release")
				}
				apply(a, remoteinput.ActionRelease)
				apply(a, remoteinput.ActionPress)
				if want := hidkeys.Encode(map[uint8]bool{0x04: true, 0x05: true}); machine != want {
					t.Fatalf("fresh A missing: %v", machine)
				}
				apply(a, remoteinput.ActionRelease)
				apply(b, remoteinput.ActionRelease)
				if machine != (hidkeys.Rows{}) {
					t.Fatalf("keys remained held: %v", machine)
				}
			})
		}
	}
}

func TestHIDFocusRefusalRetainsSafeNeutralForRelease(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	release := keyboardFrameFor(a, remoteinput.ActionRelease)
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	finish(false) // runtime refused before neutralizing; machine still holds A
	if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
		t.Fatal(err)
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatalf("release after refusal left stale row: %v", machine)
	}
}

func TestHIDFocusAdmissionHonorsPendingPostCancellation(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	ports.hid.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := c.BeginSessionDisplay(ctx, true)
	ports.hid.mu.Unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("focus admission: %v", err)
	}
	if ports.displayFocused || ports.hid.focused {
		t.Fatal("cancelled admission changed focus")
	}
	if !c.lifecycle.TryLock() {
		t.Fatal("cancelled admission kept lifecycle lock")
	}
	c.lifecycle.Unlock()
}

func TestHIDTapCrossingFocusKeepsRemoteSuppression(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := 0
	ports.hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- ports.hid.tap(context.Background(), tapFrame(4, 7), func() { close(started) }) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tap did not start")
	}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	release := keyboardFrameFor(a, remoteinput.ActionRelease)
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, errTapUnavailable) {
			t.Fatalf("retired tap = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tap did not finish")
	}
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	before := posts
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if posts != before {
		t.Fatal("tap release unsuppressed held remote key")
	}
	if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
		t.Fatal(err)
	}
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if posts <= before {
		t.Fatal("fresh remote press did not post")
	}
}

func TestHIDReleaseDuringRefusedFocusReconcilesNeutral(t *testing.T) {
	for _, failOnce := range []bool{false, true} {
		t.Run(map[bool]string{false: "neutral succeeds", true: "neutral fails then release retries"}[failOnce], func(t *testing.T) {
			ports, _, _ := newHIDPortsFixture(t, true)
			var machine hidkeys.Rows
			fail := false
			ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
				if fail {
					fail = false
					return errors.New("neutral failed")
				}
				machine = rows
				return nil
			})
			c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
			a, _ := hidkeys.Code(0x04)
			press := keyboardFrameFor(a, remoteinput.ActionPress)
			release := keyboardFrameFor(a, remoteinput.ActionRelease)
			if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
				t.Fatal(err)
			}
			finish, err := c.BeginSessionDisplay(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
				t.Fatal(err)
			}
			fail = failOnce
			finish(false) // refusal left the old physical A held
			if failOnce {
				if machine == (hidkeys.Rows{}) {
					t.Fatal("injected neutral unexpectedly succeeded")
				}
				if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
					t.Fatal(err)
				}
			}
			if machine != (hidkeys.Rows{}) {
				t.Fatalf("consumed release stranded A: %v", machine)
			}
		})
	}
}

func TestHIDSameKeySuppressionIsIndependentPerSource(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	release := keyboardFrameFor(a, remoteinput.ActionRelease)
	for _, source := range []inputSource{sourceLocal, sourceRemote} {
		if err := ports.applyContext(context.Background(), source, press); err != nil {
			t.Fatal(err)
		}
	}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	machine = hidkeys.Rows{}
	finish(true)
	if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
		t.Fatal(err)
	}
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if err := ports.applyContext(context.Background(), sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatal("remote release unsuppressed local hold")
	}
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if machine == (hidkeys.Rows{}) {
		t.Fatal("fresh remote press was lost")
	}
	if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
		t.Fatal(err)
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatal("blocked local hold replayed on remote release")
	}
}

func TestHIDMouseNeutralFailureReconcilesConsumedRelease(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	release := keyboardFrameFor(a, remoteinput.ActionRelease)
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	ports.mouse = &mouseSink{binding: &MouseBinding{PackageID: "apple2", Generation: 7}}
	ports.mouse.buttons[sourceRemote] = 1
	ports.mouse.poster = func(context.Context, string, uint64, int16, int16, uint8) error {
		if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
			t.Error(err)
		}
		return errors.New("mouse neutral failed")
	}
	if _, err := c.BeginSessionDisplay(context.Background(), true); err == nil {
		t.Fatal("mouse failure accepted")
	}
	if ports.displayFocused || ports.hid.focused {
		t.Fatal("failed mouse neutral retained focus")
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatalf("mouse failure stranded keyboard: %v", machine)
	}
}

func TestHIDUnknownFocusAndSourceDisconnectStayFenced(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := 0
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		posts++
		machine = rows
		return nil
	})
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	if err := ports.applyContext(context.Background(), sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	machine = hidkeys.Rows{}
	finish(true) // unconfirmed physical result stays fenced
	before := posts
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if posts != before {
		t.Fatal("unknown focus delivered a key")
	}
	if err := ports.hid.releaseSource(sourceRemote); err != nil {
		t.Fatal(err)
	}
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if err := ports.applyContext(context.Background(), sourceLocal, press); err != nil {
		t.Fatal(err)
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatal("local held key replayed")
	}
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if machine == (hidkeys.Rows{}) {
		t.Fatal("disconnected remote source remained suppressed")
	}
}

func TestHIDRebindDuringFocusRetainsFence(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := 0
	ports.hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	ports.hid.bind(&KeyboardHIDBinding{PackageID: "replacement", Generation: 8})
	a, _ := hidkeys.Code(0x04)
	if err := ports.applyContext(context.Background(), sourceLocal, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatal("rebind dropped the focus fence")
	}
}

func TestHIDDelayedFrameCannotCrossFocusCycle(t *testing.T) {
	for _, admittedFocused := range []bool{false, true} {
		t.Run(map[bool]string{false: "admitted before focus", true: "admitted during focus"}[admittedFocused], func(t *testing.T) {
			ports, _, _ := newHIDPortsFixture(t, true)
			var machine hidkeys.Rows
			ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
			c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
			a, _ := hidkeys.Code(0x04)
			b, _ := hidkeys.Code(0x05)
			if admittedFocused {
				finish, err := c.BeginSessionDisplay(context.Background(), true)
				if err != nil {
					t.Fatal(err)
				}
				finish(true)
			}
			// This is the exact admission snapshot taken under ports.mu before
			// applyContext releases that lock to enter the HID sink.
			ports.mu.Lock()
			epoch, focused := ports.displayFocusEpoch, ports.displayFocused
			ports.mu.Unlock()
			if !admittedFocused {
				finish, err := c.BeginSessionDisplay(context.Background(), true)
				if err != nil {
					t.Fatal(err)
				}
				finish(true)
			}
			finish, err := c.BeginSessionDisplay(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			finish(false)
			if err := ports.hid.applyFromEpoch(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress), epoch, focused); err != nil {
				t.Fatal(err)
			}
			if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(b, remoteinput.ActionPress)); err != nil {
				t.Fatal(err)
			}
			if want := hidkeys.Encode(map[uint8]bool{0x05: true}); machine != want {
				t.Fatalf("delayed A crossed focus: %v want %v", machine, want)
			}
			if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionRelease)); err != nil {
				t.Fatal(err)
			}
			if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
				t.Fatal(err)
			}
			if want := hidkeys.Encode(map[uint8]bool{0x04: true, 0x05: true}); machine != want {
				t.Fatalf("fresh A missing: %v", machine)
			}
		})
	}
}

func TestHIDUnknownFinishFencesEvenWhenSinkBusy(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := 0
	ports.hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	finish, err := c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	ports.hid.mu.Lock()
	done := make(chan struct{})
	go func() { finish(true); close(done) }() // machine-focus request returned unknown
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		ports.hid.mu.Unlock()
		t.Fatal("finish exceeded bounded wait")
	}
	ports.hid.mu.Unlock()
	if !ports.displayFocused || !ports.hid.focusFence.Load() {
		t.Fatal("unknown result was not fenced")
	}
	a, _ := hidkeys.Code(0x04)
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatal("post escaped while unknown outcome was fenced")
	}
	if err := ports.hid.tap(context.Background(), tapFrame(4, 7), nil); !errors.Is(err, errTapUnavailable) {
		t.Fatalf("tap admitted while fenced: %v", err)
	}
}

func TestHIDUnknownBusyFenceRetiresOldHoldOnClose(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	b, _ := hidkeys.Code(0x05)
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	finish, err := c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	ports.hid.mu.Lock()
	done := make(chan struct{})
	go func() { finish(true); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		ports.hid.mu.Unlock()
		t.Fatal("unknown fence did not finish")
	}
	ports.hid.mu.Unlock()
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if machine != (hidkeys.Rows{}) {
		t.Fatalf("old hold not neutralized on close: %v", machine)
	}
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(b, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	if want := hidkeys.Encode(map[uint8]bool{0x05: true}); machine != want {
		t.Fatalf("old A replayed: %v", machine)
	}
}

func TestHIDUnknownBusyPostEventuallyNeutralizesWithoutClose(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	writes := make(chan hidkeys.Rows, 4)
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { writes <- rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	<-writes
	finish, err := c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	ports.hid.mu.Lock()
	done := make(chan struct{})
	go func() { finish(true); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		ports.hid.mu.Unlock()
		t.Fatal("unknown result did not return")
	}
	ports.hid.mu.Unlock()
	select {
	case rows := <-writes:
		if rows != (hidkeys.Rows{}) {
			t.Fatalf("unknown reconciliation reasserted keys: %v", rows)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight hold was not neutralized while unknown")
	}
	if !ports.hid.focusFence.Load() {
		t.Fatal("neutral reconciliation cleared unknown fence")
	}
}

func TestHIDUnknownReconciliationCannotTouchReplacement(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	posts := 0
	ports.hid.setPoster(func(context.Context, string, uint64, hidkeys.Rows) error { posts++; return nil })
	old := KeyboardHIDBinding{PackageID: "apple2", Generation: 7}
	ports.hid.fenceEpoch.Store(9)
	ports.hid.focusFence.Store(true)
	ports.hid.bind(&KeyboardHIDBinding{PackageID: "replacement", Generation: 8})
	if err := ports.hid.settleUnknownFocus(context.Background(), 9, old); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatal("retired generation reconciled into replacement")
	}
}

func TestHIDUnknownFastInFlightPostIsNeutralized(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	entered, unblock := make(chan struct{}), make(chan struct{})
	writes := make(chan hidkeys.Rows, 4)
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error {
		if rows != (hidkeys.Rows{}) {
			close(entered)
			<-unblock
		}
		writes <- rows
		return nil
	})
	a, _ := hidkeys.Code(0x04)
	posted := make(chan error, 1)
	go func() {
		posted <- ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(a, remoteinput.ActionPress))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("HID post did not start")
	}
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	finish, err := c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		close(unblock)
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { finish(true); close(finished) }()
	for deadline := time.Now().Add(time.Second); !ports.hid.focusFence.Load(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			close(unblock)
			t.Fatal("unknown fence not raised")
		}
	}
	close(unblock)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("fast-drain finish did not complete")
	}
	if err := <-posted; err != nil {
		t.Fatal(err)
	}
	<-writes // the in-flight non-neutral write
	select {
	case rows := <-writes:
		if rows != (hidkeys.Rows{}) {
			t.Fatalf("expected neutral, got %v", rows)
		}
	case <-time.After(time.Second):
		t.Fatal("fast-drain unknown result did not neutralize")
	}
}

func TestHIDRebindDuringFocusKeepsHeldSourceBlocked(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	a, _ := hidkeys.Code(0x04)
	press := keyboardFrameFor(a, remoteinput.ActionPress)
	release := keyboardFrameFor(a, remoteinput.ActionRelease)
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	machine = hidkeys.Rows{}
	finish(true)
	ports.hid.bind(&KeyboardHIDBinding{PackageID: "replacement", Generation: 8})
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if machine != (hidkeys.Rows{}) {
		t.Fatalf("held key replayed into replacement: %v", machine)
	}
	if err := ports.applyContext(context.Background(), sourceRemote, release); err != nil {
		t.Fatal(err)
	}
	if err := ports.applyContext(context.Background(), sourceRemote, press); err != nil {
		t.Fatal(err)
	}
	if machine == (hidkeys.Rows{}) {
		t.Fatal("fresh press did not reach replacement")
	}
}

func TestHIDReleaseInOtherKeyboardModeClearsSuppression(t *testing.T) {
	for _, source := range []inputSource{sourceLocal, sourceRemote} {
		t.Run(map[inputSource]string{sourceLocal: "local", sourceRemote: "remote"}[source], func(t *testing.T) {
			ports, _, _ := newHIDPortsFixture(t, true)
			var machine hidkeys.Rows
			ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
			c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
			a, _ := hidkeys.Code(0x04)
			press := keyboardFrameFor(a, remoteinput.ActionPress)
			release := keyboardFrameFor(a, remoteinput.ActionRelease)
			if err := ports.applyContext(context.Background(), source, press); err != nil {
				t.Fatal(err)
			}
			finish, err := c.BeginSessionDisplay(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			machine = hidkeys.Rows{}
			finish(true)
			if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, Keyboard: true, CoreID: "zx81", Generation: 8}); err != nil {
				t.Fatal(err)
			}
			if err := ports.bind(nil); err != nil {
				t.Fatal(err)
			}
			if err := ports.applyContext(context.Background(), source, release); err != nil {
				t.Fatal(err)
			}
			if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "replacement", Generation: 9}}); err != nil {
				t.Fatal(err)
			}
			finish, err = c.BeginSessionDisplay(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			finish(false)
			if err := ports.applyContext(context.Background(), source, press); err != nil {
				t.Fatal(err)
			}
			if machine == (hidkeys.Rows{}) {
				t.Fatal("fresh press suppressed after key-up in non-HID mode")
			}
		})
	}
}

func TestHIDLegacyShiftPressMakesReleaseAmbiguous(t *testing.T) {
	ports, _, _ := newHIDPortsFixture(t, true)
	var machine hidkeys.Rows
	ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
	c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
	right, _ := hidkeys.Code(0xe5)
	apply := func(code remoteinput.Code, action remoteinput.Action) {
		t.Helper()
		if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(code, action)); err != nil {
			t.Fatal(err)
		}
	}
	apply(right, remoteinput.ActionPress)
	finish, err := c.BeginSessionDisplay(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	machine = hidkeys.Rows{}
	finish(true)
	if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, Keyboard: true, CoreID: "zx81", Generation: 8}); err != nil {
		t.Fatal(err)
	}
	if err := ports.bind(nil); err != nil {
		t.Fatal(err)
	}
	apply(zx81keys.KeyShift, remoteinput.ActionPress)
	apply(zx81keys.KeyShift, remoteinput.ActionRelease)
	if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "replacement", Generation: 9}}); err != nil {
		t.Fatal(err)
	}
	finish, err = c.BeginSessionDisplay(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	finish(false)
	apply(right, remoteinput.ActionPress)
	if machine != (hidkeys.Rows{}) {
		t.Fatal("ambiguous legacy release unsuppressed held right shift")
	}
	apply(right, remoteinput.ActionRelease)
	apply(right, remoteinput.ActionPress)
	if machine == (hidkeys.Rows{}) {
		t.Fatal("distinguishable release did not restore right shift")
	}
}

func TestHIDLegacyShiftReleaseRespectsAmbiguousSides(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "right only", true: "both sides"}[both], func(t *testing.T) {
			ports, _, _ := newHIDPortsFixture(t, true)
			var machine hidkeys.Rows
			ports.hid.setPoster(func(_ context.Context, _ string, _ uint64, rows hidkeys.Rows) error { machine = rows; return nil })
			c := &TargetController{ports: ports, keyboard: NewKeyboardSink()}
			left, _ := hidkeys.Code(0xe1)
			right, _ := hidkeys.Code(0xe5)
			apply := func(code remoteinput.Code, action remoteinput.Action) {
				t.Helper()
				if err := ports.applyContext(context.Background(), sourceRemote, keyboardFrameFor(code, action)); err != nil {
					t.Fatal(err)
				}
			}
			apply(right, remoteinput.ActionPress)
			if both {
				apply(left, remoteinput.ActionPress)
			}
			finish, err := c.BeginSessionDisplay(context.Background(), true)
			if err != nil {
				t.Fatal(err)
			}
			machine = hidkeys.Rows{}
			finish(true)
			if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, Keyboard: true, CoreID: "zx81", Generation: 8}); err != nil {
				t.Fatal(err)
			}
			if err := ports.bind(nil); err != nil {
				t.Fatal(err)
			}
			apply(zx81keys.KeyShift, remoteinput.ActionRelease)
			if err := ports.setObservationContext(context.Background(), CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "replacement", Generation: 9}}); err != nil {
				t.Fatal(err)
			}
			finish, err = c.BeginSessionDisplay(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			finish(false)
			apply(right, remoteinput.ActionPress)
			if both && machine != (hidkeys.Rows{}) {
				t.Fatal("ambiguous release unsuppressed a held shift")
			}
			if !both && machine == (hidkeys.Rows{}) {
				t.Fatal("unambiguous right shift remained suppressed")
			}
		})
	}
}
