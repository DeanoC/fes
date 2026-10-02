package tenfoot

import (
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func (a *App) coreOwnsPadInput() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localCoreBusyLocked() || a.needsSessionDisplayLocked() && a.session.State == "active" && (!a.roomDuringPlay || a.sessionDisplayBusy)
}

func (a *App) suppressHeldSessionPadLocked() {
	if a.playPadSuppressed == nil {
		a.playPadSuppressed = make(map[remoteinput.Code]bool)
	}
	for code := range a.playPadHeld {
		a.playPadSuppressed[code] = true
	}
}

// sessionPadLocked reuses the existing kit-local input source for a library
// session. Select opens controls on release, leaving Select+Start first
// priority while both are held. Navigation never reaches the machine.
func (a *App) sessionPadLocked(e remoteinput.Event, now time.Time) (bool, []remoteinput.Event) {
	if !a.needsSessionDisplayLocked() || a.session.State != "active" {
		return false, nil
	}
	if a.playPadHeld == nil {
		a.playPadHeld = make(map[remoteinput.Code]remoteinput.Event)
	}
	down := e.Action == remoteinput.ActionPress || e.Kind == remoteinput.KindAxis && e.Value != 0
	if down {
		a.playPadHeld[e.Code] = e
	} else {
		delete(a.playPadHeld, e.Code)
	}
	// A failed Stop retains the display binding. B may return that plane
	// while the retry-Stop lock continues to block other controller input.
	if a.sessionDisplayUncertain && !a.sessionDisplayBusy && a.stopPhase != "stopping" && !a.playHIDFailClosedLocked() &&
		e.Code == remoteinput.ButtonB && down && !a.playPadSuppressed[e.Code] {
		a.beginSessionDisplayLocked(false, false)
		return true, nil
	}
	if a.playHIDFailClosedLocked() || a.stopPhase == "stopping" || a.retryStopLock {
		a.playPadChordSince = time.Time{}
		a.suppressHeldSessionPadLocked()
		return true, nil
	}
	pair := e.Kind == remoteinput.KindButton && (e.Code == remoteinput.ButtonSelect || e.Code == remoteinput.ButtonStart)
	if pair {
		wasChord := !a.playPadChordSince.IsZero() || a.playPadChordFired
		if e.Code == remoteinput.ButtonSelect {
			a.playPadSelectDown = down
		} else {
			a.playPadStartDown = down
		}
		if a.playPadSelectDown && a.playPadStartDown {
			if a.playPadChordSince.IsZero() {
				a.playPadChordSince = now
			}
			a.suppressHeldSessionPadLocked()
			// Start may already have reached the core before Select. Release
			// it now, and never replay a swallowed chord press.
			return true, []remoteinput.Event{
				{Player: e.Player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonStart, Action: remoteinput.ActionRelease},
				{Player: e.Player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonSelect, Action: remoteinput.ActionRelease},
			}
		}
		a.playPadChordSince = time.Time{}
		if !a.playPadSelectDown && !a.playPadStartDown {
			a.playPadChordFired = false
		}
		if wasChord {
			if !down {
				delete(a.playPadSuppressed, e.Code)
			}
			return true, nil
		}
		if e.Code == remoteinput.ButtonSelect {
			if !a.sessionDisplayOfferedLocked() {
				if a.playPadSuppressed[e.Code] {
					if !down {
						delete(a.playPadSuppressed, e.Code)
					}
					return true, nil
				}
				return true, []remoteinput.Event{e}
			}
			if !down && !a.playPadSuppressed[e.Code] && !a.sessionDisplayBusy && a.stopPhase != "stopping" {
				if a.roomDuringPlay {
					a.resumeRoomSessionLocked()
				} else {
					a.openPlayingHardwareRoomLocked()
				}
			}
			if !down {
				delete(a.playPadSuppressed, e.Code)
			}
			return true, nil
		}
	}
	if a.playPadSuppressed[e.Code] {
		if !down {
			delete(a.playPadSuppressed, e.Code)
		}
		return true, nil
	}
	if a.sessionDisplayBusy || a.sessionDisplayUncertain || a.stopPhase == "stopping" || a.retryStopLock || a.playHIDFailClosedLocked() {
		if down {
			if a.playPadSuppressed == nil {
				a.playPadSuppressed = make(map[remoteinput.Code]bool)
			}
			a.playPadSuppressed[e.Code] = true
		}
		return true, nil
	}
	if a.roomDuringPlay {
		// Start remains chord-only on the kit, never the menu's Quit.
		return e.Code == remoteinput.ButtonStart, nil
	}
	return true, []remoteinput.Event{e}
}

func (a *App) tickSessionPadLocked(now time.Time) {
	if a.session.State != "active" || a.playPadChordFired || a.playPadChordSince.IsZero() || now.Sub(a.playPadChordSince) < localChordHold {
		return
	}
	a.playPadChordFired = true
	a.startStopLocked()
}
