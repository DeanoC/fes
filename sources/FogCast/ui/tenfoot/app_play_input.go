package tenfoot

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/remoteinput"
)

func (a *App) ForwardsCoreKeyboard() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.forwardsCoreKeyboardLocked()
}

func (a *App) forwardsCoreKeyboardLocked() bool {
	if !a.session.CoreKeyboard && !a.session.CoreKeyboardHID {
		return false
	}
	return a.forwardsPlayHIDLocked()
}

// forwardsKeyboardHIDLocked reports an attached fes.keyboard.hid session.
// Every physical key, including Esc and Backspace, goes to the core; Stop
// stays on the session chrome and the controller Select+Start chord.
func (a *App) forwardsKeyboardHIDLocked() bool {
	return a.session.CoreKeyboardHID && a.forwardsPlayHIDLocked()
}

// HandlePlayHIDScancode consumes one USB key with its HID usage (an SDL
// scancode is the Keyboard/Keypad usage). HID sessions forward the usage
// without chrome keys or a character map; other sessions keep the named-key
// path of HandlePlayHIDKey.
// hidKeyboardF12 is the USB HID Keyboard/Keypad usage for F12.
const hidKeyboardF12 = 0x45

func mediaSelectorKey(name string, usage uint8) bool {
	if usage == hidKeyboardF12 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(name), "f12")
}

// openRunningMediaSelector opens the running machine's media selector. An
// empty tape and a folder with no .p files still open: Eject and a typed path
// stay available. F12 opens a ROM-linked Coleco session's title selector;
// Guide opens its cartridge picker.
func (a *App) openRunningMediaSelector(down bool, now time.Time) bool {
	a.mu.Lock()
	colecoSession := a.colecoSessionLocked()
	available := a.playingHardwareRoomAvailableLocked()
	mediaOffer := available && a.sessionLiveMediaOfferedLocked()
	colecoOffer := available && colecoSession && !a.sessionLiveMediaOfferedLocked() && !a.roomDuringPlay
	a.mu.Unlock()
	if !mediaOffer && !colecoOffer {
		if down && colecoSession {
			a.mu.Lock()
			if !a.roomDuringPlay {
				a.status = colecoSelectorUnavailableStatus
			}
			a.mu.Unlock()
		}
		return false
	}
	if down {
		a.Press(CmdHome, now)
	}
	return true
}

func (a *App) HandlePlayHIDScancode(name string, usage uint8, down bool, now time.Time) bool {
	return a.handlePlayHIDScancode(-1, name, usage, down, now)
}

// source identifies a physical keyboard: SDL instance ID or native evdev fd.
// Legacy callers without a device identity use -1.
func (a *App) handlePlayHIDScancode(source int, name string, usage uint8, down bool, now time.Time) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	waitingForHost := a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing)
	a.mu.Unlock()
	if waitingForHost {
		return true
	}
	suppressed := a.consumeSuppressedPlayKeyFrom(source, name, usage, down)
	if !down {
		if a.releaseTransitionPlayKey(source, name, usage) {
			return true
		}
		a.releaseStoppedPlayKey(source, name, usage)
	}
	if suppressed {
		return true
	}
	if a.consumePlayEscape(name) {
		return true
	}
	if mediaSelectorKey(name, usage) {
		// F12 is the running selector. The session consumes the key, including an empty tape and
		// a folder that has no .p files, so it is never forwarded. With no
		// media selector to open, F12 opens the running room as Home does,
		// which the surround hint advertises (#1132). With no room either,
		// F12 stops the session (#1561).
		if !a.openRunningMediaSelector(down, now) {
			a.handlePlayingMenuKey(down, now)
		}
		return true
	}
	if a.handlePlayingRoomKey(name, down, now) {
		return true
	}
	a.mu.Lock()
	hid := a.forwardsKeyboardHIDLocked()
	a.mu.Unlock()
	if !hid {
		return a.handlePlayHIDKey(source, name, usage, down, now)
	}
	if !a.ConsumePlayHID() {
		return false
	}
	if event, ok := hidkeys.Event(usage, down); ok {
		a.sendPhysicalPlayKey(source, name, usage, event)
	}
	return true
}

func (a *App) ForwardsPlayHID() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.forwardsPlayHIDLocked()
}

func (a *App) forwardsPlayHIDLocked() bool {
	if a.roomDuringPlay || a.sessionDisplayBusy || a.sessionDisplayUncertain || a.tapePickerOpen || a.firmwarePickerOpen {
		return false
	}
	return a.playHIDRouteReadyLocked()
}

