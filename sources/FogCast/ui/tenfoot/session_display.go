package tenfoot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
)

// SetMenuDisplay connects physical presentation to the session controls. The
// host window keeps its existing independent display.
func (a *App) SetMenuDisplay(display *gfx.MenuDisplay) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuDisplay = display
}

// Transition copy for the kit HDMI plane. It describes a pending open or
// return of one session and is cleared when that session ends.
const (
	sessionDisplayOpeningStatus   = "Opening HDMI controls…"
	sessionDisplayReturningStatus = "Returning to play…"
)

func (a *App) needsSessionDisplayLocked() bool {
	return a.menuDisplay != nil || a.localCores != nil
}

func (a *App) sessionDisplayOfferedLocked() bool {
	return !a.needsSessionDisplayLocked() || hostclient.SessionDisplayCapable(a.session.CorePackage)
}

// liveControlsOfferedLocked is the kit gate for the hardware room and tape
// picker. On a menu-display kit the running package must report the
// session-display plane, including fes.computer ZX81. A host window draws
// those controls on its own display.
func (a *App) liveControlsOfferedLocked() bool {
	return a.sessionDisplayOfferedLocked()
}

// kitSessionDisplayPlaneLocked is the POST /api/v1/session/display transition.
// Opening it makes menu presentation available and binds launcher frames.
// Closing it restores the core's HDMI pixels.
func (a *App) kitSessionDisplayPlaneLocked() bool {
	return a.needsSessionDisplayLocked()
}

func (a *App) suppressHeldPlayKeysLocked() {
	if a.playKeySuppressed == nil {
		a.playKeySuppressed = make(map[string]bool)
	}
	for key := range a.playKeyHeld {
		a.playKeySuppressed[key] = true
	}
}

