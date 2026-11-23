// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// Use a fixture bitstream with the producer's real PAL C64 firmware and disk
// declarations. A matching retained ROM receipt lets the selection exercise
// the loader's contract rather than fail on an invalid archive.
func c64PackageFixture(t *testing.T, loader *ProgramLoader, profile string, edit func(*uploadShellRecord)) (packagestore.Request, map[string][]byte) {
	t.Helper()
	fixtures := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	dir := t.TempDir()
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	payload := read("payloads/fes-fixture.rbf")
	oldMap := read("maps/valid-nano-upload-16384.json")
	romMap := bytes.Replace(oldMap, []byte("16384"), []byte("36864"), 1)
	manifest := strings.NewReplacer(
		"fes.pong", "fes.c64", "FES Pong", "Commodore 64",
		"fes-gp-v1", profile, "fes.simple-game", "fes.computer", "fes.gamepad\"", "fes.gamepad.ports\"",
		"machine-rom", "c64-firmware", "source_size = 16384", "source_size = 36864",
		fmt.Sprintf("%x", sha256.Sum256(oldMap)), fmt.Sprintf("%x", sha256.Sum256(romMap)),
	).Replace(string(read("manifests/valid-nano-upload-16384.toml"))) + `
[[interfaces]]
id = "fes.media.c64-disk"
major = 1
minor = 0
required = true
`
	for name, data := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": payload, "rom-map.json": romMap} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(t.Context(), int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	inspection := archive.Inspection()
	rom := bytes.Repeat([]byte{0x65, 0x10}, c64FirmwareBytes/2)
	linked, err := archive.LinkROMs(t.Context(), []corepackage.ROMSource{{ID: "c64-firmware", Role: "firmware", Data: rom}})
	if err != nil {
		t.Fatal(err)
	}

	// Reuse the existing valid shared-shell record shape with this archive's
	// own identity and receipt to test the Spectrum-profile negative control.
	_, _, metadata, _ := packageSpectrumFixture(t, loader, false)
	var record uploadShellRecord
	if err := json.Unmarshal(metadata, &record); err != nil {
		t.Fatal(err)
	}
	record.PackageID = inspection.PackageID
	record.ROMSourceSHA256 = fmt.Sprintf("%x", sha256.Sum256(rom))
	record.ROMReceiptSHA256 = linked.ReceiptSHA256()
	if edit != nil {
		edit(&record)
	}
	metadata = programJSON(t, record)
	files := map[string][]byte{"FES/C64.FCP": raw, "FES/C64.JSN": metadata, "FES/C64.ROM": rom}
	return packagestore.Request{
		Path: "FES/C64.FCP", PackageID: inspection.PackageID, ReceiptSHA256: linked.ReceiptSHA256(),
		MetadataPath: "FES/C64.JSN", MetadataSHA256: fmt.Sprintf("%x", sha256.Sum256(metadata)),
		Sources: []packagestore.Source{{ID: "c64-firmware", Role: "firmware", Path: "FES/C64.ROM", SHA256: fmt.Sprintf("%x", sha256.Sum256(rom))}},
	}, files
}