// Cleanup can still use the active route while Stop owns launcher focus.
func (a *App) playHIDRouteReadyLocked() bool {
	if a.session.State != "active" {
		return false
	}
	// Kit-host library sessions use the local socket, not the optional host
	// remote-input attachment. The same display gate selects that socket in
	// queuePlayHIDLocked; require a real feed.
	if a.kitHost && a.client != nil && a.localFeed != nil {
		return true
	}
	if a.session.Input == nil {
		return false
	}
	return a.session.Input.Ready || a.session.Input.State == "attached" ||
		a.session.Input.State == "reconnecting"
}

// playHIDInputBlockedLocked fences new physical input while Stop is in flight
// or awaiting an explicit retry. Display recovery still uses the normal play
// forwarding state so a failed Stop can return control to the machine.
func (a *App) playHIDInputBlockedLocked() bool {
	return a.stopPhase == "stopping" || a.retryStopLock
}

// ConsumePlayHID keeps USB keys on the play-session path: no sofa browse and
// no affinity steal. False means browse/nav may handle the key.
func (a *App) ConsumePlayHID() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.forwardsPlayHIDLocked() || a.playHIDInputBlockedLocked() || a.tapePickerOpen || a.firmwarePickerOpen {
		return false
	}
	a.repeat.Clear()
	return true
}

// HandlePlayHIDKey consumes one USB key while play HID is attached. Backspace
// stops the session without NoteInput; "/" opens Load-tape when the
// active package can arm a mailbox. Other keys encode for the attached core.
// Letter s is a ZX81/core key, not chrome stop.
func (a *App) HandlePlayHIDKey(name string, down bool, now time.Time) bool {
	suppressed := a.consumeSuppressedPlayKey(name, 0, down)
	if !down {
		if a.releaseTransitionPlayKey(-1, name, 0) {
			return true
		}
		a.releaseStoppedPlayKey(-1, name, 0)
	}
	if suppressed {
		return true
	}
	if a.consumePlayEscape(name) {
		return true
	}
	return a.handlePlayHIDKey(-1, name, 0, down, now)
}

// A second Escape after returning from a room must not stop the machine.
// HID keyboards still receive Escape; while a room or recovery owns focus,
// leave it to the ordinary Back and retry paths.
func (a *App) consumePlayEscape(name string) bool {
	if !strings.EqualFold(strings.TrimSpace(name), "escape") {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.consumePlayEscapeLocked(name)
}

func (a *App) consumePlayEscapeLocked(name string) bool {
	if !strings.EqualFold(strings.TrimSpace(name), "escape") {
		return false
	}
	return a.session.State == "active" && !(a.session.CoreKeyboardHID && a.forwardsKeyboardHIDLocked()) &&
		!a.roomDuringPlay && !a.sessionDisplayBusy && !a.sessionDisplayUncertain &&
		!a.launcherDisplayFocusedLocked() && !a.tapePickerOpen && !a.firmwarePickerOpen &&
		!a.retryStopLock && a.stopPhase != "stopping"
}

// Physical key-up must reach the same active route even when Stop suppresses
// held keys or gives launcher navigation control. Key-down keeps that normal
// navigation path, including Escape and Backspace for retry/recovery.
func (a *App) releaseStoppedPlayKey(source int, name string, usage uint8) {
	a.mu.Lock()
	blocked := a.playHIDInputBlockedLocked()
	hid := a.session.CoreKeyboardHID
	keyboard := a.session.CoreKeyboard || hid
	a.mu.Unlock()
	if !blocked {
		return
	}
	var event remoteinput.Event
	var ok bool
	if hid {
		event, ok = hidkeys.Event(usage, false)
	} else {
		event, ok = playhid.Event(name, false, keyboard)
	}
	if ok {
		a.sendPhysicalPlayKey(source, name, usage, event)
	}
}

func (a *App) handlePlayHIDKey(source int, name string, usage uint8, down bool, now time.Time) bool {
	if a.handlePlayingRoomKey(name, down, now) {
		return true
	}
	if !a.ConsumePlayHID() {
		return false
	}
	if playhid.ChromeStop(name) {
		cmd := CommandFromKey(strings.ToLower(strings.TrimSpace(name)))
		if down {
			a.Press(cmd, now)
		} else {
			a.Release(cmd)
		}
		return true
	}
	if loadTapeChromeKey(name) {
		a.mu.Lock()
		open := down && a.sessionMediaOfferedLocked()
		a.mu.Unlock()
		if open {
			a.Press(CmdSearch, now)
		}
		return true
	}
	if event, ok := playhid.Event(name, down, a.ForwardsCoreKeyboard()); ok {
		a.sendPhysicalPlayKey(source, name, usage, event)
	}
	return true
}

// loadTapeChromeKey is the keyboard/mouse Load-tape binding advertised by
// sessionChromeHint (northWord → "/"). Gamepad North stays CmdSearch.
// ZX81 letters, including s and f, stay core keys.
func loadTapeChromeKey(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "/", "slash":
		return true
	default:
		return false
	}
}

