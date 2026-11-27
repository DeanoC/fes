// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"testing"

	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
)

type c64ScopeWire struct {
	args  [7]uint64
	reply [4]uint64
	err   error
	calls int
}

func (w *c64ScopeWire) Invoke(args [7]uint64) ([4]uint64, error) {
	w.calls++
	w.args = args
	return w.reply, w.err
}

func TestC64ScopeObservesExactBindingWithoutMutations(t *testing.T) {
	const owner, bridge, mac = 0xb5a48d3219, 0x100000009, 0x200000006
	ok := [4]uint64{0, uint64(g.FesEmbeddedC64ScopeTag) << 32, bridge, mac}
	w := c64ScopeWire{reply: ok}
	if err := observeC64Scope(&w, owner, bridge, mac); err != nil ||
		w.args != [7]uint64{uint64(g.FesEmbeddedC64ScopeCall), mac, owner, uint64(g.FesEmbeddedC64ScopeCookie), bridge} {
		t.Fatalf("unexpected C64 scope request %+v: %v", w.args, err)
	}
	for _, tc := range []struct {
		name  string
		reply [4]uint64
		err   error
	}{
		{"foreign tag", [4]uint64{0, uint64(g.FesEmbeddedMacTag) << 32, bridge, mac}, nil},
		{"reserved reply bits", [4]uint64{0, ok[1] | 1, bridge, mac}, nil},
		{"bridge ABA", [4]uint64{0, ok[1], bridge + 1, mac}, nil},
		{"MAC ABA", [4]uint64{0, ok[1], bridge, mac + 1}, nil},
		{"unsupported", [4]uint64{uint64(g.FesEmbeddedC64ScopeNotReady), ok[1], 0, 0}, nil},
		{"forged rejected reply", [4]uint64{uint64(g.FesEmbeddedC64ScopeRejected), ok[1], bridge, mac}, nil},
		{"interrupted", ok, errors.New("SMC transport")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := c64ScopeWire{reply: tc.reply, err: tc.err}
			if observeC64Scope(&w, owner, bridge, mac) == nil || w.calls != 1 {
				t.Fatalf("accepted or retried %s", tc.name)
			}
		})
	}
	for _, binding := range [][3]uint64{{0, bridge, mac}, {owner, 0, mac}, {owner, bridge, 1}} {
		w := c64ScopeWire{reply: ok}
		if observeC64Scope(&w, binding[0], binding[1], binding[2]) == nil || w.calls != 0 {
			t.Fatal("incomplete binding reached firmware")
		}
	}
}

// Models the retained two generation spaces separately and denies legacy bridge
// observations under active MAC. No read or write can pass an uncertain scope.
type c64ScopeCPU struct {
	*computerCPU
	next                  [4]uint64
	active                bool
	scopes, reads, writes int
	faultAt               int
}

func (c *c64ScopeCPU) Invoke(r [7]uint64) ([4]uint64, error) {
	if r[0] != uint64(g.FesEmbeddedC64ScopeCall) {
		if c.active {
			return [4]uint64{}, errors.New("legacy bridge during running MAC")
		}
		return c.computerCPU.Invoke(r)
	}
	c.scopes++
	if c.faultAt == c.scopes {
		return [4]uint64{}, errors.New("lost protected read")
	}
	return c.next, nil
}
func (c *c64ScopeCPU) ReadPhysical32(uint64) (uint32, error) { c.reads++; return 0x1234, nil }
func (c *c64ScopeCPU) WritePhysical32(uint64, uint32) error  { c.writes++; return nil }

