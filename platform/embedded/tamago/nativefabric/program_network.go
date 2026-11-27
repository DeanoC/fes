// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// StartNetwork joins the trusted platform opener to this boot before frontend
// ownership using the default 5-second startup bound. bootOwner must come from
// independently accepted boot admission, never a catalogue or cached
// application status. The opener retains partial DMA resources, honors the
// first (startup) context and uses the second context for the IP stack
// lifetime. Failure cannot trigger FPGA cleanup: a failed or late opener leaves
// DMA ownership uncertain until reboot.
func (s *ProgramService) StartNetwork(ctx context.Context, bootOwner uint64, open func(context.Context, context.Context, uint64) (*network.Controller, error)) error {
	return s.StartNetworkWithTimeout(ctx, 5*time.Second, bootOwner, open)
}

// StartNetworkWithTimeout behaves like StartNetwork but enforces the explicit
// startupTimeout on the startup context passed to open (preserving ctx as the
// unbounded stack lifetime context), e.g. allowing DHCPv4's 10s acquisition budget.
func (s *ProgramService) StartNetworkWithTimeout(ctx context.Context, startupTimeout time.Duration, bootOwner uint64, open func(context.Context, context.Context, uint64) (*network.Controller, error)) error {
	if ctx == nil || startupTimeout <= 0 || bootOwner == 0 || open == nil {
		return ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p, view, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if view.State != ProgramReady || view.Generation != 0 || view.Owner != bootOwner || p.backendClaimed || p.network != nil || p.networkStartup != nil {
		return ErrProgramSession
	}
	work, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	lifetime, stopWorkers := context.WithCancel(ctx)
	p.networkCancel = stopWorkers
	finished := false
	defer func() {
		if !finished {
			// Preserve a platform panic while preventing a frontend defer from
			// attempting ordinary FPGA cleanup under unknown DMA ownership.
			p.networkFailure(errors.New("native network startup aborted"))
		}
	}()
	p.networkStartup, err = open(work, lifetime, bootOwner)
	if err == nil {
		err = work.Err()
	}
	if err == nil && p.networkStartup == nil {
		err = errors.New("native network startup returned no controller")
	}
	if err == nil {
		err = p.networkStartup.Claim()
	}
	finished = true
	if err != nil {
		return p.networkFailure(err)
	}
	p.network = p.networkStartup
	return nil
}

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
	if p.networkCancel != nil {
		p.networkCancel() // Software cancellation does not release DMA memory.
	}
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
	allowRunning := p.menu == nil || !p.menu.networkInMenuOnly
	healthy := p.view.State == ProgramReady || p.view.State == ProgramRunning && allowRunning
	parked := p.view.State == ProgramParked
	p.viewMu.Unlock()
	if !healthy {
		if parked && p.networkCancel != nil {
			p.networkCancel()
		}
		return
	}
	// Resume has its own bounded platform deadline independent of the
	// completed program transition, which may already have timed out.
	if err := p.network.ResumeNetwork(context.Background()); err != nil {
		*result = errors.Join(*result, p.networkFailure(err))
	}
}
