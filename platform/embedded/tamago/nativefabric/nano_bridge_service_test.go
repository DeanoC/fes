// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"os"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

// This adapter is test transport for the C service, never a Nano SiP binding.
type nanoFirmwarePlatform struct {
	*rpcHooks
	wire *FirmwareBridges
}

func (n *nanoFirmwarePlatform) ReadPhysical32(address uint64) (uint32, error) {
	if address < 0x40000000 || address >= 0x40000100 {
		return 0, errors.New("outside simulated CPU aperture")
	}
	return n.Read32(uint32(address - 0x40000000))
}
func (n *nanoFirmwarePlatform) WritePhysical32(address uint64, value uint32) error {
	if address < 0x40000000 || address >= 0x40000100 {
		return errors.New("outside simulated CPU aperture")
	}
	return n.Write32(uint32(address-0x40000000), value)
}
func (n *nanoFirmwarePlatform) ObserveBridges() (BridgeSnapshot, error) {
	return n.wire.ObserveBridges()
}
func (n *nanoFirmwarePlatform) CloseBridges(r BridgeCloseRequest) (BridgeClosure, error) {
	return n.wire.CloseBridges(r)
}
func (n *nanoFirmwarePlatform) OpenBridges(r BridgeOpenRequest) (BridgeSnapshot, error) {
	return n.wire.OpenBridges(r)
}

func TestNanoFirmwareBridgeSiPGates(t *testing.T) {
	path := os.Getenv("FES_NANO_BRIDGE_RPC")
	if path == "" {
		t.Skip("run nativefabric/verify.py --nano-bridge-service")
	}
	t.Setenv("FES_EMBEDDED_RPC", path)
	r := newRPC(t)
	if _, err := NewFirmwareBridges(r); !errors.Is(err, ErrBridgeNotReady) {
		t.Fatal("missing boot proof accepted", err)
	}
	for _, request := range [][7]uint64{
		{0xc2000032, 3, 15}, // legacy mutable bridge API
		{0xc2000401},        // versioned range
		{0xc20000c8},
		{0x42000300}, // wrong calling convention for the private query
		{uint64(g.FesEmbeddedBridgeCall), 4, 0, uint64(g.FesEmbeddedBridgeCookie)}, // no init action
	} {
		reply, err := r.Invoke(request)
		if err != nil || reply[0] != 2 {
			t.Fatal("unreviewed route accepted", request, reply, err)
		}
	}
	routes, err := r.call("owned-route-stats")
	if err != nil || routes[0] != 0 {
		t.Fatal("owned call escaped into a vendor handler", routes, err)
	}
	stats, _ := r.call("stats")
	if stats[0] != 0 || stats[1] != 0 {
		t.Fatal("unready/legacy request mutated state", stats)
	}
	if _, err = r.call("bridge-boot f 1 2"); err != nil {
		t.Fatal(err)
	} // simulated trusted boot code
	b, err := NewFirmwareBridges(r)
	if err != nil {
		t.Fatal(err)
	}
	s, err := b.ObserveBridges()
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := r.Invoke([7]uint64{start}); err != nil || reply[0] != 0 {
		t.Fatal(reply, err)
	}
	if _, err := b.OpenBridges(BridgeOpenRequest{Before: s, Mask: 2}); err == nil {
		t.Fatal("opened a bridge during configuration")
	}
	if reply, _ := r.Invoke([7]uint64{start}); reply[0] != 1 {
		t.Fatal("replayed START", reply)
	}
	if reply, err := r.Invoke([7]uint64{write, 0x96000000, 128}); err != nil || reply[0] != 0 {
		t.Fatal(reply, err)
	}
	if reply, _ := r.Invoke([7]uint64{isdone}); reply[0] != 1 {
		t.Fatal("ISDONE ignored pending DMA", reply)
	}
	// The first completion poll submits the queued vendor buffer; the next
	// consumes its acknowledgement. Neither step can be replaced by ISDONE.
	if reply, err := r.Invoke([7]uint64{completed}); err != nil || reply != [4]uint64{1, 0, 0, 0} {
		t.Fatal("queued buffer was reported complete", reply, err)
	}
	if reply, _ := r.Invoke([7]uint64{isdone}); reply[0] != 1 {
		t.Fatal("ISDONE ignored submitted DMA", reply)
	}
	if reply, err := r.Invoke([7]uint64{completed}); err != nil || reply[0] != 0 || reply[1] != 0x16000000 {
		t.Fatal(reply, err)
	}
	if reply, err := r.Invoke([7]uint64{isdone}); err != nil || reply[0] != 0 {
		t.Fatal(reply, err)
	}
	if _, err := b.OpenBridges(BridgeOpenRequest{Before: s, Mask: 2}); err != nil {
		t.Fatal("completed configuration never released control", err)
	}
}

