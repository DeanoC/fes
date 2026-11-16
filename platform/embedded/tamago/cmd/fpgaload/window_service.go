//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
)

var retainedWindowService *nativefabric.WindowService
var retainedServiceWindow *nativefabric.WindowSession

// The command frontend exercises the same persistent native owner as a future
// App backend. It renders real Tenfoot snapshots, but has no ordinary package
// admission, USB input or HDMI evidence. Only the compiled package is selected.
func serveWindows(v *nativefabric.HPSContextVerifier, store *packagestore.Store, request packagestore.Request,
	control, configuration, contract []byte, limits scanout.ReaderLimits) error {
	device, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		return err
	}
	defer device.Close()
	presenter, err := tenfoot.NewPresenter(device)
	if err != nil {
		return err
	}
	defer presenter.Close()
	var grid tenfoot.Grid
	grid.Layout(1280, 720)
	if err := presenter.Draw(tenfoot.Snapshot{Grid: grid, Status: "Ready"}); err != nil {
		return err
	}
	retainedServiceWindow, err = v.OpenWindowSession(context.Background(), control, contract, retained, limits)
	if err != nil {
		return err
	}
	if _, err = retainedServiceWindow.PresentFrame(context.Background(), device.Framebuffer()); err != nil {
		_, closed := retainedServiceWindow.Close()
		return errors.Join(err, closed)
	}
	var initial nativefabric.WindowHandle
	retainedWindowService, initial, err = nativefabric.NewWindowService(retainedServiceWindow, store, control, configuration, contract, de25nano.ReserveConfiguration, limits)
	if err != nil {
		_, closed := retainedServiceWindow.Close()
		return errors.Join(err, closed)
	}
	console, err := nativefabric.NewWindowConsole(retainedWindowService, initial, request)
	if err != nil {
		_, closed := retainedWindowService.Park(context.Background(), initial)
		return errors.Join(err, closed)
	}
	writeReply := func(reply nativefabric.WindowReply) error {
		raw, err := json.Marshal(reply)
		if err != nil {
			return err
		}
		fmt.Printf("FES_NATIVE_WINDOW_REPLY %s\n", raw)
		return nil
	}
	if err := writeReply(console.Handle(context.Background(), []byte(`{"op":"status"}`), nil)); err != nil {
		return err
	}
	line := make([]byte, 0, nativefabric.ProgramCommandMaxBytes)
	discard := false
	for {
		b, ready := serial.Read()
		if !ready {
			runtime.Gosched()
			time.Sleep(time.Millisecond)
			continue
		}
		if b != '\r' && b != '\n' {
			if len(line) == nativefabric.ProgramCommandMaxBytes {
				discard = true
			}
			if !discard {
				line = append(line, b)
			}
			continue
		}
		if len(line) == 0 && !discard {
			continue
		}
		if discard {
			line = line[:0]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		reply := console.Handle(ctx, line, device.Framebuffer())
		cancel()
		line, discard = line[:0], false
		if err := writeReply(reply); err != nil {
			return err
		}
		if reply.State == nativefabric.ProgramParked || reply.State == nativefabric.ProgramRebootRequired {
			return nil
		}
		status := "Ready"
		if reply.State == nativefabric.ProgramRunning {
			status = "Running"
		}
		if !reply.OK {
			status = "Action unavailable"
		}
		if err := presenter.Draw(tenfoot.Snapshot{Grid: grid, Status: status}); err != nil {
			return err
		}
	}
}
