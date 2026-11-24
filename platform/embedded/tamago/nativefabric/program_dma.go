// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"

	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

const (
	uploadShellScanoutCPUBase = 0x20010200
	scanoutDMARejected        = 1 << 8
)

// pollDMAReject observes only the live, fixed scanout aperture of an owned
// shared-shell session. The bit requests Stop. It grants no drain or release.
func (s *ProgramService) pollDMAReject(ctx context.Context) (uint64, bool, error) {
	p := s.state
	if !p.loader.sharedCoreShell || s.Status().State != ProgramRunning || !p.operation.TryLock() {
		return 0, false, nil
	}
	defer p.operation.Unlock()
	view := s.Status()
	if view.State != ProgramRunning || p.plan == nil || p.plan.state.program.role != programRoleUploadShell {
		return 0, false, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	port := p.computer.port
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.retired || port.closing {
		return 0, false, ErrProgramRetired
	}
	before, err := port.bridges.ObserveBridges()
	if err != nil || before != port.want {
		return 0, false, errors.Join(errors.New("native fabric: scanout fault poll lost control ownership"), err)
	}
	status, err := port.cpu.ReadPhysical32(uploadShellScanoutCPUBase + uint64(cg.FesDe25ScanoutStatusOffset))
	if err != nil {
		return 0, false, err
	}
	after, err := port.bridges.ObserveBridges()
	if err != nil || after != before {
		return 0, false, errors.Join(errors.New("native fabric: scanout fault poll ownership changed"), err)
	}
	return view.Generation, status&scanoutDMARejected != 0, nil
}