func retainC64Probe(t *testing.T, r *romProgramFixture, profile string, edit func(*uploadShellRecord)) *packagestore.Selection {
	t.Helper()
	request, files := c64PackageFixture(t, r.f.loader, profile, edit)
	for name, data := range files {
		r.f.files.files[name] = &fstest.MapFile{Data: data}
	}
	selection, err := r.f.store.Retain(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func TestSharedCoreShellPALC64AdmissionBoundary(t *testing.T) {
	r, _ := mixedMenuTest(t)
	loader, err := r.f.loader.WithSharedCoreShell()
	if err != nil {
		t.Fatal(err)
	}
	req, files, _, _ := packageSpectrumFixture(t, loader, false)
	for name, data := range files {
		r.f.files.files[name] = &fstest.MapFile{Data: data}
	}
	control, err := r.f.store.Retain(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	c64 := retainC64Probe(t, r, "de25-linux-computer-v1", nil)
	shell, err := loader.decodeUploadShell(c64.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	input, err := loader.admitUploadPackage(c64, shell)
	if err != nil || input.identity.CoreID != "fes.c64" || len(input.firmware) != c64FirmwareBytes {
		t.Fatalf("common package admission rejected C64 inputs: %v", err)
	}
	before := r.cpu.calls
	if plan, err := loader.Preflight(retainC64Probe(t, r, c64Profile, nil)); err != nil || plan.Identity().CoreID != "fes.c64" {
		t.Fatalf("native upload shell did not admit the C64 selection: %v", err)
	}
	if plan, err := r.f.loader.Preflight(retainC64Probe(t, r, c64Profile, nil)); err == nil || plan != nil {
		t.Fatal("C64 admitted without the shared shell")
	}
	if plan, err := loader.Preflight(control); err != nil || plan.Identity().CoreID != "fes.spectrum" {
		t.Fatalf("Spectrum control on frozen loader: %v", err)
	}
	for _, tc := range []struct{ name, profile, rejection string }{
		{"current producer", "de25-linux-computer-v1", "selected programming profile has no matching compiled platform"},
		{"Spectrum label", spectrum48Profile, "platform driver lacks required interface fes.media.c64-disk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := loader.Preflight(retainC64Probe(t, r, tc.profile, nil))
			if plan != nil || err == nil || !strings.Contains(err.Error(), tc.rejection) || r.cpu.calls != before {
				t.Fatalf("C64 preflight must stop before hardware calls: plan=%v, err=%v", plan, err)
			}
		})
	}
}

func TestSharedCoreShellUploadBindsInputsBeforeActivation(t *testing.T) {
	r, _ := mixedMenuTest(t)
	loader, err := r.f.loader.WithSharedCoreShell()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*uploadShellRecord)
	}{
		{"other ROM receipt", func(r *uploadShellRecord) { r.ROMReceiptSHA256 = strings.Repeat("1", 64) }},
		{"other ROM digest", func(r *uploadShellRecord) { r.ROMSourceSHA256 = strings.Repeat("2", 64) }},
		{"other configuration digest", func(r *uploadShellRecord) { r.CoreSHA384 = strings.Repeat("3", 96) }},
		{"other configuration chunk", func(r *uploadShellRecord) { r.ChunkSHA384[0] = strings.Repeat("4", 96) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selection := retainC64Probe(t, r, "de25-linux-computer-v1", tc.edit)
			shell, err := loader.decodeUploadShell(selection.Metadata())
			if err != nil {
				t.Fatal(err)
			}
			if pkg, err := loader.admitUploadPackage(selection, shell); err == nil || pkg != nil {
				t.Fatal("package with mismatched retained inputs was admitted", err)
			}
		})
	}
}

func TestSharedCoreShellPALC64RetainedROMBoundary(t *testing.T) {
	decoder := buildProgramDecoder(t)
	source, config := bindingFixture(t)
	probe := func(raw []byte, receipt string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "publication.bin")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(decoder, path, "owned-shared-shell", fmt.Sprint(source.Bridges.Generation),
			hex.EncodeToString(source.Digest[:]), fmt.Sprint(config.ChunkBytes)).CombinedOutput()
		if receipt != "" {
			if err != nil || !strings.HasSuffix(string(out), receipt) {
				t.Fatalf("known shared-shell role: %v %s", err, out)
			}
			return
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || string(out) != "owned ROM descriptor rejected\n" {
			t.Fatalf("expected secure configuration rejection: %v %s", err, out)
		}
	}
	control, _, err := encodeRoleProgramBinding(source, config, programRoleUploadShell, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe(control, "role=3 abi=10203044\n")

	// Supply a hypothetical new role directly to C to separate the secure
	// allowlist rejection from the caller's matching allowlist.
	const candidateRole programRole = 4
	if _, _, err := encodeRoleProgramBinding(source, config, candidateRole, nil); err == nil {
		t.Fatal("unknown role admitted by the Go publisher")
	}
	unknown := bytes.Clone(control)
	binary.LittleEndian.PutUint32(unknown[programRoleOffset:], uint32(candidateRole))
	probe(unknown, "")

	// The ZX81 computer role's actual retained-ROM descriptor only accepts
	// 4 or 8 KiB. Try the C64 size both through Go and directly through C.
	if _, _, err := encodeRoleProgramBinding(source, config, programRoleComputer, make([]byte, c64FirmwareBytes)); err == nil {
		t.Fatal("C64 firmware admitted by the retained ZX81 ROM publisher")
	}
	legacy, _, err := encodeRoleProgramBinding(source, config, programRoleComputer, make([]byte, 8192))
	if err != nil {
		t.Fatal(err)
	}
	probe(legacy, "role=2 abi=10203041\n")
	binary.LittleEndian.PutUint32(legacy[ProgramHeaderBytes+16:], c64FirmwareBytes)
	probe(legacy, "")
}