func TestC64ScopeAdmissionAndRevokedReadback(t *testing.T) {
	const owner, bridge, returned uint64 = 13, 29, 9
	tag := uint64(g.FesEmbeddedC64ScopeTag) << 32
	baseline := BridgeSnapshot{Owner: owner, Generation: bridge, ResetMask: 13, MastersContained: true,
		Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: g.FesEmbeddedBridgeDrainMask, FabricHandshake: FabricHandshakeAbsent}}
	newPort := func() (*computerIO, *c64ScopeCPU) {
		cpu := &c64ScopeCPU{computerCPU: &computerCPU{state: baseline}}
		return &computerIO{cpu: cpu, bridges: &FirmwareBridges{cpu}, want: baseline, base: 0x20010300}, cpu
	}
	prior := maccontrol.Receipt{Owner: owner, Generation: returned}
	live := maccontrol.Receipt{Owner: owner, Generation: returned + 2, Active: true, Confirmed: true}
	for _, reply := range [][4]uint64{{5, tag, 0, 0}, {2, 0, owner, uint64(g.FesEmbeddedC64ScopeCookie)}} {
		c, cpu := newPort()
		cpu.next = reply
		if ok, err := c.prepareC64Grant(prior); err != nil || ok || cpu.reads != 0 || cpu.writes != 0 || c.c64GrantPending || c.retired {
			t.Fatal("unsupported C64 disturbed local play", ok, err)
		}
	}
	for _, reply := range [][4]uint64{{0, tag, bridge + 1, returned}, {0, tag, bridge, returned + 1}, {5, tag, 1, 0}, {2, tag | 1, 0, 0}} {
		c, cpu := newPort()
		cpu.next = reply
		if ok, err := c.prepareC64Grant(prior); err == nil || ok || c.c64GrantPending {
			t.Fatal("invalid eligibility admitted", reply)
		}
	}
	arm := func(t *testing.T) (*computerIO, *c64ScopeCPU) {
		t.Helper()
		c, cpu := newPort()
		cpu.next = [4]uint64{0, tag, bridge, returned}
		if ok, err := c.prepareC64Grant(prior); err != nil || !ok {
			t.Fatal(ok, err)
		}
		if c.beginClose() {
			t.Fatal("started Hold before pending grant finished")
		}
		cpu.active = true
		cpu.next = [4]uint64{0, tag, bridge, live.Generation}
		if err := c.enterC64Scope(live); err != nil {
			t.Fatal(err)
		}
		return c, cpu
	}
	t.Run("refuse privileged operations and restore", func(t *testing.T) {
		c, cpu := arm(t)
		if err := c.Write32(cg.FesDe25ComputerGpoOffset, cg.FesComputerOpcodeExecution<<24); err == nil || cpu.writes != 0 {
			t.Fatal("held-only operation reached live GP")
		}
		for _, offset := range []uint32{cg.FesDe25ComputerStatusOffset, cg.FesDe25ComputerFetchAddressOffset} {
			if v, err := c.Read32(offset); v != 0x1234 || err != nil {
				t.Fatal(v, err)
			}
		}
		if c.beginClose() {
			t.Fatal("permitted Hold under active MAC")
		}
		cpu.active = false
		if err := c.leaveC64Scope(maccontrol.Receipt{Owner: owner, Generation: live.Generation + 1}); err != nil || c.want != baseline || !c.beginClose() {
			t.Fatal("correct stop failed", err)
		}
	})
	t.Run("lost after access", func(t *testing.T) {
		c, cpu := arm(t)
		cpu.faultAt = cpu.scopes + 2
		if v, err := c.Read32(cg.FesDe25ComputerStatusOffset); v != 0 || err == nil || cpu.reads != 1 || !c.retired {
			t.Fatal(v, err, cpu.reads)
		}
		if _, err := c.Read32(cg.FesDe25ComputerStatusOffset); err == nil || cpu.reads != 1 || c.beginClose() {
			t.Fatal("used uncertain grant after loss")
		}
	})
	t.Run("unproved return", func(t *testing.T) {
		c, cpu := arm(t)
		cpu.active = false
		cpu.state.Generation++
		if err := c.leaveC64Scope(maccontrol.Receipt{Owner: owner, Generation: live.Generation + 1}); err == nil || !c.retired || c.beginClose() {
			t.Fatal("accepted reset-and-return bridge ABA")
		}
	})
}
