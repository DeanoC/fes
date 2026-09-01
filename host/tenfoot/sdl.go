//go:build sdl3

package tenfoot

/*
#cgo pkg-config: sdl3
#include <SDL3/SDL.h>
#include <SDL3/SDL_main.h>
#include <stdlib.h>

enum {
	FC_EV_NONE = 0,
	FC_EV_QUIT,
	FC_EV_KEY,
	FC_EV_BUTTON,
	FC_EV_AXIS,
	FC_EV_PAD_ADDED,
	FC_EV_PAD_REMOVED
};

typedef struct FogcastEvent {
	int kind;
	int code;
	int value;
	int which;
	int down;
} FogcastEvent;

void fogcast_update_pads(void) {
	SDL_UpdateJoysticks();
	SDL_UpdateGamepads();
}

int fogcast_poll(FogcastEvent *out) {
	SDL_Event e;
	while (SDL_PollEvent(&e)) {
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
)

type sdlTexture struct {
	tex *C.SDL_Texture
	w   int
	h   int
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
	defer C.SDL_DestroyRenderer(renderer)
	defer C.SDL_DestroyWindow(window)
	C.SDL_SetRenderLogicalPresentation(renderer, C.int(opts.Width), C.int(opts.Height), C.SDL_LOGICAL_PRESENTATION_LETTERBOX)
	C.SDL_SetRenderVSync(renderer, 1)

	app := NewApp(NewClient(opts.APIBase, nil), opts.Width, opts.Height, opts.MaxGames)
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
	defer func() {
		for id, item := range textures {
			C.SDL_DestroyTexture(item.tex)
			delete(textures, id)
		}
	}()

	if opts.Smoke {
		return runSmoke(ctx, opts, app, renderer, pads, textures)
	}

	var stick stickTracker
	held := map[Command]bool{}
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
		if quit := pollGamepads(app, pads, held, now); quit {
			return nil
		}
		app.Tick(now)
		snap := app.Snapshot()
		syncTextures(renderer, snap, textures)
		drawFrame(renderer, snap, textures)
		C.SDL_Delay(1)
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
	// Agent / SSH sessions often have Aqua but no Cocoa window server access.
	dummy := C.CString("dummy")
	defer C.free(unsafe.Pointer(dummy))
	hint := C.CString("SDL_VIDEO_DRIVER")
	defer C.free(unsafe.Pointer(hint))
	C.SDL_Quit()
	C.SDL_SetHint(hint, dummy)
	C.SDL_SetHint(bg, one)
	return bool(C.SDL_Init(C.SDL_INIT_VIDEO | C.SDL_INIT_GAMEPAD))
}

func runSmoke(ctx context.Context, opts Options, app *App, renderer *C.SDL_Renderer, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, textures map[string]sdlTexture) error {
	ctx, cancel := context.WithTimeout(ctx, opts.SmokeTimeout)
	defer cancel()
	evidence := map[string]any{
		"api": opts.APIBase,
	}
	deadline := time.Now().Add(opts.SmokeTimeout)

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("smoke: library load timeout: %w", err)
		}
		pumpSDL(app, pads, time.Now())
		pollGamepads(app, pads, map[Command]bool{}, time.Now())
		app.Tick(time.Now())
		snap := app.Snapshot()
		if snap.LoadErr != "" {
			return fmt.Errorf("smoke: library load: %s", snap.LoadErr)
		}
		if len(snap.Games) >= 2 && !snap.Loading {
			evidence["games"] = len(snap.Games)
			break
		}
		if len(snap.Games) >= 2 {
			evidence["games"] = len(snap.Games)
			break
		}
		C.SDL_Delay(10)
	}
	snap := app.Snapshot()
	if len(snap.Games) < 2 {
		return fmt.Errorf("smoke: need at least 2 titles, got %d (%s)", len(snap.Games), snap.Status)
	}

	coverDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(coverDeadline) {
		pumpSDL(app, pads, time.Now())
		app.Tick(time.Now())
		snap = app.Snapshot()
		syncTextures(renderer, snap, textures)
		drawFrame(renderer, snap, textures)
		if snap.CoverHits >= 1 {
			break
		}
		C.SDL_Delay(10)
	}
	snap = app.Snapshot()
	evidence["covers"] = snap.CoverHits
	if snap.CoverHits < 1 {
		return fmt.Errorf("smoke: no cover artwork decoded")
	}

	id := C.fogcast_attach_virtual_gamepad()
	if id == 0 {
		return fmt.Errorf("smoke: virtual gamepad: %s", sdlError())
	}
	defer C.SDL_DetachVirtualJoystick(id)

	padReady := time.Now().Add(2 * time.Second)
	for time.Now().Before(padReady) {
		pumpSDL(app, pads, time.Now())
		if len(pads) > 0 {
			break
		}
		C.SDL_Delay(5)
	}
	if len(pads) == 0 {
		pad := C.SDL_OpenGamepad(id)
		if pad != nil {
			pads[id] = pad
			app.SetGamepads(len(pads))
		}
	}
	if len(pads) == 0 {
		return fmt.Errorf("smoke: virtual gamepad was not opened")
	}
	evidence["gamepad"] = true
	evidence["gamepads"] = len(pads)

	focusBefore := app.Snapshot().Grid.Focus
	evidence["focus_before"] = focusBefore
	if err := virtualPress(app, pads, C.SDL_GAMEPAD_BUTTON_DPAD_RIGHT); err != nil {
		return err
	}
	focusAfter := app.Snapshot().Grid.Focus
	if focusAfter == focusBefore {
		if err := virtualPress(app, pads, C.SDL_GAMEPAD_BUTTON_DPAD_DOWN); err != nil {
			return err
		}
		focusAfter = app.Snapshot().Grid.Focus
	}
	evidence["focus_after"] = focusAfter
	if focusAfter == focusBefore {
		return fmt.Errorf("smoke: gamepad navigation did not move focus (%s)", padDump(app, pads))
	}

	if err := virtualPress(app, pads, C.SDL_GAMEPAD_BUTTON_SOUTH); err != nil {
		return err
	}
	launchDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(launchDeadline) {
		pumpSDL(app, pads, time.Now())
		app.Tick(time.Now())
		launch := app.Snapshot().Launch
		if launch.Phase == "ok" || launch.Phase == "host" || launch.Phase == "error" {
			evidence["launch_phase"] = launch.Phase
			evidence["launch_status"] = launch.HTTPStatus
			evidence["launch_error"] = launch.ErrorCode
			evidence["launch_message"] = launch.Message
			evidence["launch_game_id"] = launch.GameID
			break
		}
		C.SDL_Delay(10)
	}
	launch := app.Snapshot().Launch
	switch {
	case launch.Phase == "ok" || launch.Phase == "host":
	case launch.Phase == "error" && launch.HTTPStatus == 0:
		game, ok := app.Selected()
		if !ok || launch.GameID != game.ID || launchBlockReason(game) == "" {
			return fmt.Errorf("smoke: launch transport failed: %s", launch.Message)
		}
		evidence["launch_blocked"] = launch.Message
	default:
		return fmt.Errorf("smoke: host launch did not complete: %#v", launch)
	}

	payload, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	fmt.Printf("tenfoot-smoke %s\n", payload)
	return nil
}

func firstPad(pads map[C.SDL_JoystickID]*C.SDL_Gamepad) *C.SDL_Gamepad {
	for _, pad := range pads {
		if pad != nil {
			return pad
		}
	}
	return nil
}

func virtualPress(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, button C.int) error {
	held := map[Command]bool{}
	pad := firstPad(pads)
	set := C.fogcast_virtual_button_on(pad, button, 1)
	if set == 0 {
		return fmt.Errorf("smoke: virtual button down: %s", sdlError())
	}
	C.SDL_PumpEvents()
	now := time.Now()
	pumpSDL(app, pads, now)
	if pollGamepads(app, pads, held, now) {
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
	pollGamepads(app, pads, held, now)
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

func pollGamepads(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, held map[Command]bool, now time.Time) bool {
	pressed := map[Command]bool{}
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
			C.SDL_GAMEPAD_BUTTON_START,
			C.SDL_GAMEPAD_BUTTON_BACK,
		} {
			if bool(C.SDL_GetGamepadButton(pad, C.SDL_GamepadButton(button))) {
				pressed[commandFromSDLButton(button)] = true
			}
		}
		x := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTX))
		y := int(C.SDL_GetGamepadAxis(pad, C.SDL_GAMEPAD_AXIS_LEFTY))
		if cmd := CommandFromStick(x, y); cmd != CmdNone {
			pressed[cmd] = true
		}
	}
	for cmd := range held {
		if !pressed[cmd] {
			app.Release(cmd)
			delete(held, cmd)
		}
	}
	for cmd := range pressed {
		if cmd == CmdNone || held[cmd] {
			continue
		}
		if cmd == CmdQuit {
			return true
		}
		app.Press(cmd, now)
		held[cmd] = true
	}
	return false
}

func handleSDLEvent(app *App, pads map[C.SDL_JoystickID]*C.SDL_Gamepad, ev *C.FogcastEvent, now time.Time, stick *stickTracker) bool {
	switch ev.kind {
	case evQuit:
		return true
	case evKey:
		cmd := commandFromSDLKey(ev.code)
		if ev.down != 0 {
			if cmd == CmdQuit {
				return true
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
	case C.SDL_GAMEPAD_BUTTON_START:
		return CommandFromButton(ButtonStart)
	case C.SDL_GAMEPAD_BUTTON_BACK:
		return CommandFromButton(ButtonBack)
	default:
		return CmdNone
	}
}

func commandFromSDLKey(code C.int) Command {
	switch C.SDL_Keycode(code) {
	case C.SDLK_UP, C.SDLK_W:
		return CmdUp
	case C.SDLK_DOWN, C.SDLK_S:
		return CmdDown
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
	default:
		return CmdNone
	}
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
		if _, ok := textures[id]; ok {
			continue
		}
		tex, err := uploadTexture(renderer, img)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tenfoot: cover texture %s: %v\n", id, err)
			continue
		}
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
	return sdlTexture{tex: tex, w: w, h: h}, nil
}

func drawFrame(renderer *C.SDL_Renderer, snap Snapshot, textures map[string]sdlTexture) {
	C.SDL_SetRenderDrawColor(renderer, 12, 14, 20, 255)
	C.SDL_RenderClear(renderer)
	drawHeader(renderer, snap)
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
			dst := C.SDL_FRect{x: C.float(x), y: C.float(y), w: C.float(snap.Grid.CellW), h: C.float(snap.Grid.CellH - 36)}
			C.SDL_RenderTexture(renderer, tex.tex, nil, &dst)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(renderer, float32(x+8), float32(y+8), float32(snap.Grid.CellW-16), float32(snap.Grid.CellH-52), r, g, b, 255)
			drawDebug(renderer, x+16, y+24, initials(game.Title), 2)
		}
		drawDebug(renderer, x+6, y+snap.Grid.CellH-28, fitText(game.Title, 12), 2)
	}
	C.SDL_RenderPresent(renderer)
}

func drawHeader(renderer *C.SDL_Renderer, snap Snapshot) {
	fillRect(renderer, 0, 0, float32(snap.Grid.Width), float32(snap.Grid.HeaderHeight), 18, 20, 28, 255)
	drawDebug(renderer, 24, 24, "FOGCAST", 3)
	pad := "KB DEBUG"
	if snap.Gamepads > 0 {
		pad = fmt.Sprintf("PAD %d", snap.Gamepads)
	}
	drawDebug(renderer, snap.Grid.Width-160, 28, pad, 2)
	drawDebug(renderer, 220, 32, fitText(snap.Status, 70), 2)
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
