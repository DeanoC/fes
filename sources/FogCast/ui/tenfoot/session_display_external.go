package tenfoot

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
)

// beginCoordinatedSessionDisplayLocked sends a normal host request without
// holding the launcher fence; the target callback owns that fence for both
// local and externally initiated requests.
func (a *App) beginCoordinatedSessionDisplayLocked(target protocol.SessionDisplay, openTape bool) {
	prior, client, initialEpoch := a.session, a.client, a.sessionDisplayEpoch
	a.displayOutbound = true
	done := make(chan struct{})
	a.displayOutboundWait = done
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		result, err := client.SetSessionDisplayForSession(ctx, prior, target)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.displayOutbound = false
		a.displayOutboundWait = nil
		if !samePlayHIDSession(a.session, prior) {
			return
		}
		if err == nil && (a.sessionDisplayEpoch == initialEpoch || a.sessionDisplay != target || a.sessionDisplayUncertain) {
			// An older agent can return success without ever participating in the
			// local fence. Its physical outcome is not safe to assume.
			a.sessionDisplayUncertain = true
			err = errors.New("target did not confirm launcher display coordination")
		}
		if err != nil {
			a.status = "HDMI controls unavailable: " + err.Error()
			a.roomSessionNotice = a.status
			a.kickSessionPollLocked()
			return
		}
		a.applySessionLocked(result)
		if openTape && target == protocol.SessionDisplayFocused && a.roomDuringPlay && !a.tapePickerOpen {
			a.beginColecoCartridgeControlsLocked()
			a.openTapePickerLocked()
		}
		a.kickSessionPollLocked()
	}()
}

// prepareExternalSessionDisplay lets a target-originated display request use
// the same presentation and input fence as a Home/Back transition. The
// target waits for this preparation before changing the physical plane.
func (a *App) prepareExternalSessionDisplay(ctx context.Context, request sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
	a.mu.Lock()
	prior := a.session
	if ctx.Err() != nil || (a.displayShuttingDown && (!a.displayShutdownClose || request.Display != protocol.SessionDisplayClosed)) || a.menuDisplay == nil || a.localCores == nil || a.sessionDisplayBusy || (a.sessionDisplayUncertain && request.Display != protocol.SessionDisplayClosed) ||
		a.stopPhase == "stopping" || a.retryStopLock || a.playHIDFailClosedLocked() || prior.State != "active" ||
		prior.CorePackage == nil || prior.CorePackage.PackageID != request.PackageID || prior.CorePackage.Generation != request.Generation ||
		prior.TargetID == "" || prior.TargetID != request.TargetID || (a.kitID != "" && a.kitID != request.TargetID) || !request.Display.Valid() {
		a.mu.Unlock()
		return nil, errors.New("launcher display binding is unavailable")
	}
	from := a.sessionDisplayLocked()
	if request.Display == protocol.SessionDisplayFocused && from != request.Display && !a.playingHardwareRoomAvailableLocked() {
		a.mu.Unlock()
		return nil, errors.New("launcher controls are unavailable")
	}
	handoff := from.Focus != request.Display.Focus || a.sessionDisplayUncertain
	rebind := from.Visible != request.Display.Visible || a.sessionDisplayUncertain
	if handoff {
		a.releasePlayHIDLocked()
		a.suppressHeldPlayKeysLocked()
		a.suppressHeldSessionPadLocked()
		a.repeat.Clear()
		a.hold.Clear()
	}
	a.sessionDisplayEpoch++
	epoch := a.sessionDisplayEpoch
	a.sessionDisplayBusy = true
	a.sessionDisplayNonHandoff = !handoff && from.Focus == protocol.SessionFocusMachine && request.Display.Focus == protocol.SessionFocusMachine
	a.sessionDisplayBinding = prior
	done := make(chan struct{})
	a.sessionDisplayWait = done
	display, tail, feed := a.menuDisplay, a.playHIDTail, a.localFeed
	a.bumpSessionGenLocked()
	a.mu.Unlock()

	abort := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if epoch == a.sessionDisplayEpoch && samePlayHIDSession(a.session, prior) {
			a.sessionDisplayBusy = false
			a.sessionDisplayNonHandoff = false
			a.sessionDisplayWait = nil
			a.bumpSessionGenLocked()
			if rebind {
				display.Resume()
			}
		}
		close(done)
	}
	if rebind {
		if err := display.Pause(ctx); err != nil {
			abort()
			return nil, err
		}
	}
	if handoff {
		if tail != nil {
			select {
			case <-tail:
			case <-ctx.Done():
				abort()
				return nil, ctx.Err()
			}
		}
		if feed != nil {
			feed.Close()
		}
	}
	return func(finishCtx context.Context, outcome sessiondisplayipc.Outcome) error {
		defer close(done)
		a.mu.Lock()
		if epoch != a.sessionDisplayEpoch || !samePlayHIDSession(a.session, prior) {
			a.mu.Unlock()
			return errors.New("running machine changed during display transition")
		}
		a.sessionDisplayBusy = false
		a.sessionDisplayNonHandoff = false
		a.sessionDisplayWait = nil
		a.bumpSessionGenLocked()
		if outcome != sessiondisplayipc.Applied {
			if rebind {
				display.Resume()
			}
			if outcome == sessiondisplayipc.Unknown {
				a.sessionDisplayUncertain = true
			}
			a.mu.Unlock()
			if outcome == sessiondisplayipc.Unknown {
				return errors.New("physical display result is unknown")
			}
			return nil
		}
		a.sessionDisplay = request.Display
		if request.Display.Focus == protocol.SessionFocusMachine {
			visible := request.Display.Visible
			a.displaySurroundOverride = &visible
		}
		a.sessionDisplayUncertain = false
		if rebind {
			if request.Display.Visible {
				display.BindSession(prior.CorePackage.PackageID, prior.CorePackage.Generation)
			} else {
				display.ClearSessionBinding()
			}
			display.Resume()
		}
		roomAlreadyOpen := a.roomDuringPlay
		if request.Display == protocol.SessionDisplayFocused && !a.roomDuringPlay {
			a.openPlayingHardwareRoomLocked()
			if !a.roomDuringPlay {
				a.sessionDisplayUncertain = true
				a.mu.Unlock()
				return errors.New("launcher room could not open")
			}
		} else if handoff && request.Display.Focus == protocol.SessionFocusMachine {
			a.finishRoomSessionResumeLocked()
		}
		// The rendering loop acknowledges only a frame rendered from a
		// snapshot taken after this state has been installed.
		a.displayFrameRequest++
		frameRequest := a.displayFrameRequest
		a.displayFrameSeq = 0
		alreadyRendered := from == request.Display && (request.Display != protocol.SessionDisplayFocused || roomAlreadyOpen) && display.ReadinessDiagnostic().Ready
		// Keep Stop and competing transitions fenced until the required frame
		// actually presents. Room opening above requires the busy bit clear.
		a.sessionDisplayBusy = true
		a.sessionDisplayWait = done
		a.mu.Unlock()
		if request.Display.Visible && !alreadyRendered {
			if err := a.waitSubmittedDisplayFrame(finishCtx, display, frameRequest); err != nil {
				a.mu.Lock()
				if samePlayHIDSession(a.session, prior) && a.sessionDisplayEpoch == epoch {
					a.sessionDisplayBusy = false
					a.sessionDisplayWait = nil
					a.sessionDisplayUncertain = true
				}
				a.mu.Unlock()
				return err
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if !samePlayHIDSession(a.session, prior) || a.sessionDisplayEpoch != epoch || a.sessionDisplayLocked() != request.Display {
			return errors.New("running machine changed before display acknowledgement")
		}
		if a.stopPhase == "stopping" || a.retryStopLock {
			a.sessionDisplayBusy = false
			a.sessionDisplayWait = nil
			a.sessionDisplayUncertain = true
			return errors.New("machine stop started before display acknowledgement")
		}
		a.sessionDisplayBusy = false
		a.sessionDisplayWait = nil
		return nil
	}, nil
}

// fenceStartupFrame prevents input and new display transitions until the
// already-visible runtime plane contains the adopted launcher state.
func (a *App) fenceStartupFrameLocked() {
	if !a.sessionDisplay.Visible {
		return
	}
	a.sessionDisplayBusy = true
	a.displayStartupFenced = true
	a.displayFrameRequest++
	a.displayFrameSeq = 0
}

func (a *App) abortStartupFrame() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.displayStartupFenced {
		a.displayStartupFenced = false
		a.sessionDisplayBusy = false
		a.sessionDisplayUncertain = true
	}
}

