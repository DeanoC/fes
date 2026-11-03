// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

// StoredConfigurationPlan binds one retained selection to its validated program
// and immutable boot source before its store is contained. It supplies no hardware permission or
// general runtime compatibility admission. Keep the plan, any returned transfer
// and the platform reservation until reboot on an uncertain outcome.
type StoredConfigurationPlan struct {
	state *storedConfigurationPlan
}

// Copies of the public handle share the same consumption and retention state.
type storedConfigurationPlan struct {
	mu                     sync.Mutex
	consumed               bool
	v                      *HPSContextVerifier
	program                *selectedProgram
	selection              *packagestore.Selection
	control, configuration []byte
	payload                []byte
	region                 *Region
}

// PreflightStoredConfiguration checks only privately retained bytes and compiled
// maps. It performs no firmware call, filesystem read, reservation or SD stop.
// Rejection therefore leaves storage usable. Success freezes the maps and
// payload; a fresh secure ownership check is still required after containment.
func (v *HPSContextVerifier) PreflightStoredConfiguration(control, configuration []byte, selection *packagestore.Selection) (*StoredConfigurationPlan, error) {
	m, err := v.configurationMap(configuration)
	if err != nil {
		return nil, err
	}
	if _, err = v.checkedControlMap(control); err != nil {
		return nil, err
	}
	inspection := selection.Inspection()
	d, id := inspection.Descriptor, v.Identity()
	if inspection.PackageID != id.PackageID || d.Format != 2 || d.Core.ID != id.CoreID ||
		d.Target.Platform != id.Platform || d.Target.Device != id.Device || d.Target.ProgrammingProfile != id.ProgrammingProfile ||
		d.ABI.ID != id.ABI || d.ABI.Major != int64(id.Major) || d.ABI.Minor != int64(id.Minor) ||
		d.Build.ID != id.BuildID || d.Payload.Size != int64(m.CoreSize) || d.Payload.SHA256 != m.CoreSHA256 {
		return nil, errors.New("native fabric: retained package differs from compiled cold source")
	}
	data, err := io.ReadAll(io.LimitReader(selection.Archive().Payload(), int64(m.CoreSize)+1))
	if err != nil {
		return nil, err
	}
	if err := checkConfigurationBytes(m, data); err != nil {
		return nil, err
	}
	return &StoredConfigurationPlan{state: &storedConfigurationPlan{v: v, selection: selection, control: bytes.Clone(control),
		configuration: bytes.Clone(configuration), payload: data}}, nil
}

// NewTransfer consumes the plan once, including on failure. Only a current
// handoff for its exact selection may reach the live cold ownership checks and
// DMA reservation. No filesystem handle is used and neither device is released.
func (p *StoredConfigurationPlan) NewTransfer(handoff *packagestore.Handoff, reserve func([]byte) (*Region, error)) (*ConfigurationTransfer, HPSBootObservation, error) {
	return p.newTransfer(handoff, reserve, false)
}

// NewReconfigurationTransfer consumes a fresh plan after this verifier's own
// successful computer return. It never resumes storage or treats the older SD
// receipt as live FPGA authority. Secure rearming still checks the captured program.
func (p *StoredConfigurationPlan) NewReconfigurationTransfer(handoff *packagestore.Handoff, reserve func([]byte) (*Region, error)) (*ConfigurationTransfer, HPSBootObservation, error) {
	return p.newTransfer(handoff, reserve, true)
}

func (plan *StoredConfigurationPlan) newTransfer(handoff *packagestore.Handoff, reserve func([]byte) (*Region, error), rearm bool) (*ConfigurationTransfer, HPSBootObservation, error) {
	if plan == nil || plan.state == nil {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained configuration plan missing")
	}
	p := plan.state
	p.mu.Lock()
	if p.consumed || p.v == nil || p.selection == nil {
		p.mu.Unlock()
		return nil, HPSBootObservation{}, errors.New("native fabric: retained configuration plan invalid or consumed")
	}
	p.consumed = true
	p.mu.Unlock()
	if reserve == nil {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained configuration reservation missing")
	}
	selection, err := handoff.Selection()
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	if selection != p.selection {
		return nil, HPSBootObservation{}, errors.New("native fabric: storage handoff differs from preflight selection")
	}
	v := p.v
	receipt := handoff.Receipt()
	source, err := v.Verify(v.Identity())
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	held, generation := v.coldSource(source), receipt.Generation == source.Bridges.Generation
	if rearm {
		v.computerMu.Lock()
		returned := v.computerClosed && source.Bridges.Generation > 2 && v.computerGeneration == source.Bridges.Generation-2
		fromReturn := handoff.ReturnedFrom()
		if receipt.Generation == source.Bridges.Generation && v.computerClosed && v.computerSDResumed &&
			v.computerReturn.Generation > 0 && v.computerReturn.Generation < source.Bridges.Generation &&
			fromReturn == (sdcontrol.Receipt{Owner: v.computerReturn.Owner, Generation: v.computerReturn.Generation}) {
			returned = true
			generation = true
		} else {
			generation = receipt.Generation < source.Bridges.Generation
		}
		v.computerMu.Unlock()
		held = v.reconfigurationSource(source) && returned
	}
	if !held || !generation || receipt.Active || receipt.Prepared || receipt.Owner != source.Bridges.Owner {
		return nil, HPSBootObservation{}, errors.New("native fabric: retained storage or computer return differs from live ownership")
	}
	region, err := reserve(p.payload)
	p.region = region // Retain even a reservation returned together with an error.
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	transfer, err := v.newSourceConfigurationTransfer(p.control, p.configuration, region, source, rearm, p.program)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	prepare := transfer.beforeStart
	transfer.beforeStart = func() error {
		current, err := handoff.Selection()
		if err != nil {
			return err
		}
		if current != selection || handoff.Receipt() != receipt {
			return errors.New("native fabric: retained storage handoff changed before configuration")
		}
		return prepare()
	}
	return transfer, source, nil
}
