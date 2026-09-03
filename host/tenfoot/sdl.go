//go:build sdl3

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
	"image"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"
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

type sdlTexture struct {
	tex *C.SDL_Texture
	w   int
	h   int
	src *image.RGBA
	seq int
}

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
	// LIFO: destroy the renderer before the window it was created for.
	defer C.SDL_DestroyWindow(window)
	defer C.SDL_DestroyRenderer(renderer)
	C.SDL_SetRenderLogicalPresentation(renderer, C.int(opts.Width), C.int(opts.Height), C.SDL_LOGICAL_PRESENTATION_LETTERBOX)
	C.SDL_SetRenderVSync(renderer, 1)

	app := NewApp(NewClient(opts.APIBase, nil), opts.Width, opts.Height, opts.MaxGames)
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

	textures := map[string]sdlTexture{}
	defer destroyTextures(textures)
	labels := map[string]sdlTexture{}
	defer destroyTextures(labels)

	if opts.Smoke {
		return runSmoke(ctx, opts, app, renderer, pads, textures, labels)
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
		textInput = syncTextInput(window, snap.SearchOpen, textInput)
		gpuParked = presentFrame(renderer, snap, textures, labels, gpuParked)
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

func destroyTextures(textures map[string]sdlTexture) {
	for id, item := range textures {
		C.SDL_DestroyTexture(item.tex)
		delete(textures, id)
	}
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

func runSmoke(ctx context.Context, opts Options, app *App, renderer *C.SDL_Renderer, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, textures, labels map[string]sdlTexture) error {
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
		_ = presentFrame(renderer, snap, textures, labels, snap.GPUParked)
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
		if app.SearchOpen() {
			app.TypeText(text, now)
		}
	case evKey:
		if app.SearchOpen() {
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
			app.Press(CmdSelect, now)
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
	default:
		return CmdNone
	}
}

func presentFrame(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture, parked bool) bool {
	if snap.Attract.Active {
		for id, item := range textures {
			if id == "attract" {
				continue
			}
			C.SDL_DestroyTexture(item.tex)
			delete(textures, id)
		}
		drawAttract(renderer, snap, textures, labels)
		return parked
	}
	if item, ok := textures["attract"]; ok {
		C.SDL_DestroyTexture(item.tex)
		delete(textures, "attract")
	}
	return applyGPUPark(renderer, snap, textures, labels, parked)
}

func applyGPUPark(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture, parked bool) bool {
	if snap.GPUParked {
		if len(textures) > 0 {
			destroyTextures(textures)
		}
		if !parked {
			destroyTextures(labels)
		}
		drawNowPlaying(renderer, snap, labels)
		return true
	}
	syncTextures(renderer, snap, textures)
	drawFrame(renderer, snap, textures, labels)
	return false
}

func syncTextures(renderer *C.SDL_Renderer, snap Snapshot, textures map[string]sdlTexture) {
	start, end := snap.Grid.PrefetchRange(prefetchRows)
	needed := map[string]struct{}{}
	for i := start; i < end && i < len(snap.Games); i++ {
		id := snap.Games[i].ID
		img := snap.Covers[id]
		if img == nil {
			continue
		}
		needed[id] = struct{}{}
		if existing, ok := textures[id]; ok && existing.src == img {
			continue
		}
		if existing, ok := textures[id]; ok {
			C.SDL_DestroyTexture(existing.tex)
			delete(textures, id)
		}
		tex, err := uploadTexture(renderer, img)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tenfoot: cover texture %s: %v\n", id, err)
			continue
		}
		tex.src = img
		textures[id] = tex
	}
	for id, item := range textures {
		if _, ok := needed[id]; ok {
			continue
		}
		C.SDL_DestroyTexture(item.tex)
		delete(textures, id)
	}
}

func uploadTexture(renderer *C.SDL_Renderer, img *image.RGBA) (sdlTexture, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || len(img.Pix) == 0 {
		return sdlTexture{}, fmt.Errorf("empty image")
	}
	tex := C.SDL_CreateTexture(renderer, C.SDL_PIXELFORMAT_RGBA32, C.SDL_TEXTUREACCESS_STATIC, C.int(w), C.int(h))
	if tex == nil {
		return sdlTexture{}, fmt.Errorf("%s", sdlError())
	}
	C.SDL_SetTextureScaleMode(tex, C.SDL_SCALEMODE_LINEAR)
	if !bool(C.SDL_UpdateTexture(tex, nil, unsafe.Pointer(&img.Pix[0]), C.int(img.Stride))) {
		C.SDL_DestroyTexture(tex)
		return sdlTexture{}, fmt.Errorf("update texture: %s", sdlError())
	}
	C.SDL_SetTextureBlendMode(tex, C.SDL_BLENDMODE_BLEND)
	return sdlTexture{tex: tex, w: w, h: h}, nil
}

func drawFrame(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture) {
	C.SDL_SetRenderDrawColor(renderer, 12, 14, 20, 255)
	C.SDL_RenderClear(renderer)
	used := map[string]struct{}{}
	drawHeader(renderer, snap, labels, used)
	if snap.Grid.Mode == LayoutList {
		drawListRows(renderer, snap, textures, labels, used)
	} else {
		drawCoverCells(renderer, snap, textures, labels, used)
	}
	drawDetail(renderer, snap, labels, used)
	drawViewPicker(renderer, snap, labels, used)
	drawSettings(renderer, snap, labels, used)
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		C.SDL_DestroyTexture(item.tex)
		delete(labels, key)
	}
	C.SDL_RenderPresent(renderer)
}

