//go:build linux

package tenfoot

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/ui/shared"
)

// handleNativeKeyboardUIOnly consumes every supplied event. The return value
// means quit, not guest delivery. A token is single-use and the validation and
// UI mutation share App.mu, so a focus/quarantine transition cannot overtake
// an authorized event. No path invokes a legacy guest keyboard sender.
func (a *App) handleNativeKeyboardUIOnly(id localhid.DeviceID, rawCode uint16, name string, usage uint8, down bool, token uint64, now time.Time) bool {
	if a == nil || token == 0 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	action, ok := a.nativeUIActions[token]
	delete(a.nativeUIActions, token)
	d := a.nativeUIRaw.devices[id]
	if !ok || !a.delegatedV2Active || a.nativeKeyboardGenerationExhausted || d == nil ||
		action.generation != a.nativeKeyboardGeneration || action.id != id || action.code != rawCode ||
		action.down != down || rawCode == 0 || rawCode >= 256 || d.revision[rawCode] != action.revision ||
		d.down[rawCode] != down || (down && d.suppressed[rawCode]) {
		return false
	}
	wantName := nativeKeyName(rawCode)
	if rawCode == nativeKeyF12 {
		wantName = "f12"
	}
	if name != wantName || usage != nativeHIDUsage(rawCode) {
		return false
	}
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		return false
	}
	a.nativeUIOnlyInFlight++
	defer func() { a.nativeUIOnlyInFlight-- }()
	if a.consumePlayEscapeLocked(name) {
		return false
	}
	if mediaSelectorKey(name, usage) {
		if action.repeat {
			return false
		}
		coleco := a.colecoSessionLocked()
		available := a.playingHardwareRoomAvailableLocked()
		media := available && a.sessionLiveMediaOfferedLocked()
		colecoOffer := available && coleco && !a.sessionLiveMediaOfferedLocked() && !a.roomDuringPlay
		if !media && !colecoOffer {
			if down && coleco && !a.roomDuringPlay {
				a.status = colecoSelectorUnavailableStatus
			}
			a.playingRoomKeyLocked(down, now, true)
		} else if down {
			a.nativeUIPressLocked(id, rawCode, CmdHome, now)
		}
		return false
	}
	if strings.EqualFold(strings.TrimSpace(name), "home") && a.playingRoomKeyLocked(down, now, a.nativeUIRaw.ctrlHeld()) {
		return false
	}
	if a.forwardsPlayHIDLocked() && !a.playHIDInputBlockedLocked() && !a.tapePickerOpen && !a.firmwarePickerOpen {
		if a.nativeMappedPad.active && playhid.ChromeStop(name) {
			if down && !action.repeat {
				a.nativeUIPressLocked(id, rawCode, CommandFromKey(name), now)
			}
			return false
		}
		if a.nativeMappedPad.active && loadTapeChromeKey(name) {
			if down && !action.repeat && a.sessionMediaOfferedLocked() {
				a.nativeUIPressLocked(id, rawCode, CmdSearch, now)
			}
			return false
		}
		return false
	}
	if !down {
		a.repeat.UpNative([16]byte(id), rawCode)
		d.commands[rawCode] = CmdNone
		return false
	}
	if d.fd >= 0 {
		a.noteInputLocked(InputKeyboard, d.fd)
	}
	shift := (d.down[42] && !d.suppressed[42]) || (d.down[54] && !d.suppressed[54])
	cmd := CommandFromKey(name)
	if shift && cmd == CmdTab {
		cmd = CmdTabPrev
	}
	if a.searchOpen || a.nameEntryOpenLocked() || a.settingsOSKOpenLocked() {
		switch name {
		case "backspace":
			a.searchBackspaceLocked(now)
		case "return":
			a.confirmSearchLocked(now)
		case "escape", "tab", "up", "down", "left", "right":
			if action.repeat && isHoldable(cmd) {
				// Movement repeats through the single shared arbiter only.
			} else if action.repeat {
				a.handleCommandLocked(cmd, now)
			} else {
				a.nativeUIPressLocked(id, rawCode, cmd, now)
			}
		default:
			text := shared.SanitizeFieldText(nativeKeyText(rawCode, shift))
			if text != "" {
				a.typeTextLocked(text, now)
			}
		}
		d.commands[rawCode] = cmd
		return false
	}
	if action.repeat {
		return false
	} // App owns navigation repeat.
	d.commands[rawCode] = cmd
	return a.nativeUIPressLocked(id, rawCode, cmd, now)
}

func (a *App) nativeUIPressLocked(id localhid.DeviceID, rawCode uint16, cmd Command, now time.Time) bool {
	fired, err := a.repeat.DownNative([16]byte(id), rawCode, cmd, now)
	if err != nil {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultOverflow)
		return false
	}
	if fired == CmdQuit {
		return true
	}
	if fired != CmdNone {
		a.handleCommandLocked(fired, now)
	}
	return false
}
