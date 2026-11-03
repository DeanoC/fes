// SPDX-License-Identifier: GPL-3.0-or-later
// Package framefixture renders bounded, real tenfoot states for native and host
// diagnostics. The callback must finish consuming each private CPU frame.
package framefixture

import (
	"errors"
	"image"
	"strconv"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func Render(count int, present func(int, *image.RGBA) error) error {
	if count < 1 || count > 12 || present == nil {
		return errors.New("frame fixture: bounded count and presenter required")
	}
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
	app := tenfoot.NewApp(nil, 1280, 720, 100)
	for index := 0; index < count; index++ {
		snapshot := app.Snapshot()
		if index > 0 {
			// Exercise the real renderer with bounded view states. The memory
			// diagnostic does not run the UI's network or launch actions.
			snapshot.SearchOpen = true
			snapshot.Query = "ZX81 " + strconv.Itoa(index)
		}
		if err = presenter.Draw(snapshot); err != nil {
			return err
		}
		if err = present(index, device.Framebuffer()); err != nil {
			return err
		}
	}
	return nil
}
