// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// The actual transfer/arena/computer layers run here against modeled secure
// calls and CSRs. The strong-provider fixture separately runs the C dispatcher.
type reloadCPU struct {
	*computerCPU
	wire               *transferWire
	prepared, finished bool
	generation         uint64
	configurations     int
}

func (c *reloadCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) {
		if a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration) {
			warm := a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration)
			if c.prepared || c.state.ResetMask != 15 || !c.state.MastersContained ||
				a != ([7]uint64{uint64(g.FesEmbeddedBridgeCall), a[1], c.state.Owner, uint64(g.FesEmbeddedBridgeCookie), c.state.Generation}) ||
				(warm && (!c.finished || c.state.NeverEnabled || c.state.Generation <= c.generation)) ||
				(!warm && (!c.state.NeverEnabled || c.configurations != 0)) {
				return [4]uint64{2}, nil
			}
			c.prepared, c.finished, c.generation = true, false, c.state.Generation
			tag := uint64(g.FesEmbeddedBridgePreparationTag)
			if warm {
				tag = uint64(g.FesEmbeddedBridgeRearmTag)
			}
			return [4]uint64{0, tag<<32 | uint64(len(c.wire.source)), c.state.Owner, c.state.Generation}, nil
		}
		if a[1] == uint64(g.FesEmbeddedBridgeControl) && (!c.finished || c.generation != c.state.Generation) {
			return [4]uint64{2}, nil
		}
		return c.computerCPU.Invoke(a)
	}
	if !c.prepared || c.state.ResetMask != 15 || c.generation != c.state.Generation {
		return [4]uint64{}, errors.New("configuration outside prepared closure")
	}
	r, err := c.wire.Invoke(a)
	if a[0] == isdone && err == nil && r == ([4]uint64{}) {
		c.prepared, c.finished = false, true
		c.configurations++
	}
	return r, err
}

