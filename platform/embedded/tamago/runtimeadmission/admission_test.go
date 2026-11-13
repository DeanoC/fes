// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

func fixture(t *testing.T, mutate func(string) string) *corepackage.Archive {
	t.Helper()
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v2"
	manifest, err := os.ReadFile(filepath.Join(base, "manifests/valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads/fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		manifest = []byte(mutate(string(manifest)))
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(data)), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return archive
}
func policyFor(archive *corepackage.Archive) Policy {
	d := archive.Inspection().Descriptor
	interfaces := []Contract{}
	for _, i := range d.Interfaces {
		if i.Required {
			interfaces = append(interfaces, Contract{i.ID, i.Major, i.Minor})
		}
	}
	return Policy{Scope: "qemu-model-only", RuntimeID: strings.Repeat("a", 64), Target: Target{d.Target.Platform, d.Target.Device},
		Drivers:   []Driver{{d.Target.ProgrammingProfile, Contract{d.ABI.ID, d.ABI.Major, d.ABI.Minor}, interfaces}},
		Decisions: []Decision{{PackageID: archive.Inspection().PackageID, Code: "none"}}}
}
func controller(t *testing.T) *fabricmodel.Controller {
	t.Helper()
	data := []byte("idle")
	idle, err := fabricmodel.Prepare("model.idle", data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	c, err := fabricmodel.New(idle, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Boot(0, time.Second); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 6; n++ {
		cmd, err := c.Next(0)
		if err != nil {
			t.Fatal(err)
		}
		if cmd == nil {
			break
		}
		if err = c.Complete(cmd.Token, fabricmodel.CompleteModel(*cmd), 0); err != nil {
			t.Fatal(err)
		}
	}
	return c
}
func TestRejectBeforeOwnerMutation(t *testing.T) {
	a := fixture(t, nil)
	for _, test := range []struct {
		name, code string
		change     func(*Policy)
	}{
		{"target", "unsupported_target", func(p *Policy) { p.Target.Device = "another-device" }},
		{"profile", "unsupported_programming_profile", func(p *Policy) { p.Drivers[0].Profile = "another-profile" }},
		{"abi-minor", "unsupported_abi", func(p *Policy) { p.Drivers[0].ABI.Minor++ }},
		{"interface-minor", "unsupported_interface", func(p *Policy) { p.Drivers[0].Interfaces[0].Minor++ }},
		{"missing-decision", "backend_unavailable", func(p *Policy) { p.Decisions = nil }},
		{"runtime-reject", "unsupported_interface", func(p *Policy) { p.Decisions[0].Code = "unsupported_interface" }},
		{"wrong-package-decision", "backend_unavailable", func(p *Policy) { p.Decisions[0].PackageID = strings.Repeat("b", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := policyFor(a)
			test.change(&p)
			g, err := NewModel(p)
			if err != nil {
				t.Fatal(err)
			}
			c := controller(t)
			before := c.Status()
			frame, err := c.BeginFrame()
			if err != nil {
				t.Fatal(err)
			}
			if code := ErrorCode(g.LoadModel(c, a, a.Inspection().PackageID, 0, time.Second)); code != test.code {
				t.Fatalf("got %s want %s", code, test.code)
			}
			if c.Status() != before {
				t.Fatal("rejected package mutated owner")
			}
			if cmd, err := c.Next(0); cmd != nil || err != nil {
				t.Fatal("rejection issued command")
			}
			if err = c.FinishFrame(frame, fabricmodel.Completion{Quiescent: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNanoRecognitionDoesNotAdmitIntoExistingBackend(t *testing.T) {
	cyclone := fixture(t, nil)
	nano := fixture(t, func(m string) string {
		m = strings.ReplaceAll(m, "de10_nano", "de25_nano")
		return strings.ReplaceAll(m, "5CSEBA6U23I7", "A5EB013BB23BE4SCS")
	})
	gate, err := NewModel(policyFor(cyclone))
	if err != nil {
		t.Fatal(err)
	}
	c := controller(t)
	before := c.Status()
	if code := ErrorCode(gate.LoadModel(c, nano, nano.Inspection().PackageID, 0, time.Second)); code != "unsupported_target" {
		t.Fatalf("Nano recognized as an admitted package: %s", code)
	}
	if c.Status() != before {
		t.Fatal("unsupported target changed running owner")
	}
	if command, err := c.Next(0); command != nil || err != nil {
		t.Fatal("unsupported target issued command")
	}
}
func TestPolicyAndMetadataArePrivate(t *testing.T) {
	a := fixture(t, nil)
	id := a.Inspection().PackageID
	p := policyFor(a)
	g, err := NewModel(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Target.Device = "changed"
	p.Drivers[0].Interfaces[0].ID = "changed"
	p.Drivers[0].ABI.ID = "changed"
	p.Decisions[0].Code = "rejected"
	metadata := a.Inspection()
	metadata.Descriptor.Build.ID = "changed"
	metadata.Descriptor.Interfaces[0].ID = "changed"
	c := controller(t)
	if err = g.LoadModel(c, a, id, 0, time.Second); err != nil {
		t.Fatal(err)
	}
	var observed fabricmodel.PackageIdentity
	for n := 0; n < 10; n++ {
		cmd, err := c.Next(0)
		if err != nil {
			t.Fatal(err)
		}
		if cmd == nil {
			break
		}
		observed = cmd.Identity.Package
		if err = c.Complete(cmd.Token, fabricmodel.CompleteModel(*cmd), 0); err != nil {
			t.Fatal(err)
		}
	}
	d := a.Inspection().Descriptor
	if c.Status().State != fabricmodel.Running || c.Status().Package != observed || observed.PackageID != id || observed.BuildID != d.Build.ID ||
		observed.Platform != d.Target.Platform || observed.Device != d.Target.Device || observed.ProgrammingProfile != d.Target.ProgrammingProfile || observed.ProgrammedSHA256 != d.Payload.SHA256 {
		t.Fatal("lost full binding")
	}
}
func TestPolicyCannotNamePhysicalScope(t *testing.T) {
	a := fixture(t, nil)
	for _, mutate := range []func(*Policy){
		func(p *Policy) { p.Scope = "physical" }, func(p *Policy) { p.RuntimeID = "" },
		func(p *Policy) { p.Drivers = append(p.Drivers, p.Drivers[0]) },
		func(p *Policy) { p.Decisions = append(p.Decisions, p.Decisions[0]) },
		func(p *Policy) { p.Drivers[0].Interfaces = append(p.Drivers[0].Interfaces, p.Drivers[0].Interfaces[0]) },
	} {
		p := policyFor(a)
		mutate(&p)
		if _, err := NewModel(p); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}
