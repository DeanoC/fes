// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
)

var ErrProgramExecution = errors.New("native program service: instruction-fetch observation failed")

// ProgramFetchObservation records bus fetches, not retired instructions,
// emulation fidelity, video or permission to activate another package.
// All fields are values; callers cannot mutate the service's saved evidence.
type ProgramFetchObservation struct {
	Scope      string                 `json:"scope"`
	Owner      uint64                 `json:"owner"`
	Generation uint64                 `json:"generation"`
	PackageID  string                 `json:"package_id"`
	BuildID    string                 `json:"build_id"`
	Before     computer.FetchActivity `json:"before"`
	Held       computer.FetchActivity `json:"held"`
	Running    computer.FetchActivity `json:"running"`
	Delta      uint32                 `json:"delta"`
	Polls      uint32                 `json:"polls"`
	ElapsedNS  int64                  `json:"elapsed_ns"`
	LimitNS    int64                  `json:"limit_ns"`
}

// FetchObservation returns saved evidence only for this running generation.
// It performs no IO and never substitutes for a live ownership check.
func (s *ProgramService) FetchObservation() (ProgramFetchObservation, bool) {
	if s == nil || s.state == nil {
		return ProgramFetchObservation{}, false
	}
	p := s.state
	p.viewMu.Lock()
	defer p.viewMu.Unlock()
	o, v := p.execution, p.view
	ok := v.State == ProgramRunning && !v.Held && o.Generation == v.Generation && o.Owner == v.Owner && o.Delta > 0 && o.PackageID == v.Identity.PackageID && o.BuildID == v.Identity.BuildID
	if !ok {
		return ProgramFetchObservation{}, false
	}
	return o, true
}

// Caller holds operation. The entire Hold/two samples/Run/progress sequence
// shares the existing computer limit; each individual IO retains its own live
// CSR/bridge/mapping guards. No uncertain command is replayed.
func (p *programService) startExecution(ctx context.Context, view ProgramServiceStatus) (ProgramFetchObservation, error) {
	o := ProgramFetchObservation{Scope: "nano-computer-fetch-v1", Owner: view.Owner, Generation: view.Generation, PackageID: view.Identity.PackageID, BuildID: view.Identity.BuildID, LimitNS: int64(p.limits.Timeout)}
	start := p.limits.Now()
	last := start
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.limits.Now()
		if now.Before(last) || now.Sub(start) >= p.limits.Timeout {
			return fmt.Errorf("%w: elapsed=%s limit=%s", computer.ErrLimit, now.Sub(start), p.limits.Timeout)
		}
		last = now
		return nil
	}
	run := func(call func() error) error {
		if err := check(); err != nil {
			return err
		}
		if err := call(); err != nil {
			return err
		}
		return check()
	}
	c := p.computer.Computer()
	if err := run(func() error { return c.Hold(ctx) }); err != nil {
		return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
	}
	sample := func(dst *computer.FetchActivity) error {
		return run(func() error { var err error; *dst, err = c.Activity(ctx); return err })
	}
	if err := sample(&o.Before); err != nil {
		return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
	}
	p.limits.Yield()
	if err := sample(&o.Held); err != nil {
		return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
	}
	if o.Before != o.Held {
		return ProgramFetchObservation{}, fmt.Errorf("%w: fetches changed while held", ErrProgramExecution)
	}
	if p.plan != nil && p.plan.state != nil && p.plan.state.program != nil && len(p.plan.state.program.tape) != 0 {
		// SD is contained. Only bytes privately retained and admitted before
		// configuration reach the existing CRC/ACK media transport. A failure
		// leaves execution held and retires the owner; no Run or replay follows.
		if err := run(func() error { return c.LoadInitialTape(ctx, p.plan.state.program.tape) }); err != nil {
			return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
		}
		var held computer.FetchActivity
		if err := sample(&held); err != nil {
			return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
		}
		if held != o.Held {
			return ProgramFetchObservation{}, fmt.Errorf("%w: fetches changed during held tape load", ErrProgramExecution)
		}
	}
	if err := run(func() error { return c.Run(ctx) }); err != nil {
		return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
	}
	for poll := uint32(0); poll < p.limits.MaxPolls; poll++ {
		o.Polls = poll + 1
		if err := sample(&o.Running); err != nil {
			return ProgramFetchObservation{}, errors.Join(ErrProgramExecution, err)
		}
		o.Delta = o.Running.Cycles - o.Held.Cycles
		if o.Delta > math.MaxUint32/2 {
			return ProgramFetchObservation{}, fmt.Errorf("%w: counter moved backwards or reset", ErrProgramExecution)
		}
		if o.Delta != 0 {
			o.ElapsedNS = int64(last.Sub(start))
			return o, nil
		}
		p.limits.Yield()
	}
	return ProgramFetchObservation{}, fmt.Errorf("%w: no progress after %d observations", ErrProgramExecution, p.limits.MaxPolls)
}
