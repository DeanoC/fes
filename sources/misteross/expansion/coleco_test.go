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

func colecoBoundaryAsset(t *testing.T) (Shell, Asset) {
	t.Helper()
	shell, asset := colecoFixture(t)
	base, err := loadRBF(shell.Payload)
	if err != nil {
		t.Fatal(err)
	}
	cart, err := loadRBF(asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	patchBits := make([]CRAMPatchBit, 0, len(colecoResponseBoundaryCoordinates))
	for _, coordinate := range colecoResponseBoundaryCoordinates {
		value := cramBit(base.cram, coordinate.x, coordinate.y) ^ 1
		setCramBit(cart.cram, coordinate.x, coordinate.y, value)
		patchBits = append(patchBits, CRAMPatchBit{
			Value: int(value), X: coordinate.x, Y: coordinate.y,
		})
	}
	cartBytes := saveRBF(cart)
	manifest := asset.Manifest
	manifest.CartSHA256, manifest.CartSize = hash(cartBytes), int64(len(cartBytes))
	manifest.BoundaryPatch = &BoundaryPatch{Contract: colecoResponseBoundaryContract, Bits: patchBits}
	patched, err := NewAsset(manifest, cartBytes)
	if err != nil {
		t.Fatal(err)
	}
	return shell, patched
}

func TestColecoResponseBoundaryPatchIsClosedAndComposed(t *testing.T) {
	shell, asset := colecoBoundaryAsset(t)
	_, linked, err := Compose(shell, asset)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := loadRBF(linked)
	if err != nil {
		t.Fatal(err)
	}
	cart, err := loadRBF(asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	for _, coordinate := range colecoResponseBoundaryCoordinates {
		if got, want := cramBit(decoded.cram, coordinate.x, coordinate.y), cramBit(cart.cram, coordinate.x, coordinate.y); got != want {
			t.Fatalf("linked response bit (%d,%d) = %d, want cart value %d", coordinate.x, coordinate.y, got, want)
		}
	}

	badValue := asset.Manifest
	patch := *asset.Manifest.BoundaryPatch
	patch.Bits = append([]CRAMPatchBit(nil), patch.Bits...)
	patch.Bits[0].Value ^= 1
	badValue.BoundaryPatch = &patch
	wrongValue, err := NewAsset(badValue, asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compose(shell, wrongValue); err == nil || !strings.Contains(err.Error(), "value differs from manifest") {
		t.Fatalf("wrong declared response value: %v", err)
	}

	badCart, err := loadRBF(asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	setCramBit(badCart.cram, 100, 100, cramBit(badCart.cram, 100, 100)^1)
	badCartBytes := saveRBF(badCart)
	badManifest := asset.Manifest
	badManifest.CartSHA256, badManifest.CartSize = hash(badCartBytes), int64(len(badCartBytes))
	withExtraChange, err := NewAsset(badManifest, badCartBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compose(shell, withExtraChange); err == nil || !strings.Contains(err.Error(), "outside socket at 100,100") {
		t.Fatalf("unlisted outside response change: %v", err)
	}

	missingCart, err := loadRBF(asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	first := colecoResponseBoundaryCoordinates[0]
	setCramBit(missingCart.cram, first.x, first.y, cramBit(baseCRAM(t, shell), first.x, first.y))
	missingBytes := saveRBF(missingCart)
	missingManifest := asset.Manifest
	missingManifest.CartSHA256, missingManifest.CartSize = hash(missingBytes), int64(len(missingBytes))
	missingPatchBit, err := NewAsset(missingManifest, missingBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Compose(shell, missingPatchBit); err == nil || !strings.Contains(err.Error(), "did not change CRAM bit") {
		t.Fatalf("missing declared response change: %v", err)
	}
}

func baseCRAM(t *testing.T, shell Shell) []byte {
	t.Helper()
	base, err := loadRBF(shell.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return base.cram
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
