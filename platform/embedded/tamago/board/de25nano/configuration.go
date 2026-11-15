//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var configurationMu sync.Mutex
var configurationAttempted bool
var configurationArena *nativefabric.ConfigurationArena
var tapeArena *nativefabric.InitialTapeArena
var tapeArenaAttempted bool

// ReserveConfiguration copies into the build's explicitly excluded uncached
// arena. The mapping stays pinned until reboot; a new lease can overwrite it
// only after the previous transfer has completed. An uncertain lease or failed
// initialization cannot be reused. No ordinary profile supplies these constants.
func ReserveConfiguration(source []byte) (*nativefabric.Region, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	if profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 || len(source) == 0 || len(source) > nativefabric.MaxProgramBytes {
		return nil, errors.New("DE25: cold configuration reservation unavailable")
	}
	if configurationArena == nil {
		if configurationAttempted {
			return nil, errors.New("DE25: configuration arena initialization was already attempted")
		}
		configurationAttempted = true
		if err := InitDevices(); err != nil {
			return nil, err
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileConfigurationBase))), profileConfigurationBytes)
		var arena *nativefabric.ConfigurationArena
		var err error
		if profileConfigurationRetained {
			arena, err = nativefabric.NewRetainedProgramArena(profileConfigurationBase, data, configurationPublication)
		} else {
			arena, err = nativefabric.NewProgramArena(profileConfigurationBase, data, configurationPublication)
		}
		if err != nil {
			return nil, err
		}
		configurationArena = arena
	}
	return configurationArena.Reserve(source)
}

func configurationMapping() (arm64map.Observation, error) {
	if profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 {
		return arm64map.Observation{}, errors.New("DE25: configuration mapping unavailable")
	}
	return arm64map.Verify(profileRAMStart, profileRAMSize, []arm64map.Range{{Base: profileConfigurationBase, Bytes: profileConfigurationBytes, Kind: arm64map.Uncached}})
}

func configurationPublication() error {
	memoryBarrier()
	_, err := configurationMapping()
	return err
}

func initialTapeArena() (*nativefabric.InitialTapeArena, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	if !profileInitialTapeEnabled || configurationArena == nil ||
		profileInitialTapeBase != nativefabric.InitialTapeArenaBase || profileInitialTapeBytes != nativefabric.InitialTapeArenaBytes {
		return nil, errors.New("DE25: completed pinned initial tape arena unavailable")
	}
	if tapeArena == nil {
		if tapeArenaAttempted {
			return nil, errors.New("DE25: independent tape arena initialization was already attempted")
		}
		tapeArenaAttempted = true
		if err := InitDevices(); err != nil {
			return nil, err
		}
		if err := initialTapePublication(); err != nil {
			return nil, err
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileInitialTapeBase))), profileInitialTapeBytes)
		arena, err := nativefabric.NewInitialTapeArena(profileInitialTapeBase, data, configurationArena, initialTapePublication)
		if err != nil {
			return nil, err
		}
		tapeArena = arena
	}
	return tapeArena, nil
}

// Verify the complete paired resources around every tape publication and SMC.
// Verify also checks the normal heap and the secure firmware exclusion.
func initialTapeMapping() (arm64map.Observation, error) {
	if !profileInitialTapeEnabled || profileInitialTapeBase != nativefabric.InitialTapeArenaBase ||
		profileInitialTapeBytes != nativefabric.InitialTapeArenaBytes ||
		profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 ||
		profileReaderMemoryBase != 0x95000000 || profileReaderMemoryArenaBytes != 8<<20 {
		return arm64map.Observation{}, errors.New("DE25: independent tape mapping unavailable")
	}
	return arm64map.Verify(profileRAMStart, profileRAMSize, []arm64map.Range{
		{Base: profileInitialTapeBase, Bytes: profileInitialTapeBytes, Kind: arm64map.Uncached},
		{Base: profileConfigurationBase, Bytes: profileConfigurationBytes, Kind: arm64map.Uncached},
		{Base: profileReaderMemoryBase, Bytes: profileReaderMemoryArenaBytes, Kind: arm64map.Uncached},
	})
}

func initialTapePublication() error {
	memoryBarrier()
	_, err := initialTapeMapping()
	return err
}
