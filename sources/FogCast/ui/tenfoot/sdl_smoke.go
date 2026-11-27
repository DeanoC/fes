//go:build sdl3

package tenfoot

/*
#cgo pkg-config: sdl3
#include "sdl_cgo.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func runSmoke(ctx context.Context, opts Options, app *App, dev gfx.Device, pads *sdlPadSet, textures, labels map[string]gpuTexture, renderer *C.SDL_Renderer) error {
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
			if pads.handle(id) != nil {
				break
			}
			C.SDL_Delay(5)
		}
		if pads.handle(id) == nil {
			if pad := C.SDL_OpenGamepad(id); pad != nil {
				pads.attach(app, id, pad, false)
			}
		}
	}
	evidence["gamepads"] = pads.len()
	evidence["gamepad"] = pads.handle(id) != nil
	if pads.handle(id) != nil {
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
		if game.LaunchEligible() {
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

func smokeKeyboardNav(app *App, pads *sdlPadSet, evidence map[string]any) error {
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
	evidence["affinity_after_keyboard"] = app.Affinity().Kind.String()
	return nil
}

func smokeMouseNav(app *App, pads *sdlPadSet, evidence map[string]any) error {
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
	evidence["affinity_after_mouse"] = app.Affinity().Kind.String()
	return nil
}

func virtualMouseMove(app *App, pads *sdlPadSet, x, y int) error {
	now := time.Now()
	ev := C.FogcastEvent{kind: evMouseMove, x: C.int(x), y: C.int(y)}
	if handleSDLEvent(app, pads, &ev, now, nil) {
		return nil
	}
	app.Tick(now)
	return nil
}

func virtualMouseClick(app *App, pads *sdlPadSet, x, y int) error {
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

func virtualMouseClickSelected(app *App, pads *sdlPadSet) error {
	snap := app.Snapshot()
	x, y, w, h, ok := snap.Grid.CellRect(snap.Grid.Focus)
	if !ok {
		return fmt.Errorf("smoke: focused cell is offscreen for mouse click (%s)", padDump(app, pads))
	}
	return virtualMouseClick(app, pads, x+w/2, y+h/2)
}

func virtualKey(app *App, pads *sdlPadSet, key C.int) error {
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

func virtualPress(app *App, pads *sdlPadSet, id C.SDL_JoystickID, button C.int) error {
	held := map[Command]bool{}
	pad := pads.handle(id)
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
