// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdimage "image"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func TestMenuStopCauseSurvivesFrontendRetirement(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	ctx := context.Background()
	serial, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false },
		func(context.Context, []byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	platform, err := NewProgramMenuPlatform(f.s, serial, func() *stdimage.RGBA { return menuFrame(0) })
	if err != nil {
		t.Fatal(err)
	}
	f.menu.cpu.fault = "local-stop"
	if err := f.s.ParkMenu(ctx); err == nil || !strings.Contains(err.Error(), "menu-stop at command") {
		t.Fatal("local stop failure was not exercised", err)
	}
	view := f.s.Status()
	if view.State != ProgramRebootRequired || !strings.Contains(view.Error, "menu-stop at command") ||
		f.menu.cpu.state.ResetMask != 15 || f.menu.cpu.independentStops != 1 || f.menu.cpu.releases != 0 {
		t.Fatal("local failure lost its independent closure or retained resources", view)
	}
	accesses := 0
	f.menu.cpu.after = func(string) { accesses++ }
	for _, check := range []struct {
		name string
		call func() error
	}{
		{"present", func() error { return platform.Present(ctx, tenfoot.Snapshot{}) }},
		{"park", func() error { return f.s.ParkMenu(ctx) }},
		{"frontend", func() error { return f.s.Retire(errors.New("frontend failed after stop")) }},
	} {
		err := check.call()
		if !errors.Is(err, ErrProgramRetired) || !strings.Contains(err.Error(), view.Error) {
			t.Fatalf("%s replaced the original failure: %v", check.name, err)
		}
	}
	if accesses != 0 || f.menu.cpu.independentStops != 1 || f.menu.cpu.releases != 0 || f.r.f.lease.r.Active {
		t.Fatal("reporting a retired error performed more device work", accesses)
	}
}

func TestMenuProgramFullTenfootDisplayCoreReturnAndFailures(t *testing.T) {
	for _, fault := range []string{"", "launch", "return", "restore", "present", "serial", "input"} {
		t.Run(fault, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "zx81", Title: "ZX81", Request: request}})
			if err != nil {
				t.Fatal(err)
			}
			device, err := gfx.NewSoftware(1280, 720)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			queue := []byte{}
			source := &pausableKeyboard{}
			stage, frames := 0, 0
			serial, err := NewProgramSerialPlatform(f.s, func() (byte, bool) {
				if len(queue) == 0 {
					return 0, false
				}
				b := queue[0]
				queue = queue[1:]
				return b, true
			}, func(_ context.Context, raw []byte) error {
				if !bytes.HasPrefix(raw, []byte("FES_NATIVE_TENFOOT_STATE ")) {
					return nil
				}
				var view struct {
					Loading bool                 `json:"loading"`
					Titles  []string             `json:"titles"`
					Session string               `json:"session"`
					Native  ProgramServiceStatus `json:"native"`
				}
				if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("FES_NATIVE_TENFOOT_STATE ")), &view); err != nil {
					return err
				}
				switch stage {
				case 0:
					if !view.Loading && len(view.Titles) == 1 && view.Native.State == ProgramReady {
						if fault == "launch" {
							f.fault = "computer-identity"
						}
						if fault == "present" {
							f.menu.cpu.fault = "frame-command"
						}
						if fault == "serial" {
							return errors.New("serial link failed")
						}
						queue, stage = []byte("\r"), 1
					}
				case 1:
					if view.Session == "active" && view.Native.State == ProgramRunning {
						if fault == "input" {
							source.err = errors.New("USB input disconnected during play")
						}
						if fault == "return" {
							f.r.f.cpu.loseClose = true
						}
						if fault == "restore" {
							f.fault = "menu-identity"
						}
						queue, stage = []byte("s"), 2
					}
				case 2:
					if view.Session == "idle" && view.Native.State == ProgramReady {
						// Quit after the restored menu has received another real frame.
						queue, stage = []byte("q"), 3
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			usb, err := NewProgramInputPlatform(serial, backend, source)
			if err != nil {
				t.Fatal(err)
			}
			platform, err := NewProgramMenuPlatform(f.s, usb, func() *stdimage.RGBA {
				frames++
				return device.Framebuffer()
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewProgramMenuPlatform(f.s, serial, device.Framebuffer); err == nil {
				t.Fatal("duplicate display owner admitted")
			}
			app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 1280, 720, 400)
			if err := app.SetPreferenceStore(nativePreferences{}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err = tenfoot.RunPlatform(ctx, app, device, platform)
			if fault != "" {
				if err == nil {
					t.Fatal("terminal frontend/native failure hidden")
				}
				assertMenuRetired(t, f)
				return
			}
			if err != nil || stage != 3 || frames < 3 || f.r.f.cpu.configurations != 3 || f.s.Status().State != ProgramReady || f.r.f.lease.r.Active {
				t.Fatal("App did not finish displayed menu/core/menu cycle", stage, frames, f.s.Status(), err)
			}
			if source.polls == 0 || source.pauses != 2 || source.resumes != 2 || source.paused {
				t.Fatal("display/core/return lost input servicing or transition drains", source.polls, source.pauses, source.resumes)
			}
			// Compare every pixel, including channel conversion, against the
			// actual App renderer, not a synthetic service frame or UART state.
			frame := device.Framebuffer()
			arena := f.r.f.loader.boot.readerMemoryRegion.Bytes[int(f.menu.cpu.slot)*(4<<20):]
			nonblank := false
			for i := 0; i < len(frame.Pix); i += 4 {
				want := []byte{frame.Pix[i+2], frame.Pix[i+1], frame.Pix[i], 0xff}
				if !bytes.Equal(arena[i:i+4], want) {
					t.Fatalf("real App pixel %d was not scanned out", i/4)
				}
				nonblank = nonblank || frame.Pix[i] != 0 || frame.Pix[i+1] != 0 || frame.Pix[i+2] != 0
			}
			if !nonblank {
				t.Fatal("App rendered only the constructor blank")
			}
			if err := f.s.ParkMenu(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
