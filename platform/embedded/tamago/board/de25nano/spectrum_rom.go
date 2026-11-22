//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// This address is part of the selected Nano shell. The package may neither
// move the aperture nor choose device access outside the four mailbox CSRs.
const spectrumROMBase = 0x20010300

func (FabricCPUTransport) WithSpectrumROMBatch(base uint64, send func(computer.IO) error) error {
	return withHeldDataBatch(base, g.FesFirmwareUploadOpcodeData, send)
}

func (FabricCPUTransport) WithHeldMediaBatch(base uint64, send func(computer.IO) error) error {
	return withHeldDataBatch(base, g.FesComputerOpcodeMediaData, send)
}

func withHeldDataBatch(base uint64, opcode uint32, send func(computer.IO) error) error {
	if base != spectrumROMBase || send == nil {
		return errors.New("DE25: held mailbox data batch differs from the fixed mailbox")
	}
	return arm64map.WithDeviceMapping(profileRAMStart, profileRAMSize, spectrumROMBase,
		uint64(g.FesDe25ComputerSpan), func() error {
			port := spectrumROMPort{active: true, opcode: opcode}
			defer func() { port.active = false }()
			return send(&port)
		})
}

type spectrumROMPort struct {
	active bool
	writes uint32
	opcode uint32
}

func (p *spectrumROMPort) Read32(offset uint32) (uint32, error) {
	if p.active {
		switch offset {
		case g.FesDe25ComputerStatusOffset, g.FesDe25ComputerGpoOffset,
			g.FesDe25ComputerGpiOffset, g.FesDe25ComputerFaultOffset:
			return load32(spectrumROMBase + uintptr(offset)), nil
		}
	}
	return 0, errors.New("DE25: read outside the held mailbox data batch")
}

func (p *spectrumROMPort) Write32(offset, value uint32) error {
	if !p.active || offset != g.FesDe25ComputerGpoOffset ||
		(value>>24)&0x7f != p.opcode ||
		p.writes >= computer.C64DataBatchWords {
		return errors.New("DE25: write outside the held mailbox data batch")
	}
	p.writes++
	store32(spectrumROMBase+uintptr(offset), value)
	return nil
}
