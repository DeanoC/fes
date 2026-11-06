//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"sync"
)

var deviceMapOnce sync.Once
var deviceMapError error

func InitDevices() error {
	deviceMapOnce.Do(func() {
		deviceMapError = arm64map.RequireFirmwareExclusion(profileRAMStart, profileRAMSize)
		if deviceMapError == nil && profileNetworkEnabled {
			deviceMapError = arm64map.NormalUncached(profileNetworkDMAStart, profileNetworkDMABytes, profileRAMStart, profileRAMSize)
		}
		if deviceMapError == nil && profileDisplayEnabled {
			deviceMapError = arm64map.NormalUncached(profileDisplayBase, profileDisplayBytes, profileRAMStart, profileRAMSize)
		}
		if deviceMapError == nil && profileFPGAProbeEnabled {
			deviceMapError = arm64map.NormalUncached(profileFPGAProbeBase, profileFPGAProbeBytes, profileRAMStart, profileRAMSize)
		}
		if deviceMapError == nil && profileConfigurationBase != 0 {
			deviceMapError = arm64map.NormalUncached(profileConfigurationBase, profileConfigurationBytes, profileRAMStart, profileRAMSize)
		}
		if deviceMapError == nil && profileReaderMemoryBase != 0 {
			deviceMapError = arm64map.NormalUncached(profileReaderMemoryBase, profileReaderMemoryArenaBytes, profileRAMStart, profileRAMSize)
		}
	})
	return deviceMapError
}
func memoryBarrier()
