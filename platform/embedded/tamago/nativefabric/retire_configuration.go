// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/hex"
	"errors"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// ConfigurationRetirement acknowledges withdrawal of this boot's configuration
// authority. Status describes a separate bounded SDM diagnostic. Neither result
// proves completion, drain, or permission to release any reserved memory.
type ConfigurationRetirement struct {
	Owner, Generation uint64
	Status            int32
}

// RetireConfiguration is one-shot and only applies to a failed cold transfer
// whose possible outstanding buffers are still retained. The earlier verified
// source is bracketed by a fresh held-bridge snapshot; source-context queries
// cannot interleave with a prepared transfer. The secure provider retires
// authority before issuing its fixed status request. Keep t until reboot even
// if that query reports a ready FPGA or the retirement reply is uncertain.
func (v *HPSContextVerifier) RetireConfiguration(t *ConfigurationTransfer, source HPSBootObservation, configuration []byte) (ConfigurationRetirement, error) {
	var m coldConfigurationMap
	var err error
	if t != nil && t.binding != nil && t.binding.program != nil {
		p := t.binding.program
		if p.loader == nil || p.loader.boot != v || t.binding.source != source || !bytes.Equal(configuration, p.configurationRaw) {
			return ConfigurationRetirement{}, errors.New("native fabric: retirement differs from selected program")
		}
		m = p.configuration
	} else {
		m, err = v.configurationMap(configuration)
	}
	if err != nil {
		return ConfigurationRetirement{}, err
	}
	if v.cpu == nil || t == nil || !t.Status().Failed || t.done || len(t.pending) == 0 ||
		t.diagnosticAttempted || t.region == nil || t.region.Release == nil ||
		t.region.Address != 0x94800000 || len(t.region.Bytes) != m.CoreSize ||
		t.dramBase != m.DRAMBase || t.chunk != m.ChunkBytes || hex.EncodeToString(t.digest[:]) != m.CoreSHA256 ||
		!v.coldSource(source) {
		return ConfigurationRetirement{}, errors.New("native fabric: retained failed cold transfer required for retirement")
	}
	t.diagnosticAttempted = true
	before, err := (&FirmwareBridges{cpu: v.cpu}).ObserveBridges()
	if err != nil {
		return ConfigurationRetirement{}, err
	}
	if before != source.Bridges {
		return ConfigurationRetirement{}, errors.New("native fabric: retirement lost the original closed source")
	}
	r, err := v.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeRetireConfiguration),
		before.Owner, uint64(g.FesEmbeddedBridgeCookie), before.Generation})
	if err != nil {
		return ConfigurationRetirement{}, err
	}
	status := int32(uint32(r[1]))
	if r[0] != 0 || r[1]>>32 != uint64(g.FesEmbeddedBridgeRetirementTag) ||
		r[2] != before.Owner || r[3] != before.Generation || status > 0 {
		return ConfigurationRetirement{}, errors.New("native fabric: cold retirement acknowledgement rejected")
	}
	return ConfigurationRetirement{before.Owner, before.Generation, status}, nil
}