func (a *App) checkStartupFrame(dev gfx.Device, expired bool) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.displayStartupFenced {
		return false, nil
	}
	display, ok := dev.(*gfx.MenuDisplay)
	presented := ok && a.displayFrameSeq != 0 && display.PresentedSequence(a.displayFrameSeq)
	if presented {
		if a.startupFrameVerify == nil {
			return true, errors.New("startup display identity verifier unavailable")
		}
		if a.startupFrameCheck != nil {
			select {
			case err := <-a.startupFrameCheck:
				a.startupFrameCheck = nil
				if err == nil && samePlayHIDSession(a.session, a.sessionDisplayBinding) && display.PresentedSequence(a.displayFrameSeq) {
					a.displayStartupFenced = false
					a.sessionDisplayBusy = false
					return false, nil
				}
				if expired {
					a.displayStartupFenced = false
					a.sessionDisplayBusy = false
					a.sessionDisplayUncertain = true
					return true, errors.New("startup session display identity could not be confirmed")
				}
				a.startupFrameNextCheck = time.Now().Add(250 * time.Millisecond)
			default:
			}
		} else if !time.Now().Before(a.startupFrameNextCheck) {
			verify, parent := a.startupFrameVerify, a.ctx
			if parent == nil {
				parent = context.Background()
			}
			result := make(chan error, 1)
			a.startupFrameCheck = result
			go func() {
				ctx, cancel := context.WithTimeout(parent, 5*time.Second)
				defer cancel()
				result <- verify(ctx)
			}()
		}
		return true, nil
	}
	if expired {
		a.displayStartupFenced = false
		a.sessionDisplayBusy = false
		a.sessionDisplayUncertain = true
		return true, errors.New("startup session display frame was not presented")
	}
	return true, nil
}

func (a *App) pendingDisplayFrameLocked() uint64 {
	if a.sessionDisplayBusy && a.displayFrameSeq == 0 {
		return a.displayFrameRequest
	}
	return 0
}

func (a *App) submittedDisplayFrame(request, seq uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessionDisplayBusy && a.displayFrameRequest == request && a.displayFrameSeq == 0 {
		a.displayFrameSeq = seq
	}
}

func (a *App) waitSubmittedDisplayFrame(ctx context.Context, display *gfx.MenuDisplay, request uint64) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		a.mu.Lock()
		seq := a.displayFrameSeq
		valid := a.displayFrameRequest == request
		a.mu.Unlock()
		if !valid {
			return errors.New("display frame request was replaced")
		}
		if seq != 0 && display.PresentedSequence(seq) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
