//go:build sdl3

// SDL window, event, gamepad, mouse, and text-input loop for the tenfoot launcher.
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
	FC_EV_TEXT,
	FC_EV_MOUSE_MOVE,
	FC_EV_MOUSE_BUTTON
};

typedef struct FogcastEvent {
	int kind;
	int code;
	int value;
	int which;
	int down;
	int x;
	int y;
	char *text;
} FogcastEvent;

void fogcast_update_pads(void) {
	SDL_UpdateJoysticks();
	SDL_UpdateGamepads();
}

int fogcast_poll(FogcastEvent *out, SDL_Renderer *renderer) {
	SDL_Event e;
	while (SDL_PollEvent(&e)) {
		out->text = NULL;
		out->x = 0;
		out->y = 0;
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
		case SDL_EVENT_MOUSE_MOTION:
			if (renderer != NULL) {
				SDL_ConvertEventToRenderCoordinates(renderer, &e);
			}
			out->kind = FC_EV_MOUSE_MOVE;
			out->x = (int)e.motion.x;
			out->y = (int)e.motion.y;
			return 1;
		case SDL_EVENT_MOUSE_BUTTON_DOWN:
		case SDL_EVENT_MOUSE_BUTTON_UP:
			if (renderer != NULL) {
				SDL_ConvertEventToRenderCoordinates(renderer, &e);
			}
			out->kind = FC_EV_MOUSE_BUTTON;
			out->code = (int)e.button.button;
			out->down = e.button.down ? 1 : 0;
			out->x = (int)e.button.x;
			out->y = (int)e.button.y;
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
	"strings"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	evQuit        = C.FC_EV_QUIT
	evKey         = C.FC_EV_KEY
	evButton      = C.FC_EV_BUTTON
	evAxis        = C.FC_EV_AXIS
	evPadAdded    = C.FC_EV_PAD_ADDED
	evPadRemoved  = C.FC_EV_PAD_REMOVED
	evText        = C.FC_EV_TEXT
	evMouseMove   = C.FC_EV_MOUSE_MOVE
	evMouseButton = C.FC_EV_MOUSE_BUTTON
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
	// Options.GFX may select software, fpga, or fpga-stub; those still use
	// this SDL window shell. fpga records FC2D and rasters with Software
	// (IsStub true; no programmed 2D core). linuxfb is the kit framebuffer
	// Device and is rejected here.
	defer C.SDL_DestroyWindow(window)
	defer C.SDL_DestroyRenderer(renderer)
	dev, err := openGFXDevice(opts, unsafe.Pointer(renderer))
	if err != nil {
		return fmt.Errorf("gfx device: %w", err)
	}
	defer dev.Close()

	app := NewApp(NewClient(opts.APIBase, nil).withAPIHost(opts.APIHost), opts.Width, opts.Height, opts.MaxGames)
	if spec := strings.TrimSpace(opts.InputProfile); spec != "" {
		profile, err := inputmap.Resolve(spec)
		if err != nil {
			return fmt.Errorf("input profile: %w", err)
		}
		remap, err := inputmap.NewRemapper(profile)
		if err != nil {
			return fmt.Errorf("input profile: %w", err)
		}
		app.SetRemapper(remap)
	}
	look, err := theme.Resolve(opts.Theme)
	if err != nil {
		return fmt.Errorf("theme: %w", err)
	}
	app.SetTheme(look)
	app.SetDebugHUD(opts.DebugHUD)
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
		return runSmoke(ctx, opts, app, dev, pads, textures, labels, renderer)
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
			if C.fogcast_poll(&ev, renderer) == 0 {
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
	case gfx.BackendFPGA:
		return gfx.NewFPGA(opts.Width, opts.Height)
	case gfx.BackendFPGAStub:
		return gfx.NewFPGAStub(opts.Width, opts.Height)
	case gfx.BackendLinuxFB:
		return nil, fmt.Errorf("linuxfb is the kit framebuffer Device; use cmd/tenfoot-linuxfb-spike")
	default:
		return gfx.WrapSDLRenderer(renderer, opts.Width, opts.Height)
	}
}

func runSmoke(ctx context.Context, opts Options, app *App, dev gfx.Device, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, textures, labels map[string]gpuTexture, renderer *C.SDL_Renderer) error {
	evidence := map[string]any{
		"api": opts.APIBase,
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: library load timeout: %w", err)
		}
		pumpSDL(app, pads, time.Now(), renderer)
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
		pumpSDL(app, pads, time.Now(), renderer)
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

	if err := smokeKeyboardNav(app, pads, evidence); err != nil {
		return err
	}
	if err := smokeMouseNav(app, pads, evidence); err != nil {
		return err
	}

	id := C.fogcast_attach_virtual_gamepad()
	if id != 0 {
		defer C.SDL_DetachVirtualJoystick(id)
		padReady := time.Now().Add(2 * time.Second)
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(padReady) {
			padReady = deadline
		}
		for time.Now().Before(padReady) {
			if err := ctx.Err(); err != nil {
				break
			}
			pumpSDL(app, pads, time.Now(), renderer)
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
	}
	evidence["gamepads"] = len(pads)
	evidence["gamepad"] = pads[id] != nil
	if pads[id] != nil {
		padFocusBefore := app.Snapshot().Grid.Focus
		if err := virtualPress(app, pads, id, C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT); err == nil {
			if app.Snapshot().Grid.Focus != padFocusBefore {
				evidence["gamepad_nav"] = true
			}
		}
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
	if err := virtualMouseClickSelected(app, pads); err != nil {
		return err
	}
	evidence["mouse_launch"] = true
	for {
		launch := app.Snapshot().Launch
		if launch.HTTPStatus != 0 || launch.Phase == "ok" || launch.Phase == "host" || launch.Phase == "error" {
			break
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: host launch did not POST /api/v1/session/launch: %#v: %w", launch, err)
		}
		pumpSDL(app, pads, time.Now(), renderer)
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

func smokeKeyboardNav(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, evidence map[string]any) error {
	focusBefore := app.Snapshot().Grid.Focus
	evidence["keyboard_focus_before"] = focusBefore
	if err := virtualKey(app, pads, C.SDLK_RIGHT); err != nil {
		return err
	}
	focusAfter := app.Snapshot().Grid.Focus
	if focusAfter == focusBefore {
		if err := virtualKey(app, pads, C.SDLK_DOWN); err != nil {
			return err
		}
		focusAfter = app.Snapshot().Grid.Focus
	}
	evidence["keyboard_focus_after"] = focusAfter
	if focusAfter == focusBefore {
		return fmt.Errorf("smoke: keyboard navigation did not move focus (%s)", padDump(app, pads))
	}

	snap := app.Snapshot()
	cols := snap.Grid.Columns
	if cols < 1 {
		cols = 1
	}
	if n := len(snap.Games); n > 0 {
		rowStart := ((n - 1) / cols) * cols
		if !app.focusIndex(rowStart) {
			return fmt.Errorf("smoke: keyboard failed to focus last row (%s)", padDump(app, pads))
		}
	}
	if err := virtualKey(app, pads, C.SDLK_DOWN); err != nil {
		return err
	}
	if !app.Snapshot().Detail.Open {
		return fmt.Errorf("smoke: keyboard did not open detail (%s)", padDump(app, pads))
	}
	if err := virtualKey(app, pads, C.SDLK_ESCAPE); err != nil {
		return err
	}
	if app.Snapshot().Detail.Open {
		return fmt.Errorf("smoke: Esc did not close detail")
	}

	if err := virtualKey(app, pads, C.SDLK_TAB); err != nil {
		return err
	}
	if !app.Snapshot().SearchOpen {
		return fmt.Errorf("smoke: Tab did not open search")
	}
	if err := virtualKey(app, pads, C.SDLK_ESCAPE); err != nil {
		return err
	}
	if app.Snapshot().SearchOpen {
		return fmt.Errorf("smoke: Esc did not close search")
	}
	evidence["keyboard_nav"] = true
	return nil
}

func smokeMouseNav(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, evidence map[string]any) error {
	if !app.focusIndex(0) {
		return fmt.Errorf("smoke: mouse failed to focus first title (%s)", padDump(app, pads))
	}
	grid := app.Snapshot().Grid
	x, y, w, h, ok := grid.CellRect(1)
	if !ok {
		return fmt.Errorf("smoke: mouse target cell is offscreen (%s)", padDump(app, pads))
	}
	if err := virtualMouseMove(app, pads, x+w/2, y+h/2); err != nil {
		return err
	}
	focus := app.Snapshot().Grid.Focus
	evidence["mouse_focus_after"] = focus
	if focus != 1 {
		return fmt.Errorf("smoke: mouse hover did not move focus (got %d want 1; %s)", focus, padDump(app, pads))
	}
	evidence["mouse_nav"] = true
	return nil
}

func virtualMouseMove(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, x, y int) error {
	now := time.Now()
	ev := C.FogcastEvent{kind: evMouseMove, x: C.int(x), y: C.int(y)}
	if handleSDLEvent(app, pads, &ev, now, nil) {
		return nil
	}
	app.Tick(now)
	return nil
}

func virtualMouseClick(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, x, y int) error {
	now := time.Now()
	down := C.FogcastEvent{kind: evMouseButton, code: C.SDL_BUTTON_LEFT, down: 1, x: C.int(x), y: C.int(y)}
	if handleSDLEvent(app, pads, &down, now, nil) {
		return nil
	}
	app.Tick(now)
	up := C.FogcastEvent{kind: evMouseButton, code: C.SDL_BUTTON_LEFT, down: 0, x: C.int(x), y: C.int(y)}
	_ = handleSDLEvent(app, pads, &up, now, nil)
	app.Tick(now)
	return nil
}

func virtualMouseClickSelected(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad) error {
	snap := app.Snapshot()
	x, y, w, h, ok := snap.Grid.CellRect(snap.Grid.Focus)
	if !ok {
		return fmt.Errorf("smoke: focused cell is offscreen for mouse click (%s)", padDump(app, pads))
	}
	return virtualMouseClick(app, pads, x+w/2, y+h/2)
}

func virtualKey(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, key C.int) error {
	now := time.Now()
	down := C.FogcastEvent{kind: evKey, code: key, down: 1}
	if handleSDLEvent(app, pads, &down, now, nil) {
		return nil
	}
	app.Tick(now)
	up := C.FogcastEvent{kind: evKey, code: key, down: 0}
	_ = handleSDLEvent(app, pads, &up, now, nil)
	app.Tick(now)
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
	pumpSDL(app, pads, now, nil)
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
	pumpSDL(app, pads, now, nil)
	pollGamepads(app, pads, held, nil, now)
	app.Tick(now)
	return nil
}

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
	remap := app.remapper()
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
				pressed[remapSDLCommand(remap, commandFromSDLButton(button))] = true
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
		if app.ForwardsCoreKeyboard() {
			// Play-session ZX81 matrix: do not steal keys for sofa browse/nav.
			// Drop sofa hold-repeat so a direction held at launch cannot walk
			// the grid after the key is released into this path.
			app.repeat.Clear()
			if event, ok := coreKeyFromSDL(ev.code, ev.down != 0); ok {
				app.SendCoreKey(event)
			}
			return false
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
		app.PointerMove(int(ev.x), int(ev.y), now)
	case evMouseButton:
		if ev.code != C.SDL_BUTTON_LEFT {
			return false
		}
		if ev.down != 0 {
			app.PointerClick(int(ev.x), int(ev.y), now)
		}
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

func remapSDLCommand(remap *inputmap.Remapper, cmd Command) Command {
	if cmd == CmdNone || cmd == CmdSettings || remap == nil {
		return cmd
	}
	button := buttonFromCommand(cmd)
	if button == ButtonNone {
		return cmd
	}
	e := remoteinput.Event{
		Device: remoteinput.DeviceGamepad,
		Kind:   remoteinput.KindButton,
		Action: remoteinput.ActionPress,
		Code:   LogicalFromButton(button),
	}
	return CommandFromLogical(remap.Apply(e))
}

func buttonFromCommand(cmd Command) Button {
	switch cmd {
	case CmdUp:
		return ButtonDPadUp
	case CmdDown:
		return ButtonDPadDown
	case CmdLeft:
		return ButtonDPadLeft
	case CmdRight:
		return ButtonDPadRight
	case CmdSelect:
		return ButtonSouth
	case CmdBack:
		return ButtonEast
	case CmdQuit:
		return ButtonStart
	case CmdLayoutCycle:
		return ButtonBack
	case CmdSortCycle:
		return ButtonWest
	case CmdSearch:
		return ButtonNorth
	case CmdFilterPrev:
		return ButtonLeftShoulder
	case CmdFilterNext:
		return ButtonRightShoulder
	default:
		return ButtonNone
	}
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
	shift := C.SDL_GetModState()&C.SDL_KMOD_SHIFT != 0
	tabCmd := CmdTab
	if shift {
		tabCmd = CmdTabPrev
	}
	if ev.down != 0 {
		switch key {
		case C.SDLK_BACKSPACE:
			app.SearchBackspace(now)
		case C.SDLK_ESCAPE:
			app.Press(CmdBack, now)
		case C.SDLK_RETURN:
			app.ConfirmSearch(now)
		case C.SDLK_TAB:
			app.Press(tabCmd, now)
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
	case C.SDLK_TAB:
		app.Release(tabCmd)
	case C.SDLK_UP, C.SDLK_DOWN, C.SDLK_LEFT, C.SDLK_RIGHT:
		app.Release(commandFromSDLKey(ev.code))
	}
	return false
}

// coreKeyFromSDL maps play-session keys onto the ZX81 matrix. Browse/nav
// uses CommandFromKey; this path runs only while ForwardsCoreKeyboard.
func coreKeyFromSDL(code C.int, down bool) (remoteinput.Event, bool) {
	var key remoteinput.Code
	switch C.SDL_Keycode(code) {
	case C.SDLK_RETURN:
		key = zx81keys.KeyEnter
	case C.SDLK_SPACE:
		key = zx81keys.KeySpace
	case C.SDLK_LSHIFT, C.SDLK_RSHIFT:
		key = zx81keys.KeyShift
	case C.SDLK_PERIOD:
		key = zx81keys.KeyPeriod
	case C.SDLK_0:
		key = zx81keys.Digit(0)
	case C.SDLK_1:
		key = zx81keys.Digit(1)
	case C.SDLK_2:
		key = zx81keys.Digit(2)
	case C.SDLK_3:
		key = zx81keys.Digit(3)
	case C.SDLK_4:
		key = zx81keys.Digit(4)
	case C.SDLK_5:
		key = zx81keys.Digit(5)
	case C.SDLK_6:
		key = zx81keys.Digit(6)
	case C.SDLK_7:
		key = zx81keys.Digit(7)
	case C.SDLK_8:
		key = zx81keys.Digit(8)
	case C.SDLK_9:
		key = zx81keys.Digit(9)
	case C.SDLK_A:
		key = zx81keys.Letter('A')
	case C.SDLK_B:
		key = zx81keys.Letter('B')
	case C.SDLK_C:
		key = zx81keys.Letter('C')
	case C.SDLK_D:
		key = zx81keys.Letter('D')
	case C.SDLK_E:
		key = zx81keys.Letter('E')
	case C.SDLK_F:
		key = zx81keys.Letter('F')
	case C.SDLK_G:
		key = zx81keys.Letter('G')
	case C.SDLK_H:
		key = zx81keys.Letter('H')
	case C.SDLK_I:
		key = zx81keys.Letter('I')
	case C.SDLK_J:
		key = zx81keys.Letter('J')
	case C.SDLK_K:
		key = zx81keys.Letter('K')
	case C.SDLK_L:
		key = zx81keys.Letter('L')
	case C.SDLK_M:
		key = zx81keys.Letter('M')
	case C.SDLK_N:
		key = zx81keys.Letter('N')
	case C.SDLK_O:
		key = zx81keys.Letter('O')
	case C.SDLK_P:
		key = zx81keys.Letter('P')
	case C.SDLK_Q:
		key = zx81keys.Letter('Q')
	case C.SDLK_R:
		key = zx81keys.Letter('R')
	case C.SDLK_S:
		key = zx81keys.Letter('S')
	case C.SDLK_T:
		key = zx81keys.Letter('T')
	case C.SDLK_U:
		key = zx81keys.Letter('U')
	case C.SDLK_V:
		key = zx81keys.Letter('V')
	case C.SDLK_W:
		key = zx81keys.Letter('W')
	case C.SDLK_X:
		key = zx81keys.Letter('X')
	case C.SDLK_Y:
		key = zx81keys.Letter('Y')
	case C.SDLK_Z:
		key = zx81keys.Letter('Z')
	default:
		return remoteinput.Event{}, false
	}
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: action, Code: key}, true
}

func commandFromSDLKey(code C.int) Command {
	return CommandFromKey(sdlKeyName(code))
}

func sdlKeyName(code C.int) string {
	switch C.SDL_Keycode(code) {
	case C.SDLK_UP:
		return "up"
	case C.SDLK_W:
		return "w"
	case C.SDLK_DOWN:
		return "down"
	case C.SDLK_S:
		return "s"
	case C.SDLK_LEFT:
		return "left"
	case C.SDLK_A:
		return "a"
	case C.SDLK_RIGHT:
		return "right"
	case C.SDLK_D:
		return "d"
	case C.SDLK_RETURN:
		return "return"
	case C.SDLK_SPACE:
		return "space"
	case C.SDLK_ESCAPE:
		return "escape"
	case C.SDLK_BACKSPACE:
		return "backspace"
	case C.SDLK_TAB:
		return "tab"
	case C.SDLK_Q:
		return "q"
	case C.SDLK_LEFTBRACKET:
		return "leftbracket"
	case C.SDLK_RIGHTBRACKET:
		return "rightbracket"
	case C.SDLK_X:
		return "x"
	case C.SDLK_SLASH:
		return "slash"
	case C.SDLK_F:
		return "f"
	case C.SDLK_C:
		return "c"
	case C.SDLK_V:
		return "v"
	case C.SDLK_MINUS:
		return "minus"
	case C.SDLK_EQUALS:
		return "equals"
	case C.SDLK_PLUS:
		return "plus"
	case C.SDLK_L:
		return "l"
	case C.SDLK_O:
		return "o"
	case C.SDLK_G:
		return "g"
	default:
		return ""
	}
}

func sdlError() string {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		return "unknown SDL error"
	}
	return msg
}
