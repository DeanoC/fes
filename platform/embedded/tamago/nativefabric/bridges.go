// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"math"
)

// FabricHandshake describes the optional HPS-to-fabric quiescence handshake,
// not the separate endpoint generation/cookie drain or hardware fence/drain.
// The zero value is deliberately unspecified and cannot authorize a close.
type FabricHandshake uint8

const (
	FabricHandshakeAbsent FabricHandshake = iota + 1
	FabricHandshakePresent
)

// BridgeCapabilities must be qualified by the platform against its running
// HPS/FPGA and firmware build. They are not inferred from package metadata or
// registered as a physical programming profile. Masks use platform-defined
// reset bits; HardwareDrainMask names the data paths that must fence/drain.
type BridgeCapabilities struct {
	Mask, ControlMask, HardwareDrainMask uint32
	FabricHandshake                      FabricHandshake
}

// BridgeSnapshot is a trusted, stable platform observation. Owner is a
// nonzero boot-scoped exclusive ownership token. Generation must advance for
// every bridge/ownership change, including an external reset-and-restore.
// NeverEnabled requires boot-chain evidence that no bridge/master could have
// issued a transaction; reset readback alone cannot establish it.
// MastersContained covers every relevant master, not just the FPGA reader.
type BridgeSnapshot struct {
	Capabilities      BridgeCapabilities
	Owner, Generation uint64
	ResetMask         uint32
	NeverEnabled      bool
	MastersContained  bool
}

type BridgeCloseRequest struct {
	Before                 BridgeSnapshot
	RequireFabricHandshake bool
	// PreserveDataContainment is issued only from this owner's verified
	// control-only state. No data bridge has reopened since its closure.
	PreserveDataContainment bool
}

type BridgeClosure struct {
	After                 BridgeSnapshot
	ProducersQuiescent    bool
	FabricAcknowledged    bool
	HardwareDrainedMask   uint32
	DataRemainedContained bool
}

type BridgeOpenRequest struct {
	Before BridgeSnapshot
	Mask   uint32
}

// BridgeServices extends the existing CPU platform services. Calls are
// serialized and bounded-return. Mutations must compare Before atomically,
// stop all producers before drain, and publish one new generation on success.
// They must never echo a request or recycle an old ACK as drain evidence.
// Observe must account for changes made by firmware/configuration too. A
// platform that cannot establish exclusive ownership must return an error.
type BridgeServices interface {
	ObserveBridges() (BridgeSnapshot, error)
	CloseBridges(BridgeCloseRequest) (BridgeClosure, error)
	OpenBridges(BridgeOpenRequest) (BridgeSnapshot, error)
}

type bridgeAccess uint8

const (
	bridgesUnknown bridgeAccess = iota
	bridgesClosed
	bridgesControlOnly
	bridgesActive
)

func validBridgeSnapshot(s BridgeSnapshot) bool {
	c := s.Capabilities
	return c.Mask != 0 && c.ControlMask != 0 && c.ControlMask != c.Mask && c.ControlMask&^c.Mask == 0 &&
		c.HardwareDrainMask != 0 && c.HardwareDrainMask&^(c.Mask&^c.ControlMask) == 0 &&
		(c.FabricHandshake == FabricHandshakeAbsent || c.FabricHandshake == FabricHandshakePresent) &&
		s.Owner != 0 && s.Generation != 0 && s.ResetMask&^c.Mask == 0 &&
		(!s.NeverEnabled || (s.ResetMask == c.Mask && s.MastersContained))
}

func (h *CPUHooks) bridgeFailure(reason string) error {
	h.failed = true // uncertain state is retained; no retry through these hooks
	return errors.New("native fabric: " + reason)
}

