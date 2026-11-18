package tenfoot

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type delayedDisplayPresenter struct {
	countingSessionMenu
	mu2     sync.Mutex
	entered chan struct{}
	gate    chan struct{}
	fail    bool
}

func (p *delayedDisplayPresenter) Present(ctx context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	p.mu2.Lock()
	entered, gate, fail := p.entered, p.gate, p.fail
	p.mu2.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-gate:
		case <-ctx.Done():
			return menudisplay.Result{}, ctx.Err()
		}
	}
	if fail {
		return menudisplay.Result{}, errors.New("injected present failure")
	}
	return p.countingSessionMenu.Present(ctx, generation, pixels)
}

func externalDisplayFixture(t *testing.T) (*App, *gfx.MenuDisplay, *countingSessionMenu, sessiondisplayipc.Transition) {
	t.Helper()
	server := httptest.NewServer(nil)
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	prior := app.session
	app.mu.Unlock()
	presenter := &countingSessionMenu{}
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	display.SetChangeDriven(true)
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	return app, display, presenter, sessiondisplayipc.Transition{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, TargetID: prior.TargetID, Display: protocol.SessionDisplayFocused}
}

func TestExternalDisplayRejectsStaleAndBusyBeforePause(t *testing.T) {
	app, display, _, request := externalDisplayFixture(t)
	stale := request
	stale.Generation++
	if _, err := app.prepareExternalSessionDisplay(context.Background(), stale); err == nil {
		t.Fatal("stale generation accepted")
	}
	foreign := request
	foreign.TargetID = "other-kit"
	if _, err := app.prepareExternalSessionDisplay(context.Background(), foreign); err == nil {
		t.Fatal("other target accepted")
	}
	app.mu.Lock()
	app.sessionDisplayBusy = true
	app.mu.Unlock()
	if _, err := app.prepareExternalSessionDisplay(context.Background(), request); err == nil {
		t.Fatal("busy local transition accepted")
	}
	if display.ReadinessDiagnostic().Paused {
		t.Fatal("rejected request paused the display")
	}
}

func TestExternalDisplayRequiresFreshPresentedFrame(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !display.ReadinessDiagnostic().Paused {
		t.Fatal("prepare did not pause display")
	}
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := finish(ctx, sessiondisplayipc.Applied); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ack without a frame = %v", err)
	}
	app.mu.Lock()
	uncertain := app.sessionDisplayUncertain
	app.mu.Unlock()
	if !uncertain {
		t.Fatal("unpresented applied display was not marked uncertain")
	}
}

func TestExternalDisplayWaitsForNewPresentedSequence(t *testing.T) {
	presenter := &delayedDisplayPresenter{}
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: strings.Repeat("a", 64), CoreGeneration: 9, Generation: 4})
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	defer display.Close()
	display.SetChangeDriven(true)
	display.BindSession(strings.Repeat("a", 64), 9)
	display.Present()
	deadline := time.Now().Add(time.Second)
	for !display.PresentedSequence(display.QueuedSeq()) {
		if time.Now().After(deadline) {
			t.Fatal("first frame did not present")
		}
		time.Sleep(time.Millisecond)
	}
	presenter.mu2.Lock()
	presenter.entered, presenter.gate, presenter.fail = make(chan struct{}), make(chan struct{}), true
	entered, gate := presenter.entered, presenter.gate
	presenter.mu2.Unlock()
	display.Framebuffer().Pix[0] ^= 1
	display.Present()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	waiter := &App{displayFrameRequest: 1, displayFrameSeq: display.QueuedSeq()}
	go func() { done <- waiter.waitSubmittedDisplayFrame(ctx, display, 1) }()
	select {
	case err := <-done:
		t.Fatalf("acknowledged queued frame before presentation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(gate)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failed frame acknowledged: %v", err)
	}
}

