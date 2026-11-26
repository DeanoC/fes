//go:build sdl3

package tenfoot

/*
#cgo pkg-config: sdl3
#include "sdl_cgo.h"
*/
import "C"

import (
	"fmt"
	"sort"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/remoteinput"
)

type stickTracker struct {
	x, y int
	cmd  Command
}

func pumpSDL(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, now time.Time, renderer *C.SDL_Renderer) {
	var stick stickTracker
	C.fogcast_update_pads()
	for {
		var ev C.FogcastEvent
		if C.fogcast_poll(&ev, renderer) == 0 {
			return
		}
		_ = handleSDLEvent(app, pads, &ev, now, &stick)
	}
}

func padDump(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad) string {
	snap := app.Snapshot()
	return fmt.Sprintf("games=%d cols=%d focus=%d pads=%d", len(snap.Games), snap.Grid.Columns, snap.Grid.Focus, len(pads))
}

func pollGamepads(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, held map[Command]bool, stick *stickTracker, now time.Time) bool {
	pressed := map[Command]bool{}
	prevStick := CmdNone
	if stick != nil {
		prevStick = stick.cmd
	}
	stickCmd := CmdNone
	active := make([]int, 0, len(pads))
	for id, pad := range pads {
		if pad == nil {
			continue
		}
		used := false
		for _, button := range []C.int{
			C.SDL_GAMEPAD_BUTTON_DPAD_UP,
			C.SDL_GAMEPAD_BUTTON_DPAD_DOWN,
			C.SDL_GAMEPAD_BUTTON_DPAD_LEFT,
			C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT,
			C.SDL_GAMEPAD_BUTTON_SOUTH,
			C.SDL_GAMEPAD_BUTTON_EAST,
			C.SDL_GAMEPAD_BUTTON_WEST,
			C.SDL_GAMEPAD_BUTTON_NORTH,
			C.SDL_GAMEPAD_BUTTON_LEFT_SHOULDER,
			C.SDL_GAMEPAD_BUTTON_RIGHT_SHOULDER,
			C.SDL_GAMEPAD_BUTTON_START,
			C.SDL_GAMEPAD_BUTTON_BACK,
			C.SDL_GAMEPAD_BUTTON_GUIDE,
		} {
			if bool(C.SDL_GetGamepadButton(pad, C.SDL_GamepadButton(button))) {
				pressed[commandFromSDLButton(button)] = true
				used = true
			}
		}
		x := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTX))
		y := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTY))
		if cmd := CommandFromStickHeld(x, y, prevStick); cmd != CmdNone {
			pressed[cmd] = true
			stickCmd = cmd
			used = true
		}
		if used {
			active = append(active, int(id))
		}
	}
	if stick != nil {
		stick.cmd = stickCmd
	}
	if len(active) > 0 {
		app.NoteInput(InputGamepad, pickActiveGamepad(app, active))
	}
	return applyPressed(app, pressed, held, now)
}

func pickActiveGamepad(app *App, ids []int) int {
	if len(ids) == 1 {
		return ids[0]
	}
	cur := app.Affinity()
	if cur.Kind == InputGamepad {
		for _, id := range ids {
			if id == cur.ID {
				return id
			}
		}
	}
	sort.Ints(ids)
	return ids[0]
}

func takeEventText(ev *C.FogcastEvent) string {
	if ev.text == nil {
		return ""
	}
	text := C.GoString(ev.text)
	C.SDL_free(unsafe.Pointer(ev.text))
	ev.text = nil
	return text
}

func dispatchSyntheticSDL(app *App, kind, which int) bool {
	ev := C.FogcastEvent{kind: C.int(kind), which: C.int(which)}
	return handleSDLEvent(app, map[C.SDL_JoystickID]*C.SDL_Gamepad{}, &ev, time.Now(), nil)
}

func dispatchSyntheticSDLKey(app *App, keyboardID, keycode, scancode int, down bool, now time.Time) bool {
	ev := C.FogcastEvent{
		kind: C.int(evKey), code: C.int(keycode), value: C.int(scancode),
		which: C.int(keyboardID), down: 0,
	}
	if down {
		ev.down = 1
	}
	return handleSDLEvent(app, map[C.SDL_JoystickID]*C.SDL_Gamepad{}, &ev, now, nil)
}

