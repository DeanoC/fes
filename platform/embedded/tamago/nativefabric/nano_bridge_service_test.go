// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

// This adapter is test transport for the C service, never a Nano SiP binding.
type nanoFirmwarePlatform struct{ *rpcHooks }

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
func nanoSnapshot(words []uint64) (BridgeSnapshot, error) {
	if len(words) != 6 || words[2] > 15 || words[3] > 2 || words[4] > 1 || words[5] > 1 {
		return BridgeSnapshot{}, errors.New("invalid C bridge snapshot")
	}
	return BridgeSnapshot{Capabilities: BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: FabricHandshake(words[3])},
		Owner: words[0], Generation: words[1], ResetMask: uint32(words[2]), NeverEnabled: words[4] != 0, MastersContained: words[5] != 0}, nil
}
func (n *nanoFirmwarePlatform) ObserveBridges() (BridgeSnapshot, error) {
	words, err := n.call("bridge-observe")
	if err != nil {
		return BridgeSnapshot{}, err
	}
	return nanoSnapshot(words)
}
func (n *nanoFirmwarePlatform) CloseBridges(r BridgeCloseRequest) (BridgeClosure, error) {
	words, err := n.call("bridge-close %x %x %x %x", r.Before.Owner, r.Before.Generation, boolWord(r.RequireFabricHandshake), boolWord(r.PreserveDataContainment))
	if err != nil {
		return BridgeClosure{}, err
	}
	if len(words) != 10 {
		return BridgeClosure{}, errors.New("invalid C bridge closure")
	}
	s, err := nanoSnapshot(words[:6])
	return BridgeClosure{After: s, ProducersQuiescent: words[6] == 1, FabricAcknowledged: words[7] == 1, HardwareDrainedMask: uint32(words[8]), DataRemainedContained: words[9] == 1}, err
}
func (n *nanoFirmwarePlatform) OpenBridges(r BridgeOpenRequest) (BridgeSnapshot, error) {
	words, err := n.call("bridge-open %x %x %x", r.Before.Owner, r.Before.Generation, r.Mask)
	if err != nil {
		return BridgeSnapshot{}, err
	}
	return nanoSnapshot(words)
}
func boolWord(b bool) uint64 {
	if b {
		return 1
	}
	return 0
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
			platform := &nanoFirmwarePlatform{r}
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
