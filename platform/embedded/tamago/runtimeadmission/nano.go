// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"github.com/DeanoC/FogCast/corepackage"
	n "github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano/generated"
)

// Freeze the generated registry before any caller can mutate its exported
// slice. Every constructed checker additionally retains its own private copy.
var nanoComputerPairs = func() []ProgrammingProfile {
	profiles := make([]ProgrammingProfile, 0, len(n.De25NanoProgrammingProfilePairs))
	for _, p := range n.De25NanoProgrammingProfilePairs {
		profiles = append(profiles, ProgrammingProfile{p.Profile, p.ABI, p.Major, p.DiagnosticOnly})
	}
	return profiles
}()

// NewNanoComputerCompatibility selects the shared native computer pairing.
// It supplies no driver inventory, live boot proof or physical activation.
// RuntimeCompatibility must separately require the integration's actual drivers.
func NewNanoComputerCompatibility() (*Compatibility, error) {
	return NewCompatibility(Target{n.De25NanoProgrammingPlatform, n.De25NanoProgrammingDevice}, nanoComputerPairs)
}

// The CPU-fed ROM is a separate platform delivery endpoint, not an extra GP
// opcode. Only this exact native computer contract can require that extension.
// Archive validation independently binds the role, size and cpu-csr-v1 map.
func nanoComputerProfile(d corepackage.Descriptor) bool {
	if d.Target.Platform != n.De25NanoProgrammingPlatform || d.Target.Device != n.De25NanoProgrammingDevice ||
		!isComputer(d) {
		return false
	}
	for _, p := range nanoComputerPairs {
		if !p.DiagnosticOnly && p.Profile == d.Target.ProgrammingProfile && p.ABI == d.ABI.ID && int64(p.Major) == d.ABI.Major {
			return true
		}
	}
	return false
}

func nanoComputerROM(d corepackage.Descriptor) bool {
	return nanoComputerProfile(d) && d.ABI.Minor == 0 && d.Format == 3 && d.ROM != nil && d.ROM.Role == "firmware"
}