func drawCoverCells(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture, used map[string]struct{}) {
	start, end := snap.Grid.VisibleRange()
	for i := start; i < end && i < len(snap.Games); i++ {
		x, y, ok := snap.Grid.CellOrigin(i)
		if !ok {
			continue
		}
		game := snap.Games[i]
		focused := i == snap.Grid.Focus
		if focused {
			fillRect(renderer, float32(x-4), float32(y-4), float32(snap.Grid.CellW+8), float32(snap.Grid.CellH+8), 255, 184, 48, 255)
		}
		fillRect(renderer, float32(x), float32(y), float32(snap.Grid.CellW), float32(snap.Grid.CellH-36), 28, 32, 44, 255)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := coverDestRect(x, y, snap.Grid.CellW, snap.Grid.CellH-36, tex.w, tex.h)
			dst := C.SDL_FRect{x: C.float(dx), y: C.float(dy), w: C.float(dw), h: C.float(dh)}
			C.SDL_RenderTexture(renderer, tex.tex, nil, &dst)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(renderer, float32(x+8), float32(y+8), float32(snap.Grid.CellW-16), float32(snap.Grid.CellH-52), r, g, b, 255)
			drawLabel(renderer, labels, used, "i:"+game.ID, x+16, y+24, snap.Grid.CellW-32, 22, initials(game.Title))
		}
		drawLabel(renderer, labels, used, "t:"+game.ID, x+6, y+snap.Grid.CellH-28, snap.Grid.CellW-12, 16, game.Title)
	}
}

func drawListRows(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture, used map[string]struct{}) {
	start, end := snap.Grid.VisibleRange()
	for i := start; i < end && i < len(snap.Games); i++ {
		x, y, ok := snap.Grid.CellOrigin(i)
		if !ok {
			continue
		}
		game := snap.Games[i]
		focused := i == snap.Grid.Focus
		if focused {
			fillRect(renderer, float32(x-4), float32(y-2), float32(snap.Grid.CellW+8), float32(snap.Grid.CellH+4), 255, 184, 48, 255)
		}
		fillRect(renderer, float32(x), float32(y), float32(snap.Grid.CellW), float32(snap.Grid.CellH), 28, 32, 44, 255)
		tx, ty, tw, th := snap.Grid.listThumbRect(x, y)
		fillRect(renderer, float32(tx), float32(ty), float32(tw), float32(th), 18, 20, 28, 255)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := coverDestRect(tx, ty, tw, th, tex.w, tex.h)
			dst := C.SDL_FRect{x: C.float(dx), y: C.float(dy), w: C.float(dw), h: C.float(dh)}
			C.SDL_RenderTexture(renderer, tex.tex, nil, &dst)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(renderer, float32(tx+2), float32(ty+2), float32(tw-4), float32(th-4), r, g, b, 255)
		}
		textX := tx + tw + 16
		textW := x + snap.Grid.CellW - textX - 12
		if textW < 1 {
			textW = 1
		}
		title := game.Title
		if game.Favorite {
			title = "* " + title
		}
		drawLabel(renderer, labels, used, "lt:"+game.ID, textX, y+12, textW, 22, title)
		meta := strings.TrimSpace(game.System)
		if year := strings.TrimSpace(game.Year); year != "" {
			if meta != "" {
				meta += "  ·  " + year
			} else {
				meta = year
			}
		}
		drawLabel(renderer, labels, used, "lm:"+game.ID, textX, y+40, textW, 16, meta)
	}
}

