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
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

type stickTracker struct {
	x, y int
	cmd  Command
}

type sdlPad struct {
	handle        *C.SDL_Gamepad
	info          PadInfo
	remap         *inputmap.Remapper
	profileGen    uint64
	settingsCombo bool
	chordFired    bool
	// sent remembers the remapped code pressed while a physical button is down so
	// profile reloads release the same logical code that went down.
	sent map[C.int]remoteinput.Code
	down map[C.int]bool
}

type sdlPadSet struct {
	pads       map[C.SDL_JoystickID]*sdlPad
	profileGen uint64
}

func newSDLPadSet() *sdlPadSet {
	return &sdlPadSet{pads: map[C.SDL_JoystickID]*sdlPad{}}
}

func (s *sdlPadSet) close() {
	if s == nil {
		return
	}
	for id, entry := range s.pads {
		if entry != nil && entry.handle != nil {
			C.SDL_CloseGamepad(entry.handle)
		}
		delete(s.pads, id)
	}
}

func (s *sdlPadSet) len() int {
	if s == nil {
		return 0
	}
	return len(s.pads)
}

func (s *sdlPadSet) handle(id C.SDL_JoystickID) *C.SDL_Gamepad {
	if s == nil {
		return nil
	}
	if entry := s.pads[id]; entry != nil {
		return entry.handle
	}
	return nil
}

func (s *sdlPadSet) freePlayer() int {
	var taken [5]bool
	for _, entry := range s.pads {
		if entry != nil && entry.info.Player >= 1 && entry.info.Player < len(taken) {
			taken[entry.info.Player] = true
		}
	}
	for player := 1; player < len(taken); player++ {
		if !taken[player] {
			return player
		}
	}
	return 0
}

