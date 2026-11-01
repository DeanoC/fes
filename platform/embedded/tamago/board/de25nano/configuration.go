//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var configurationMu sync.Mutex
var configurationAttempted, configurationCompleted bool
var configurationRegion *nativefabric.Region

// ReserveConfiguration copies into the build's explicitly excluded uncached
// arena. This cold reservation stays pinned until reboot, even after protocol
// completion, a constructor error or failed SMC. No ordinary profile supplies
// these constants, and no later call can reuse an ambiguous reservation.
func ReserveConfiguration(source []byte) (*nativefabric.Region, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	if configurationAttempted || profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 || len(source) == 0 || len(source) > profileConfigurationBytes {
		return nil, errors.New("DE25: cold configuration reservation unavailable")
	}
	configurationAttempted = true
	if err := InitDevices(); err != nil {
		return nil, err
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileConfigurationBase))), len(source))
	copy(data, source)
	memoryBarrier()
	configurationRegion = &nativefabric.Region{Address: profileConfigurationBase, Bytes: data, Release: func() error {
		configurationMu.Lock()
		defer configurationMu.Unlock()
		if configurationCompleted {
			return errors.New("DE25: cold configuration completion was already consumed")
		}
		memoryBarrier()
		configurationCompleted = true
		return nil // Lifetime remains boot-pinned; this does not release the arena.
	}}
	return configurationRegion, nil
}
