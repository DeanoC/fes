// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

func fixtureProfile() IdentityProfile {
	return IdentityProfile{Platform: "test-board", Device: "test-device", ProgrammingProfile: "contained-test", ABI: "test.abi", Major: 1, Minor: 2, ControlABI: 0x1234}
}
func packageBinding(data []byte) fabricmodel.PackageIdentity {
	digest := sha256.Sum256(data)
	return fabricmodel.PackageIdentity{PackageID: strings.Repeat("a", 64), CoreID: "test.core", BuildID: "0123456789abcdeffedcba9876543210", Platform: "test-board", Device: "test-device", ProgrammingProfile: "contained-test", ABI: "test.abi", Major: 1, Minor: 2, ProgrammedSHA256: hex.EncodeToString(digest[:])}
}
func packageImage(t *testing.T, p fabricmodel.PackageIdentity, data []byte) *fabricmodel.Image {
	t.Helper()
	image, err := fabricmodel.Prepare("model.package", data, sha256.Sum256(data), fabricmodel.Identity{ABI: 1, Core: 1, Build: 1, Package: p}, false)
	if err != nil {
		t.Fatal(err)
	}
	return image
}
func TestBuildIDWordOrderAndRejection(t *testing.T) {
	words, err := BuildWords("0123456789abcdeffedcba9876543210")
	if err != nil || words != ([4]uint32{0x76543210, 0xfedcba98, 0x89abcdef, 0x01234567}) {
		t.Fatalf("words %x: %v", words, err)
	}
	for _, bad := range []string{"", strings.Repeat("0", 32), "0123456789ABCDEFFEDCBA9876543210", strings.Repeat("g", 32), strings.Repeat("a", 31), strings.Repeat("a", 33)} {
		if _, err = BuildWords(bad); err == nil {
			t.Errorf("accepted build ID %q", bad)
		}
	}
}
func TestPreflightBindsExactProfileBuildAndPayload(t *testing.T) {
	data := []byte("retained programming payload")
	binding := packageBinding(data)
	profiles := []IdentityProfile{fixtureProfile()}
	endpoint, err := New(&fake{}, Options{DRAMBase: 0x80000000, ChunkBytes: 32, IdentityProfiles: profiles})
	if err != nil {
		t.Fatal(err)
	}
	profiles[0].ControlABI = 999 // Caller mutation cannot alter admitted mapping.
	image := packageImage(t, binding, data)
	if err = endpoint.Preflight(image); err != nil {
		t.Fatal(err)
	}
	got, err := endpoint.resolve(fabricmodel.Identity{Package: binding})
	want := embeddedcontrol.Identity{ABI: 0x1234, Build: [4]uint32{0x76543210, 0xfedcba98, 0x89abcdef, 0x01234567}}
	if err != nil || got != want {
		t.Fatalf("identity = %+v, %v", got, err)
	}
	for name, mutate := range map[string]func(*fabricmodel.PackageIdentity){
		"package-id":          func(p *fabricmodel.PackageIdentity) { p.PackageID = strings.Repeat("A", 64) },
		"core-id":             func(p *fabricmodel.PackageIdentity) { p.CoreID = "" },
		"build-id":            func(p *fabricmodel.PackageIdentity) { p.BuildID = strings.Repeat("f", 31) + "G" },
		"platform":            func(p *fabricmodel.PackageIdentity) { p.Platform += "-other" },
		"device":              func(p *fabricmodel.PackageIdentity) { p.Device += "-other" },
		"programming-profile": func(p *fabricmodel.PackageIdentity) { p.ProgrammingProfile += "-other" },
		"abi":                 func(p *fabricmodel.PackageIdentity) { p.ABI += "-other" },
		"abi-major":           func(p *fabricmodel.PackageIdentity) { p.Major++ },
		"abi-minor":           func(p *fabricmodel.PackageIdentity) { p.Minor++ },
		"payload-digest":      func(p *fabricmodel.PackageIdentity) { p.ProgrammedSHA256 = strings.Repeat("b", 64) },
		"activation-receipt":  func(p *fabricmodel.PackageIdentity) { p.ActivationReceiptSHA256 = "bad-receipt" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := binding
			mutate(&changed)
			if err := endpoint.Preflight(packageImage(t, changed, data)); err == nil {
				t.Fatal("mismatched binding accepted")
			}
		})
	}
	if err = endpoint.Preflight(nil); err == nil {
		t.Fatal("nil image accepted")
	}
	if err = endpoint.Preflight(packageImage(t, fabricmodel.PackageIdentity{}, data)); err == nil {
		t.Fatal("unpackaged image accepted")
	}
}
func TestProfileValidationRejectsAmbiguity(t *testing.T) {
	p := fixtureProfile()
	for _, profiles := range [][]IdentityProfile{{p, p}, {{Platform: "", Device: "x", ProgrammingProfile: "x", ABI: "x", Major: 1, ControlABI: 1}}, {{Platform: "x", Device: "x", ProgrammingProfile: "x", ABI: "x", Major: 1, ControlABI: 0}}} {
		if _, err := New(&fake{}, Options{DRAMBase: 1, ChunkBytes: 1, IdentityProfiles: profiles}); err == nil {
			t.Fatal("invalid identity profiles accepted")
		}
	}
}