func TestExternalDisplayOpensAndReturnsAfterPresentation(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- finish(ctx, sessiondisplayipc.Applied) }()
	for !app.Snapshot().Session.ControlsOpen {
		select {
		case <-ctx.Done():
			t.Fatal("launcher room did not open")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case err := <-done:
		t.Fatalf("acknowledged before presentation: %v", err)
	default:
	}
	snap := app.Snapshot()
	display.ForceNextFrame()
	display.Present()
	app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("presented frame was not acknowledged")
	}
	request.Display = protocol.SessionDisplayClosed
	finish, err = app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	presenter.set(menudisplay.Status{Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation})
	if err := finish(ctx, sessiondisplayipc.Applied); err != nil {
		t.Fatal(err)
	}
	if app.Snapshot().Session.ControlsOpen {
		t.Fatal("close kept launcher controls open")
	}
}

func TestExternalDisplayRejectedRestoresOldBinding(t *testing.T) {
	app, display, _, request := externalDisplayFixture(t)
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = finish(context.Background(), sessiondisplayipc.Rejected); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	got, busy, uncertain := app.sessionDisplayLocked(), app.sessionDisplayBusy, app.sessionDisplayUncertain
	app.mu.Unlock()
	if got != protocol.SessionDisplayClosed || busy || uncertain || display.ReadinessDiagnostic().Paused {
		t.Fatal("rejection did not restore the previous launcher state")
	}
}

func TestExternalDisplayUnknownAllowsOnlyBoundCloseRecovery(t *testing.T) {
	app, _, _, request := externalDisplayFixture(t)
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(context.Background(), sessiondisplayipc.Unknown); err == nil {
		t.Fatal("unknown outcome acknowledged")
	}
	if _, err := app.prepareExternalSessionDisplay(context.Background(), request); err == nil {
		t.Fatal("uncertain show admitted")
	}
	request.Display = protocol.SessionDisplayClosed
	finish, err = app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatalf("close recovery rejected: %v", err)
	}
	if err := finish(context.Background(), sessiondisplayipc.Applied); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.sessionDisplayUncertain || app.sessionDisplay != protocol.SessionDisplayClosed {
		t.Fatal("close did not clear uncertainty")
	}
}

func TestExternalDisplayRejectsShowDuringShutdown(t *testing.T) {
	app, _, _, request := externalDisplayFixture(t)
	app.mu.Lock()
	app.displayShuttingDown = true
	app.displayShutdownClose = true
	app.mu.Unlock()
	if _, err := app.prepareExternalSessionDisplay(context.Background(), request); err == nil {
		t.Fatal("show admitted during shutdown")
	}
	request.Display = protocol.SessionDisplayClosed
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatalf("shutdown close rejected: %v", err)
	}
	if err := finish(context.Background(), sessiondisplayipc.Rejected); err != nil {
		t.Fatal(err)
	}
}

func TestExternalDisplayStopBeforePresentedFrameFailsClosed(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	finish, err := app.prepareExternalSessionDisplay(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- finish(ctx, sessiondisplayipc.Applied) }()
	for app.Snapshot().displayFrameRequest == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("frame not requested")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	snap := app.Snapshot()
	app.mu.Lock()
	app.stopPhase = "stopping"
	app.mu.Unlock()
	display.ForceNextFrame()
	display.Present()
	app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
	if err := <-done; err == nil {
		t.Fatal("acknowledged after stop started")
	}
	app.mu.Lock()
	app.stopPhase = ""
	uncertain := app.sessionDisplayUncertain
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.sessionDisplayUncertain = false
	app.mu.Unlock()
	if !uncertain {
		t.Fatal("late stop did not mark display uncertain")
	}
}

func TestExternalSurroundOverrideYieldsToSetting(t *testing.T) {
	app, _, _, _ := externalDisplayFixture(t)
	app.mu.Lock()
	defer app.mu.Unlock()
	shown := true
	app.displaySurroundOverride = &shown
	if got := app.sessionReturnDisplayLocked(); got != protocol.SessionDisplayShown {
		t.Fatalf("external show lost: %v", got)
	}
	app.setSurroundLocked(false, false)
	if got := app.sessionReturnDisplayLocked(); got != protocol.SessionDisplayClosed {
		t.Fatalf("explicit setting lost: %v", got)
	}
}

