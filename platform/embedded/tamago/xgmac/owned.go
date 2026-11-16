// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"context"

	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// OpenControlled joins initial device startup to this boot's private firmware
// lease. The caller supplies independently accepted boot ownership and retains
// the pinned arena until reboot, including on failure. No packet workers may
// use the device before this function succeeds. It never retries or attempts
// firmware cleanup after an uncertain boundary.
//
// A non-nil failed device is retired without further MMIO and must be retained.
// The returned controls consume firmware confirmation after each driver restart
// and must be bound to the same network Link before lifecycle ownership moves.
func OpenControlled(ctx context.Context, cpu maccontrol.Caller, bootOwner uint64, io Registers, memory Memory, config Config) (*Device, network.DMAControl, error) {
	lease, err := maccontrol.New(ctx, cpu, bootOwner)
	if err != nil {
		return nil, network.DMAControl{}, err
	}
	if err = lease.Acquire(ctx); err != nil {
		return nil, network.DMAControl{}, err
	}
	if err = ctx.Err(); err != nil {
		return nil, network.DMAControl{}, err
	}
	d, err := Open(io, memory, config)
	if d != nil {
		d.lease = lease // retain exact last ownership, even if startup fails
	}
	if err == nil {
		err = lease.Confirm(ctx)
	}
	var control network.DMAControl
	if err == nil {
		control, err = lease.BindPlatform()
	}
	if err == nil {
		control, err = d.BindControl(control)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if d != nil {
			err = d.retireControl(err)
		}
		return d, network.DMAControl{}, err
	}
	return d, control, nil
}
