//go:build tamago && arm64 && de25nano && de25tenfootserial

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"runtime"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

// Remain rooted even if the UI stops with an uncertain native transition.
var nativeProgramBackend *nativefabric.ProgramBackend
var nativeProgramPlatform tenfoot.Platform

const tenfootSerialLinked = true

func serveProgramTenfoot(loader *nativefabric.ProgramLoader, store *packagestore.Store, files fs.FS) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	choices, err := nativefabric.ReadProgramSelections(ctx, files, "FES/CORESEL.JSN")
	if err != nil {
		cancel()
		panic(err)
	}
	entries, err := nativefabric.ReadProgramCatalog(ctx, files, "FES/UI.JSON", choices)
	cancel()
	if err != nil {
		panic(err)
	}
	programService, err = nativefabric.NewProgramService(loader, store, de25nano.ReserveConfiguration, computer.Limits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: runtime.Gosched})
	if err != nil {
		panic(err)
	}
	nativeProgramBackend, err = nativefabric.NewProgramBackend(programService, entries)
	if err != nil {
		panic(err)
	}
	// Launch requests include bounded SD reads and configuration. The existing
	// request-service budget is 100 seconds, independent of the 20s SDM bound.
	client := tenfoot.NewClient("http://native.invalid", &http.Client{Transport: nativeProgramBackend, Timeout: 100 * time.Second})
	const width, height = 1280, 720
	device, err := gfx.NewSoftware(width, height)
	if err != nil {
		panic(err)
	}
	defer device.Close()
	app := tenfoot.NewApp(client, width, height, 400)
	if err := app.SetPreferenceStore(&nativefabric.ProgramPreferences{}); err != nil {
		panic(err)
	}
	serialPlatform, err := nativefabric.NewProgramSerialPlatform(programService, serial.Read, func(ctx context.Context, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := fmt.Print(string(data))
		return err
	})
	if err != nil {
		panic(err)
	}
	var platform tenfoot.Platform = serialPlatform
	inputMode := "uart"
	if expectedUSBControl != "" {
		if expectedUSBInput == "gamepad" {
			keys := nativefabric.ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
			// This diagnostic assigns S/start and Space to the first two HID buttons.
			keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
			platform, err = nativefabric.NewProgramGamepadPlatform(serialPlatform, nativeProgramBackend, programGamepadSource, programGamepadDescriptor, input.GamepadButtons{Confirm: 1, Back: 2}, keys)
		} else {
			platform, err = nativefabric.NewProgramInputPlatform(serialPlatform, nativeProgramBackend, programKeyboardSource)
		}
		if err != nil {
			panic(err)
		}
		inputMode = "uart+shared-usb-" + expectedUSBInput
	}

	fmt.Printf("FES_NATIVE_TENFOOT_BEGIN display=serial-state-only input=%s preferences=ram\n", inputMode)
	nativeProgramPlatform = platform
	if err := tenfoot.RunPlatform(context.Background(), app, device, nativeProgramPlatform); err != nil {
		panic(err)
	}
	// ProgramSerialPlatform turns Quit during a running core into owned Stop,
	// then waits for Ready or terminal containment before exiting the App.
	status := programService.Status()
	if status.State != nativefabric.ProgramReady && status.State != nativefabric.ProgramRebootRequired && status.State != nativefabric.ProgramParked {
		panic("Tenfoot exited without a completed owned transition")
	}
	if status.State == nativefabric.ProgramReady {
		if err := closeProgramUSB(); err != nil {
			panic(err)
		}
	}
	fmt.Printf("FES_NATIVE_TENFOOT_EXIT state=%s generation=%d\n", status.State, status.Generation)
}