// playHeldKey names one physical key in playKeyHeld and playKeySuppressed: its
// HID usage, or its name when the caller has no usage. Keys such as F1 and the
// keypad have no play-keyboard name, and the left and right modifiers share
// one. The name does not depend on the session, so a key pressed before a
// fes.keyboard.hid session starts or ends still matches its release.
func playHeldKey(name string, usage uint8) string {
	if usage != 0 {
		return fmt.Sprintf("hid:%02x", usage)
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// Every physical key passes here, including room navigation. A held key must
// be released before either the room or the machine can see another press.
// usage is the key's HID usage, or 0 where the caller has only a name.
func (a *App) consumeSuppressedPlayKey(name string, usage uint8, down bool) bool {
	if a == nil {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	a.mu.Lock()
	defer a.mu.Unlock()
	key := playHeldKey(name, usage)
	if a.playKeyHeld == nil {
		a.playKeyHeld = make(map[string]bool)
	}
	if down {
		a.playKeyHeld[key] = true
	} else {
		delete(a.playKeyHeld, key)
	}
	suppressed := a.playKeySuppressed[key]
	if !down {
		delete(a.playKeySuppressed, key)
	}
	if a.sessionDisplayBusy || a.sessionDisplayUncertain && name != "home" && name != "escape" && name != "backspace" {
		if down {
			if a.playKeySuppressed == nil {
				a.playKeySuppressed = make(map[string]bool)
			}
			a.playKeySuppressed[key] = true
		}
		return true
	}
	return suppressed
}

// sessionDisplayLocked is the confirmed plane state. A session starts closed.
func (a *App) sessionDisplayLocked() protocol.SessionDisplay {
	if !a.sessionDisplay.Valid() {
		return protocol.SessionDisplayClosed
	}
	return a.sessionDisplay
}

// launcherDisplayFocusedLocked reports a plane on which the launcher holds input.
func (a *App) launcherDisplayFocusedLocked() bool {
	return a.sessionDisplay == protocol.SessionDisplayFocused
}

// surroundOfferedLocked keeps the plane shown around a running machine on the
// kit's HDMI while the machine keeps input. A surround that fails stays off
// for the rest of that session.
func (a *App) surroundOfferedLocked() bool {
	return a.surround && !a.surroundFailed && a.menuDisplay != nil && a.session.State == "active" &&
		hostclient.SessionDisplayCapable(a.session.CorePackage)
}

// sessionReturnDisplayLocked gives input back to the machine: the surround
// when it is offered, otherwise machine pixels only.
func (a *App) sessionReturnDisplayLocked() protocol.SessionDisplay {
	if a.surroundOfferedLocked() {
		return protocol.SessionDisplayShown
	}
	return protocol.SessionDisplayClosed
}

// syncSurroundLocked shows or closes the surround while the machine has
// input. Launcher transitions, an uncertain plane and Stop take precedence.
func (a *App) syncSurroundLocked() {
	if a.menuDisplay == nil || a.session.State != "active" || a.sessionDisplayBusy || a.sessionDisplayUncertain ||
		a.roomDuringPlay || a.launcherDisplayFocusedLocked() || a.stopPhase == "stopping" || a.retryStopLock || a.localPresentsPaused {
		return
	}
	if want := a.sessionReturnDisplayLocked(); want != a.sessionDisplayLocked() {
		a.beginSessionDisplayLocked(want, false)
	}
}

// beginSessionDisplayLocked moves the kit plane to target for the captured
// session. Moving focus hands held input over; showing or closing the
// surround under the machine's own input leaves its input alone.
func (a *App) beginSessionDisplayLocked(target protocol.SessionDisplay, openTape bool) {
	if a.sessionDisplayBusy || a.stopPhase == "stopping" || a.client == nil || a.session.State != "active" || !hostclient.SessionDisplayCapable(a.session.CorePackage) || a.playHIDFailClosedLocked() {
		return
	}
	from := a.sessionDisplayLocked()
	// An uncertain plane may hold launcher focus and either binding.
	handoff := from.Focus != target.Focus || a.sessionDisplayUncertain
	rebind := from.Visible != target.Visible || a.sessionDisplayUncertain
	if handoff {
		a.releasePlayHIDLocked()
		a.suppressHeldPlayKeysLocked()
		a.suppressHeldSessionPadLocked()
		a.repeat.Clear()
		a.hold.Clear()
	}
	a.sessionDisplayEpoch++
	a.sessionDisplayBusy = true
	prior, epoch := a.session, a.sessionDisplayEpoch
	done := make(chan struct{})
	a.sessionDisplayWait = done
	a.sessionDisplayBinding = prior
	display, tail, feed := a.menuDisplay, a.playHIDTail, a.localFeed
	if !rebind {
		display = nil
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	switch {
	case target == protocol.SessionDisplayFocused:
		a.status = sessionDisplayOpeningStatus
	case handoff:
		a.status = sessionDisplayReturningStatus
	}
	a.bumpSessionGenLocked()
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		var err error
		if display != nil {
			err = display.Pause(ctx)
		}
		if handoff {
			if err == nil && tail != nil {
				select {
				case <-tail:
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
			if feed != nil {
				// Disconnecting releases just this local source in the existing
				// agent input controller, including buttons held before Home.
				feed.Close()
			}
		}
		var result hostclient.SessionResult
		if err == nil {
			result, err = a.client.SetSessionDisplayForSession(ctx, prior, target)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if epoch != a.sessionDisplayEpoch || !samePlayHIDSession(a.session, prior) {
			return
		}
		a.sessionDisplayBusy = false
		a.sessionDisplayWait = nil
		a.bumpSessionGenLocked()
		if err != nil {
			if display != nil {
				display.Resume()
			}
			if target == protocol.SessionDisplayShown {
				// An unconfirmed request can leave input fenced at the kit,
				// so the surround stays off for this session and the plane
				// closes, which also ends a return from the hardware room.
				// Until a close is confirmed the plane is uncertain: Back
				// retries the close if this one fails too.
				a.surroundFailed = true
				a.sessionDisplayUncertain = true
				a.beginSessionDisplayLocked(protocol.SessionDisplayClosed, false)
				a.status = fmt.Sprintf("TV surround unavailable: %s", err)
				return
			}
			if target.Visible {
				a.sessionDisplayUncertain = true
			}
			a.status = fmt.Sprintf("HDMI controls unavailable: %s. Back returns to play; Home retries.", err)
			a.roomSessionNotice = a.status
			a.kickSessionPollLocked()
			return
		}
		a.applySessionLocked(result)
		a.sessionDisplay = target
		a.sessionDisplayUncertain = false
		a.roomSessionNotice = ""
		if display != nil {
			if target.Visible {
				display.BindSession(prior.CorePackage.PackageID, prior.CorePackage.Generation)
			} else {
				display.ClearSessionBinding()
			}
			display.Resume()
		}
		if target == protocol.SessionDisplayFocused {
			if a.stopPhase == "stopping" || a.retryStopLock {
				// A late open must keep the guarded return available if Stop
				// fails, without reopening the room or issuing another mutation.
				a.sessionDisplayUncertain = true
				a.kickSessionPollLocked()
				return
			}
			if !(openTape && a.beginColecoCartridgeControlsLocked()) {
				a.openPlayingHardwareRoomLocked()
			}
			if !a.roomDuringPlay {
				a.beginSessionDisplayLocked(a.sessionReturnDisplayLocked(), false)
				return
			}
			if openTape && !a.tapePickerOpen {
				a.openTapePickerLocked()
			}
		} else if handoff {
			if a.status == sessionDisplayReturningStatus {
				a.status = ""
			}
			a.finishRoomSessionResumeLocked()
		}
		a.kickSessionPollLocked()
	}()
}

func (a *App) invalidateSessionDisplayLocked() {
	a.sessionDisplayEpoch++
	a.sessionDisplayBusy = false
	a.sessionDisplay = protocol.SessionDisplayClosed
	a.surroundFailed = false
	a.sessionDisplayUncertain = false
	a.sessionDisplayBinding = hostclient.SessionResult{}
	a.roomDuringPlay = false
	// The notice describes this session's plane, which no longer exists.
	if a.roomSessionNotice == sessionDisplayUnavailableNotice {
		a.roomSessionNotice = ""
	}
	a.mediaControlsDirect = false
	a.playPadChordSince = time.Time{}
	a.playPadChordFired = false
	// Releases outside a session are not tracked, so a pair held now
	// counts only when it is pressed again.
	a.playPadPair = chordPairs{}
	a.suppressHeldPlayKeysLocked()
	a.suppressHeldSessionPadLocked()
	// The epoch bump drops any pending open or return, including one ended
	// by a host-side stop, so its copy must not outlive the session.
	switch a.status {
	case sessionDisplayOpeningStatus, sessionDisplayReturningStatus, playingHardwareRoomStatus:
		a.status = ""
	}
	if a.menuDisplay != nil {
		a.menuDisplay.ClearSessionBinding()
		a.menuDisplay.Resume()
	}
}

// Shell exit returns the captured HDMI plane without stopping the machine.
func (a *App) returnSessionDisplayOnExit() {
	a.mu.Lock()
	visible, prior, display, client := a.sessionDisplay.Visible || a.sessionDisplayUncertain, a.sessionDisplayBinding, a.menuDisplay, a.client
	a.mu.Unlock()
	if !visible || client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if display != nil {
		if err := display.Pause(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "tenfoot: returning HDMI on exit: %v\n", err)
			return
		}
	}
	if _, err := client.SetSessionDisplayForSession(ctx, prior, protocol.SessionDisplayClosed); err != nil {
		fmt.Fprintf(os.Stderr, "tenfoot: returning HDMI on exit: %v\n", err)
	}
}
