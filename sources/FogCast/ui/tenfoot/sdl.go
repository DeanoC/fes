//go:build sdl3

// SDL window, event, gamepad, mouse, and text-input loop for the tenfoot launcher.
// 2D draw, present, and texture upload go through gfx.Device (SDL3 backend).

package tenfoot

/*
#cgo pkg-config: sdl3
#include <SDL3/SDL_main.h>
#include <stddef.h>
#include <stdlib.h>
#include "sdl_cgo.h"

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
		out->which = 0;
		out->dx = 0;
		out->dy = 0;
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
			out->value = (int)e.key.scancode;
			out->down = e.key.down ? 1 : 0;
			out->which = (int)e.key.which;
			return 1;
		case SDL_EVENT_MOUSE_MOTION:
			out->dx = e.motion.xrel;
			out->dy = e.motion.yrel;
			if (renderer != NULL) {
				SDL_ConvertEventToRenderCoordinates(renderer, &e);
			}
			out->kind = FC_EV_MOUSE_MOVE;
			out->which = (int)e.motion.which;
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
			out->which = (int)e.button.which;
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
		case SDL_EVENT_KEYBOARD_ADDED:
			out->kind = FC_EV_KEY_ADDED;
			out->which = (int)e.kdevice.which;
			return 1;
		case SDL_EVENT_KEYBOARD_REMOVED:
			out->kind = FC_EV_KEY_REMOVED;
			out->which = (int)e.kdevice.which;
			return 1;
		case SDL_EVENT_MOUSE_ADDED:
			out->kind = FC_EV_MOUSE_ADDED;
			out->which = (int)e.mdevice.which;
			return 1;
		case SDL_EVENT_WINDOW_FOCUS_LOST:
			out->kind = FC_EV_FOCUS_LOST;
			return 1;
		case SDL_EVENT_MOUSE_REMOVED:
			out->kind = FC_EV_MOUSE_REMOVED;
			out->which = (int)e.mdevice.which;
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
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/DeanoC/FogCast/ui/gfx"
)

const (
	evQuit         = C.FC_EV_QUIT
	evKey          = C.FC_EV_KEY
	evButton       = C.FC_EV_BUTTON
	evAxis         = C.FC_EV_AXIS
	evPadAdded     = C.FC_EV_PAD_ADDED
	evPadRemoved   = C.FC_EV_PAD_REMOVED
	evText         = C.FC_EV_TEXT
	evMouseMove    = C.FC_EV_MOUSE_MOVE
	evMouseButton  = C.FC_EV_MOUSE_BUTTON
	evKeyAdded     = C.FC_EV_KEY_ADDED
	evKeyRemoved   = C.FC_EV_KEY_REMOVED
	evMouseAdded   = C.FC_EV_MOUSE_ADDED
	evMouseRemoved = C.FC_EV_MOUSE_REMOVED
	evFocusLost    = C.FC_EV_FOCUS_LOST
)

func runWindow(ctx context.Context, opts Options) (resultErr error) {
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
	// (IsStub true; no programmed 2D core). linuxfb and menu-display are
	// native shells and are rejected here.
	defer C.SDL_DestroyWindow(window)
	defer C.SDL_DestroyRenderer(renderer)
	dev, err := openGFXDevice(opts, unsafe.Pointer(renderer))
	if err != nil {
		return fmt.Errorf("gfx device: %w", err)
	}
	defer dev.Close()

	app, err := configuredApp(opts)
	if err != nil {
		return err
	}
	padRecovery, err := configureSDLPadRecovery(app, nativeKeyboardInputEnabled(opts))
	if err != nil {
		app.Stop()
		return err
	}
	if opts.Smoke {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.SmokeTimeout)
		defer cancel()
	}
	app.Start(ctx)
	defer app.Stop()
	if padRecovery != nil {
		worker := &sdlPadRecovery{padOnlyRecovery: padRecovery}
		worker.start()
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, worker.stop(stopCtx))
		}()
	}

	pads := newSDLPadSet()
	defer pads.close()
	openExistingGamepads(pads, app)
	seedSDLKeyboards(app)
	seedSDLMice(app)
	app.FinishInputSeed()

	textures := map[string]gpuTexture{}
	defer destroyTextures(dev, textures)
	labels := map[string]gpuTexture{}

	if opts.Smoke {
		return runSmoke(ctx, opts, app, dev, pads, textures, labels, renderer)
	}

	var stick stickTracker
	held := map[Command]bool{}
	textInput := false
	mouseCapture := false
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
		if capture := app.ForwardsPlayMouse(); capture != mouseCapture {
			if bool(C.SDL_SetWindowRelativeMouseMode(window, C.bool(capture))) {
				mouseCapture = capture
			}
		}
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
		return nil, fmt.Errorf("linuxfb uses the native framebuffer shell; select -gfx linuxfb through tenfoot.Run")
	case gfx.BackendMenuDisplay:
		return nil, fmt.Errorf("menu-display uses the native menu shell; select -gfx menu-display through tenfoot.Run")
	default:
		return gfx.WrapSDLRenderer(renderer, opts.Width, opts.Height)
	}
}