func TestNativeReloadJoinsPinnedArenaTransferComputerAndReturn(t *testing.T) {
	for _, fault := range []string{"pass", "write", "release", "open", "close", "identity"} {
		t.Run(fault, func(t *testing.T) {
			_, port, control, computerMap := computerFixture(t, "")
			raw, mapping, prototype := configurationSource(t)
			data := bytes.Clone(prototype.Bytes)
			var descriptor coldConfigurationMap
			if err := json.Unmarshal(mapping, &descriptor); err != nil {
				t.Fatal(err)
			}
			descriptor.ChunkBytes = 64
			sum := sha512.Sum384(data[:64])
			descriptor.ChunkSHA384 = []string{hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:])}
			mapping = canonicalConfiguration(t, descriptor)
			port.digest = sha256.Sum256(raw)
			cpu := &reloadCPU{computerCPU: port}
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			barriers := 0
			arena, err := NewConfigurationArena(0x94800000, make([]byte, 256), func() error {
				barriers++
				if fault == "release" && barriers == 4 {
					return errors.New("uncertain completion ordering")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var previous *Region
			for cycle := 0; cycle < 3; cycle++ {
				region, err := arena.Reserve(data)
				if err != nil {
					t.Fatal(err)
				}
				if previous != nil {
					if previous.Release() == nil {
						t.Fatal("stale completion released next transfer")
					}
					if _, err := arena.Reserve(data); err == nil {
						t.Fatal("stale completion permitted overwrite")
					}
				}
				wire := &transferWire{t: t, source: bytes.Clone(data), region: region}
				if fault == "write" && cycle == 1 {
					wire.fault = "write"
				}
				cpu.wire = wire
				source, err := v.Verify(v.Identity())
				if err != nil {
					t.Fatal(err)
				}
				var transfer *ConfigurationTransfer
				if cycle == 0 {
					transfer, err = v.NewColdConfigurationTransfer(control, mapping, region, source)
				} else {
					transfer, err = v.NewReconfigurationTransfer(control, mapping, region, source)
				}
				if err != nil {
					t.Fatal(err)
				}
				for steps := 0; steps < 30 && !transfer.Status().Done && !transfer.Status().Failed; steps++ {
					_, _, err = transfer.Advance()
				}
				if cycle == 1 && (fault == "write" || fault == "release") {
					if err == nil || !transfer.Status().Failed || transfer.Status().RetainedBytes != len(data) {
						t.Fatal("failed load not retained", transfer.Status(), err)
					}
					calls, opens := port.calls, port.opens
					if _, err := v.OpenComputer(context.Background(), control, computerMap, transfer, computerLimits()); err == nil || port.calls != calls || port.opens != opens {
						t.Fatal("failed transfer granted opening")
					}
					if _, err := arena.Reserve(data); err == nil {
						t.Fatal("failed transfer memory overwritten")
					}
					return
				}
				if err != nil || !transfer.Status().Done {
					t.Fatal(transfer.Status(), err)
				}
				if cycle == 1 && fault == "open" {
					port.loseOpen = true
				}
				if cycle == 1 && fault == "identity" {
					port.words[15] ^= 1
				}
				session, err := v.OpenComputer(context.Background(), control, computerMap, transfer, computerLimits())
				if cycle == 1 && (fault == "open" || fault == "identity") {
					if err == nil || session != nil {
						t.Fatal("failed opening accepted")
					}
					calls := port.calls
					if _, err := v.OpenComputer(context.Background(), control, computerMap, transfer, computerLimits()); err == nil || port.calls != calls {
						t.Fatal("failed opening replayed")
					}
					later := computerCompletionFixture(v, port)
					later.binding.source.Bridges.Generation = v.computerGeneration + 2
					later.binding.rearm = true
					if _, err := v.OpenComputer(context.Background(), control, computerMap, later, computerLimits()); err == nil || port.calls != calls {
						t.Fatal("later transfer bypassed unsuccessful return")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := session.Computer().Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				if cycle == 1 && fault == "close" {
					port.loseClose = true
				}
				observed, err := session.Close(context.Background())
				if cycle == 1 && fault == "close" {
					if err == nil || v.computerClosed {
						t.Fatal("unknown return granted next load")
					}
				} else if err != nil || !observed.HoldAcknowledged || observed.After.Generation != uint64(3+cycle*2) || observed.After.ResetMask != 15 || !v.computerClosed {
					t.Fatal(observed, err)
				}
				calls, reads, writes := port.calls, port.reads, port.writes
				if _, err := v.OpenComputer(context.Background(), control, computerMap, transfer, computerLimits()); err == nil {
					t.Fatal("completed configuration reused for another opening")
				}
				if session.Computer().Run(context.Background()) == nil {
					t.Fatal("old computer retained access")
				}
				if calls != port.calls || reads != port.reads || writes != port.writes {
					t.Fatal("retired objects touched hardware")
				}
				if fault == "close" && cycle == 1 {
					later := computerCompletionFixture(v, port)
					later.binding.source.Bridges.Generation = v.computerGeneration + 2
					later.binding.rearm = true
					if _, err := v.OpenComputer(context.Background(), control, computerMap, later, computerLimits()); err == nil || port.calls != calls {
						t.Fatal("new transfer bypassed unknown return")
					}
					return
				}
				previous = region
			}
			if cpu.configurations != 3 || port.opens != 3 || port.closes != 3 {
				t.Fatal("missing load/open/return", cpu.configurations, port.opens, port.closes)
			}
		})
	}
}

func TestComputerOpeningRequiresExactCompletedTransferBinding(t *testing.T) {
	for _, mode := range []string{"missing", "incomplete", "failed", "pending", "accepted", "offset", "region", "phase", "unbound", "other-verifier", "changed-generation", "changed-context"} {
		t.Run(mode, func(t *testing.T) {
			v, cpu, control, mapping := computerFixture(t, "")
			transfer := computerCompletionFixture(v, cpu)
			switch mode {
			case "missing":
				transfer = nil
			case "incomplete":
				transfer.done = false
			case "failed":
				transfer.err = errors.New("uncertain completion")
			case "pending":
				transfer.pending = []configurationBuffer{{0, 1}}
			case "accepted":
				transfer.accepted = 0
			case "offset":
				transfer.offset = 0
			case "region":
				transfer.region = &Region{}
			case "phase":
				transfer.phase = "done"
			case "unbound":
				transfer.binding = nil
			case "other-verifier":
				transfer.binding.verifier = &HPSContextVerifier{}
			case "changed-generation":
				transfer.binding.source.Bridges.Generation++
			case "changed-context":
				transfer.binding.source.Digest[0] ^= 1
			}
			if s, err := v.OpenComputer(context.Background(), control, mapping, transfer, computerLimits()); err == nil || s != nil || cpu.opens != 0 || cpu.reads != 0 || cpu.writes != 0 {
				t.Fatal("unqualified completion reached control", err)
			}
		})
	}
}
