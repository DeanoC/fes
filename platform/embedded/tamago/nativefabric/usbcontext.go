// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import "github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"

const usbInspectionProfile = "usb-only-hps-inspection"

// USBHPSContextVerifier exposes only source identity and pin observations.
// Its underlying verifier and FPGA/program/ROM methods remain private.
type USBHPSContextVerifier struct {
	boot *HPSContextVerifier
}

// NewUSBHPSContextVerifier admits the supported held-ROM/native source ABI for
// USB inspection, without constructing computer, ROM or programming resources.
// It performs no I/O and supplies no live boot or bridge-opening authority.
func NewUSBHPSContextVerifier(cpu FirmwareCaller, source []byte) (*USBHPSContextVerifier, error) {
	boot, err := newHPSContextVerifier(cpu, source, usbInspectionProfile)
	if err != nil {
		return nil, err
	}
	return &USBHPSContextVerifier{boot: boot}, nil
}

func (v *USBHPSContextVerifier) Identity() fabricmodel.PackageIdentity {
	return v.boot.Identity()
}

func (v *USBHPSContextVerifier) Verify(id fabricmodel.PackageIdentity) (HPSBootObservation, error) {
	return v.boot.Verify(id)
}

func (v *USBHPSContextVerifier) ProbeHPSIO(source []byte) (HPSIOObservation, error) {
	return v.boot.ProbeHPSIO(source)
}
