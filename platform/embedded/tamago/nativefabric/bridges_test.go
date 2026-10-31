// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

// Only injected replies are modeled here, never real FIFOs or electrical ACKs.
type bridgeFixture struct {
	state                   BridgeSnapshot
	closes, opens, observes int
	requests                []BridgeCloseRequest
	close                   func() error
	open                    func(uint32) error
	fault                   string
}

func bridgeState(handshake FabricHandshake) BridgeSnapshot {
	return BridgeSnapshot{Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: handshake},
		Owner: 0x1234, Generation: 1}
}

func (b *bridgeFixture) ObserveBridges() (BridgeSnapshot, error) {
	b.observes++
	if b.fault == "observe" || (b.fault == "readback" && b.closes != 0) {
		return BridgeSnapshot{}, errors.New("bridge observation failed")
	}
	return b.state, nil
}
func (b *bridgeFixture) CloseBridges(r BridgeCloseRequest) (BridgeClosure, error) {
	b.closes++
	b.requests = append(b.requests, r)
	if r.Before != b.state {
		return BridgeClosure{}, errors.New("stale bridge mutation")
	}
	if b.close != nil {
		if err := b.close(); err != nil {
			return BridgeClosure{}, err
		}
	}
	b.state.Generation++
	b.state.ResetMask, b.state.NeverEnabled, b.state.MastersContained = 15, false, true
	rc := BridgeClosure{After: b.state, ProducersQuiescent: true, FabricAcknowledged: r.RequireFabricHandshake, HardwareDrainedMask: 12}
	if r.PreserveDataContainment {
		rc.DataRemainedContained, rc.HardwareDrainedMask = true, 0
	}
	switch b.fault {
	case "close":
		return rc, errors.New("close outcome unknown")
	case "producer":
		rc.ProducersQuiescent = false
	case "fabric":
		rc.FabricAcknowledged = !rc.FabricAcknowledged
	case "hardware":
		rc.HardwareDrainedMask = 4
	case "held-data":
		rc.DataRemainedContained = !rc.DataRemainedContained
	case "reset":
		rc.After.ResetMask = 7
	case "owner":
		rc.After.Owner++
	case "generation":
		rc.After.Generation--
	case "capability":
		rc.After.Capabilities.ControlMask = 1
	case "masters":
		rc.After.MastersContained = false
	case "readback-reset":
		b.state.ResetMask = 7
	}
	return rc, nil
}
func (b *bridgeFixture) OpenBridges(r BridgeOpenRequest) (BridgeSnapshot, error) {
	b.opens++
	if r.Before != b.state {
		return BridgeSnapshot{}, errors.New("stale bridge mutation")
	}
	if b.open != nil {
		if err := b.open(r.Mask); err != nil {
			return BridgeSnapshot{}, err
		}
	}
	b.state.Generation++
	b.state.ResetMask &^= r.Mask
	b.state.NeverEnabled, b.state.MastersContained = false, r.Mask == 2
	switch b.fault {
	case "open":
		return b.state, errors.New("open outcome unknown")
	case "open-reset":
		b.state.ResetMask = 0
	case "open-owner":
		b.state.Owner++
	}
	return b.state, nil
}

type bridgeTestServices struct {
	*fake
	*bridgeFixture
}

func testCPUBridges(t *testing.T, s BridgeSnapshot) (*CPUHooks, *bridgeTestServices) {
	t.Helper()
	f := &fake{control: true, expected: map[byte]embeddedcontrol.Identity{1: fpgaID(1), 2: fpgaID(2)}}
	services := &bridgeTestServices{fake: f, bridgeFixture: &bridgeFixture{state: s}}
	services.close = func() error { _, err := f.Contain(); return err }
	services.open = func(mask uint32) error {
		var ok bool
		var err error
		if mask == 2 {
			ok, err = f.ControlOnly()
		} else {
			ok, err = f.ReleaseData()
		}
		if !ok && err == nil {
			err = errors.New("platform release unproven")
		}
		return err
	}
	if s.ResetMask == 15 {
		f.contained, f.control = true, false
	}
	cpu := &cpuTestTransport{base: 0x40000000, device: f}
	h, err := NewCPUHooks(cpu, services, cpu.base, 256)
	if err != nil {
		t.Fatal(err)
	}
	return h, services
}

