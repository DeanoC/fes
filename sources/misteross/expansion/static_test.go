package expansion

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func staticFixture(t *testing.T) (StaticShell, Asset) {
	t.Helper()
	root := StaticPersona{ControlBuildID: strings.Repeat("1", 32), HardwareID: EmptyPersonaID, RootQDBSHA256: strings.Repeat("2", 64), InterfaceSHA256: strings.Repeat("3", 64), ClockResetSHA256: strings.Repeat("4", 64), POF0Hash: strings.Repeat("5", 64)}
	base := []byte("opaque full vendor image\x00\xff")
	empty := []byte("opaque empty partial\xff")
	ram := []byte("opaque RAM partial\x01")
	shell := StaticShell{PackageID: strings.Repeat("a", 64), BuildID: strings.Repeat("b", 32), Base: base, DefaultPersona: empty, BaseSHA256: hash(base), DefaultPersonaSHA256: hash(empty), Default: root}
	selected := root
	selected.HardwareID = RAM16KPersonaID
	m := Manifest{Format: 2, Device: StaticDevice, Slot: Slot, SlotMajor: 2, Map: StaticMap, CartSize: int64(len(ram)), CartSHA256: hash(ram), RecipeSHA256: strings.Repeat("c", 64), Revision: strings.Repeat("d", 40), ShellBuildID: shell.BuildID, ShellPackageID: shell.PackageID, ShellSHA256: hash(base), Partial: &selected}
	asset, err := NewAsset(m, ram)
	if err != nil {
		t.Fatal(err)
	}
	return shell, asset
}
func TestStaticCompositionRoundTripSelectsTwoUnchangedImages(t *testing.T) {
	shell, asset := staticFixture(t)
	baseBefore := bytes.Clone(shell.Base)
	defaultBefore := bytes.Clone(shell.DefaultPersona)
	cartBefore := bytes.Clone(asset.Cart)
	empty, err := ComposeStatic(shell, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := ComposeStatic(shell, &asset)
	if err != nil {
		t.Fatal(err)
	}
	if empty.HardwareID != EmptyPersonaID || selected.HardwareID != RAM16KPersonaID || empty.ExpansionID != "" || selected.ExpansionID != asset.ID || empty.ID == selected.ID || empty.BaseSHA256 != selected.BaseSHA256 {
		t.Fatal("lost static selection identity", empty, selected)
	}
	if empty.PersonaSHA256 != hash(defaultBefore) || selected.PersonaSHA256 != hash(cartBefore) || selected.BaseSize != int64(len(baseBefore)) || selected.PersonaSize != int64(len(cartBefore)) {
		t.Fatal("plan does not bind two independent images")
	}
	var archive bytes.Buffer
	if err := asset.Write(&archive); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadAsset(&archive)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ComposeStatic(shell, &decoded)
	if err != nil || again != selected {
		t.Fatal("archive changed selection", err)
	}
	if !bytes.Equal(shell.Base, baseBefore) || !bytes.Equal(shell.DefaultPersona, defaultBefore) || !bytes.Equal(asset.Cart, cartBefore) {
		t.Fatal("static composition edited an image")
	}
	if _, body, err := Compose(Shell{PackageID: shell.PackageID, BuildID: shell.BuildID, Payload: shell.Base, Slot: Slot, SlotMajor: 2}, asset); err == nil || body != nil {
		t.Fatal("partial entered the CRAM linker")
	}
}
func TestStaticCompositionRejectsWrongBaseAndFrozenRoot(t *testing.T) {
	for name, change := range map[string]func(*StaticShell){
		"package":          func(s *StaticShell) { s.PackageID = strings.Repeat("e", 64) },
		"guest-build":      func(s *StaticShell) { s.BuildID = strings.Repeat("e", 32) },
		"base-bytes":       func(s *StaticShell) { s.Base = bytes.Clone(s.Base); s.Base[0] ^= 1 },
		"default-bytes":    func(s *StaticShell) { s.DefaultPersona = bytes.Clone(s.DefaultPersona); s.DefaultPersona[0] ^= 1 },
		"controller":       func(s *StaticShell) { s.Default.ControlBuildID = strings.Repeat("e", 32) },
		"root":             func(s *StaticShell) { s.Default.RootQDBSHA256 = strings.Repeat("e", 64) },
		"interface":        func(s *StaticShell) { s.Default.InterfaceSHA256 = strings.Repeat("e", 64) },
		"clock-reset":      func(s *StaticShell) { s.Default.ClockResetSHA256 = strings.Repeat("e", 64) },
		"POF0":             func(s *StaticShell) { s.Default.POF0Hash = strings.Repeat("e", 64) },
		"nonempty-default": func(s *StaticShell) { s.Default.HardwareID = RAM16KPersonaID },
	} {
		t.Run(name, func(t *testing.T) {
			shell, asset := staticFixture(t)
			change(&shell)
			if _, err := ComposeStatic(shell, &asset); err == nil {
				t.Fatal("accepted mismatched static input")
			}
		})
	}
}
func TestStaticAssetsOwnPartialMetadataAndRefuseInvalidDialects(t *testing.T) {
	_, asset := staticFixture(t)
	m := asset.Manifest
	owned := *m.Partial
	m.Partial = &owned
	source := bytes.Clone(asset.Cart)
	saved, err := NewAsset(m, source)
	if err != nil {
		t.Fatal(err)
	}
	owned.HardwareID = 0
	source[0] ^= 1
	if err := saved.Validate(); err != nil {
		t.Fatal("caller mutation changed owned asset", err)
	}
	for name, change := range map[string]func(*Manifest){
		"format1-with-partial": func(m *Manifest) { m.Format = 1 },
		"CycloneV":             func(m *Manifest) { m.Device = Device },
		"CRAM-map":             func(m *Manifest) { m.Map = MapV2 },
		"wrong-bus":            func(m *Manifest) { m.Slot = ColecoSlot },
		"old-bus":              func(m *Manifest) { m.SlotMajor = 1 },
		"multiple-slots":       func(m *Manifest) { m.SlotIndex = 1 },
		"missing-partial":      func(m *Manifest) { m.Partial = nil },
		"zero-controller":      func(m *Manifest) { m.Partial.ControlBuildID = strings.Repeat("0", 32) },
		"zero-root":            func(m *Manifest) { m.Partial.RootQDBSHA256 = strings.Repeat("0", 64) },
		"unknown-persona":      func(m *Manifest) { m.Partial.HardwareID = 123 },
		"bad-size":             func(m *Manifest) { m.CartSize = 0 },
		"bad-payload":          func(m *Manifest) { m.CartSHA256 = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			_, a := staticFixture(t)
			m := a.Manifest
			change(&m)
			if _, err := NewAsset(m, a.Cart); err == nil {
				t.Fatal("accepted invalid static asset")
			}
		})
	}
}
func TestStaticCompositionCancellationAndLegacyRefusal(t *testing.T) {
	shell, _ := staticFixture(t)
	_, legacy := assetFixture(t)
	if _, err := ComposeStatic(shell, &legacy); err == nil {
		t.Fatal("CRAM asset entered static composition")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ComposeStaticContext(ctx, shell, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}
