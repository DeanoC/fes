//go:build tamago && arm64 && de25nano && de25tenfootserial

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

// Remain rooted even if the UI stops with an uncertain native transition.
var nativeProgramBackend *nativefabric.ProgramBackend
var nativeProgramPlatform tenfoot.Platform

const tenfootSerialLinked = true

func serveProgramTenfoot(loader *nativefabric.ProgramLoader, store *packagestore.Store, files fs.FS) {
	paired := expectedCompiledMenuProgram != ""
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	var entries []nativefabric.ProgramEntry
	var err error
	if programWritableStorageSelected {
		entries, err = nativefabric.ReadInstallerBootstrap(ctx, store)
	} else {
		var choices []packagestore.Request
		choices, err = nativefabric.ReadProgramSelections(ctx, files, "FES/CORESEL.JSN")
		if err == nil {
			entries, err = nativefabric.ReadProgramCatalog(ctx, files, "FES/UI.JSON", choices)
		}
		if err == nil {
			entries, err = nativefabric.ReadInstalledPrograms(ctx, store, entries)
		}
	}
	var menuChoices []packagestore.Request
	if err == nil && paired {
		menuChoices, err = nativefabric.ReadProgramSelections(ctx, files, "FES/MENUSEL.JSN")
		if err == nil && len(menuChoices) != 1 {
			err = errors.New("native menu requires exactly one startup selection")
		}
	}
	var preferences *nativefabric.ProgramPreferences
	var staged *nativefabric.StagedProgram
	var stagedError error
	if err == nil {
		preferences, err = nativefabric.LoadProgramPreferences(ctx, store, false)
	}
	if err == nil && programWritableStorageSelected {
		staged, err = nativefabric.ReadStagedProgram(ctx, store, "FES/INSTALL.JSN")
		if errors.Is(err, fs.ErrNotExist) {
			_, activeErr := store.ActiveReceipt()
			err = activeErr
		} else if err != nil {
			_, activeErr := store.ActiveReceipt()
			if activeErr == nil {
				stagedError = err
				err = nil
			} else {
				err = activeErr
			}
		}
	}
	cancel()
	if err != nil {
		panic(err)
	}
	limits := computer.Limits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: runtime.Gosched}
	if paired {
		load, done := context.WithTimeout(context.Background(), 100*time.Second)
		programService, err = nativefabric.NewProgramServiceWithMenu(load, loader, store, de25nano.ReserveConfiguration, limits,
			nativefabric.ProgramMenuOptions{Request: menuChoices[0], Reserve: de25nano.ReserveMenuMemory,
				Observe: emitProgramMenu,
				Limits: scanout.MenuLimits{PublicationTimeout: 4 * time.Second,
					ReaderLimits: scanout.ReaderLimits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: func() { time.Sleep(time.Millisecond) }}}})
		done()
	} else {
		programService, err = nativefabric.NewProgramService(loader, store, de25nano.ReserveConfiguration, limits)
	}
	if err != nil {
		panic(err)
	}
	if err := programService.AttachPreferences(preferences); err != nil {
		panic(err)
	}
	defer func() {
		if paired && programService.Status().State != nativefabric.ProgramParked && programService.Status().State != nativefabric.ProgramRebootRequired {
			_ = programService.Retire(errors.New("native Tenfoot exited before owned menu shutdown"))
		}
	}()
	if err := startProgramNetwork(); err != nil {
		panic(err)
	}
	nativeProgramBackend, err = nativefabric.NewProgramBackend(programService, entries)
	if err != nil {
		panic(err)
	}
	if staged != nil {
		if err = nativeProgramBackend.AttachStagedProgram(context.Background(), staged); err != nil {
			if programService.Status().State != nativefabric.ProgramReady {
				panic(err)
			}
			stagedError = err
		}
	}
	nativeProgramBackend.SetStagedSourceError(stagedError)
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
	if err := app.SetPreferenceStore(preferences); err != nil {
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
	if programWritableStorageSelected {
		serialPlatform.EnableInstallerViews()
	}
	inputMode := "uart"
	if expectedUSBControl != "" {
		if expectedUSBInput == "gamepad" {
			keys := nativefabric.ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
			// This diagnostic assigns S/start and Space to the first two HID buttons.
			keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
			platform, err = nativefabric.NewProgramGamepadPlatform(serialPlatform, nativeProgramBackend, &sharedProgramGamepad{programGamepadSource, &sharedInputMetrics}, programGamepadDescriptor, input.GamepadButtons{Confirm: 1, Back: 2}, keys)
		} else {
			platform, err = nativefabric.NewProgramInputPlatform(serialPlatform, nativeProgramBackend, &sharedProgramKeyboard{programKeyboardSource, &sharedInputMetrics})
		}
		if err != nil {
			panic(err)
		}
		inputMode = "uart+shared-usb-" + expectedUSBInput
	}

	display := "serial-state-only"
	if paired {
		platform, err = nativefabric.NewProgramMenuPlatform(programService, platform, device.Framebuffer)
		if err != nil {
			panic(err)
		}
		display = "fpga-menu"
	}
	fmt.Printf("FES_NATIVE_TENFOOT_BEGIN display=%s input=%s preferences=ram\n", display, inputMode)
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
		if paired {
			if err := programService.ParkMenu(context.Background()); err != nil {
				panic(err)
			}
			status = programService.Status()
		}
		if err := closeProgramUSB(); err != nil {
			panic(err)
		}
	}
	fmt.Printf("FES_NATIVE_TENFOOT_EXIT state=%s generation=%d\n", status.State, status.Generation)
}

func emitProgramMenu(observation nativefabric.ProgramMenuObservation) error {
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	_, err = fmt.Printf("FES_NATIVE_MENU_STATE %s\n", raw)
	return err
}