func handleSDLEvent(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, ev *C.FogcastEvent, now time.Time, stick *stickTracker) bool {
	switch ev.kind {
	case evQuit:
		return true
	case evText:
		text := takeEventText(ev)
		if app.OSKOpen() {
			app.TypeText(text, now)
		}
	case evKey:
		if app.handlePlayHIDScancode(int(ev.which), sdlPlayKeyName(ev.code), sdlHIDUsage(ev.value), ev.down != 0, now) {
			return false
		}
		if ev.down != 0 {
			app.NoteInput(InputKeyboard, int(ev.which))
		}
		if app.OSKOpen() {
			return handleSearchKey(app, ev, now)
		}
		cmd := commandFromSDLKey(ev.code)
		if ev.down != 0 {
			if C.SDL_GetModState()&C.SDL_KMOD_SHIFT != 0 {
				switch cmd {
				case CmdViewNext:
					cmd = CmdViewPrev
				case CmdTab:
					cmd = CmdTabPrev
				}
			}
			if cmd == CmdQuit {
				return true
			}
			if app.AttractActive() && cmd == CmdNone {
				app.DismissAttract(now)
				return false
			}
			app.Press(cmd, now)
		} else {
			app.Release(cmd)
		}
	case evButton, evAxis:
		// Gamepad buttons and sticks are polled each frame so virtual devices
		// and dummy video still drive the focus graph.
		return false
	case evMouseMove:
		if app.HandlePlayMouseMotion(int(ev.which), float64(ev.dx), float64(ev.dy)) {
			return false
		}
		app.PointerMoveFrom(int(ev.which), int(ev.x), int(ev.y), now)
	case evMouseButton:
		if app.HandlePlayMouseButton(int(ev.which), int(ev.code), ev.down != 0) {
			return false
		}
		if ev.code != C.SDL_BUTTON_LEFT {
			return false
		}
		if ev.down != 0 {
			app.PointerClickFrom(int(ev.which), int(ev.x), int(ev.y), now)
		}
	case evPadAdded:
		id := C.SDL_JoystickID(ev.which)
		if pads[id] != nil {
			return false
		}
		pad := C.SDL_OpenGamepad(id)
		if pad != nil {
			pads[id] = pad
			app.AttachInput(InputGamepad, int(id))
			app.SetGamepads(len(pads))
		}
	case evPadRemoved:
		id := C.SDL_JoystickID(ev.which)
		if pad := pads[id]; pad != nil {
			C.SDL_CloseGamepad(pad)
			delete(pads, id)
			app.DetachInput(InputGamepad, int(id))
			app.SetGamepads(len(pads))
		}
	case evKeyAdded:
		app.AttachInput(InputKeyboard, int(ev.which))
	case evKeyRemoved:
		app.releaseSDLKeyboard(int(ev.which))
		app.DetachInput(InputKeyboard, int(ev.which))
	case evMouseAdded:
		app.AttachInput(InputMouse, int(ev.which))
	case evFocusLost:
		app.ReleaseAllPlayMouse()
	case evMouseRemoved:
		app.ReleasePlayMouse(int(ev.which))
		app.DetachInput(InputMouse, int(ev.which))
	}
	return false
}

// releaseSDLKeyboard retires this keyboard's physical holds even when SDL
// cannot deliver their key-up events. The admitted mapping is the one that
// must be neutralized; a surviving keyboard can still hold the same core key.
func (a *App) releaseSDLKeyboard(source int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for physical, key := range a.playKeyAdmitted {
		if physical.source != source {
			continue
		}
		delete(a.playKeyHeld, physical)
		delete(a.playKeySuppressed, physical)
		delete(a.playKeyAdmitted, physical)
		if a.hasOtherPlayKeyContributorLocked(physical, key) {
			continue
		}
		if _, held := a.playHIDHeld[key]; !held {
			continue
		}
		// An in-flight transition is bound to its original session. Never
		// send or defer an old neutral against a replacement session.
		if a.sessionDisplayBusy && a.sessionDisplayNonHandoff && !samePlayHIDSession(a.session, a.sessionDisplayBinding) {
			continue
		}
		event := remoteinput.Event{Player: key.player, Device: key.device, Kind: key.kind, Code: key.code, Action: remoteinput.ActionRelease}
		sent := false
		if a.sessionDisplayBusy && a.sessionDisplayNonHandoff {
			if a.playHIDRouteReadyLocked() && !a.playHIDFailClosedLocked() && a.client != nil {
				a.rememberPlayHIDLocked(event)
				a.queuePlayHIDLocked([]remoteinput.Event{event})
				sent = true
			}
		} else {
			sent = a.sendPlayHIDLocked(event)
		}
		if !sent && a.session.State == "active" {
			if a.playKeyPendingRelease == nil {
				a.playKeyPendingRelease = make(map[physicalPlayKey]playHIDKey)
			}
			a.playKeyPendingRelease[physical] = key
		}
	}
	// Suppressed or otherwise unadmitted keys have no core neutral to send.
	for physical := range a.playKeyHeld {
		if physical.source == source {
			delete(a.playKeyHeld, physical)
			delete(a.playKeySuppressed, physical)
		}
	}
}

func openExistingGamepads(pads map[C.SDL_JoystickID]*C.SDL_Gamepad, app *App) {
	var count C.int
	ids := C.SDL_GetGamepads(&count)
	if ids == nil {
		return
	}
	defer C.SDL_free(unsafe.Pointer(ids))
	slice := unsafe.Slice(ids, int(count))
	for _, id := range slice {
		if pads[id] != nil {
			continue
		}
		pad := C.SDL_OpenGamepad(id)
		if pad != nil {
			pads[id] = pad
			app.SeedInput(InputGamepad, int(id))
		}
	}
	app.SetGamepads(len(pads))
}

func seedSDLKeyboards(app *App) {
	var count C.int
	ids := C.SDL_GetKeyboards(&count)
	if ids == nil {
		return
	}
	defer C.SDL_free(unsafe.Pointer(ids))
	slice := unsafe.Slice(ids, int(count))
	for _, id := range slice {
		app.SeedInput(InputKeyboard, int(id))
	}
}

func seedSDLMice(app *App) {
	var count C.int
	ids := C.SDL_GetMice(&count)
	if ids == nil {
		return
	}
	defer C.SDL_free(unsafe.Pointer(ids))
	slice := unsafe.Slice(ids, int(count))
	for _, id := range slice {
		app.SeedInput(InputMouse, int(id))
	}
}
