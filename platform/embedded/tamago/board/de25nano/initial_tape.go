// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

// This private wrapper is selected only by the matched board startup. Its
// accessor supplies the existing pinned program arena after configuration;
// neither construction nor metadata grants firmware or memory ownership.
type initialTapeCPU struct {
	nativefabric.CPUTransport
	arena func() (*nativefabric.ConfigurationArena, error)
}

func selectProgramCPU(cpu nativefabric.CPUTransport, arena func() (*nativefabric.ConfigurationArena, error), selected, compiled bool) (nativefabric.CPUTransport, error) {
	if cpu == nil || selected != compiled || selected && arena == nil {
		return nil, errors.New("DE25: initial tape selection differs from compiled platform")
	}
	if !selected {
		return cpu, nil
	}
	return initialTapeCPU{cpu, arena}, nil
}

func (cpu initialTapeCPU) LoadInitialTape(ctx context.Context, scope nativefabric.BridgeSnapshot, request computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	if cpu.CPUTransport == nil || cpu.arena == nil {
		return computer.InitialTapeReceipt{}, errors.New("DE25: initial tape platform missing")
	}
	arena, err := cpu.arena()
	if err != nil {
		return computer.InitialTapeReceipt{}, err
	}
	return arena.PublishInitialTape(ctx, cpu.CPUTransport, scope, request)
}

var _ nativefabric.InitialTapePlatform = initialTapeCPU{}