func drawLabel(renderer *C.SDL_Renderer, labels map[string]sdlTexture, used map[string]struct{}, key string, x, y, maxW, sizePx int, text string) {
	text = strings.TrimSpace(text)
	if text == "" || maxW < 1 {
		return
	}
	key = labelCacheKey(key, text, maxW, sizePx)
	used[key] = struct{}{}
	tex, ok := labels[key]
	if !ok {
		img := rasterizeLabel(text, maxW, sizePx)
		if img == nil {
			delete(used, key)
			return
		}
		uploaded, err := uploadTexture(renderer, img)
		if err != nil {
			delete(used, key)
			return
		}
		tex = uploaded
		labels[key] = tex
	}
	dst := C.SDL_FRect{x: C.float(x), y: C.float(y), w: C.float(tex.w), h: C.float(tex.h)}
	C.SDL_RenderTexture(renderer, tex.tex, nil, &dst)
}

func drawHeader(renderer *C.SDL_Renderer, snap Snapshot, labels map[string]sdlTexture, used map[string]struct{}) {
	x := snap.Grid.contentLeft()
	y := snap.Grid.headerY()
	w := snap.Grid.contentWidth()
	fillRect(renderer, float32(x), float32(y), float32(w), float32(snap.Grid.HeaderHeight), 18, 20, 28, 255)
	drawDebug(renderer, x+24, y+18, "FOGCAST", 3)
	pad := "KB DEBUG"
	if snap.Gamepads > 0 {
		pad = fmt.Sprintf("PAD %d", snap.Gamepads)
	}
	drawDebug(renderer, x+w-160, y+22, pad, 2)
	chrome := snap.ChromeLine()
	drawLabel(renderer, labels, used, "chrome", x+24, y+52, w-48, 18, chrome)
	hint := "LB/RB platform  X sort  Y search  hold A view  hold Y fav  SELECT layout  GUIDE settings"
	if snap.GPUParked || snap.Session.State == "active" {
		hint = "B stop  START quit  SELECT layout"
	}
	drawDebug(renderer, x+24, y+72, hint, 1)
}

func drawNowPlaying(renderer *C.SDL_Renderer, snap Snapshot, labels map[string]sdlTexture) {
	C.SDL_SetRenderDrawColor(renderer, 12, 14, 20, 255)
	C.SDL_RenderClear(renderer)
	used := map[string]struct{}{}
	drawHeader(renderer, snap, labels, used)
	pad := 48
	x := snap.Grid.contentLeft() + pad
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 40
	maxW := snap.Grid.contentWidth() - 2*pad
	if maxW < 1 {
		maxW = snap.Grid.contentWidth()
		x = snap.Grid.contentLeft() + 8
	}
	title := strings.TrimSpace(snap.Session.Title)
	if title == "" {
		title = strings.TrimSpace(snap.Session.GameID)
	}
	if title == "" {
		title = "Session active"
	}
	drawLabel(renderer, labels, used, "np-title", x, y, maxW, 28, title)
	y += 40
	meta := strings.TrimSpace(strings.TrimPrefix(snap.NowPlayingLine(), "Now playing"))
	meta = strings.TrimSpace(strings.TrimPrefix(meta, "  ·  "))
	if meta == "" {
		meta = snap.Session.State
	}
	drawLabel(renderer, labels, used, "np-meta", x, y, maxW, 18, meta)
	y += 32
	if progress := strings.TrimSpace(snap.Session.Progress); progress != "" {
		drawLabel(renderer, labels, used, "np-progress", x, y, maxW, 16, progress)
		y += 28
	}
	if status := strings.TrimSpace(snap.Status); status != "" && status != snap.NowPlayingLine() {
		drawLabel(renderer, labels, used, "np-status", x, y, maxW, 16, status)
	}
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		C.SDL_DestroyTexture(item.tex)
		delete(labels, key)
	}
	C.SDL_RenderPresent(renderer)
}