func coldBridgeState(handshake FabricHandshake) BridgeSnapshot {
	s := bridgeState(handshake)
	s.ResetMask, s.NeverEnabled, s.MastersContained = 15, true, true
	return s
}

func TestCPUBridgeColdAndOwnedClosedAreIdempotent(t *testing.T) {
	for _, handshake := range []FabricHandshake{FabricHandshakeAbsent, FabricHandshakePresent} {
		h, s := testCPUBridges(t, coldBridgeState(handshake))
		for range 2 {
			if ok, err := h.Contain(); !ok || err != nil {
				t.Fatal(err)
			}
		}
		if s.closes != 0 || s.opens != 0 {
			t.Fatal("already-closed boot issued a hardware handshake")
		}
		if ok, err := h.ControlOnly(); !ok || err != nil || s.state.ResetMask != 13 {
			t.Fatalf("control-only: %v %+v", err, s.state)
		}
		// The endpoint, not bridge reset bits, supplies reader-enable evidence.
		s.status = g.FesEmbeddedStatusEnabled
		if ok, err := h.ReleaseData(); !ok || err != nil || s.state.ResetMask != 0 {
			t.Fatalf("data release: %v %+v", err, s.state)
		}
		if ok, err := h.Contain(); !ok || err != nil {
			t.Fatal(err)
		}
		if s.closes != 1 || s.requests[0].RequireFabricHandshake != (handshake == FabricHandshakePresent) {
			t.Fatal("active shutdown did not use explicit fabric capability")
		}
		if ok, err := h.Contain(); !ok || err != nil || s.closes != 1 {
			t.Fatal("same-owner closed bridge was reset again", err)
		}
	}
}

func TestCPUBridgeActiveClosureRequiresIndependentEvidence(t *testing.T) {
	for _, handshake := range []FabricHandshake{FabricHandshakeAbsent, FabricHandshakePresent} {
		for _, fault := range []string{"", "close", "producer", "fabric", "hardware", "held-data", "reset", "owner", "generation", "capability", "masters", "readback", "readback-reset"} {
			t.Run(fmt.Sprintf("handshake-%d/%s", handshake, fault), func(t *testing.T) {
				h, s := testCPUBridges(t, bridgeState(handshake))
				s.bridgeFixture.fault = fault
				ok, err := h.Contain()
				if fault == "" {
					if !ok || err != nil || s.closes != 1 {
						t.Fatal(err)
					}
					return
				}
				if ok || err == nil || s.closes != 1 {
					t.Fatalf("accepted %s: %v %v", fault, ok, err)
				}
				assertBridgeRetired(t, h, s)
			})
		}
	}
}

func TestCPUBridgeControlOnlyClosureDoesNotInventDataAcknowledgements(t *testing.T) {
	for _, fault := range []string{"", "held-data", "fabric", "hardware"} {
		t.Run(fault, func(t *testing.T) {
			h, s := testCPUBridges(t, coldBridgeState(FabricHandshakePresent))
			if _, err := h.Contain(); err != nil {
				t.Fatal(err)
			}
			if _, err := h.ControlOnly(); err != nil {
				t.Fatal(err)
			}
			s.bridgeFixture.fault = fault
			ok, err := h.Contain()
			if fault == "" {
				if !ok || err != nil || !s.requests[0].PreserveDataContainment || s.requests[0].RequireFabricHandshake {
					t.Fatal("held data was reported as a new ACK", err)
				}
			} else {
				if ok || err == nil {
					t.Fatal("invented/missing held-data evidence accepted")
				}
				assertBridgeRetired(t, h, s)
			}
		})
	}
}

