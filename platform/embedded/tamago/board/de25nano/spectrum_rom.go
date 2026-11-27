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
	return withMailboxDataBatch(base, g.FesFirmwareUploadOpcodeData, send)
}

func (FabricCPUTransport) WithHeldMediaBatch(base uint64, send func(computer.IO) error) error {
	return withMailboxDataBatch(base, g.FesComputerOpcodeMediaData, send)
}

// WithLiveMediaBatch exposes only the fixed chunk and data opcodes to the
// already-owned running C64 session. The caller brackets each batch with its
// secure bridge observations; the MMIO aperture and write bound are identical.
func (FabricCPUTransport) WithLiveMediaBatch(base uint64, send func(computer.IO) error) error {
	return withMailboxDataBatch(base, g.FesComputerOpcodeMediaData, send)
}

// WithInputBatch brackets one keyboard/controller/mouse input flush in a single
// device-mapping check and permits writes only to FES input opcodes.
func (FabricCPUTransport) WithInputBatch(base uint64, send func(computer.IO) error) error {
	if base != spectrumROMBase || send == nil {
		return errors.New("DE25: input batch differs from the fixed mailbox")
	}
	return arm64map.WithDeviceMapping(profileRAMStart, profileRAMSize, spectrumROMBase,
		uint64(g.FesDe25ComputerSpan), func() error {
			port := inputBatchPort{active: true}
			defer func() { port.active = false }()
			return send(&port)
		})
}

const maxInputBatchWrites = 12

type inputBatchPort struct {
	active bool
	writes uint32
}

func (p *inputBatchPort) Read32(offset uint32) (uint32, error) {
	if p.active {
		switch offset {
		case g.FesDe25ComputerStatusOffset, g.FesDe25ComputerGpoOffset,
			g.FesDe25ComputerGpiOffset, g.FesDe25ComputerFaultOffset:
			return load32(spectrumROMBase + uintptr(offset)), nil
		}
	}
	return 0, errors.New("DE25: read outside the input mailbox batch")
}

func (p *inputBatchPort) Write32(offset, value uint32) error {
	if !p.active || offset != g.FesDe25ComputerGpoOffset || p.writes >= maxInputBatchWrites {
		return errors.New("DE25: write outside the input mailbox batch")
	}
	opcode := (value >> 24) & 0x7f
	switch opcode {
	case g.FesComputerOpcodeKeyboardHid, g.FesComputerOpcodeControllerButtons, g.FesComputerOpcodeMouseRelative:
	default:
		return errors.New("DE25: non-input opcode rejected in input batch")
	}
	p.writes++
	store32(spectrumROMBase+uintptr(offset), value)
	return nil
}

func withMailboxDataBatch(base uint64, opcode uint32, send func(computer.IO) error) error {
	if base != spectrumROMBase || send == nil {
		return errors.New("DE25: held mailbox data batch differs from the fixed mailbox")
	}
	return arm64map.WithDeviceMapping(profileRAMStart, profileRAMSize, spectrumROMBase,
		uint64(g.FesDe25ComputerSpan), func() error {
			port := spectrumROMPort{active: true, opcode: opcode}
			defer func() { port.active = false }()
			if err := send(&port); err != nil {
				return err
			}
			if opcode == g.FesComputerOpcodeMediaData && (port.mediaWords == 0 || port.writes != 3+port.mediaWords) {
				return errors.New("DE25: held media scope did not complete its chunk")
			}
			return nil
		})
}

type spectrumROMPort struct {
	active     bool
	writes     uint32
	opcode     uint32
	mediaWords uint32
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
	if !p.active || offset != g.FesDe25ComputerGpoOffset || !p.admitWrite(value) {
		return errors.New("DE25: write outside the held mailbox data batch")
	}
	p.writes++
	store32(spectrumROMBase+uintptr(offset), value)
	return nil
}

func (p *spectrumROMPort) admitWrite(value uint32) bool {
	opcode, index := (value>>24)&0x7f, (value>>16)&0xff
	if p.opcode != g.FesComputerOpcodeMediaData {
		return opcode == p.opcode && p.writes < computer.C64DataBatchWords
	}
	if p.writes > g.FesComputerMediaChunkLengthWord {
		return opcode == p.opcode && index == p.writes-3 && index < p.mediaWords
	}
	if opcode != g.FesComputerOpcodeMediaChunk || index != p.writes {
		return false
	}
	if index == g.FesComputerMediaChunkLengthWord {
		length := value & 0xffff
		if length == 0 || length > g.FesComputerMediaChunkMaxBytes {
			return false
		}
		p.mediaWords = (length + 1) / 2
	}
	return true
}