func (h *CPUHooks) observeBridges() (BridgeSnapshot, error) {
	if !h.ready() {
		return BridgeSnapshot{}, errCPUHooksUnavailable
	}
	s, err := h.services.ObserveBridges()
	if err != nil {
		h.failed = true
		return BridgeSnapshot{}, err
	}
	if !validBridgeSnapshot(s) || (h.bridge != nil && s != *h.bridge) {
		return BridgeSnapshot{}, h.bridgeFailure("bridge capability, state or ownership changed")
	}
	return s, nil
}

func (h *CPUHooks) bridgeReadback(want BridgeSnapshot, access bridgeAccess) (bool, error) {
	s, err := h.services.ObserveBridges()
	if err != nil {
		h.failed = true
		return false, err
	}
	if !validBridgeSnapshot(s) || s != want {
		return false, h.bridgeFailure("bridge mutation readback differs from owned result")
	}
	h.bridge, h.access = &s, access
	return true, nil
}

// Contain is idempotent only for a proven never-enabled boot state or this
// owner's unchanged completed closure. Unknown/partial reset snapshots do not
// stand in for drain. No hardware shutdown request is sent on the closed path.
func (h *CPUHooks) Contain() (bool, error) {
	s, err := h.observeBridges()
	if err != nil {
		return false, err
	}
	c := s.Capabilities
	if s.ResetMask == c.Mask {
		if !s.MastersContained || (!s.NeverEnabled && h.access != bridgesClosed) {
			return false, h.bridgeFailure("already-reset bridges lack owned closure evidence")
		}
		h.bridge, h.access = &s, bridgesClosed
		return true, nil
	}
	if (h.bridge == nil && s.ResetMask != 0) || s.Generation == math.MaxUint64 {
		return false, h.bridgeFailure("partial or exhausted bridge ownership")
	}
	held := h.access == bridgesControlOnly
	request := BridgeCloseRequest{Before: s, RequireFabricHandshake: !held && c.FabricHandshake == FabricHandshakePresent, PreserveDataContainment: held}
	h.access = bridgesUnknown // invalidate prior proof before any mutation
	closed, err := h.services.CloseBridges(request)
	if err != nil {
		h.failed = true
		return false, err
	}
	want := s
	want.Generation++
	want.ResetMask, want.NeverEnabled, want.MastersContained = c.Mask, false, true
	drain := c.HardwareDrainMask
	if held {
		drain = 0 // data stayed reset under this owner's preceding closure
	}
	if closed.After != want || !closed.ProducersQuiescent || closed.HardwareDrainedMask != drain ||
		closed.FabricAcknowledged != request.RequireFabricHandshake || closed.DataRemainedContained != held {
		return false, h.bridgeFailure("bridge closure lacks matching quiescence, handshake or hardware drain")
	}
	return h.bridgeReadback(want, bridgesClosed)
}

func (h *CPUHooks) openBridges(control bool) (bool, error) {
	s, err := h.observeBridges()
	if err != nil {
		return false, err
	}
	c := s.Capabilities
	phase, reset, mask, next := bridgesControlOnly, c.Mask&^c.ControlMask, c.Mask&^c.ControlMask, bridgesActive
	if control {
		phase, reset, mask, next = bridgesClosed, c.Mask, c.ControlMask, bridgesControlOnly
	}
	if h.access != phase || s.ResetMask != reset || !s.MastersContained || s.Generation == math.MaxUint64 {
		return false, h.bridgeFailure("bridge release lacks owned containment or control-only access")
	}
	h.access = bridgesUnknown
	after, err := h.services.OpenBridges(BridgeOpenRequest{Before: s, Mask: mask})
	if err != nil {
		h.failed = true
		return false, err
	}
	want := s
	want.Generation++
	want.ResetMask &^= mask
	want.NeverEnabled, want.MastersContained = false, control
	if after != want {
		return false, h.bridgeFailure("bridge release changed an unrequested path or ownership")
	}
	return h.bridgeReadback(want, next)
}

func (h *CPUHooks) ControlOnly() (bool, error) { return h.openBridges(true) }
func (h *CPUHooks) ReleaseData() (bool, error) { return h.openBridges(false) }
