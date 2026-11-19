// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"
)

// Exercise the actual MENU close/firmware path with a retained input owner.
// The close callback represents the long synchronous firmware/trace interval
// during which a pending USB packet must already have been retired.
func TestBackendFinalParkDrainsBeforeMenuClosure(t *testing.T) {
	for _, mode := range []string{"success", "pause-failure", "menu-failure", "canceled", "busy", "boundary-exhausted", "no-usb"} {
		t.Run(mode, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			b, err := NewProgramBackend(f.s, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := &pausableKeyboard{}
			platform, err := NewProgramInputPlatform(&programInputBase{}, b, source)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "no-usb" {
				b.inputPolling = nil
			}
			cause := errors.New("drain failed")
			if mode == "pause-failure" {
				source.pauseErr = cause
			}
			if mode == "menu-failure" {
				f.menu.cpu.fault = "local-stop"
			}
			if mode == "boundary-exhausted" {
				b.inputBoundary = ^uint64(0)
			}
			writes := 0
			f.menu.cpu.after = func(string) {
				writes++
				if mode != "no-usb" && !source.paused {
					t.Fatal("firmware close before input drain")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			if mode == "busy" {
				b.operation.Lock()
			}
			err = b.ParkMenu(ctx)
			if mode == "busy" {
				b.operation.Unlock()
			}
			if mode == "success" || mode == "no-usb" {
				if err != nil || f.s.Status().State != ProgramParked || writes == 0 || source.resumes != 0 || f.r.f.lease.r.Active {
					t.Fatal(err, f.s.Status(), writes)
				}
			} else if err == nil {
				t.Fatal("failed shutdown accepted")
			}
			if mode == "pause-failure" && (!errors.Is(err, cause) || writes != 0 || source.pauses != 1) {
				t.Fatal("failed drain reached MENU", err, writes)
			}
			if mode == "menu-failure" && (!source.paused || f.s.Status().State != ProgramRebootRequired) {
				t.Fatal("failed MENU resumed input", err)
			}
			if mode == "canceled" || mode == "busy" || mode == "boundary-exhausted" {
				if writes != 0 || source.pauses != 0 {
					t.Fatal("rejected call touched input or MENU")
				}
				return
			}
			calls, pauses := writes, source.pauses
			if err := b.ParkMenu(context.Background()); err == nil || writes != calls || source.pauses != pauses {
				t.Fatal("shutdown replayed", err)
			}
			if _, err := platform.Next(context.Background()); err == nil || writes != calls || source.resumes != 0 {
				t.Fatal("terminal shutdown polled/resumed", err)
			}
		})
	}
}
