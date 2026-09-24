package expansion

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func colecoFixture(t *testing.T) (Shell, Asset) {
	t.Helper()
	shell, _ := fixtures()
	decoded, err := loadRBF(shell)
	if err != nil {
		t.Fatal(err)
	}
	setCramBit(decoded.cram, 2000, 100, cramBit(decoded.cram, 2000, 100)^1)
	cart := saveRBF(decoded)
	base := Shell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32), Payload: shell,
		Slot: ColecoSlot, SlotMajor: 1}
	manifest := Manifest{CartSHA256: hash(cart), CartSize: int64(len(cart)), Device: Device,
		Format: 1, Map: ColecoMap, RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40),
		ShellBuildID: base.BuildID, ShellPackageID: base.PackageID, ShellSHA256: hash(shell),
		Slot: ColecoSlot, SlotMajor: 1}
	asset, err := NewAsset(manifest, cart)
	if err != nil {
		t.Fatal(err)
	}
	return base, asset
}

func TestColecoSlotAdmissionAndRectangle(t *testing.T) {
	shell, asset := colecoFixture(t)
	composition, linked, err := Compose(shell, asset)
	if err != nil {
		t.Fatal(err)
	}
	if composition.PayloadSHA256 != hash(linked) || bytes.Equal(linked, shell.Payload) {
		t.Fatal("Coleco composition lost the socket change")
	}
	for name, change := range map[string]func(*Shell, *Asset){
		"zx81-shell":    func(s *Shell, _ *Asset) { s.Slot = Slot },
		"zx81-map":      func(_ *Shell, a *Asset) { a.Manifest.Map = Map },
		"wrong-version": func(s *Shell, _ *Asset) { s.SlotMinor = 1 },
		"wrong-shell":   func(s *Shell, _ *Asset) { s.PackageID = strings.Repeat("e", 64) },
		"corrupt-cart":  func(_ *Shell, a *Asset) { a.Cart[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			otherShell, otherAsset := shell, asset
			otherAsset.Cart = bytes.Clone(asset.Cart)
			change(&otherShell, &otherAsset)
			if _, _, err := Compose(otherShell, otherAsset); err == nil {
				t.Fatal("accepted incompatible Coleco expansion")
			}
		})
	}
	outside, err := loadRBF(asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	setCramBit(outside.cram, 2000, 1034, cramBit(outside.cram, 2000, 1034)^1)
	changed := asset.Manifest
	changedCart := saveRBF(outside)
	changed.CartSHA256, changed.CartSize = hash(changedCart), int64(len(changedCart))
	bad, err := NewAsset(changed, changedCart)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compose(shell, bad); err == nil {
		t.Fatal("accepted Coleco CRAM change below the socket")
	}
}

// This optional cross-language check consumes exact, independently routed
// development artifacts; ordinary unit tests need no compiler output.
func TestColecoPhysicalGolden(t *testing.T) {
	root := os.Getenv("FES_COLECO_GOLDEN_ROOT")
	if root == "" {
		t.Skip("set FES_COLECO_GOLDEN_ROOT to a sealed diagnostic build")
	}
	archives, err := filepath.Glob(filepath.Join(root, "*.tar"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("expected one diagnostic archive: %v, %v", archives, err)
	}
	f, err := os.Open(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	asset, err := ReadAsset(f)
	if err != nil {
		t.Fatal(err)
	}
	shellBytes, err := os.ReadFile(filepath.Join(root, "..", "..", "fes-coleco-socket-dev", "core.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := os.ReadFile(filepath.Join(root, "linked.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	shell := Shell{PackageID: asset.Manifest.ShellPackageID, BuildID: asset.Manifest.ShellBuildID,
		Payload: shellBytes, Slot: ColecoSlot, SlotMajor: 1}
	_, linked, err := Compose(shell, asset)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(linked, oracle) {
		got, gotErr := loadRBF(linked)
		want, wantErr := loadRBF(oracle)
		if gotErr == nil && wantErr == nil {
			for y := 32; y < cramHeight; y++ {
				for x := 0; x < cramWidth; x++ {
					if cramBit(got.cram, x, y) != cramBit(want.cram, x, y) {
						t.Fatalf("Go composition differs from compiler oracle at CRAM %d,%d", x, y)
					}
				}
			}
			t.Fatalf("Go composition %s differs in framing from compiler oracle %s", hash(linked), hash(oracle))
		}
		t.Fatalf("Go composition %s differs from compiler oracle %s (decode %v, %v)", hash(linked), hash(oracle), gotErr, wantErr)
	}
}
