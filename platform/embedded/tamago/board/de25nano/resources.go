//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
)

// FabricResources reports live EL1 mappings for the explicit compiled ranges.
// This does not assert RAM-controller readiness or authorize FPGA activation.
type FabricResources struct {
	Mapping                                                                                  arm64map.Observation
	RAMBase, RAMBytes, ControlBase, ControlBytes, DMABase, DMABytes, ReaderBase, ReaderBytes uint64
}

func ObserveFabricResources() (FabricResources, error) {
	if profileRAMStart != 0x84000000 || profileRAMSize != 256<<20 || profileFabricControlBase != 0x20010400 ||
		profileFabricControlBytes != 256 || profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 {
		return FabricResources{}, errors.New("DE25: source-bound fabric resources unavailable")
	}
	if err := InitDevices(); err != nil {
		return FabricResources{}, err
	}
	ranges := []arm64map.Range{{Base: profileRAMStart, Bytes: profileRAMSize, Kind: arm64map.WriteBack},
		{Base: profileFabricControlBase, Bytes: profileFabricControlBytes, Kind: arm64map.Device},
		{Base: profileConfigurationBase, Bytes: profileConfigurationBytes, Kind: arm64map.Uncached}}
	if profileReaderMemoryBase != 0 {
		ranges = append(ranges, arm64map.Range{Base: profileReaderMemoryBase, Bytes: profileReaderMemoryArenaBytes, Kind: arm64map.Uncached})
	}
	observed, err := arm64map.Verify(profileRAMStart, profileRAMSize, ranges)
	if err != nil {
		return FabricResources{}, err
	}
	return FabricResources{Mapping: observed, RAMBase: profileRAMStart, RAMBytes: profileRAMSize,
		ControlBase: profileFabricControlBase, ControlBytes: profileFabricControlBytes, DMABase: profileConfigurationBase,
		DMABytes: profileConfigurationBytes, ReaderBase: profileReaderMemoryBase, ReaderBytes: profileReaderMemoryArenaBytes}, nil
}
