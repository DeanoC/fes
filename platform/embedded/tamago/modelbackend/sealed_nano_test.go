// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

type sealedActorCPU struct {
	*actorRPC
	bridges     nativefabric.BridgeSnapshot
	bridgeFault string
	base        uint64
	identity    embeddedcontrol.Identity
	pair        *nativefabric.HPSFirstPair
	boot        *nativefabric.HPSFirstHandoff
}

func actorBridgeState() nativefabric.BridgeSnapshot {
	return nativefabric.BridgeSnapshot{Capabilities: nativefabric.BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: nativefabric.FabricHandshakePresent}, Owner: 0x1234, Generation: 1}
}
func (a *sealedActorCPU) ObserveBridges() (nativefabric.BridgeSnapshot, error) {
	return a.bridges, nil
}
func (a *sealedActorCPU) CloseBridges(r nativefabric.BridgeCloseRequest) (nativefabric.BridgeClosure, error) {
	if r.Before != a.bridges {
		return nativefabric.BridgeClosure{}, errors.New("stale simulated ownership")
	}
	if _, err := a.call("contain"); err != nil {
		return nativefabric.BridgeClosure{}, err
	}
	a.bridges.Generation++
	a.bridges.ResetMask, a.bridges.NeverEnabled, a.bridges.MastersContained = 15, false, true
	closed := nativefabric.BridgeClosure{After: a.bridges, ProducersQuiescent: true, FabricAcknowledged: r.RequireFabricHandshake, HardwareDrainedMask: r.Before.Capabilities.HardwareDrainMask &^ r.Before.ResetMask}
	if r.PreserveDataContainment {
		closed.DataRemainedContained, closed.HardwareDrainedMask = true, 0
	}
	if a.programs != 0 {
		switch a.bridgeFault {
		case "hardware":
			closed.HardwareDrainedMask ^= 4
		case "fabric":
			closed.FabricAcknowledged = false
		case "owner":
			closed.After.Owner++
		}
	}
	return closed, nil
}
func (a *sealedActorCPU) OpenBridges(r nativefabric.BridgeOpenRequest) (nativefabric.BridgeSnapshot, error) {
	if r.Before != a.bridges {
		return nativefabric.BridgeSnapshot{}, errors.New("stale simulated ownership")
	}
	op := "release"
	if r.Mask == 2 {
		op = "control"
	}
	if _, err := a.call(op); err != nil {
		return nativefabric.BridgeSnapshot{}, err
	}
	a.bridges.Generation++
	a.bridges.ResetMask &^= r.Mask
	a.bridges.NeverEnabled, a.bridges.MastersContained = false, r.Mask == 2
	return a.bridges, nil
}

func (a *sealedActorCPU) ReadPhysical32(address uint64) (uint32, error) {
	if address < a.base || address-a.base > 255 {
		return 0, errors.New("outside synthetic CPU aperture")
	}
	return a.Read32(uint32(address - a.base))
}
func (a *sealedActorCPU) WritePhysical32(address uint64, value uint32) error {
	if address < a.base || address-a.base > 255 {
		return errors.New("outside synthetic CPU aperture")
	}
	return a.Write32(uint32(address-a.base), value)
}
func (a *sealedActorCPU) Resolve(fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	return a.identity, nil
}
func (a *sealedActorCPU) VerifyBootHandoff(id fabricmodel.PackageIdentity) error {
	if a.pair == nil || a.boot == nil {
		return errors.New("simulated boot handoff missing")
	}
	return nativefabric.VerifyHPSFirstPair(*a.pair, *a.boot, id)
}

