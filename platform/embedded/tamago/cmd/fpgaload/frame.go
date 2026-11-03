//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"errors"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// The renderer publishes only to private CPU storage. The later boot-pinned
// copy still grants no FPGA data access or permission to recycle the arena.
type windowPixels []byte

func (p windowPixels) Size() uint64 { return uint64(len(p)) }
func (p windowPixels) Write(offset uint64, src []byte) error {
	if offset > uint64(len(p)) || uint64(len(src)) > uint64(len(p))-offset {
		return errors.New("window frame copy exceeds private pixels")
	}
	copy(p[offset:], src)
	return nil
}
func (windowPixels) Publish() error { return nil }

func windowFrame() ([]byte, error) {
	device, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		return nil, err
	}
	defer device.Close()
	presenter, err := tenfoot.NewPresenter(device)
	if err != nil {
		return nil, err
	}
	defer presenter.Close()
	app := tenfoot.NewApp(nil, 1280, 720, 100)
	if err := presenter.Draw(app.Snapshot()); err != nil {
		return nil, err
	}
	pixels := make(windowPixels, 5120*720)
	surface, err := scanout.New(1280, 720, 5120, pixels)
	if err != nil {
		return nil, err
	}
	if err := surface.Present(device.Framebuffer()); err != nil {
		return nil, err
	}
	return pixels, nil
}
