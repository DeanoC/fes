//go:build sdl3

// SDL window, event, gamepad, and text-input loop for the tenfoot launcher.
// 2D draw, present, and texture upload go through gfx.Device (SDL3 backend).

package tenfoot

/*
#cgo pkg-config: sdl3
#include <SDL3/SDL.h>
#include <SDL3/SDL_main.h>
#include <stddef.h>
#include <stdlib.h>

enum {
	FC_EV_NONE = 0,
	FC_EV_QUIT,
	FC_EV_KEY,
	FC_EV_BUTTON,
	FC_EV_AXIS,
	FC_EV_PAD_ADDED,
	FC_EV_PAD_REMOVED,
	FC_EV_TEXT
};

typedef struct FogcastEvent {
	int kind;
	int code;
	int value;
	int which;
	int down;
	char *text;
} FogcastEvent;

void fogcast_update_pads(void) {
	SDL_UpdateJoysticks();
	SDL_UpdateGamepads();
}

int fogcast_poll(FogcastEvent *out) {
	SDL_Event e;
	while (SDL_PollEvent(&e)) {
		out->text = NULL;
		switch (e.type) {
		case SDL_EVENT_QUIT:
			out->kind = FC_EV_QUIT;
			return 1;
		case SDL_EVENT_KEY_DOWN:
		case SDL_EVENT_KEY_UP:
			if (e.key.repeat) {
				continue;
			}
			out->kind = FC_EV_KEY;
			out->code = (int)e.key.key;
			out->down = e.key.down ? 1 : 0;
			return 1;
		case SDL_EVENT_TEXT_INPUT:
			out->kind = FC_EV_TEXT;
			if (e.text.text != NULL) {
				out->text = SDL_strdup(e.text.text);
			}
			return 1;
		case SDL_EVENT_GAMEPAD_BUTTON_DOWN:
		case SDL_EVENT_GAMEPAD_BUTTON_UP:
			out->kind = FC_EV_BUTTON;
			out->code = (int)e.gbutton.button;
			out->down = e.gbutton.down ? 1 : 0;
			out->which = (int)e.gbutton.which;
			return 1;
		case SDL_EVENT_JOYSTICK_BUTTON_DOWN:
		case SDL_EVENT_JOYSTICK_BUTTON_UP:
			out->kind = FC_EV_BUTTON;
			out->code = (int)e.jbutton.button;
			out->down = e.jbutton.down ? 1 : 0;
			out->which = (int)e.jbutton.which;
			return 1;
		case SDL_EVENT_JOYSTICK_HAT_MOTION:
			out->kind = FC_EV_BUTTON;
			out->which = (int)e.jhat.which;
			out->down = e.jhat.value != SDL_HAT_CENTERED ? 1 : 0;
			if (e.jhat.value & SDL_HAT_UP) {
				out->code = SDL_GAMEPAD_BUTTON_DPAD_UP;
			} else if (e.jhat.value & SDL_HAT_DOWN) {
				out->code = SDL_GAMEPAD_BUTTON_DPAD_DOWN;
			} else if (e.jhat.value & SDL_HAT_LEFT) {
				out->code = SDL_GAMEPAD_BUTTON_DPAD_LEFT;
			} else if (e.jhat.value & SDL_HAT_RIGHT) {
				out->code = SDL_GAMEPAD_BUTTON_DPAD_RIGHT;
			} else {
				out->code = SDL_GAMEPAD_BUTTON_DPAD_UP;
				out->down = 0;
			}
			return 1;
		case SDL_EVENT_GAMEPAD_AXIS_MOTION:
			out->kind = FC_EV_AXIS;
			out->code = (int)e.gaxis.axis;
			out->value = (int)e.gaxis.value;
			out->which = (int)e.gaxis.which;
			return 1;
		case SDL_EVENT_GAMEPAD_ADDED:
			out->kind = FC_EV_PAD_ADDED;
			out->which = (int)e.gdevice.which;
			return 1;
		case SDL_EVENT_GAMEPAD_REMOVED:
			out->kind = FC_EV_PAD_REMOVED;
			out->which = (int)e.gdevice.which;
			return 1;
		default:
			continue;
		}
	}
	return 0;
}

SDL_JoystickID fogcast_attach_virtual_gamepad(void) {
	SDL_VirtualJoystickDesc desc;
	SDL_INIT_INTERFACE(&desc);
	desc.type = SDL_JOYSTICK_TYPE_GAMEPAD;
	desc.naxes = 2;
	desc.nbuttons = SDL_GAMEPAD_BUTTON_COUNT;
	desc.nhats = 1;
	desc.button_mask = 0xffffffffu;
	desc.axis_mask = 0x00000003u;
	desc.name = "FogCast Smoke Pad";
	return SDL_AttachVirtualJoystick(&desc);
}

int fogcast_virtual_button_on(SDL_Gamepad *pad, int button, int down) {
	SDL_Joystick *js = NULL;
	if (pad != NULL) {
		js = SDL_GetGamepadJoystick(pad);
	}
	if (js == NULL) {
		return 0;
	}
	if (!SDL_SetJoystickVirtualButton(js, button, down)) {
		return 0;
	}
	if (button == SDL_GAMEPAD_BUTTON_DPAD_UP || button == SDL_GAMEPAD_BUTTON_DPAD_DOWN ||
		button == SDL_GAMEPAD_BUTTON_DPAD_LEFT || button == SDL_GAMEPAD_BUTTON_DPAD_RIGHT) {
		Uint8 hat = SDL_HAT_CENTERED;
		if (down) {
			if (button == SDL_GAMEPAD_BUTTON_DPAD_UP) {
				hat = SDL_HAT_UP;
			} else if (button == SDL_GAMEPAD_BUTTON_DPAD_DOWN) {
				hat = SDL_HAT_DOWN;
			} else if (button == SDL_GAMEPAD_BUTTON_DPAD_LEFT) {
				hat = SDL_HAT_LEFT;
			} else {
				hat = SDL_HAT_RIGHT;
			}
		}
		SDL_SetJoystickVirtualHat(js, 0, hat);
	}
	SDL_UpdateJoysticks();
	SDL_UpdateGamepads();
	return SDL_GetGamepadButton(pad, (SDL_GamepadButton)button) ? 1 : 2;
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const (
	evQuit       = C.FC_EV_QUIT
	evKey        = C.FC_EV_KEY
	evButton     = C.FC_EV_BUTTON
	evAxis       = C.FC_EV_AXIS
	evPadAdded   = C.FC_EV_PAD_ADDED
	evPadRemoved = C.FC_EV_PAD_REMOVED
	evText       = C.FC_EV_TEXT
)

func runWindow(ctx context.Context, opts Options) error {
	runtime.LockOSThread()
	C.SDL_SetMainReady()
	if !initSDLVideo() {
		return fmt.Errorf("sdl init: %s", sdlError())
	}
	defer C.SDL_Quit()

	var flags C.SDL_WindowFlags = C.SDL_WINDOW_RESIZABLE
	if opts.Fullscreen {
		flags |= C.SDL_WINDOW_FULLSCREEN
	}
	if opts.Hidden {
		flags |= C.SDL_WINDOW_HIDDEN
	}
	title := C.CString("FogCast")
	defer C.free(unsafe.Pointer(title))
	var window *C.SDL_Window
	var renderer *C.SDL_Renderer
	if !bool(C.SDL_CreateWindowAndRenderer(title, C.int(opts.Width), C.int(opts.Height), flags, &window, &renderer)) {
		return fmt.Errorf("sdl window: %s", sdlError())
	}
	// LIFO: close the gfx device (textures), then the renderer, then the window.
	// Window, events, gamepad, and text input stay on SDL; draw/present/textures
	// go through gfx.Device. Default is SDL3 WrapSDLRenderer. TENFOOT_GFX /
	// Options.GFX may select software or fpga-stub for tests; those still use
	// this SDL window shell.
	defer C.SDL_DestroyWindow(window)
	defer C.SDL_DestroyRenderer(renderer)
	dev, err := openGFXDevice(opts, unsafe.Pointer(renderer))
	if err != nil {
		return fmt.Errorf("gfx device: %w", err)
	}
	defer dev.Close()

	app := NewApp(NewClient(opts.APIBase, nil).withAPIHost(opts.APIHost), opts.Width, opts.Height, opts.MaxGames)
	app.SetPrefsPath(opts.prefsPath())
	app.SetLayout(parseLayout(opts.Layout))
	app.SetSafeAreaPct(opts.SafeAreaPct)
	app.ConfigureAttract(opts.NoAttract, opts.attractForced())
	if opts.Smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.SmokeTimeout)
		defer cancel()
	}
	app.Start(ctx)
	defer app.Stop()

	pads := map[C.SDL_JoystickID]*C.SDL_Gamepad{}
	defer func() {
		for id, pad := range pads {
			C.SDL_CloseGamepad(pad)
			delete(pads, id)
		}
	}()
	openExistingGamepads(pads, app)

	textures := map[string]gpuTexture{}
	defer destroyTextures(dev, textures)
	labels := map[string]gpuTexture{}
	defer destroyTextures(dev, labels)

	if opts.Smoke {
		return runSmoke(ctx, opts, app, dev, pads, textures, labels)
	}

	var stick stickTracker
	held := map[Command]bool{}
	textInput := false
	gpuParked := false
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		now := time.Now()
		C.fogcast_update_pads()
		for {
			var ev C.FogcastEvent
			if C.fogcast_poll(&ev) == 0 {
				break
			}
			if quit := handleSDLEvent(app, pads, &ev, now, &stick); quit {
				return nil
			}
		}
		if quit := pollGamepads(app, pads, held, &stick, now); quit {
			return nil
		}
		app.Tick(now)
		snap := app.Snapshot()
		textInput = syncTextInput(window, snap.OSK.Open, textInput)
		gpuParked = presentFrame(dev, snap, textures, labels, gpuParked)
		C.SDL_Delay(1)
	}
}

func syncTextInput(window *C.SDL_Window, want, on bool) bool {
	if want == on {
		return on
	}
	if want {
		C.SDL_StartTextInput(window)
		return true
	}
	C.SDL_StopTextInput(window)
	return false
}

func initSDLVideo() bool {
	bg := C.CString("SDL_JOYSTICK_ALLOW_BACKGROUND_EVENTS")
	defer C.free(unsafe.Pointer(bg))
	one := C.CString("1")
	defer C.free(unsafe.Pointer(one))
	C.SDL_SetHint(bg, one)
	if bool(C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_GAMEPAD)) {
		return true
	}
	// Headless sessions (SSH, no window server) fall back to SDL dummy video.
	dummy := C.CString("dummy")
	defer C.free(unsafe.Pointer(dummy))
	hint := C.CString("SDL_VIDEO_DRIVER")
	defer C.free(unsafe.Pointer(hint))
	C.SDL_Quit()
	C.SDL_SetHint(hint, dummy)
	C.SDL_SetHint(bg, one)
	return bool(C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_GAMEPAD))
}

func openGFXDevice(opts Options, renderer unsafe.Pointer) (gfx.Device, error) {
	backend, err := gfx.ParseBackend(opts.GFX)
	if err != nil {
		return nil, err
	}
	switch backend {
	case gfx.BackendSoftware:
		return gfx.NewSoftware(opts.Width, opts.Height)
	case gfx.BackendFPGAStub:
		return gfx.NewFPGAStub(opts.Width, opts.Height)
	default:
		return gfx.WrapSDLRenderer(renderer, opts.Width, opts.Height)
	}
}

func runSmoke(ctx context.Context, opts Options, app *App, dev gfx.Device, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, textures, labels map[string]gpuTexture) error {
	evidence := map[string]any{
		"api": opts.APIBase,
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: library load timeout: %w", err)
		}
		pumpSDL(app, pads, time.Now())
		pollGamepads(app, pads, map[Command]bool{}, nil, time.Now())
		app.Tick(time.Now())
		snap := app.Snapshot()
		if snap.LoadErr != "" {
			return fmt.Errorf("smoke: library load: %s", snap.LoadErr)
		}
		if len(snap.Games) >= 2 && !snap.Loading {
			evidence["games"] = len(snap.Games)
			break
		}
		C.SDL_Delay(10)
	}
	snap := app.Snapshot()
	if len(snap.Games) < 2 {
		return fmt.Errorf("smoke: need at least 2 titles, got %d (%s)", len(snap.Games), snap.Status)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: no cover artwork decoded: %w", err)
		}
		pumpSDL(app, pads, time.Now())
		app.Tick(time.Now())
		snap = app.Snapshot()
		_ = presentFrame(dev, snap, textures, labels, snap.GPUParked)
		if snap.CoverHits >= 1 || snap.GPUParked {
			break
		}
		C.SDL_Delay(10)
	}
	snap = app.Snapshot()
	evidence["covers"] = snap.CoverHits
	evidence["gpu_parked"] = snap.GPUParked
	if snap.CoverHits < 1 && !snap.GPUParked {
		return fmt.Errorf("smoke: no cover artwork decoded")
	}

	id := C.fogcast_attach_virtual_gamepad()
	if id == 0 {
		return fmt.Errorf("smoke: virtual gamepad: %s", sdlError())
	}
	defer C.SDL_DetachVirtualJoystick(id)

	padReady := time.Now().Add(2 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(padReady) {
		padReady = deadline
	}
	for time.Now().Before(padReady) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: virtual gamepad: %w", err)
		}
		pumpSDL(app, pads, time.Now())
		if pads[id] != nil {
			break
		}
		C.SDL_Delay(5)
	}
	if pads[id] == nil {
		pad := C.SDL_OpenGamepad(id)
		if pad != nil {
			pads[id] = pad
			app.SetGamepads(len(pads))
		}
	}
	if pads[id] == nil {
		return fmt.Errorf("smoke: virtual gamepad was not opened")
	}
	evidence["gamepad"] = true
	evidence["gamepads"] = len(pads)

	focusBefore := app.Snapshot().Grid.Focus
	evidence["focus_before"] = focusBefore
	if err := virtualPress(app, pads, id, C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT); err != nil {
		return err
	}
	focusAfter := app.Snapshot().Grid.Focus
	if focusAfter == focusBefore {
		if err := virtualPress(app, pads, id, C.SDL_GAMEPAD_BUTTON_DPAD_DOWN); err != nil {
			return err
		}
		focusAfter = app.Snapshot().Grid.Focus
	}
	evidence["focus_after"] = focusAfter
	if focusAfter == focusBefore {
		return fmt.Errorf("smoke: gamepad navigation did not move focus (%s)", padDump(app, pads))
	}

	snap = app.Snapshot()
	launchIdx := -1
	for i, game := range snap.Games {
		if launchBlockReason(game) == "" {
			launchIdx = i
			break
		}
	}
	if launchIdx < 0 {
		return fmt.Errorf("smoke: no unblocked title for POST /api/v1/session/launch")
	}
	if !app.focusIndex(launchIdx) {
		return fmt.Errorf("smoke: failed to focus unblocked title %d", launchIdx)
	}
	evidence["launch_focus"] = launchIdx

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %w", err)
	}
	if err := virtualPress(app, pads, id, C.SDL_GAMEPAD_BUTTON_SOUTH); err != nil {
		return err
	}
	for {
		launch := app.Snapshot().Launch
		if launch.HTTPStatus != 0 || launch.Phase == "ok" || launch.Phase == "host" || launch.Phase == "error" {
			break
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %#v: %w", launch, err)
		}
		pumpSDL(app, pads, time.Now())
		app.Tick(time.Now())
		C.SDL_Delay(10)
	}
	launch := app.Snapshot().Launch
	httpStatus := launch.HTTPStatus
	if httpStatus == 0 && (launch.Phase == "launching" || launch.Phase == "idle") {
		return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %#v", launch)
	}
	if httpStatus == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %#v: %w", launch, err)
		}
		game, ok := app.Selected()
		if !ok {
			return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %#v", launch)
		}
		if reason := launchBlockReason(game); reason != "" {
			return fmt.Errorf("smoke: selected title is blocked (%s); refusing POST /api/v1/session/launch fallback: %#v", reason, launch)
		}
		result, err := app.client.Launch(ctx, game.ID)
		if err != nil {
			return fmt.Errorf("smoke: POST /api/v1/session/launch: %w", err)
		}
		if result.HTTPStatus == 0 {
			return fmt.Errorf("smoke: POST /api/v1/session/launch returned no HTTP status")
		}
		httpStatus = result.HTTPStatus
		evidence["launch_ui"] = launch.Message
		evidence["launch_phase"] = "host"
		evidence["launch_error"] = result.ErrorCode
		evidence["launch_message"] = result.ErrorMessage
		evidence["launch_game_id"] = game.ID
	} else {
		evidence["launch_phase"] = launch.Phase
		evidence["launch_error"] = launch.ErrorCode
		evidence["launch_message"] = launch.Message
		evidence["launch_game_id"] = launch.GameID
	}
	evidence["launch_status"] = httpStatus
	if httpStatus == 0 {
		return fmt.Errorf("smoke: host launch HTTP status missing: %#v", evidence)
	}

	payload, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	fmt.Printf("tenfoot-smoke %s\n", payload)
	return nil
}

func virtualPress(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, id C.SDL_JoystickID, button C.int) error {
	held := map[Command]bool{}
	pad := pads[id]
	if pad == nil {
		return fmt.Errorf("smoke: virtual gamepad %d is not open (%s)", int(id), padDump(app, pads))
	}
	set := C.fogcast_virtual_button_on(pad, button, 1)
	if set == 0 {
		return fmt.Errorf("smoke: virtual button down: %s", sdlError())
	}
	C.SDL_PumpEvents()
	now := time.Now()
	pumpSDL(app, pads, now)
	if pollGamepads(app, pads, held, nil, now) {
		return nil
	}
	if len(held) == 0 {
		cmd := commandFromSDLButton(button)
		if cmd != CmdNone && cmd != CmdQuit {
			return fmt.Errorf("smoke: SDL gamepad path did not apply %v (%s)", cmd, padDump(app, pads))
		}
	}
	app.Tick(now)
	if C.fogcast_virtual_button_on(pad, button, 0) == 0 {
		return fmt.Errorf("smoke: virtual button up: %s", sdlError())
	}
	C.SDL_PumpEvents()
	now = time.Now()
	pumpSDL(app, pads, now)
	pollGamepads(app, pads, held, nil, now)
	app.Tick(now)
	return nil
}

type stickTracker struct {
	x, y int
	cmd  Command
}

func pumpSDL(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, now time.Time) {
	var stick stickTracker
	C.fogcast_update_pads()
	for {
		var ev C.FogcastEvent
		if C.fogcast_poll(&ev) == 0 {
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
	for _, pad := range pads {
		if pad == nil {
			continue
		}
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
			}
		}
		x := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTX))
		y := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTY))
		if cmd := CommandFromStickHeld(x, y, prevStick); cmd != CmdNone {
			pressed[cmd] = true
			stickCmd = cmd
		}
	}
	if stick != nil {
		stick.cmd = stickCmd
	}
	return applyPressed(app, pressed, held, now)
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
		if app.OSKOpen() {
			return handleSearchKey(app, ev, now)
		}
		cmd := commandFromSDLKey(ev.code)
		if ev.down != 0 {
			if C.SDL_GetModState()&C.SDL_KMOD_SHIFT != 0 && cmd == CmdViewNext {
				cmd = CmdViewPrev
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
	case evPadAdded:
		id := C.SDL_JoystickID(ev.which)
		if pads[id] != nil {
			return false
		}
		pad := C.SDL_OpenGamepad(id)
		if pad != nil {
			pads[id] = pad
			app.SetGamepads(len(pads))
		}
	case evPadRemoved:
		id := C.SDL_JoystickID(ev.which)
		if pad := pads[id]; pad != nil {
			C.SDL_CloseGamepad(pad)
			delete(pads, id)
			app.SetGamepads(len(pads))
		}
	}
	return false
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
		}
	}
	app.SetGamepads(len(pads))
}

func commandFromSDLButton(code C.int) Command {
	switch code {
	case C.SDL_GAMEPAD_BUTTON_DPAD_UP:
		return CommandFromButton(ButtonDPadUp)
	case C.SDL_GAMEPAD_BUTTON_DPAD_DOWN:
		return CommandFromButton(ButtonDPadDown)
	case C.SDL_GAMEPAD_BUTTON_DPAD_LEFT:
		return CommandFromButton(ButtonDPadLeft)
	case C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT:
		return CommandFromButton(ButtonDPadRight)
	case C.SDL_GAMEPAD_BUTTON_SOUTH:
		return CommandFromButton(ButtonSouth)
	case C.SDL_GAMEPAD_BUTTON_EAST:
		return CommandFromButton(ButtonEast)
	case C.SDL_GAMEPAD_BUTTON_WEST:
		return CommandFromButton(ButtonWest)
	case C.SDL_GAMEPAD_BUTTON_NORTH:
		return CommandFromButton(ButtonNorth)
	case C.SDL_GAMEPAD_BUTTON_LEFT_SHOULDER:
		return CommandFromButton(ButtonLeftShoulder)
	case C.SDL_GAMEPAD_BUTTON_RIGHT_SHOULDER:
		return CommandFromButton(ButtonRightShoulder)
	case C.SDL_GAMEPAD_BUTTON_START:
		return CommandFromButton(ButtonStart)
	case C.SDL_GAMEPAD_BUTTON_BACK:
		return CommandFromButton(ButtonBack)
	case C.SDL_GAMEPAD_BUTTON_GUIDE:
		return CmdSettings
	default:
		return CmdNone
	}
}

func handleSearchKey(app *App, ev *C.FogcastEvent, now time.Time) bool {
	key := C.SDL_Keycode(ev.code)
	if ev.down != 0 {
		switch key {
		case C.SDLK_BACKSPACE:
			app.SearchBackspace(now)
		case C.SDLK_ESCAPE:
			app.Press(CmdBack, now)
		case C.SDLK_RETURN:
			app.ConfirmSearch(now)
		case C.SDLK_UP, C.SDLK_DOWN, C.SDLK_LEFT, C.SDLK_RIGHT:
			app.Press(commandFromSDLKey(ev.code), now)
		}
		return false
	}
	switch key {
	case C.SDLK_ESCAPE:
		app.Release(CmdBack)
	case C.SDLK_RETURN:
		app.Release(CmdSelect)
	case C.SDLK_UP, C.SDLK_DOWN, C.SDLK_LEFT, C.SDLK_RIGHT:
		app.Release(commandFromSDLKey(ev.code))
	}
	return false
}

func commandFromSDLKey(code C.int) Command {
	switch C.SDL_Keycode(code) {
	case C.SDLK_UP, C.SDLK_W:
		return CmdUp
	case C.SDLK_DOWN:
		return CmdDown
	case C.SDLK_S:
		return CmdStop
	case C.SDLK_LEFT, C.SDLK_A:
		return CmdLeft
	case C.SDLK_RIGHT, C.SDLK_D:
		return CmdRight
	case C.SDLK_RETURN, C.SDLK_SPACE:
		return CmdSelect
	case C.SDLK_ESCAPE, C.SDLK_BACKSPACE:
		return CmdBack
	case C.SDLK_Q:
		return CmdQuit
	case C.SDLK_LEFTBRACKET:
		return CmdFilterPrev
	case C.SDLK_RIGHTBRACKET:
		return CmdFilterNext
	case C.SDLK_X:
		return CmdSortCycle
	case C.SDLK_SLASH, C.SDLK_F:
		return CmdSearch
	case C.SDLK_C:
		return CmdViewNext
	case C.SDLK_V:
		return CmdFavorite
	case C.SDLK_MINUS:
		return CmdSafeAreaOut
	case C.SDLK_EQUALS, C.SDLK_PLUS:
		return CmdSafeAreaIn
	case C.SDLK_L:
		return CmdLayoutCycle
	case C.SDLK_O:
		return CmdSettings
	case C.SDLK_G:
		return CmdFilters
	default:
		return CmdNone
	}
}

func sdlError() string {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		return "unknown SDL error"
	}
	return msg
}