func TestSealedNanoTenfootModel(t *testing.T) {
	packagePath, rpcPath := os.Getenv("FES_DE25_SEALED_PACKAGE"), os.Getenv("FES_SEALED_TFA_RPC")
	if packagePath == "" || rpcPath == "" {
		t.Skip("run verify_sealed.py for the sealed Nano model")
	}
	body, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	i, d := archive.Inspection(), archive.Inspection().Descriptor
	if d.Format != 2 || d.Target.Platform != "de25_nano" || d.Target.Device != "A5EB013BB23BE4SCS" || (d.Target.ProgrammingProfile != "de25-embedded-diagnostic-v1" && d.Target.ProgrammingProfile != nativefabric.HPSFirstDiagnosticProfile) || d.ABI.ID != "fes.de25-embedded-diagnostic" {
		t.Fatalf("unexpected diagnostic package: %+v", d)
	}
	words, err := nativefabric.BuildWords(d.Build.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity := embeddedcontrol.Identity{ABI: 0x10203040, Build: words}
	var pair *nativefabric.HPSFirstPair
	var boot *nativefabric.HPSFirstHandoff
	if d.Target.ProgrammingProfile == nativefabric.HPSFirstDiagnosticProfile {
		raw, err := os.ReadFile(os.Getenv("FES_DE25_HPS_PAIR"))
		if err != nil {
			t.Fatal(err)
		}
		pair = new(nativefabric.HPSFirstPair)
		if err = json.Unmarshal(raw, pair); err != nil {
			t.Fatal(err)
		}
		phase1, err := os.ReadFile(os.Getenv("FES_DE25_HPS_PHASE1"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(phase1)
		if pair.Phase1Size != int64(len(phase1)) || pair.Phase1SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("phase-one artifact differs from pair")
		}
		boot = &nativefabric.HPSFirstHandoff{BootOrder: "HPS FIRST", Phase1SHA256: pair.Phase1SHA256, HPSIOHash: pair.HPSIOHash, PFGIdentity: pair.PFGIdentity}
	}
	t.Setenv("FES_EMBEDDED_RPC", rpcPath)
	scenarios := []string{"launch-return", "wrong-live-id", "missing-model-decision", "unexpected-receipt", "unexpected-source", "unexpected-part", "unexpected-composition"}
	if pair != nil {
		scenarios = append(scenarios, "missing-boot-handoff", "wrong-boot-handoff")
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			policy := runtimeadmission.Policy{Scope: "qemu-model-only", RuntimeID: strings.Repeat("a", 64),
				Target:    runtimeadmission.Target{Platform: d.Target.Platform, Device: d.Target.Device},
				Drivers:   []runtimeadmission.Driver{{Profile: d.Target.ProgrammingProfile, ABI: runtimeadmission.Contract{ID: d.ABI.ID, Major: d.ABI.Major, Minor: d.ABI.Minor}}},
				Decisions: []runtimeadmission.Decision{{PackageID: i.PackageID, Code: "none"}}}
			check := "return"
			if scenario == "wrong-live-id" {
				check = "identity"
			}
			if scenario == "missing-model-decision" {
				check = "preserve"
				policy.Decisions = nil
			}
			if scenario == "missing-boot-handoff" || scenario == "wrong-boot-handoff" {
				check = "preserve"
			}
			entry := Entry{ID: "nano-sealed", Title: "Nano diagnostic", Path: "nano.fcore", PackageID: i.PackageID, Check: check}
			if scenario == "unexpected-receipt" {
				entry.Check = "preserve"
				entry.ReceiptSHA256 = strings.Repeat("b", 64)
			}
			if scenario == "unexpected-source" {
				entry.Check = "preserve"
				entry.Sources = []Source{{ID: "rom", Path: "other", SHA256: strings.Repeat("c", 64)}}
			}
			if scenario == "unexpected-part" {
				entry.Check = "preserve"
				entry.Parts = []Part{{ID: "part", Path: "other", SHA256: strings.Repeat("c", 64)}}
			}
			if scenario == "unexpected-composition" {
				entry.Check = "preserve"
				entry.CompositionID = strings.Repeat("c", 64)
			}
			config := Config{Scope: "qemu-model-only", Entries: []Entry{entry}}
			r := newActorRPC(t)
			r.wrongIdentity = scenario == "wrong-live-id"
			cpu := &sealedActorCPU{actorRPC: r, base: 0x40000000, identity: identity, pair: pair, bridges: actorBridgeState()}
			if boot != nil {
				copy := *boot
				cpu.boot = &copy
			}
			if scenario == "missing-boot-handoff" {
				cpu.boot = nil
			}
			if scenario == "wrong-boot-handoff" {
				cpu.boot.HPSIOHash = strings.Repeat("f", 64)
			}
			hooks, err := nativefabric.NewCPUHooks(cpu, cpu, cpu.base, 256, 13)
			if err != nil {
				t.Fatal(err)
			}
			profile := nativefabric.IdentityProfile{Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile, ABI: d.ABI.ID, Major: uint16(d.ABI.Major), Minor: uint16(d.ABI.Minor), ControlABI: identity.ABI, RequireBootHandoff: pair != nil}
			endpoint, err := nativefabric.New(hooks, nativefabric.Options{DRAMBase: 0x80000000, ChunkBytes: 65536, IdentityProfiles: []nativefabric.IdentityProfile{profile}})
			if err != nil {
				t.Fatal(err)
			}
			read := func(path string) ([]byte, error) {
				if path != "nano.fcore" {
					return nil, errors.New("unexpected model path")
				}
				return bytes.Clone(body), nil
			}
			backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer closeBackend(t, backend)
			device, err := gfx.NewSoftware(640, 480)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			app := tenfoot.NewApp(tenfoot.NewClient("http://model.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 640, 480, 100)
			if err = app.SetPreferenceStore(preferences{}); err != nil {
				t.Fatal(err)
			}
			p := &scriptPlatform{app: app, backend: backend, entries: config.Entries, device: device}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err = tenfoot.RunPlatform(ctx, app, device, p); err != nil {
				t.Fatalf("%s: %v stage=%d view=%+v", scenario, err, p.stage, backend.View())
			}
			if p.index != 1 || backend.View().Status.State != fabricmodel.Idle {
				t.Fatalf("case incomplete: %s %+v", scenario, backend.View())
			}
			stats, err := r.call("stats")
			if err != nil || len(stats) != 3 {
				t.Fatalf("firmware stats: %v %v", stats, err)
			}
			starts, releases := uint64(3), uint64(3)
			if scenario == "wrong-live-id" {
				releases = 2
			}
			if entry.Check == "preserve" {
				starts, releases = 1, 1
			}
			if stats[0] != starts || stats[1] != releases {
				t.Fatalf("unexpected firmware transitions: %v", stats)
			}
		})
	}
}
