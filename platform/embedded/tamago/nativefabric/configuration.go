// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// FPGAConfigurationObservation is SDM readiness under unchanged closed source
// ownership. It is not endpoint identity, boot attestation or admission.
type FPGAConfigurationObservation struct {
	Source     HPSBootObservation
	Configured bool
}

// Configuration requests one fixed read-only SDM query. The secure service
// cannot open a bridge or access an FPGA register through this operation.
func (v *HPSContextVerifier) Configuration(raw []byte) (FPGAConfigurationObservation, error) {
	if _, err := v.checkedControlMap(raw); err != nil {
		return FPGAConfigurationObservation{}, err
	}
	source, err := v.Verify(v.Identity())
	if err != nil {
		return FPGAConfigurationObservation{}, err
	}
	before := source.Bridges
	if !v.coldSource(source) {
		return FPGAConfigurationObservation{}, errors.New("native fabric: configuration query requires fresh closed cold scope")
	}
	r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeConfiguration), before.Owner, uint64(g.FesEmbeddedBridgeCookie), before.Generation})
	if err != nil {
		return FPGAConfigurationObservation{}, err
	}
	if r[0] != 0 || uint32(r[1]>>32) != g.FesEmbeddedBridgeConfigurationTag || uint32(r[1]) > 1 || r[2] != before.Owner || r[3] != before.Generation {
		return FPGAConfigurationObservation{}, errors.New("native fabric: incomplete secure configuration observation")
	}
	after, err := (&FirmwareBridges{cpu: v.cpu}).ObserveBridges()
	if err != nil || after != before {
		return FPGAConfigurationObservation{}, errors.New("native fabric: configuration query changed closed ownership")
	}
	return FPGAConfigurationObservation{source, uint32(r[1]) == 1}, nil
}
