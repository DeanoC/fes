// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"encoding/binary"
	"fmt"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	a "github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission/generated"
)

// StopWithDisplay retires the local scanout reader, verifies the GP display
// plane is quiesced and then holds the machine. Both apertures must belong to
// the same live owner, with display submissions blocked by the caller. The
// acknowledgement is local to the reader and supplies no bridge drain proof.
func (c *Computer) StopWithDisplay(ctx context.Context, display IO) (err error) {
	if display == nil || c.identity.Capabilities&g.FesComputerCapabilityVideoSessionDisplay == 0 {
		return ErrArgument
	}
	o, err := c.begin(ctx, "display-stop-and-hold")
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	read := func(offset uint32) (uint32, error) {
		o.phase, o.offset = "scanout-read", offset
		if err := o.check(); err != nil {
			return 0, err
		}
		value, err := display.Read32(offset)
		if err != nil {
			return 0, err
		}
		return value, o.check()
	}
	for _, field := range [...]struct{ offset, want uint32 }{
		{g.FesDe25ScanoutIdOffset, g.FesDe25ScanoutIdentity},
		{g.FesDe25ScanoutVersionOffset, g.FesDe25ScanoutVersion},
		{g.FesDe25ScanoutBuildId0Offset, binary.BigEndian.Uint32(c.identity.Build[12:16])},
		{g.FesDe25ScanoutBuildId1Offset, binary.BigEndian.Uint32(c.identity.Build[8:12])},
		{g.FesDe25ScanoutBuildId2Offset, binary.BigEndian.Uint32(c.identity.Build[4:8])},
		{g.FesDe25ScanoutBuildId3Offset, binary.BigEndian.Uint32(c.identity.Build[0:4])},
	} {
		value, err := read(field.offset)
		if err != nil {
			return err
		}
		if value != field.want {
			return fmt.Errorf("%w: scanout register %#x", ErrIdentity, field.offset)
		}
	}
	o.phase, o.offset = "scanout-stop", g.FesDe25ScanoutCommandOffset
	if err := o.check(); err != nil {
		return err
	}
	if err := display.Write32(g.FesDe25ScanoutCommandOffset, g.FesDe25ScanoutCommandStop); err != nil {
		return err
	}
	for poll := uint32(0); ; poll++ {
		if poll == c.limits.MaxPolls {
			return o.limit("scanout-polls", o.last)
		}
		status, err := read(g.FesDe25ScanoutStatusOffset)
		if err != nil {
			return err
		}
		if status&(g.FesDe25ScanoutStatusFault|g.FesDe25ScanoutStatusClockFault) != 0 {
			return fmt.Errorf("%w: scanout fault during drain", ErrProtocol)
		}
		if status&(g.FesDe25ScanoutStatusDrained|g.FesDe25ScanoutStatusPending|g.FesDe25ScanoutStatusRunning) == g.FesDe25ScanoutStatusDrained {
			break
		}
		c.limits.Yield()
	}
	if err := o.mutate(a.FesApplicationOpcodeMenuControl, a.FesApplicationControlIndex,
		uint16(a.FesApplicationMenuControlQuiesce)); err != nil {
		return err
	}
	state, err := o.exchange(a.FesApplicationOpcodeMenuInfo, a.FesApplicationMenuInfoStateIndex, 0)
	if err != nil {
		return err
	}
	if uint32(state) & ^a.FesApplicationMenuStateConfigured != a.FesApplicationMenuStateQuiesced {
		return fmt.Errorf("%w: GP display plane not quiesced", ErrProtocol)
	}
	return o.mutate(g.FesComputerOpcodeExecution, g.FesComputerControlIndex,
		uint16(g.FesComputerExecutionHoldReset))
}