func drawDetail(renderer *C.SDL_Renderer, snap Snapshot, labels map[string]sdlTexture, used map[string]struct{}) {
	h := snap.Grid.FooterHeight
	if h < 1 {
		return
	}
	y := snap.Grid.footerY()
	x := snap.Grid.contentLeft()
	w := snap.Grid.contentWidth()
	fillRect(renderer, float32(x), float32(y), float32(w), float32(h), 16, 18, 26, 255)
	detail := snap.FocusDetail
	pad := 24
	title := strings.TrimSpace(detail.Title)
	if title == "" {
		title = "No title focused"
	}
	if detail.Favorite {
		title = "* " + title
	}
	drawLabel(renderer, labels, used, "d-title", x+pad, y+12, w-2*pad, 26, title)
	metaWidth := w - 2*pad
	facts, factsX, factsW, attr, attrX, attrW := layoutDetailMeta(detail, x+pad, metaWidth, 16)
	if facts != "" && factsW > 0 {
		drawLabel(renderer, labels, used, "d-meta", factsX, y+44, factsW, 16, facts)
	}
	if attr != "" && attrW > 0 {
		drawLabel(renderer, labels, used, "d-attr", attrX, y+44, attrW, 16, attr)
	}
	summaryWidth := w - 2*pad
	maxChars := summaryWidth / 8
	if maxChars < 20 {
		maxChars = 20
	}
	for i, line := range wrapWords(detail.Summary, maxChars, 2) {
		drawLabel(renderer, labels, used, fmt.Sprintf("d-sum-%d", i), x+pad, y+68+i*20, summaryWidth, 16, line)
	}
}

