// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"fmt"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// FirmwareCaller is an injected synchronous, bounded-return SMCCC transport.
type FirmwareCaller interface {
	Invoke([7]uint64) ([4]uint64, error)
}

// FirmwareBridges decodes the opt-in owned bridge service. It supplies no boot
// proof, physical programming profile, CPU aperture or memory reservation.
type FirmwareBridges struct{ cpu FirmwareCaller }

var ErrBridgeNotReady = errors.New("native fabric: firmware bridge ownership not initialized")

func NewFirmwareBridges(cpu FirmwareCaller) (*FirmwareBridges, error) {
	if cpu == nil {
		return nil, errors.New("native fabric: bridge firmware transport missing")
	}
	b := &FirmwareBridges{cpu: cpu}
	if _, err := b.ObserveBridges(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *FirmwareBridges) call(operation uint32, owner, generation uint64, closure bool) (BridgeSnapshot, uint32, error) {
	if b == nil || b.cpu == nil {
		return BridgeSnapshot{}, 0, errCPUHooksUnavailable
	}
	r, err := b.cpu.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(operation), owner, uint64(g.FesEmbeddedBridgeCookie), generation})
	if err != nil {
		return BridgeSnapshot{}, 0, err
	}
	if r[1]>>32 != uint64(g.FesEmbeddedBridgeTag) {
		return BridgeSnapshot{}, 0, errors.New("native fabric: unknown bridge firmware tag")
	}
	if r[0] == uint64(g.FesEmbeddedBridgeNotReady) && r[1] == uint64(g.FesEmbeddedBridgeTag)<<32 && r[2] == 0 && r[3] == 0 {
		return BridgeSnapshot{}, 0, ErrBridgeNotReady
	}
	if r[0] != 0 {
		return BridgeSnapshot{}, 0, fmt.Errorf("native fabric: bridge firmware status %d", r[0])
	}
	flags := uint32(r[1])
	allowed := g.FesEmbeddedBridgeSnapshotFlags
	if closure {
		allowed = g.FesEmbeddedBridgeClosureFlags
	}
	if flags & ^allowed != 0 {
		return BridgeSnapshot{}, 0, errors.New("native fabric: bridge firmware reserved flags")
	}
	s := BridgeSnapshot{Capabilities: BridgeCapabilities{Mask: g.FesEmbeddedBridgeMask, ControlMask: g.FesEmbeddedBridgeControlMask, HardwareDrainMask: g.FesEmbeddedBridgeDrainMask, FabricHandshake: FabricHandshake((flags >> g.FesEmbeddedBridgeFabricShift) & 3)},
		Owner: r[2], Generation: r[3], ResetMask: flags & g.FesEmbeddedBridgeMask, NeverEnabled: flags&g.FesEmbeddedBridgeNeverEnabled != 0, MastersContained: flags&g.FesEmbeddedBridgeMastersContained != 0}
	if !validBridgeSnapshot(s) {
		return BridgeSnapshot{}, 0, errors.New("native fabric: invalid bridge firmware snapshot")
	}
	return s, flags, nil
}

func (b *FirmwareBridges) ObserveBridges() (BridgeSnapshot, error) {
	s, _, err := b.call(g.FesEmbeddedBridgeSnapshot, 0, 0, false)
	return s, err
}
func (b *FirmwareBridges) CloseBridges(r BridgeCloseRequest) (BridgeClosure, error) {
	if !validBridgeSnapshot(r.Before) {
		return BridgeClosure{}, errors.New("native fabric: invalid expected bridge state")
	}
	op := g.FesEmbeddedBridgeClose
	if r.RequireFabricHandshake {
		op |= g.FesEmbeddedBridgeRequestFabric
	}
	if r.PreserveDataContainment {
		op |= g.FesEmbeddedBridgeRequestHeld
	}
	s, flags, err := b.call(op, r.Before.Owner, r.Before.Generation, true)
	if err != nil {
		return BridgeClosure{}, err
	}
	return BridgeClosure{After: s, ProducersQuiescent: flags&g.FesEmbeddedBridgeProducersQuiescent != 0, FabricAcknowledged: flags&g.FesEmbeddedBridgeFabricAcknowledged != 0,
		HardwareDrainedMask: (flags >> g.FesEmbeddedBridgeDrainShift) & 15, DataRemainedContained: flags&g.FesEmbeddedBridgeDataHeld != 0}, nil
}
func (b *FirmwareBridges) OpenBridges(r BridgeOpenRequest) (BridgeSnapshot, error) {
	if !validBridgeSnapshot(r.Before) {
		return BridgeSnapshot{}, errors.New("native fabric: invalid expected bridge state")
	}
	op := g.FesEmbeddedBridgeControl
	if r.Mask != 0 && r.Mask&^(g.FesEmbeddedBridgeMask&^g.FesEmbeddedBridgeControlMask) == 0 {
		op = g.FesEmbeddedBridgeRelease | r.Mask<<g.FesEmbeddedBridgeReleaseShift
	} else if r.Mask != g.FesEmbeddedBridgeControlMask {
		return BridgeSnapshot{}, errors.New("native fabric: invalid bridge open mask")
	}
	s, _, err := b.call(op, r.Before.Owner, r.Before.Generation, false)
	return s, err
}
