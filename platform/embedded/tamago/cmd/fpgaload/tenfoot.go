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
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

// Remain rooted even if the UI stops with an uncertain native transition.
var nativeProgramBackend *nativefabric.ProgramBackend
var nativeProgramPlatform tenfoot.Platform

const tenfootSerialLinked = true

var expectedLifecycleTrace string

func serveProgramTenfoot(loader *nativefabric.ProgramLoader, store *packagestore.Store, lease *sdcontrol.Session) {
	uartIRQ := false
	var activeGIC *gic.Controller
	if g := de25nano.GIC(); g != nil {
		if err := g.Init(); err != nil {
			if !errors.Is(err, gic.ErrUnsupported) {
				panic(err)
			}
		} else {
			if err := g.EnableIdle(); err != nil {
				panic(err)
			}
			defer g.DisableIdle()
			stop := make(chan struct{})
			done := make(chan struct{})
			go func() {
				defer close(done)
				g.Serve(stop)
			}()
			defer func() {
				close(stop)
				<-done
			}()
			if err := serial.EnableIRQ(g); err != nil {
				panic(err)
			}
			defer serial.DisableIRQ(g)
			if lease != nil {
				if err := lease.EnableIRQ(g, de25nano.IRQSDMMC); err != nil {
					panic(err)
				}
				defer lease.DisableIRQ()
			}
			uartIRQ = true
			activeGIC = g
		}
	}
	paired := expectedCompiledMenuProgram != ""
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	startup, err := nativefabric.ReadProgramStartup(ctx, store, programWritableStorageSelected, paired)
	cancel()
	if err != nil {
		panic(err)
	}
	limits := computer.Limits{Timeout: time.Second, MaxPolls: 64, Now: time.Now, Yield: runtime.Gosched}
	if paired {
		load, done := context.WithTimeout(context.Background(), 100*time.Second)
		programService, err = nativefabric.NewProgramServiceWithMenu(load, loader, store, de25nano.ReserveConfiguration, limits,
			nativefabric.ProgramMenuOptions{Request: startup.Menu, Reserve: de25nano.ReserveMenuMemory,
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
	if err := programService.AttachPreferences(startup.Preferences); err != nil {
		panic(err)
	}
	defer func() {
		if paired && programService.Status().State != nativefabric.ProgramParked && programService.Status().State != nativefabric.ProgramRebootRequired {
			_ = programService.Retire(errors.New("native Tenfoot exited before owned menu shutdown"))
		}
	}()
	cleanupNetwork, err := startProgramNetwork(activeGIC)
	if err != nil {
		panic(err)
	}
	defer cleanupNetwork()
	nativeProgramBackend, err = nativefabric.NewProgramBackend(programService, startup.Entries)
	if err != nil {
		panic(err)
	}
	if len(startup.Staged) != 0 {
		if err = nativeProgramBackend.AttachStagedPrograms(context.Background(), startup.Staged); err != nil {
			if programService.Status().State != nativefabric.ProgramReady {
				panic(err)
			}
			startup.StagedError = err
		}
	}
	nativeProgramBackend.SetStagedSourceError(startup.StagedError)
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
	if err := app.SetPreferenceStore(startup.Preferences); err != nil {
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
	if expectedLifecycleTrace == "1" {
		serialPlatform.EnableLifecycleTrace()
	}
	if uartIRQ {
		serialPlatform.SetReady(serial.Ready())
	}
	var platform tenfoot.Platform = serialPlatform
	if programWritableStorageSelected {
		serialPlatform.EnableInstallerViews()
	}
	inputMode := "uart"
	var activeHub *usbhub.Hub
	defer func() {
		if activeHub != nil {
			activeHub.Close()
		}
	}()
	if expectedUSBControl != "" && expectedTenfootUARTOnly != "1" {
		cleanupUSBIRQ, err := enableProgramUSBIRQ(activeGIC)
		if err != nil {
			panic(err)
		}
		defer cleanupUSBIRQ()
		switch expectedUSBInput {
		case "gamepad":
			keys := nativefabric.ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
			// This diagnostic assigns S/start and Space to the first two HID buttons.
			keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
			platform, err = nativefabric.NewProgramGamepadPlatform(serialPlatform, nativeProgramBackend, &sharedProgramGamepad{programGamepadSource, &sharedInputMetrics}, programGamepadDescriptor, input.GamepadButtons{Confirm: 1, Back: 2}, keys)
		case "multi":
			bonds := l2cap.NewBondStore()
			fw := programBTFirmwareLoader{store: store}
			if store != nil {
				loadCtx, loadCancel := context.WithTimeout(context.Background(), 5*time.Second)
				raw, bondErr := store.ReadBTBonds(loadCtx)
				loadCancel()
				if bondErr == nil {
					if unmarshalErr := bonds.UnmarshalJSON(raw); unmarshalErr != nil {
						fmt.Printf("FES_NATIVE_BT_BONDS WARN action=load err=%v\n", unmarshalErr)
					}
				} else if !errors.Is(bondErr, fs.ErrNotExist) {
					fmt.Printf("FES_NATIVE_BT_BONDS WARN action=load err=%v\n", bondErr)
				}
				fw.preload("rtl8761bu_fw.bin", "rtl8761bu_config.bin")
			}
			// Like preferences, bonds stay in RAM without the writable storage
			// plan: the read-only FAT refuses every write, so a writer would
			// only retry and report ErrReadOnly for the rest of the session.
			var persistBonds func(context.Context, []byte) error
			if store != nil && programWritableStorageSelected {
				persistBonds = func(pctx context.Context, raw []byte) error {
					writeCtx, writeCancel := context.WithTimeout(pctx, 5*time.Second)
					defer writeCancel()
					return store.WriteBTBonds(writeCtx, raw)
				}
			}
			hub, hubErr := usbhub.NewHub(programBusHost, usbhub.Config{
				Bonds:        bonds,
				Firmware:     fw,
				PersistBonds: persistBonds,
				OnDeviceEvent: func(ev usbhub.DeviceEvent) {
					fmt.Printf("FES_NATIVE_USB_HUB event=%s addr=%d iface=%d slot=%d name=%q\n", ev.Kind, ev.Address, ev.Interface, ev.Slot, ev.Name)
				},
			})
			if hubErr != nil {
				panic(hubErr)
			}
			activeHub = hub
			keys := nativefabric.ProgramGamepadKeys{Up: 0x52, Down: 0x51, Left: 0x50, Right: 0x4f}
			keys.Buttons[0], keys.Buttons[1] = 0x16, 0x2c
			var multiPlatform *nativefabric.ProgramMultiInputPlatform
			multiPlatform, err = nativefabric.NewProgramMultiInputPlatform(serialPlatform, nativeProgramBackend, &sharedProgramMulti{hub: hub, metrics: &sharedInputMetrics}, nativefabric.ProgramMultiConfig{
				MenuButtons:     input.GamepadButtons{Confirm: 1, Back: 2},
				CoreKeys:        keys,
				PairingDuration: 30 * time.Second,
				OnPairingEvent: func(active bool, pairErr error) {
					fmt.Printf("FES_NATIVE_BT_PAIRING active=%t err=%v\n", active, pairErr)
				},
				OnBondsSaved: func(saveErr error) {
					fmt.Printf("FES_NATIVE_BT_BONDS action=save err=%v\n", saveErr)
				},
			})
			if err == nil {
				err = app.SetBluetooth(multiPlatform)
			}
			platform = multiPlatform
		default:
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
	stopWatchdog, err := startProgramWatchdog(context.Background(), lease)
	if err != nil {
		panic(err)
	}
	defer stopWatchdog()
	fmt.Printf("FES_NATIVE_TENFOOT_BEGIN display=%s input=%s preferences=%s\n", display, inputMode, startup.PreferencesMode)
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
			if err := nativeProgramBackend.ParkMenu(context.Background()); err != nil {
				panic(err)
			}
			status = programService.Status()
		}
		if activeHub != nil {
			activeHub.Close()
		}
		if err := closeProgramUSB(); err != nil {
			panic(err)
		}
	}
	fmt.Printf("FES_NATIVE_TENFOOT_EXIT state=%s generation=%d\n", status.State, status.Generation)
}

type programBTFirmwareLoader struct {
	store  *packagestore.Store
	cached map[string][]byte
}

func (l *programBTFirmwareLoader) preload(names ...string) {
	if l.store == nil {
		return
	}
	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		b, err := l.store.ReadBTFirmware(ctx, name)
		cancel()
		if err == nil {
			if l.cached == nil {
				l.cached = make(map[string][]byte, len(names))
			}
			l.cached[name] = b
		}
	}
}

func (l programBTFirmwareLoader) LoadFirmware(name string) ([]byte, error) {
	if b, ok := l.cached[name]; ok {
		return b, nil
	}
	if l.store == nil {
		return nil, fs.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := l.store.ReadBTFirmware(ctx, name)
	if errors.Is(err, packagestore.ErrState) {
		return nil, fs.ErrNotExist
	}
	return b, err
}

func emitProgramMenu(observation nativefabric.ProgramMenuObservation) error {
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	_, err = fmt.Printf("FES_NATIVE_MENU_STATE %s\n", raw)
	return err
}