func drawViewPicker(renderer *C.SDL_Renderer, snap Snapshot, labels map[string]sdlTexture, used map[string]struct{}) {
	if !snap.ViewPicker || len(snap.Views) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	panelW := 420
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 200 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 40
	maxRows := 10
	if maxRows > len(snap.Views) {
		maxRows = len(snap.Views)
	}
	panelH := headerH + maxRows*rowH + 16
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	fillRect(renderer, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(renderer, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(renderer, labels, used, "view-title", x+16, y+10, panelW-32, 18, "Library view")
	start := snap.ViewPickerIndex - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(snap.Views) {
		start = len(snap.Views) - maxRows
	}
	if start < 0 {
		start = 0
	}
	for i := 0; i < maxRows; i++ {
		idx := start + i
		if idx >= len(snap.Views) {
			break
		}
		rowY := y + headerH + i*rowH
		if idx == snap.ViewPickerIndex {
			fillRect(renderer, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		label := snap.Views[idx].Label
		if label == "" {
			label = snap.Views[idx].ID
		}
		if snap.Views[idx].ID == snap.Collection {
			label = label + "  *"
		}
		drawLabel(renderer, labels, used, fmt.Sprintf("view-%d", idx), x+20, rowY, panelW-40, 16, label)
	}
}

func drawSettings(renderer *C.SDL_Renderer, snap Snapshot, labels map[string]sdlTexture, used map[string]struct{}) {
	if !snap.Settings.Open || len(snap.Settings.Rows) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	C.SDL_SetRenderDrawBlendMode(renderer, C.SDL_BLENDMODE_BLEND)
	fillRect(renderer, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	C.SDL_SetRenderDrawBlendMode(renderer, C.SDL_BLENDMODE_NONE)
	panelW := 720
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 280 {
		panelW = contentW - 24
	}
	rowH := 32
	headerH := 44
	footerH := 28
	rows := snap.Settings.Rows
	panelH := headerH + len(rows)*rowH + footerH
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	if panelH > maxH {
		panelH = maxH
	}
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (contentH-panelH)/2
	}
	fillRect(renderer, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(renderer, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	title := "Settings"
	if snap.Settings.LibraryCount > 0 {
		title = fmt.Sprintf("Settings  ·  %d libraries", snap.Settings.LibraryCount)
	}
	drawLabel(renderer, labels, used, "set-title", x+16, y+12, panelW-32, 18, title)
	visible := (panelH - headerH - footerH) / rowH
	if visible < 1 {
		visible = 1
	}
	if visible > len(rows) {
		visible = len(rows)
	}
	start := snap.Settings.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}
	if start < 0 {
		start = 0
	}
	labelW := 140
	if labelW > panelW/3 {
		labelW = panelW / 3
	}
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= len(rows) {
			break
		}
		rowY := y + headerH + i*rowH
		if idx == snap.Settings.Index {
			fillRect(renderer, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		row := rows[idx]
		drawLabel(renderer, labels, used, fmt.Sprintf("set-l-%s", row.ID), x+20, rowY+4, labelW, 16, row.Label)
		drawLabel(renderer, labels, used, fmt.Sprintf("set-v-%s-%d", row.ID, idx), x+20+labelW, rowY+4, panelW-labelW-40, 16, row.Value)
	}
	status := strings.TrimSpace(snap.Settings.Status)
	if status == "" {
		status = "A confirm  B close  Left/Right change"
	}
	if snap.Settings.Loading {
		status = "loading host settings"
	}
	drawLabel(renderer, labels, used, "set-status", x+16, y+panelH-24, panelW-32, 14, status)
}

func drawAttract(renderer *C.SDL_Renderer, snap Snapshot, textures, labels map[string]sdlTexture) {
	C.SDL_SetRenderDrawColor(renderer, 8, 8, 12, 255)
	C.SDL_RenderClear(renderer)
	used := map[string]struct{}{}
	x := snap.Grid.contentLeft()
	y := snap.Grid.contentTop()
	w := snap.Grid.contentWidth()
	h := snap.Grid.contentHeight()
	titleH := 56
	stageH := h - titleH
	if stageH < 1 {
		stageH = h
		titleH = 0
	}
	if img := snap.Attract.Image; img != nil {
		b := img.Bounds()
		tw, th := b.Dx(), b.Dy()
		existing, ok := textures["attract"]
		needUpload := !ok || existing.src != img || existing.w != tw || existing.h != th || existing.seq != snap.Attract.FrameSeq
		if needUpload && ok && existing.tex != nil && existing.w == tw && existing.h == th && len(img.Pix) > 0 {
			if bool(C.SDL_UpdateTexture(existing.tex, nil, unsafe.Pointer(&img.Pix[0]), C.int(img.Stride))) {
				existing.src = img
				existing.seq = snap.Attract.FrameSeq
				textures["attract"] = existing
				needUpload = false
			} else {
				C.SDL_DestroyTexture(existing.tex)
				delete(textures, "attract")
				ok = false
			}
		}
		if needUpload {
			if ok {
				C.SDL_DestroyTexture(existing.tex)
				delete(textures, "attract")
			}
			if tex, err := uploadTexture(renderer, img); err == nil {
				tex.src = img
				tex.seq = snap.Attract.FrameSeq
				textures["attract"] = tex
			}
		}
		if tex, ok := textures["attract"]; ok {
			dx, dy, dw, dh := coverDestRect(x, y, w, stageH, tex.w, tex.h)
			dst := C.SDL_FRect{x: C.float(dx), y: C.float(dy), w: C.float(dw), h: C.float(dh)}
			C.SDL_RenderTexture(renderer, tex.tex, nil, &dst)
		}
	} else if item, ok := textures["attract"]; ok {
		C.SDL_DestroyTexture(item.tex)
		delete(textures, "attract")
	}
	title := strings.TrimSpace(snap.Attract.Title)
	if title == "" {
		title = strings.TrimSpace(snap.Attract.GameID)
	}
	if title != "" && titleH > 0 {
		drawLabel(renderer, labels, used, "attract-title", x+24, y+stageH+12, w-48, 28, title)
	}
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		C.SDL_DestroyTexture(item.tex)
		delete(labels, key)
	}
	C.SDL_RenderPresent(renderer)
}

func fillRect(renderer *C.SDL_Renderer, x, y, w, h float32, r, g, b, a uint8) {
	C.SDL_SetRenderDrawColor(renderer, C.Uint8(r), C.Uint8(g), C.Uint8(b), C.Uint8(a))
	rect := C.SDL_FRect{x: C.float(x), y: C.float(y), w: C.float(w), h: C.float(h)}
	C.SDL_RenderFillRect(renderer, &rect)
}

func drawDebug(renderer *C.SDL_Renderer, x, y int, text string, scale int) {
	if text == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	cstr := C.CString(text)
	defer C.free(unsafe.Pointer(cstr))
	C.SDL_SetRenderScale(renderer, C.float(scale), C.float(scale))
	C.SDL_SetRenderDrawColor(renderer, 236, 240, 248, 255)
	C.SDL_RenderDebugText(renderer, C.float(x)/C.float(scale), C.float(y)/C.float(scale), cstr)
	C.SDL_SetRenderScale(renderer, 1, 1)
}

func fitText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if maxChars < 1 || len(runes) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func initials(title string) string {
	fields := strings.Fields(title)
	if len(fields) == 0 {
		return "?"
	}
	out := []rune{[]rune(fields[0])[0]}
	if len(fields) > 1 {
		out = append(out, []rune(fields[1])[0])
	}
	return strings.ToUpper(string(out))
}

func placeholderColor(title string) (uint8, uint8, uint8) {
	var h uint32
	for i := 0; i < len(title); i++ {
		h = h*33 + uint32(title[i])
	}
	return uint8(40 + h%80), uint8(50 + (h>>8)%90), uint8(70 + (h>>16)%100)
}

func sdlError() string {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		return "unknown SDL error"
	}
	return msg
}