func assertBridgeRetired(t *testing.T, h *CPUHooks, s *bridgeTestServices) {
	t.Helper()
	beforeClose, beforeOpen, beforeObserve := s.closes, s.opens, s.observes
	for _, call := range []func() (bool, error){h.Contain, h.ControlOnly, h.ReleaseData} {
		if ok, err := call(); ok || err == nil {
			t.Fatal("uncertain ownership retried")
		}
	}
	if _, err := h.Invoke([7]uint64{start}); err == nil {
		t.Fatal("uncertain ownership configured FPGA")
	}
	if s.closes != beforeClose || s.opens != beforeOpen || s.observes != beforeObserve || s.programs != 0 {
		t.Fatal("retired hooks contacted platform")
	}
}

func TestCPUBridgeRejectsUnprovedInitialStates(t *testing.T) {
	for _, name := range []string{"unknown-closed", "partial", "partial-seven", "cold-master", "unspecified", "owner-zero", "generation-zero", "mask", "control-mask", "drain-mask", "no-drain-mask", "overflow", "observe"} {
		t.Run(name, func(t *testing.T) {
			s := bridgeState(FabricHandshakeAbsent)
			switch name {
			case "unknown-closed":
				s.ResetMask, s.MastersContained = 15, true
			case "partial":
				s.ResetMask = 4
			case "partial-seven":
				s.ResetMask = 7
			case "cold-master":
				s = coldBridgeState(FabricHandshakeAbsent)
				s.MastersContained = false
			case "unspecified":
				s.Capabilities.FabricHandshake = 0
			case "owner-zero":
				s.Owner = 0
			case "generation-zero":
				s.Generation = 0
			case "mask":
				s.Capabilities.Mask = 0
			case "control-mask":
				s.Capabilities.ControlMask = 16
			case "drain-mask":
				s.Capabilities.HardwareDrainMask = 2
			case "no-drain-mask":
				s.Capabilities.HardwareDrainMask = 0
			case "overflow":
				s.Generation = math.MaxUint64
			}
			h, f := testCPUBridges(t, s)
			if name == "observe" {
				f.bridgeFixture.fault = name
			}
			if ok, err := h.Contain(); ok || err == nil || f.closes != 0 || f.opens != 0 {
				t.Fatal("unproved state accepted or mutated", err)
			}
			assertBridgeRetired(t, h, f)
		})
	}
}

func TestCPUBridgeClosedReceiptCannotSurviveOwnershipOrResetChange(t *testing.T) {
	for _, name := range []string{"owner", "generation", "reset", "capability", "masters"} {
		t.Run(name, func(t *testing.T) {
			h, s := testCPUBridges(t, coldBridgeState(FabricHandshakeAbsent))
			if _, err := h.Contain(); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "owner":
				s.state.Owner++
			case "generation":
				s.state.Generation += 2 // reset then restore: identical bits, new epoch
			case "reset":
				s.state.ResetMask = 7
			case "capability":
				s.state.Capabilities.FabricHandshake = FabricHandshakePresent
			case "masters":
				s.state.MastersContained = false
			}
			if ok, err := h.Contain(); ok || err == nil || s.closes != 0 {
				t.Fatal("stale closure reused", err)
			}
			assertBridgeRetired(t, h, s)
		})
	}
}

func TestCPUBridgeReleaseOrderAndReadbackFailures(t *testing.T) {
	for _, name := range []string{"no-contain", "data-before-control", "open", "open-reset", "open-owner", "configuration-active"} {
		t.Run(name, func(t *testing.T) {
			h, s := testCPUBridges(t, coldBridgeState(FabricHandshakeAbsent))
			if name != "no-contain" {
				if _, err := h.Contain(); err != nil {
					t.Fatal(err)
				}
			}
			s.bridgeFixture.fault = name
			var ok bool
			var err error
			switch name {
			case "data-before-control":
				ok, err = h.ReleaseData()
			case "configuration-active":
				if _, err = h.ControlOnly(); err != nil {
					t.Fatal(err)
				}
				_, err = h.Invoke([7]uint64{start})
			default:
				ok, err = h.ControlOnly()
			}
			if ok || err == nil {
				t.Fatal("unsafe release/configuration accepted")
			}
			assertBridgeRetired(t, h, s)
		})
	}
}