func (s *sdlPadSet) sortedIDs() []C.SDL_JoystickID {
	ids := make([]C.SDL_JoystickID, 0, len(s.pads))
	for id, entry := range s.pads {
		if entry != nil && entry.handle != nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *sdlPadSet) attach(app *App, id C.SDL_JoystickID, pad *C.SDL_Gamepad, seed bool) {
	if s == nil || pad == nil || s.pads[id] != nil {
		return
	}
	player := s.freePlayer()
	info := sdlPadInfo(id, pad, player)
	var (
		profile    inputmap.Profile
		remap      *inputmap.Remapper
		profileGen uint64
	)
	if app != nil {
		if p, err := app.padProfile(info.GUID); err == nil {
			profile = p
			remap, _ = profileRemap(profile)
		}
		profileGen = app.PadProfileGeneration()
	}
	s.pads[id] = &sdlPad{
		handle:        pad,
		info:          info,
		remap:         remap,
		profileGen:    profileGen,
		settingsCombo: padSettingsComboEnabled(info, profile),
	}
	if player >= 1 && player <= 4 {
		C.SDL_SetGamepadPlayerIndex(pad, C.int(player-1))
		rgb := playerLEDColors[player-1]
		C.SDL_SetGamepadLED(pad, C.Uint8(rgb[0]), C.Uint8(rgb[1]), C.Uint8(rgb[2]))
	} else {
		C.SDL_SetGamepadPlayerIndex(pad, -1)
		C.SDL_SetGamepadLED(pad, 0, 0, 0)
	}
	if app != nil {
		app.SetPadInfo(int(id), info)
		if seed {
			app.SeedInput(InputGamepad, int(id))
		} else {
			app.AttachInput(InputGamepad, int(id))
		}
		app.SetGamepads(len(s.pads))
	}
}

func (s *sdlPadSet) detach(app *App, id C.SDL_JoystickID) {
	if s == nil {
		return
	}
	entry := s.pads[id]
	if entry == nil {
		return
	}
	if entry.handle != nil {
		C.SDL_CloseGamepad(entry.handle)
	}
	delete(s.pads, id)
	if app != nil {
		app.DetachInput(InputGamepad, int(id))
		app.SetGamepads(len(s.pads))
	}
}

func (s *sdlPadSet) syncProfiles(app *App) {
	if s == nil || app == nil {
		return
	}
	gen := app.PadProfileGeneration()
	if gen == s.profileGen {
		return
	}
	waiting := false
	for _, entry := range s.pads {
		if entry == nil || entry.info.GUID == "" || entry.profileGen == gen {
			continue
		}
		if len(entry.sent) > 0 || len(entry.down) > 0 {
			waiting = true
			continue
		}
		profile, err := app.padProfile(entry.info.GUID)
		if err != nil {
			profile = inputmap.Profile{}
		}
		entry.remap, _ = profileRemap(profile)
		entry.settingsCombo = padSettingsComboEnabled(entry.info, profile)
		if !entry.settingsCombo {
			entry.chordFired = false
		}
		entry.profileGen = gen
	}
	if !waiting {
		s.profileGen = gen
	}
}

func sdlBusFromGUID(guid string) uint16 {
	if len(guid) < 4 {
		return 0
	}
	lo, errLo := strconv.ParseUint(guid[0:2], 16, 8)
	hi, errHi := strconv.ParseUint(guid[2:4], 16, 8)
	if errLo != nil || errHi != nil {
		return 0
	}
	return uint16(hi)<<8 | uint16(lo)
}

func sdlPadFamily(padType C.SDL_GamepadType, dev controller.Device) gfx.PromptFamily {
	switch padType {
	case C.SDL_GAMEPAD_TYPE_XBOX360, C.SDL_GAMEPAD_TYPE_XBOXONE:
		return gfx.PromptXbox
	case C.SDL_GAMEPAD_TYPE_PS3, C.SDL_GAMEPAD_TYPE_PS4, C.SDL_GAMEPAD_TYPE_PS5:
		return gfx.PromptPlayStation
	case C.SDL_GAMEPAD_TYPE_NINTENDO_SWITCH_PRO,
		C.SDL_GAMEPAD_TYPE_NINTENDO_SWITCH_JOYCON_LEFT,
		C.SDL_GAMEPAD_TYPE_NINTENDO_SWITCH_JOYCON_RIGHT,
		C.SDL_GAMEPAD_TYPE_NINTENDO_SWITCH_JOYCON_PAIR,
		C.SDL_GAMEPAD_TYPE_GAMECUBE:
		return gfx.PromptNintendo
	default:
		return promptFamily(dev.Family())
	}
}

func sdlPadInfo(id C.SDL_JoystickID, pad *C.SDL_Gamepad, player int) PadInfo {
	var name string
	var vendor, product, version uint16
	var padType C.SDL_GamepadType
	if pad != nil {
		name = strings.TrimSpace(C.GoString(C.SDL_GetGamepadName(pad)))
		vendor = uint16(C.SDL_GetGamepadVendor(pad))
		product = uint16(C.SDL_GetGamepadProduct(pad))
		version = uint16(C.SDL_GetGamepadProductVersion(pad))
		padType = C.SDL_GetGamepadType(pad)
	}
	var guidBuf [33]C.char
	C.SDL_GUIDToString(C.SDL_GetJoystickGUIDForID(id), &guidBuf[0], C.int(len(guidBuf)))
	guid := strings.TrimSpace(C.GoString(&guidBuf[0]))
	dev := controller.Device{
		Name:    name,
		Bus:     sdlBusFromGUID(guid),
		Vendor:  vendor,
		Product: product,
		Version: version,
	}
	if guid == "" || strings.Trim(guid, "0") == "" {
		guid = dev.GUID()
	}
	return PadInfo{
		Name:       name,
		GUID:       guid,
		Vendor:     vendor,
		Product:    product,
		Family:     sdlPadFamily(padType, dev),
		Recognised: true,
		Bluetooth:  dev.Bluetooth(),
		Tested:     dev.Tested(),
		NoLearn:    true,
		NoGuide:    pad != nil && !bool(C.SDL_GamepadHasButton(pad, C.SDL_GAMEPAD_BUTTON_GUIDE)),
		Player:     player,
	}
}

func pumpSDL(app *App, pads *sdlPadSet, now time.Time, renderer *C.SDL_Renderer) {
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

func padDump(app *App, pads *sdlPadSet) string {
	snap := app.Snapshot()
	return fmt.Sprintf("games=%d cols=%d focus=%d pads=%d", len(snap.Games), snap.Grid.Columns, snap.Grid.Focus, pads.len())
}

func pollGamepads(app *App, pads *sdlPadSet, held map[Command]bool, stick *stickTracker, now time.Time) bool {
	if pads == nil {
		return applyPressed(app, map[Command]bool{}, held, now)
	}
	pads.syncProfiles(app)
	pressed := map[Command]bool{}
	prevStick := CmdNone
	if stick != nil {
		prevStick = stick.cmd
	}
	stickCmd := CmdNone
	active := make([]int, 0, pads.len())
	for _, id := range pads.sortedIDs() {
		entry := pads.pads[id]
		pad := entry.handle
		used := false
		var (
			padBuf     [13]remoteinput.Code
			padButtons = padBuf[:0]
			selectDown bool
			startDown  bool
		)
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
			down := bool(C.SDL_GetGamepadButton(pad, C.SDL_GamepadButton(button)))
			if entry.down[button] != down {
				if entry.down == nil {
					entry.down = map[C.int]bool{}
				}
				if down {
					entry.down[button] = true
				} else {
					delete(entry.down, button)
				}
				if logical := logicalFromSDLButton(button); logical != 0 {
					app.NotePadButton(int(id), logical, down)
				}
			}
			if button == C.SDL_GAMEPAD_BUTTON_GUIDE {
				if down {
					pressed[CmdSettings] = true
					used = true
				}
				continue
			}
			logical := logicalFromSDLButton(button)
			if down {
				code, ok := entry.sent[button]
				if !ok {
					ev := remoteinput.Event{Kind: remoteinput.KindButton, Code: logical, Action: remoteinput.ActionPress}
					if entry.remap != nil {
						ev = entry.remap.Apply(ev)
					}
					code = ev.Code
					if entry.sent == nil {
						entry.sent = map[C.int]remoteinput.Code{}
					}
					entry.sent[button] = code
				}
				switch code {
				case remoteinput.ButtonSelect:
					selectDown = true
				case remoteinput.ButtonStart:
					startDown = true
				}
				padButtons = append(padButtons, code)
				used = true
			} else {
				delete(entry.sent, button)
			}
		}
		chordActive := entry.settingsCombo && selectDown && startDown
		if chordActive {
			entry.chordFired = true
			pressed[CmdSettings] = true
		} else if entry.chordFired && !selectDown && !startDown {
			entry.chordFired = false
		}
		suppressSelect := entry.settingsCombo && (chordActive || entry.chordFired)
		for _, code := range padButtons {
			if entry.settingsCombo && (code == remoteinput.ButtonStart || (code == remoteinput.ButtonSelect && suppressSelect)) {
				continue
			}
			if cmd := CommandFromLogical(remoteinput.Event{Kind: remoteinput.KindButton, Code: code}); cmd != CmdNone {
				pressed[cmd] = true
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
	return handleSDLEvent(app, newSDLPadSet(), &ev, time.Now(), nil)
}

func dispatchSyntheticSDLKey(app *App, keyboardID, keycode, scancode int, down bool, now time.Time) bool {
	ev := C.FogcastEvent{
		kind: C.int(evKey), code: C.int(keycode), value: C.int(scancode),
		which: C.int(keyboardID), down: 0,
	}
	if down {
		ev.down = 1
	}
	return handleSDLEvent(app, newSDLPadSet(), &ev, now, nil)
}

func handleSDLEvent(app *App, pads *sdlPadSet, ev *C.FogcastEvent, now time.Time, stick *stickTracker) bool {
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
		if pads.handle(id) != nil {
			return false
		}
		if pad := C.SDL_OpenGamepad(id); pad != nil {
			pads.attach(app, id, pad, false)
		}
	case evPadRemoved:
		pads.detach(app, C.SDL_JoystickID(ev.which))
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

func openExistingGamepads(pads *sdlPadSet, app *App) {
	var count C.int
	ids := C.SDL_GetGamepads(&count)
	if ids == nil {
		return
	}
	defer C.SDL_free(unsafe.Pointer(ids))
	slice := unsafe.Slice(ids, int(count))
	for _, id := range slice {
		if pads.handle(id) != nil {
			continue
		}
		if pad := C.SDL_OpenGamepad(id); pad != nil {
			pads.attach(app, id, pad, true)
		}
	}
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

func sdlPadFamilyCases() []struct {
	name string
	got  gfx.PromptFamily
	want gfx.PromptFamily
} {
	return []struct {
		name string
		got  gfx.PromptFamily
		want gfx.PromptFamily
	}{
		{"ps5", sdlPadFamily(C.SDL_GAMEPAD_TYPE_PS5, controller.Device{}), gfx.PromptPlayStation},
		{"xboxone", sdlPadFamily(C.SDL_GAMEPAD_TYPE_XBOXONE, controller.Device{}), gfx.PromptXbox},
		{"switch_pro", sdlPadFamily(C.SDL_GAMEPAD_TYPE_NINTENDO_SWITCH_PRO, controller.Device{}), gfx.PromptNintendo},
		{"fallback_sony", sdlPadFamily(C.SDL_GAMEPAD_TYPE_STANDARD, controller.Device{Vendor: 0x054c, Name: "Sony Pad"}), gfx.PromptPlayStation},
	}
}

func initSDLGamepadSubsystem() bool {
	return bool(C.SDL_InitSubSystem(C.SDL_INIT_GAMEPAD))
}

func quitSDLGamepadSubsystem() {
	C.SDL_QuitSubSystem(C.SDL_INIT_GAMEPAD)
}

func attachVirtualSDLPadForTest(app *App, pads *sdlPadSet) (uint32, func(), error) {
	id := C.fogcast_attach_virtual_gamepad()
	if id == 0 {
		return 0, nil, fmt.Errorf("attach virtual gamepad: %s", sdlError())
	}
	pad := C.SDL_OpenGamepad(id)
	if pad == nil {
		C.SDL_DetachVirtualJoystick(id)
		return 0, nil, fmt.Errorf("SDL_OpenGamepad: %s", sdlError())
	}
	pads.attach(app, id, pad, false)
	cleanup := func() {
		pads.detach(app, id)
		C.SDL_DetachVirtualJoystick(id)
	}
	return uint32(id), cleanup, nil
}

func setVirtualSDLButtonForTest(pads *sdlPadSet, id uint32, btn remoteinput.Code, down bool) error {
	var sdlBtn C.int
	switch btn {
	case remoteinput.ButtonA:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_SOUTH
	case remoteinput.ButtonB:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_EAST
	case remoteinput.ButtonX:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_WEST
	case remoteinput.ButtonY:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_NORTH
	case remoteinput.ButtonSelect:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_BACK
	case remoteinput.ButtonStart:
		sdlBtn = C.SDL_GAMEPAD_BUTTON_START
	default:
		return fmt.Errorf("unsupported test button %v", btn)
	}
	v := C.int(0)
	if down {
		v = 1
	}
	pad := pads.handle(C.SDL_JoystickID(id))
	if C.fogcast_virtual_button_on(pad, sdlBtn, v) == 0 {
		return fmt.Errorf("virtual button failed: %s", sdlError())
	}
	C.SDL_PumpEvents()
	return nil
}

func sdlPadSentCode(pads *sdlPadSet, id uint32, btn remoteinput.Code) remoteinput.Code {
	entry := pads.pads[C.SDL_JoystickID(id)]
	if entry == nil {
		return 0
	}
	switch btn {
	case remoteinput.ButtonA:
		return entry.sent[C.SDL_GAMEPAD_BUTTON_SOUTH]
	case remoteinput.ButtonB:
		return entry.sent[C.SDL_GAMEPAD_BUTTON_EAST]
	case remoteinput.ButtonY:
		return entry.sent[C.SDL_GAMEPAD_BUTTON_NORTH]
	default:
		return 0
	}
}
