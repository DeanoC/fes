// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

type testBootHooks struct {
	*fake
	pair   HPSFirstPair
	boot   HPSFirstHandoff
	checks int
}

func (h *testBootHooks) VerifyBootHandoff(id fabricmodel.PackageIdentity) error {
	h.checks++
	return VerifyHPSFirstPair(h.pair, h.boot, id)
}

func TestHPSFirstPairRequiresExactPackageAndTrustedHandoff(t *testing.T) {
	data := []byte("core bytes")
	id := packageBinding(data)
	id.Platform, id.Device, id.ProgrammingProfile = "de25_nano", "A5EB013BB23BE4SCS", HPSFirstDiagnosticProfile
	id.CoreID, id.ABI, id.Major, id.Minor = "fes.de25-embedded-diagnostic", "fes.de25-embedded-diagnostic", 1, 0
	pair := HPSFirstPair{Format: 1, Classification: "unqualified-hps-first-diagnostic", PackageID: id.PackageID,
		BuildID: id.BuildID, BuildInputsSHA256: strings.Repeat("b", 64), HPSIOHash: strings.Repeat("c", 64),
		PFGIdentity: "wrapper-sha256:" + strings.Repeat("a", 64) + "; binary-sha256:" + strings.Repeat("b", 64), QuartusVersion: "26.3", Phase1SHA256: strings.Repeat("d", 64),
		Phase1Size: 10, CoreSHA256: id.ProgrammedSHA256, CoreSize: int64(len(data)), VendorSPLSHA256: strings.Repeat("e", 64)}
	boot := HPSFirstHandoff{BootOrder: "HPS FIRST", Phase1SHA256: pair.Phase1SHA256, HPSIOHash: pair.HPSIOHash, PFGIdentity: pair.PFGIdentity}
	if err := VerifyHPSFirstPair(pair, boot, id); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*HPSFirstPair, *HPSFirstHandoff, *fabricmodel.PackageIdentity){
		"package": func(_ *HPSFirstPair, _ *HPSFirstHandoff, id *fabricmodel.PackageIdentity) {
			id.PackageID = strings.Repeat("f", 64)
		},
		"build": func(p *HPSFirstPair, _ *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) {
			p.BuildID = strings.Repeat("f", 32)
		},
		"core": func(p *HPSFirstPair, _ *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) {
			p.CoreSHA256 = strings.Repeat("f", 64)
		},
		"phase1": func(_ *HPSFirstPair, b *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) {
			b.Phase1SHA256 = strings.Repeat("f", 64)
		},
		"io-hash": func(_ *HPSFirstPair, b *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) {
			b.HPSIOHash = strings.Repeat("f", 64)
		},
		"tool":       func(_ *HPSFirstPair, b *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) { b.PFGIdentity = "other" },
		"boot-order": func(_ *HPSFirstPair, b *HPSFirstHandoff, _ *fabricmodel.PackageIdentity) { b.BootOrder = "FPGA FIRST" },
	} {
		t.Run(name, func(t *testing.T) {
			p, b, i := pair, boot, id
			mutate(&p, &b, &i)
			if VerifyHPSFirstPair(p, b, i) == nil {
				t.Fatal("mismatch accepted")
			}
		})
	}
	profile := IdentityProfile{Platform: id.Platform, Device: id.Device, ProgrammingProfile: id.ProgrammingProfile, ABI: id.ABI, Major: id.Major, Minor: id.Minor, ControlABI: 0x10203040}
	if _, err := New(&fake{}, Options{DRAMBase: 1, ChunkBytes: 32, IdentityProfiles: []IdentityProfile{profile}}); err == nil {
		t.Fatal("HPS-first profile without boot guard accepted")
	}
	profile.RequireBootHandoff = true
	unverified, err := New(&fake{}, Options{DRAMBase: 1, ChunkBytes: 32, IdentityProfiles: []IdentityProfile{profile}})
	if err != nil || unverified.Preflight(packageImage(t, id, data)) == nil {
		t.Fatal("missing boot evidence accepted")
	}
	hooks := &testBootHooks{fake: &fake{}, pair: pair, boot: boot}
	endpoint, err := New(hooks, Options{DRAMBase: 1, ChunkBytes: 32, IdentityProfiles: []IdentityProfile{profile}})
	if err != nil {
		t.Fatal(err)
	}
	image := packageImage(t, id, data)
	if err = endpoint.Preflight(image); err != nil || hooks.checks != 1 || len(hooks.trace) != 0 {
		t.Fatalf("valid preflight: %v", err)
	}
	hooks.boot.HPSIOHash = strings.Repeat("f", 64)
	if endpoint.Preflight(image) == nil || len(hooks.trace) != 0 {
		t.Fatal("mismatched boot reached hardware")
	}
	// The boot assertion can change after preflight. Recheck it after staging
	// and immediately before issuing the firmware START command.
	hooks.boot = boot
	endpoint.options.DRAMBase = 0x80000000
	endpoint.command = &fabricmodel.Command{Operation: fabricmodel.Program, Identity: fabricmodel.Identity{Package: id}, Bytes: data}
	endpoint.phase = "begin"
	if done, result := endpoint.program(); done || result.Err != nil || hooks.region == nil {
		t.Fatal("staging failed")
	}
	hooks.boot.HPSIOHash = strings.Repeat("f", 64)
	if done, result := endpoint.program(); !done || result.Err == nil || !result.Uncertain || hooks.programs != 0 || hooks.region == nil || len(endpoint.retained) != 1 {
		t.Fatal("changed boot handoff reached firmware or lost its reservation")
	}
}
