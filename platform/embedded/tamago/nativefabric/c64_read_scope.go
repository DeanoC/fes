// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"

	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
)

// observeC64Scope compares two independently owned generations. Acquisition,
// configuration, shutdown and whole-system containment use their own contracts.
func observeC64Scope(cpu FirmwareCaller, owner, bridgeGeneration, macGeneration uint64) error {
	if cpu == nil || owner == 0 || bridgeGeneration == 0 || macGeneration < 2 {
		return errors.New("native C64: incomplete control or confirmed MAC binding")
	}
	// The service has just one read-only operation. X1 is the complete private
	// MAC generation. Both trailing argument registers must remain zero.
	r, err := cpu.Invoke([7]uint64{uint64(g.FesEmbeddedC64ScopeCall), macGeneration, owner,
		uint64(g.FesEmbeddedC64ScopeCookie), bridgeGeneration})
	if err != nil {
		return err
	}
	want := [4]uint64{uint64(g.FesEmbeddedC64ScopeOK), uint64(g.FesEmbeddedC64ScopeTag) << 32, bridgeGeneration, macGeneration}
	if r != want {
		return errors.New("native C64: secure control scope absent, stale or changed")
	}
	return nil
}

// prepareC64Grant first proves the original control-only scope. This
// observation cannot request a grant. An absent supplier leaves C64 playable
// through the same independently checked legacy ownership.
func (c *computerIO) prepareC64Grant(r maccontrol.Receipt) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.retired || c.c64GrantPending || c.c64MACGeneration != 0 ||
		!c.want.MastersContained || c.want.NeverEnabled || !validBridgeSnapshot(c.want) ||
		c.want.ResetMask != g.FesEmbeddedBridgeMask&^g.FesEmbeddedBridgeControlMask ||
		r.Active || r.Confirmed || r.Owner != c.want.Owner || r.Generation < 3 || r.Generation > ^uint64(0)-3 {
		return false, errors.New("native C64: no fresh returned MENU MAC and original control scope")
	}
	if err := c.observeAperture(); err != nil {
		c.retired = true
		return false, err
	}
	tag := uint64(g.FesEmbeddedC64ScopeTag) << 32
	reply, err := c.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedC64ScopeCall), 0, c.want.Owner, uint64(g.FesEmbeddedC64ScopeCookie), c.want.Generation})
	if err != nil {
		return false, err
	}
	if err = c.observeAperture(); err != nil {
		c.retired = true
		return false, err
	}
	if reply == [4]uint64{uint64(g.FesEmbeddedC64ScopeNotReady), tag, 0, 0} ||
		reply[0] == uint64(g.FesEmbeddedC64ScopeRejected) && reply[1] == 0 {
		// The pinned MENU-only dispatcher returns a one-register REJECTED
		// for unknown service IDs. Its other result registers are unspecified.
		return false, nil
	}
	if reply != [4]uint64{0, tag, c.want.Generation, r.Generation} {
		return false, errors.New("native C64: pregrant scope response changed")
	}
	c.c64GrantPending = true
	// Acquire then confirm advance the existing public MAC generation twice.
	c.c64MACGeneration = r.Generation + 2
	return true, nil
}

// Caller holds the aperture mutex. Held-only transitions always go through the
// original bridge engine. An active C64 uses the dedicated secure observation.
func (c *computerIO) observeAperture() error {
	if c.c64GrantPending {
		return errors.New("native C64: grant confirmation pending")
	}
	if c.c64MACGeneration != 0 {
		return observeC64Scope(c.cpu, c.want.Owner, c.want.Generation, c.c64MACGeneration)
	}
	s, err := c.bridges.ObserveBridges()
	if err != nil || s != c.want {
		return errors.Join(errors.New("native fabric: computer control scope changed"), err)
	}
	return nil
}

func c64LiveMailboxWord(value uint32) bool {
	// Live chunk words and picker registers use their own bounded BSP ports.
	// Execution, initial ROM upload and unbatched media data stay contained.
	switch (value & cg.FesComputerOpcodeMask) >> 24 {
	case cg.FesComputerOpcodeKeyboardHid, cg.FesComputerOpcodeControllerButtons,
		cg.FesComputerOpcodeMouseRelative, cg.FesComputerOpcodeMediaInfo,
		cg.FesComputerOpcodeMediaBegin, cg.FesComputerOpcodeMediaCommit:
		return true
	}
	return false
}

// Called only after the actor has accepted this exact C64 and seen its fetches,
// and the ordinary controller has confirmed one fresh MAC grant.
func (c *computerIO) enterC64Scope(r maccontrol.Receipt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.closing || !c.c64GrantPending || c.c64MACGeneration != r.Generation || !c.want.MastersContained || c.want.NeverEnabled ||
		c.want.ResetMask != g.FesEmbeddedBridgeMask&^g.FesEmbeddedBridgeControlMask ||
		!r.Active || !r.Confirmed || r.Owner != c.want.Owner || r.Generation < 2 {
		return errors.New("native C64: missing original control aperture or confirmed MAC grant")
	}
	// Retain even if the first protected read is lost. No dependent cleanup
	// may infer that the MAC was contained from the missing response.
	if err := observeC64Scope(c.cpu, r.Owner, c.want.Generation, r.Generation); err != nil {
		c.retired = true
		return err
	}
	c.c64GrantPending = false
	return nil
}

// Called after the same controller has completed drain and revocation. Check
// the original unmodified, all-masters-contained snapshot before Hold or SD.
func (c *computerIO) leaveC64Scope(r maccontrol.Receipt) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.retired || c.closing || c.c64GrantPending || c.c64MACGeneration == 0 || c.c64MACGeneration == ^uint64(0) ||
		r.Active || r.Confirmed || r.Owner != c.want.Owner || r.Generation != c.c64MACGeneration+1 {
		return errors.New("native C64: missing matching MAC stop receipt")
	}
	s, err := c.bridges.ObserveBridges()
	if err != nil || s != c.want {
		c.retired = true
		return errors.Join(errors.New("native C64: intact original containment required after MAC stop"), err)
	}
	c.c64MACGeneration = 0
	return nil
}

func (c *computerIO) scopedState() (active, retired bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c64MACGeneration != 0, c.retired || c.c64GrantPending
}

func (c *computerIO) observeRunningC64() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.c64MACGeneration == 0 || c.retired || c.closing {
		return errors.New("native C64: no current read scope")
	}
	if err := c.observeAperture(); err != nil {
		c.retired = true
		return err
	}
	return nil
}