func TestNanoFirmwareBridgeCyclesAndFailures(t *testing.T) {
	for _, name := range []string{"cold", "active", "fabric-absent", "missing-hardware", "missing-fabric", "stop-failure", "ownership-ABA", "reset-readback", "control-open", "data-open", "ownership-during-stop", "identity-recovery", "lost-transfer"} {
		t.Run(name, func(t *testing.T) {
			path := os.Getenv("FES_NANO_BRIDGE_RPC")
			if path == "" {
				t.Skip("run nativefabric/verify.py --nano-bridge-service")
			}
			t.Setenv("FES_EMBEDDED_RPC", path)
			r := newRPC(t)
			reset, proof, fabric := 15, 1, 2
			if name == "active" {
				reset, proof = 0, 0
			}
			if name == "fabric-absent" {
				fabric = 1
			}
			if _, err := r.call("bridge-boot %x %x %x", reset, proof, fabric); err != nil {
				t.Fatal(err)
			}
			wire, err := NewFirmwareBridges(r)
			if err != nil {
				t.Fatal(err)
			}
			platform := &nanoFirmwarePlatform{rpcHooks: r, wire: wire}
			hooks, err := NewCPUHooks(platform, platform, 0x40000000, 256)
			if err != nil {
				t.Fatal(err)
			}
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
				t.Fatal(err, c.Status())
			}
			stats, err := r.call("bridge-stats")
			if err != nil {
				t.Fatal(err)
			}
			wantClose := uint64(0)
			if name == "active" {
				wantClose = 1
			}
			if stats[0] != wantClose {
				t.Fatal("START redundantly reset an owned closed bridge", stats)
			}
			fault := map[string]int{"missing-hardware": 1, "missing-fabric": 2, "stop-failure": 3, "ownership-ABA": 4, "reset-readback": 5, "control-open": 8, "data-open": 9, "ownership-during-stop": 7}[name]
			if fault != 0 {
				if _, err = r.call("bridge-fault %x", fault); err != nil {
					t.Fatal(err)
				}
			}
			if name == "identity-recovery" {
				r.wrongIdentity = true
			}
			if name == "lost-transfer" {
				if _, err = r.call("lose"); err != nil {
					t.Fatal(err)
				}
			}
			if err = c.Load(image(2, "model.game", false), now, now+time.Second); err != nil {
				t.Fatal(err)
			}
			err = drive(c, e, &now, 1100)
			if fault == 0 && name != "identity-recovery" && name != "lost-transfer" {
				if err != nil || c.Status().State != fabricmodel.Running {
					t.Fatal(err, c.Status())
				}
				if err = c.Stop(now, now+time.Second); err != nil {
					t.Fatal(err)
				}
				if err = drive(c, e, &now, 500); err != nil || c.Status().State != fabricmodel.Idle {
					t.Fatal(err, c.Status())
				}
				stats, err = r.call("stats")
				if err != nil || stats[0] != 3 || stats[1] != 3 || r.releases != 3 {
					t.Fatal(stats, err)
				}
			} else if name == "identity-recovery" {
				stats, _ = r.call("stats")
				if err == nil || c.Status().State != fabricmodel.Idle || stats[0] != 3 || stats[1] != 2 {
					t.Fatal(err, c.Status(), stats)
				}
			} else {
				if err == nil || c.Status().State != fabricmodel.RebootRequired {
					t.Fatal(err, c.Status())
				}
				stats, _ = r.call("stats")
				programs := uint64(1)
				if name == "lost-transfer" || name == "control-open" || name == "data-open" {
					programs = 2
				}
				if stats[0] != programs || stats[1] != 1 {
					t.Fatal("program/release passed a failed bridge", stats)
				}
				if name == "lost-transfer" && (r.region == nil || r.releases != 1) {
					t.Fatal("lost transfer released its reservation")
				}
				if e.Step(c, now) == nil {
					t.Fatal("retired endpoint replayed")
				}
				after, _ := r.call("stats")
				if after[0] != stats[0] || after[1] != stats[1] {
					t.Fatal("failed command replayed")
				}
			}
		})
	}
}
