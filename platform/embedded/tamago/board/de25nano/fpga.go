//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/sdm"
)

var fpgaOnce sync.Once
var fpgaProbe *sdm.Client
var fpgaError error

// OpenFPGAProbe retains the firmware service arena until reboot. It exposes
// only read-only discovery and never touches TF-A's mailbox MMIO registers.
func OpenFPGAProbe() (*sdm.Client, error) {
	fpgaOnce.Do(func() {
		if !profileFPGAProbeEnabled {
			fpgaError = errors.New("SDM: read-only firmware service not admitted by handoff")
			return
		}
		if fpgaError = InitDevices(); fpgaError != nil {
			return
		}
		response := unsafe.Slice((*uint32)(unsafe.Pointer(uintptr(profileFPGAProbeBase))), 8)
		fpgaProbe, fpgaError = sdm.New(smc, response, profileFPGAProbeBase)
	})
	return fpgaProbe, fpgaError
}

func smc(request [7]uint64) (response [4]uint64)
