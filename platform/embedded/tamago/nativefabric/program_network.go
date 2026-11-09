// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// AttachNetwork transfers one active controller before frontend ownership.
// Only a trusted platform with a qualified boot/MAC/DMA supplier may call it.
// It performs no hardware IO and cannot enable an unqualified board profile.
func (s *ProgramService) AttachNetwork(control *network.Controller) error {
	if control == nil {
		return ErrProgramSession
	}
	p, view, err := s.begin(context.Background())
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if view.State != ProgramReady || view.Generation != 0 || p.backendClaimed || p.network != nil {
		return ErrProgramSession
	}
	if err = control.Claim(); err != nil {
		return err
	}
	p.network = control
	return nil
}

// All helpers hold programService.operation. A failed network boundary cannot
// invoke fail's ordinary FPGA cleanup: the platform DMA state is then unknown.
func (p *programService) networkFailure(err error) error {
	p.preferences.seal(err)
	p.viewMu.Lock()
	p.view.State, p.view.Error = ProgramRebootRequired, err.Error()
	// Keep Held and identity unchanged: retirement is not a hardware receipt.
	p.viewMu.Unlock()
	return errors.Join(ErrProgramRetired, err)
}

func (p *programService) pauseNetwork(ctx context.Context) error {
	if p.network == nil {
		return nil
	}
	if err := p.network.PauseNetwork(ctx); err != nil {
		return p.networkFailure(err)
	}
	return nil
}

// Registered after request validation and a successful pause. Runs before the
// operation mutex unlocks, and returns restart failure through the original API.
func (p *programService) finishNetwork(result *error) {
	if p.network == nil {
		return
	}
	p.viewMu.Lock()
	healthy := p.view.State == ProgramReady || p.view.State == ProgramRunning
	p.viewMu.Unlock()
	if !healthy {
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.network.ResumeNetwork(cleanup); err != nil {
		*result = errors.Join(*result, p.networkFailure(err))
	}
}