func (a *App) playHIDFailClosedLocked() bool {
	if (a.client == nil || !a.client.paired) && a.healthHave && kitlease.ForeignHID(kitlease.Status{
		State: a.health.Connection.State,
		Owner: a.health.Connection.Owner,
	}) {
		return true
	}
	if a.client != nil && a.client.paired && (!a.kitLeaseHave || a.kitLease.Unavailable) {
		return true
	}
	if !a.kitLeaseHave || a.kitLease.Unavailable {
		return false
	}
	switch strings.TrimSpace(a.kitLease.ErrorCode) {
	case "KIT_LEASE_DENIED", "KIT_LEASE_BUSY":
		return true
	}
	return kitlease.ForeignHID(kitlease.Status{
		State:   a.kitLease.State,
		Owner:   a.kitLease.Owner,
		Purpose: a.kitLease.Purpose,
	})
}

func (a *App) SendCoreKey(event remoteinput.Event) {
	a.SendPlayHID(event)
}

// neutralPlayHIDEventLocked only clears input state; mouse reports may remove
// held buttons, but may not introduce a new button or any motion.
func (a *App) neutralPlayHIDEventLocked(event remoteinput.Event) bool {
	if event.Device == remoteinput.DeviceMouse {
		held := a.playHIDHeld[playHIDKey{device: remoteinput.DeviceMouse}].Code
		return event.Kind == remoteinput.KindRelative && event.Action == remoteinput.ActionRelative && event.Value == 0 && event.Code & ^held == 0
	}
	switch event.Kind {
	case remoteinput.KindAxis:
		return event.Action == remoteinput.ActionAbsolute && event.Value == 0
	case remoteinput.KindKey, remoteinput.KindButton:
		return event.Action == remoteinput.ActionRelease
	default:
		return false
	}
}

// SendPlayHID posts one play-session HID event. Foreign kit leases fail closed.
func (a *App) SendPlayHID(event remoteinput.Event) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sendPlayHIDLocked(event)
}

// releaseTransitionPlayKey sends only a previously admitted neutral key-up
// while a same-session surround transition keeps input with the machine. It
// never opens a route for new presses or crosses an actual focus handoff.
func (a *App) releaseTransitionPlayKey(source int, name string, usage uint8) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	physical := physicalPlayKey{source: source, key: playHeldKey(name, usage)}
	key, admitted := a.playKeyAdmitted[physical]
	if !admitted || !a.sessionDisplayBusy || !a.sessionDisplayNonHandoff {
		return false
	}
	if !samePlayHIDSession(a.session, a.sessionDisplayBinding) {
		delete(a.playKeyAdmitted, physical)
		return true
	}
	if a.hasOtherPlayKeyContributorLocked(physical, key) {
		delete(a.playKeyAdmitted, physical)
		a.traceKeyRouteLocked("coalesced-release", source)
		return true
	}
	delete(a.playKeyAdmitted, physical)
	if !a.playHIDRouteReadyLocked() || a.keyboardHIDFailClosedLocked() || a.client == nil {
		if a.playKeyPendingRelease == nil {
			a.playKeyPendingRelease = make(map[physicalPlayKey]playHIDKey)
		}
		a.playKeyPendingRelease[physical] = key
		a.traceKeyRouteLocked("transition-release-deferred", source)
		return true
	}
	event := remoteinput.Event{Player: key.player, Device: key.device, Kind: key.kind, Code: key.code, Action: remoteinput.ActionRelease}
	a.rememberPlayHIDLocked(event)
	a.queuePhysicalKeyboardLocked(event)
	a.traceKeyRouteLocked("transition-release", source)
	return true
}

// Retry neutral releases only on the original active, machine-focused route.
// Session replacement and explicit handoff clear these along with the holds.
func (a *App) flushPendingPlayKeyReleasesLocked() {
	if len(a.playKeyPendingRelease) == 0 || !a.forwardsPlayHIDLocked() || a.playHIDInputBlockedLocked() || a.keyboardHIDFailClosedLocked() || a.client == nil {
		return
	}
	for physical, key := range a.playKeyPendingRelease {
		event := remoteinput.Event{Player: key.player, Device: key.device, Kind: key.kind, Code: key.code, Action: remoteinput.ActionRelease}
		a.rememberPlayHIDLocked(event)
		a.queuePhysicalKeyboardLocked(event)
		delete(a.playKeyPendingRelease, physical)
	}
}

