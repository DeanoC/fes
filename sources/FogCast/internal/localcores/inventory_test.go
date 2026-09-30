package localcores

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type coreOpts struct {
	rom          []byte
	romRole      string
	badROMHash   bool
	selectionSHA string
}

func installCore(t *testing.T, packages, selections, coreID, name, abi string, opts coreOpts) string {
	t.Helper()
	sum := sha256.Sum256([]byte(coreID))
	id := hex.EncodeToString(sum[:])
	payload := []byte("rbf:" + coreID)
	payloadSum := sha256.Sum256(payload)
	payloadSHA := hex.EncodeToString(payloadSum[:])
	dir := filepath.Join(packages, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = %q
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
`, coreID, name, len(payload), payloadSHA, abi)
	if opts.rom != nil {
		romSum := sha256.Sum256(opts.rom)
		romSHA := hex.EncodeToString(romSum[:])
		if opts.badROMHash {
			bad := sha256.Sum256([]byte("not-the-map"))
			romSHA = hex.EncodeToString(bad[:])
		}
		role := opts.romRole
		if role == "" {
			role = "firmware"
		}
		if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), opts.rom, 0o644); err != nil {
			t.Fatal(err)
		}
		manifest += fmt.Sprintf(`[rom]
id = "machine-rom"
role = %q
source_size = 1024
file = "rom-map.json"
size = %d
sha256 = %q
`, role, len(opts.rom), romSHA)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.rbf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	selectionSHA := payloadSHA
	if opts.selectionSHA != "" {
		selectionSHA = opts.selectionSHA
	}
	stem := coreID[len("fes."):]
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n",
		coreID, id, selectionSHA, dir)
	if err := os.WriteFile(filepath.Join(selections, "fes-"+stem+".package.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func byCore(cores []Core) map[string]Core {
	out := map[string]Core{}
	for _, core := range cores {
		out[core.CoreID] = core
	}
	return out
}

func TestReadInstalledCores(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "core-packages")
	selections := filepath.Join(root, "selections")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	rom := []byte(`{"format":1}` + "\n")
	menu := installCore(t, packages, selections, "fes.menu", "FES Menu", "fes.menu", coreOpts{})
	pong := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	zx81 := installCore(t, packages, selections, "fes.zx81", "ZX81  (sealed)", "fes.simple-computer", coreOpts{rom: rom})
	coleco := installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{})
	sms := installCore(t, packages, selections, "fes.sms", "FES Master System", "fes.simple-computer", coreOpts{})
	sg := installCore(t, packages, selections, "fes.sg1000", "FES SG-1000", "fes.simple-computer", coreOpts{})
	c64 := installCore(t, packages, selections, "fes.c64", "FES Commodore 64", "fes.computer", coreOpts{rom: rom, romRole: "firmware"})
	spectrum := installCore(t, packages, selections, "fes.spectrum", "FES ZX Spectrum", "fes.computer", coreOpts{rom: rom, romRole: "firmware"})
	wrong := sha256.Sum256([]byte("wrong"))
	mismatch := installCore(t, packages, selections, "fes.mismatch", "Mismatch", "fes.simple-game", coreOpts{selectionSHA: hex.EncodeToString(wrong[:])})

	got := byCore(ReadInstalledCores(selections, packages))
	if _, ok := got["fes.menu"]; ok {
		t.Fatal("fes.menu was listed")
	}
	if _, ok := got[mismatch]; ok {
		t.Fatal("sha mismatch was listed by package id key")
	}
	for _, id := range []string{menu, mismatch} {
		for _, core := range got {
			if core.PackageID == id {
				t.Fatalf("dropped package listed: %s %s", core.CoreID, id)
			}
		}
	}
	want := []struct {
		id, packageID, name, abi, needs, block string
		launchable                             bool
	}{
		{"fes.pong", pong, "FES Pong", "fes.simple-game", "none", "", true},
		{"fes.zx81", zx81, "ZX81  (sealed)", "fes.simple-computer", "firmware", "Needs firmware", false},
		{"fes.coleco", coleco, "FES Coleco", "fes.application", "media", "Needs a cartridge", false},
		{"fes.sms", sms, "FES Master System", "fes.simple-computer", "media", "Needs a cartridge", false},
		{"fes.sg1000", sg, "FES SG-1000", "fes.simple-computer", "media", "Needs a cartridge", false},
		{"fes.c64", c64, "FES Commodore 64", "fes.computer", "firmware", "Needs firmware", false},
		{"fes.spectrum", spectrum, "FES ZX Spectrum", "fes.computer", "firmware", "Needs firmware", false},
	}
	if len(got) != len(want) {
		t.Fatalf("cores %d: %+v", len(got), got)
	}
	for _, item := range want {
		core, ok := got[item.id]
		if !ok {
			t.Fatalf("missing %s", item.id)
		}
		if core.PackageID != item.packageID || core.Name != item.name || core.ABI != item.abi ||
			core.Needs != item.needs || core.Block != item.block || core.Launchable != item.launchable {
			t.Fatalf("%s = %+v", item.id, core)
		}
	}
}

func TestZX81UnverifiedROMNeedsFirmware(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.zx81", "ZX81", "fes.simple-computer", coreOpts{
		rom:        []byte(`{"format":1}` + "\n"),
		badROMHash: true,
	})
	got := ReadInstalledCores(selections, packages)
	if len(got) != 1 || got[0].Needs != "firmware" || got[0].Block != "Needs firmware" || got[0].Launchable {
		t.Fatalf("%+v", got)
	}
}

func TestC64WithoutFirmwareIsLaunchable(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installCore(t, packages, selections, "fes.c64", "FES Commodore 64", "fes.computer", coreOpts{})
	installCore(t, packages, selections, "fes.spectrum", "FES ZX Spectrum", "fes.computer", coreOpts{})
	got := byCore(ReadInstalledCores(selections, packages))
	for _, id := range []string{"fes.c64", "fes.spectrum"} {
		core, ok := got[id]
		if !ok || core.Needs != "none" || !core.Launchable || core.Block != "" {
			t.Fatalf("%s %+v", id, core)
		}
	}
}
