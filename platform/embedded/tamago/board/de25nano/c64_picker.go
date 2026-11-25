//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
)

const c64PickerGuard = 0x20010200

var _ c64LiveBatcher = FabricCPUTransport{}

// WithC64PickerBatch maps only the fixed control aperture. The running
// ComputerSession independently owns the bridge before and after this call.
func (FabricCPUTransport) WithC64PickerBatch(base uint64, send func(computer.IO) error) error {
	if base != spectrumROMBase || send == nil {
		return errors.New("DE25: picker differs from the fixed C64 aperture")
	}
	return arm64map.WithDeviceMapping(profileRAMStart, profileRAMSize, c64PickerGuard, 256, func() error {
		port := c64PickerPort{active: true}
		defer func() { port.active = false }()
		return send(&port)
	})
}

type c64PickerPort struct {
	active bool
	writes uint16
}

func (p *c64PickerPort) Read32(offset uint32) (uint32, error) {
	if p.active {
		switch offset {
		case 0x74, 0x7c, 0x80, 0x88:
			return load32(c64PickerGuard + uintptr(offset)), nil
		}
	}
	return 0, errors.New("DE25: read outside the C64 picker scope")
}

func (p *c64PickerPort) Write32(offset, value uint32) error {
	if !p.active || p.writes >= 194 || (offset != 0x78 && (offset != 0x7c || value < 1 || value > 3)) {
		return errors.New("DE25: write outside the C64 picker scope")
	}
	p.writes++
	store32(c64PickerGuard+uintptr(offset), value)
	return nil
}