func (a *App) sendPhysicalPlayKey(source int, name string, usage uint8, event remoteinput.Event) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	physical := physicalPlayKey{source: source, key: playHeldKey(name, usage)}
	if event.Action == remoteinput.ActionRelease {
		if admitted, ok := a.playKeyAdmitted[physical]; ok {
			if a.hasOtherPlayKeyContributorLocked(physical, admitted) {
				delete(a.playKeyAdmitted, physical)
				a.traceKeyRouteLocked("coalesced-release", source)
				return true
			}
			event = remoteinput.Event{Player: admitted.player, Device: admitted.device, Kind: admitted.kind, Code: admitted.code, Action: remoteinput.ActionRelease}
		}
	}
	if event.Action == remoteinput.ActionPress {
		// A repeat of an already-held physical key keeps its first admitted
		// mapping. If the keyboard mode changed while it was held, the repeat
		// would arrive under a different mapping; forwarding it would press a
		// second core key and replace the admission its key-up must release.
		if admitted, ok := a.playKeyAdmitted[physical]; ok && admitted != (playHIDKey{event.Player, event.Device, event.Kind, event.Code}) {
			return false
		}
		// Distinct physical keys may contribute the same core key. Keep the
		// second contribution for its eventual release, but don't send the core
		// a second press while the first contribution is still held. Repeats
		// from an already admitted physical key retain their existing behavior.
		if _, alreadyAdmitted := a.playKeyAdmitted[physical]; !alreadyAdmitted {
			key := playHIDKey{event.Player, event.Device, event.Kind, event.Code}
			if a.hasOtherPlayKeyContributorLocked(physical, key) {
				// Apply the same admission gates as an ordinary press. Flush
				// deferred releases first so an old neutral cannot follow a new
				// contribution on the same route.
				if !a.forwardsPlayHIDLocked() || a.playHIDInputBlockedLocked() || a.keyboardHIDFailClosedLocked() || a.client == nil || a.playHIDPending >= maxPendingPlayHID {
					return false
				}
				a.flushPendingPlayKeyReleasesLocked()
				if _, stillHeld := a.playHIDHeld[key]; stillHeld {
					a.playKeyAdmitted[physical] = key
					a.traceKeyRouteLocked("coalesced", source)
					return true
				}
			}
		}
	}
	if !a.sendPhysicalKeyboardLocked(event) {
		if event.Action == remoteinput.ActionRelease {
			if key, ok := a.playKeyAdmitted[physical]; ok {
				delete(a.playKeyAdmitted, physical)
				if a.playKeyPendingRelease == nil {
					a.playKeyPendingRelease = make(map[physicalPlayKey]playHIDKey)
				}
				a.playKeyPendingRelease[physical] = key
			}
		}
		return false
	}
	if event.Action == remoteinput.ActionRelease {
		delete(a.playKeyAdmitted, physical)
		a.traceKeyRouteLocked("released", source)
	}
	if event.Action == remoteinput.ActionPress {
		if a.playKeyAdmitted == nil {
			a.playKeyAdmitted = make(map[physicalPlayKey]playHIDKey)
		}
		a.playKeyAdmitted[physical] = playHIDKey{event.Player, event.Device, event.Kind, event.Code}
		a.traceKeyRouteLocked("admitted", source)
	}
	return true
}

// hasOtherPlayKeyContributorLocked reports whether another unsuppressed
// physical hold already contributes this admitted core key.
func (a *App) hasOtherPlayKeyContributorLocked(physical physicalPlayKey, key playHIDKey) bool {
	for other, heldKey := range a.playKeyAdmitted {
		if other != physical && heldKey == key && a.playKeyHeld[other] && !a.playKeySuppressed[other] {
			return true
		}
	}
	return false
}

func (a *App) sendPlayHIDLocked(event remoteinput.Event) bool {
	if a.kitV2InputDisabled {
		return false
	}
	if a.kitSDLKeyboardGuestBlockedLocked() &&
		(event.Device == remoteinput.DeviceKeyboard || event.Kind == remoteinput.KindKey) {
		return false
	}
	ready := a.forwardsPlayHIDLocked()
	blocked := a.playHIDInputBlockedLocked()
	if blocked {
		if !a.neutralPlayHIDEventLocked(event) {
			return false
		}
		ready = a.playHIDRouteReadyLocked()
	}
	foreign := a.playHIDFailClosedLocked()
	if !ready || foreign || a.client == nil {
		return false
	}
	if a.playHIDPending >= maxPendingPlayHID && !a.neutralPlayHIDEventLocked(event) {
		return false
	}
	a.flushPendingPlayKeyReleasesLocked()
	a.rememberPlayHIDLocked(event)
	a.queuePlayHIDLocked([]remoteinput.Event{event})
	return true
}