func TestStartupDisplayRemainsFencedUntilExactFrame(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	display.BindSession(request.PackageID, request.Generation)
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayFocused
	app.sessionDisplayBinding = app.session
	app.startupFrameVerify = func(context.Context) error { return nil }
	app.mu.Unlock()
	app.mu.Lock()
	app.fenceStartupFrameLocked()
	app.mu.Unlock()
	if blocked, err := app.checkStartupFrame(display, false); err != nil || !blocked {
		t.Fatal("startup accepted without frame")
	}
	snap := app.Snapshot()
	display.ForceNextFrame()
	display.Present()
	app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
	deadline := time.Now().Add(time.Second)
	for {
		blocked, err := app.checkStartupFrame(display, false)
		if err != nil {
			t.Fatal(err)
		}
		if !blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("presented startup frame did not release fence")
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.mu.Unlock()
}

func TestPresentedFrameFenceSurvivesLaterFrame(t *testing.T) {
	presenter := &countingSessionMenu{}
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: strings.Repeat("a", 64), CoreGeneration: 9, Generation: 4})
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	defer display.Close()
	display.SetChangeDriven(true)
	display.BindSession(strings.Repeat("a", 64), 9)
	display.Present()
	first := display.QueuedSeq()
	deadline := time.Now().Add(time.Second)
	for !display.PresentedSequence(first) {
		if time.Now().After(deadline) {
			t.Fatal("first frame not presented")
		}
		time.Sleep(time.Millisecond)
	}
	display.Framebuffer().Pix[0] ^= 1
	display.Present()
	second := display.QueuedSeq()
	for !display.PresentedSequence(second) {
		if time.Now().After(deadline) {
			t.Fatal("second frame not presented")
		}
		time.Sleep(time.Millisecond)
	}
	if !display.PresentedSequence(first) {
		t.Fatal("superseded successful frame was forgotten")
	}
	display.BindSession(strings.Repeat("a", 64), 10)
	if display.PresentedSequence(first) {
		t.Fatal("old binding frame survived rebind")
	}
}

func TestStartupDisplayWaitsForIdentityRecheck(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	display.BindSession(request.PackageID, request.Generation)
	entered, release := make(chan struct{}), make(chan struct{})
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayFocused
	app.sessionDisplayBinding = app.session
	app.startupFrameVerify = func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	app.fenceStartupFrameLocked()
	app.mu.Unlock()
	snap := app.Snapshot()
	display.ForceNextFrame()
	display.Present()
	app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
	deadline := time.Now().Add(time.Second)
	for {
		_, err := app.checkStartupFrame(display, false)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
			goto waiting
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("identity check did not start")
		}
		time.Sleep(time.Millisecond)
	}
waiting:
	if blocked, err := app.checkStartupFrame(display, false); err != nil || !blocked {
		t.Fatal("input released before identity recheck")
	}
	close(release)
	for {
		blocked, err := app.checkStartupFrame(display, false)
		if err != nil {
			t.Fatal(err)
		}
		if !blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("identity recheck did not release fence")
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.mu.Unlock()
}

func TestStartupDisplayIdentityFailureRequiresFreshReconciliation(t *testing.T) {
	app, display, presenter, request := externalDisplayFixture(t)
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: request.PackageID, CoreGeneration: request.Generation, Generation: 4})
	display.BindSession(request.PackageID, request.Generation)
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayFocused
	app.sessionDisplayBinding = app.session
	app.startupFrameVerify = func(context.Context) error { return errors.New("runtime replaced") }
	app.fenceStartupFrameLocked()
	app.mu.Unlock()
	snap := app.Snapshot()
	display.ForceNextFrame()
	display.Present()
	app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
	deadline := time.Now().Add(time.Second)
	for {
		_, err := app.checkStartupFrame(display, true)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failed identity recheck did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	uncertain := app.sessionDisplayUncertain
	app.sessionDisplayUncertain = false
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.mu.Unlock()
	if !uncertain {
		t.Fatal("identity failure not marked uncertain")
	}
}
