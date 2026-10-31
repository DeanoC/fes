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

// sessionKeypadLocked is a running machine with keypad ports, such as the
// Coleco. Its Select is the keypad * key, so Select stays with the machine.
// Home/F12 opens the Coleco title room; Guide opens cartridge media controls.
func (a *App) sessionKeypadLocked() bool {
	p := a.session.CorePackage
	if p == nil {
		return false
	}
	ports, keypad := false, false
	for _, i := range p.ActiveInterfaces {
		if i.Major == 1 && i.Minor == 0 {
			ports = ports || i.ID == "fes.gamepad.ports"
			keypad = keypad || i.ID == "fes.keypad.ports"
		}
	}
	return ports && keypad
}

// sessionMenuOwnsPadInputLocked reports that launcher controls currently own
// the display and should receive Select instead of a keypad machine.
func (a *App) sessionMenuOwnsPadInputLocked() bool {
	return a.roomDuringPlay || a.launcherDisplayFocusedLocked() || a.roomPickerOpen || a.firmwarePickerOpen || a.tapePickerOpen ||
		a.settingsOpen || a.settingsOSKOpenLocked() || a.searchOpen || a.nameEntryOpenLocked() || a.detailOpen || a.viewPickerOpen ||
		a.filtersOpen || a.collectionManageOpen || a.collectionConfirmOpen || a.coreLibrary.Open
}

// localPadCommand maps the Coleco keypad Select to menu confirmation only
// while launcher controls own input. During play it remains the keypad '*'.
func (a *App) localPadCommand(e remoteinput.Event) Command {
	if a != nil && e.Kind == remoteinput.KindButton && e.Code == remoteinput.ButtonSelect {
		a.mu.Lock()
		menuSelect := a.sessionKeypadLocked() && a.sessionMenuOwnsPadInputLocked()
		a.mu.Unlock()
		if menuSelect {
			return CmdSelect
		}
	}
	return CommandFromLogical(e)
}

func (a *App) suppressHeldSessionPadLocked() {
	if a.playPadSuppressed == nil {
		a.playPadSuppressed = make(map[padKey]bool)
	}
	for key := range a.playPadHeld {
		a.playPadSuppressed[key] = true
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
		a.playPadHeld = make(map[padKey]remoteinput.Event)
	}
	key := padKey{e.Player, e.Code}
	down := e.Action == remoteinput.ActionPress || e.Kind == remoteinput.KindAxis && e.Value != 0
	if down {
		a.playPadHeld[key] = e
	} else {
		delete(a.playPadHeld, key)
	}
	// A failed Stop retains the display binding. B may return that plane
	// while the retry-Stop lock continues to block other controller input.
	if a.sessionDisplayUncertain && !a.sessionDisplayBusy && a.stopPhase != "stopping" && !a.playHIDFailClosedLocked() &&
		e.Code == remoteinput.ButtonB && down && !a.playPadSuppressed[key] {
		a.beginSessionDisplayLocked(a.sessionReturnDisplayLocked(), false)
		return true, nil
	}
	if a.playHIDFailClosedLocked() || a.stopPhase == "stopping" || a.retryStopLock {
		a.playPadChordSince = time.Time{}
		a.suppressHeldSessionPadLocked()
		if !down {
			delete(a.playPadSuppressed, key)
		}
		// Each pad's Select and Start still follow the hardware, so a later
		// lone press cannot complete a pair that was let go meanwhile.
		if e.Kind == remoteinput.KindButton {
			a.playPadPair.set(e.Player, e.Code, down)
		}
		return true, nil
	}
	pair := e.Kind == remoteinput.KindButton && (e.Code == remoteinput.ButtonSelect || e.Code == remoteinput.ButtonStart)
	if pair {
		wasChord := a.playPadPair.at(e.Player).held() || a.playPadChordFired
		if a.playPadPair.press(e.Player, e.Code, down, &a.playPadChordSince, now) {
			a.suppressPadLocked(e.Player)
			// Start may already have reached the core before Select. Release
			// it now, and never replay a swallowed chord press.
			return true, []remoteinput.Event{
				{Player: e.Player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonStart, Action: remoteinput.ActionRelease},
				{Player: e.Player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonSelect, Action: remoteinput.ActionRelease},
			}
		}
		if !a.playPadPair.anyDown() {
			a.playPadChordFired = false
		}
		if wasChord {
			if !down {
				delete(a.playPadSuppressed, key)
			}
			return true, nil
		}
		if e.Code == remoteinput.ButtonSelect {
			if a.sessionKeypadLocked() && a.sessionMenuOwnsPadInputLocked() {
				if a.playPadSuppressed[key] {
					if !down {
						delete(a.playPadSuppressed, key)
					}
					return true, nil
				}
				if !down {
					delete(a.playPadSuppressed, key)
				}
				return false, nil
			}
			if !a.liveControlsOfferedLocked() || a.sessionKeypadLocked() {
				if a.playPadSuppressed[key] {
					if !down {
						delete(a.playPadSuppressed, key)
					}
					return true, nil
				}
				return true, []remoteinput.Event{e}
			}
			if !down && !a.playPadSuppressed[key] && !a.sessionDisplayBusy && a.stopPhase != "stopping" {
				if a.roomDuringPlay {
					a.resumeRoomSessionLocked()
				} else {
					a.openPlayingHardwareRoomLocked()
				}
			}
			if !down {
				delete(a.playPadSuppressed, key)
			}
			return true, nil
		}
	}
	if a.playPadSuppressed[key] {
		if !down {
			delete(a.playPadSuppressed, key)
		}
		return true, nil
	}
	if a.sessionDisplayBusy || a.sessionDisplayUncertain || a.stopPhase == "stopping" || a.retryStopLock || a.playHIDFailClosedLocked() {
		if down {
			if a.playPadSuppressed == nil {
				a.playPadSuppressed = make(map[padKey]bool)
			}
			a.playPadSuppressed[key] = true
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
	// Select+Start ends the library session. It is not the rooms Soft-stop:
	// idle cleanup must release this kit's grant and stop renewal.
	a.startReleaseStopLocked()
}