func TestCPUBridgeContractLaunchReturnAndFailures(t *testing.T) {
	for _, name := range []string{"cold", "active", "absent-fabric", "hardware-drain", "fabric-ack", "control-open", "data-open", "identity-recovery", "lost-completion"} {
		t.Run(name, func(t *testing.T) {
			s := coldBridgeState(FabricHandshakePresent)
			if name == "active" {
				s = bridgeState(FabricHandshakePresent)
			}
			if name == "absent-fabric" {
				s.Capabilities.FabricHandshake = FabricHandshakeAbsent
			}
			hooks, services := testCPUBridges(t, s)
			e, err := New(hooks, Options{DRAMBase: 0x80000000, ChunkBytes: 32})
			if err != nil {
				t.Fatal(err)
			}
			c, err := fabricmodel.New(image(1, "model.idle", true), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Duration(0)
			if err = c.Boot(now, time.Second); err != nil {
				t.Fatal(err)
			}
			if err = drive(c, e, &now, 500); err != nil {
				t.Fatal(err)
			}
			beforePrograms, beforeReleases := services.programs, services.dataReleases
			switch name {
			case "hardware-drain":
				services.bridgeFixture.fault = "hardware"
			case "fabric-ack":
				services.bridgeFixture.fault = "fabric"
			case "control-open":
				services.fake.fault = "control"
			case "data-open":
				services.fake.fault = "release"
			case "identity-recovery":
				services.fake.fault = "identity"
			case "lost-completion":
				services.fake.fault = "lost"
			}
			if err = c.Load(image(2, "model.game", false), now, now+time.Second); err != nil {
				t.Fatal(err)
			}
			if name == "absent-fabric" {
				services.pending = 1
				for range 8 {
					now += time.Millisecond
					if err = e.Step(c, now); err != nil {
						t.Fatal(err)
					}
				}
				if services.closes != 0 || services.programs != beforePrograms || services.disableCommands != 1 {
					t.Fatal("missing optional handshake bypassed the endpoint generation/cookie drain")
				}
				services.drain()
			}
			err = drive(c, e, &now, 1100)
			if name == "cold" || name == "active" || name == "absent-fabric" {
				if err != nil || c.Status().State != fabricmodel.Running {
					t.Fatal(err, c.Status())
				}
				if err = c.Stop(now, now+time.Second); err != nil {
					t.Fatal(err)
				}
				if err = drive(c, e, &now, 500); err != nil || c.Status().State != fabricmodel.Idle {
					t.Fatal(err, c.Status())
				}
				closes := 2
				if name == "active" {
					closes++
				}
				if services.disableCommands != 2 || services.programs != 3 || services.dataReleases != 3 || services.closes != closes {
					t.Fatalf("launch/return sequence: %+v %+v", services.fake, services.bridgeFixture)
				}
			} else if name == "identity-recovery" {
				if err == nil || c.Status().State != fabricmodel.Idle || services.programs != 3 || services.dataReleases != 2 {
					t.Fatal(err, c.Status())
				}
			} else {
				if err == nil || c.Status().State != fabricmodel.RebootRequired || services.dataReleases != beforeReleases {
					t.Fatal(err, c.Status())
				}
				if (name == "hardware-drain" || name == "fabric-ack") && services.programs != beforePrograms {
					t.Fatal("programmed after failed closure")
				}
				if name == "lost-completion" && (services.region == nil || services.released != 1) {
					t.Fatal("lost completion released DMA memory")
				}
				programs, opens := services.programs, services.opens
				if e.Step(c, now) == nil || services.programs != programs || services.opens != opens {
					t.Fatal("failed lifecycle replayed")
				}
			}
		})
	}
}
