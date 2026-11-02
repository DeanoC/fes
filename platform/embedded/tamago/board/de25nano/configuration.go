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
var configurationAttempted bool
var configurationArena *nativefabric.ConfigurationArena

// ReserveConfiguration copies into the build's explicitly excluded uncached
// arena. The mapping stays pinned until reboot; a new lease can overwrite it
// only after the previous transfer has completed. An uncertain lease or failed
// initialization cannot be reused. No ordinary profile supplies these constants.
func ReserveConfiguration(source []byte) (*nativefabric.Region, error) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	if profileConfigurationBase != 0x94800000 || profileConfigurationBytes != 8<<20 || len(source) == 0 || len(source) > profileConfigurationBytes {
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
		arena, err := nativefabric.NewConfigurationArena(profileConfigurationBase, data, func() error { memoryBarrier(); return nil })
		if err != nil {
			return nil, err
		}
		configurationArena = arena
	}
	return configurationArena.Reserve(source)
}
