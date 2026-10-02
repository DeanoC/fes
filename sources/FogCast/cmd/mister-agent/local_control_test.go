package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/misteross/expansion"
)

// cartridgeControl is the off-kit device seam. LoadCoreOwned still links the
// ROM and then asks this control to program the derived bitstream.
type cartridgeControl struct {
	t            *testing.T
	linkedCalls  int
	inspectCalls int
	loadCalls    int
	programmed   []byte
	link         corepackage.ROMLinkIdentity
}

func (c *cartridgeControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		Capabilities: misterruntime.Protocol2Capabilities{ROMLinking: 1},
	}, nil
}

func (c *cartridgeControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (c *cartridgeControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("development RBF load is not the cartridge path")
	return misterruntime.Protocol2Response{}, nil
}

func (c *cartridgeControl) LoadCore(context.Context, string, string) (misterruntime.Protocol2Response, error) {
	c.loadCalls++
	c.t.Fatal("bare load_core is not the cartridge path")
	return misterruntime.Protocol2Response{}, nil
}

func (c *cartridgeControl) InspectCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	c.inspectCalls++
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if inspection.PackageID != packageID {
		c.t.Fatalf("inspect id %s, staged %s", inspection.PackageID, packageID)
	}
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		InspectedPackage: &misterruntime.Protocol2Inspection{
			PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true,
		},
	}, nil
}

func (c *cartridgeControl) LoadROMLinkedCore(_ context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath string, link corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.linkedCalls++
	if root != "" || expansionPath != "" || payloadPath != "" || composition != nil {
		c.t.Fatalf("cartridge load gained library or expansion context: %q %q %q %#v", root, expansionPath, payloadPath, composition)
	}
	programmed, err := os.ReadFile(programmedPath)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	base, err := os.ReadFile(filepath.Join(path, "core.rbf"))
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if bytes.Equal(programmed, base) || link.ProgrammedSHA256 != fmt.Sprintf("%x", sha256.Sum256(programmed)) || link.ProgrammedSize != int64(len(programmed)) {
		c.t.Fatal("runtime did not derive a programmed bitstream from the cartridge")
	}
	c.programmed = append([]byte(nil), programmed...)
	c.link = link
	generation := uint64(1)
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test",
		Generation:   &generation,
		Capabilities: misterruntime.Protocol2Capabilities{ROMLinking: 1},
		ActivePackage: &misterruntime.Protocol2ActivePackage{
			PackageID: packageID,
			ROMLink:   &link,
		},
	}, nil
}

func readGzipFixture(t *testing.T, name string) []byte {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "..", "misteross", "expansion", "testdata", "rom", name+".gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// installFormat3Core writes the extracted kit layout: manifest.toml, core.rbf,
// and rom-map.json. The directory name is the package id.
func installFormat3Core(t *testing.T, root string) (string, []byte) {
	t.Helper()
	base := readGzipFixture(t, "blank.rbf")
	mapping := readGzipFixture(t, "map.json")
	rom := readGzipFixture(t, "ramp.rom")
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(fixture, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(fixture, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(manifest), "format = 2", "format = 3", 1)
	text = strings.Replace(text, `id = "fes.pong"`, `id = "fes.sms"`, 1)
	text = strings.ReplaceAll(text, fmt.Sprintf("%x", sha256.Sum256(old)), fmt.Sprintf("%x", sha256.Sum256(base)))
	text = strings.ReplaceAll(text, fmt.Sprintf("size = %d", len(old)), fmt.Sprintf("size = %d", len(base)))
	text += fmt.Sprintf("\n[rom]\nid = \"data-storm\"\nrole = \"cartridge\"\nsource_size = %d\nfile = \"rom-map.json\"\nsize = %d\nsha256 = \"%x\"\n", len(rom), len(mapping), sha256.Sum256(mapping))
	staging := filepath.Join(t.TempDir(), "staging")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{"manifest.toml": []byte(text), "core.rbf": base, "rom-map.json": mapping} {
		if err := os.WriteFile(filepath.Join(staging, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := corepackage.InspectPackage(staging)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Descriptor.Core.ID != "fes.sms" || inspection.Descriptor.Format != 3 || inspection.Descriptor.ROM == nil || inspection.Descriptor.ROM.Role != "cartridge" {
		t.Fatalf("fixture descriptor %#v", inspection.Descriptor)
	}
	install := filepath.Join(root, inspection.PackageID)
	if err := os.Rename(staging, install); err != nil {
		t.Fatal(err)
	}
	return install, rom
}

func TestLoadCartridgeProgramsInstalledCore(t *testing.T) {
	root := t.TempDir()
	install, rom := installFormat3Core(t, root)
	inspection, err := corepackage.InspectPackage(install)
	if err != nil {
		t.Fatal(err)
	}
	control := &cartridgeControl{t: t}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	program := &kitLocalProgram{}
	native := nativeLocalRuntime{runtime: runtime, program: program}
	t.Cleanup(func() {
		_ = native.Stop(context.Background(), context.Background())
	})

	if err = native.LoadCartridge(context.Background(), context.Background(), install, inspection.PackageID, rom); err != nil {
		t.Fatal(err)
	}
	if control.linkedCalls != 1 || control.loadCalls != 0 || control.inspectCalls != 1 || !program.active() {
		t.Fatalf("linked=%d loads=%d inspects=%d programmed=%v", control.linkedCalls, control.loadCalls, control.inspectCalls, program.active())
	}
	if control.link.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(rom)) || control.link.SourceSize != int64(len(rom)) {
		t.Fatalf("source identity %+v", control.link)
	}
	if control.link.ROMID != "data-storm" {
		t.Fatalf("rom id %s", control.link.ROMID)
	}
}

func TestLoadCartridgeReportsAMissingCore(t *testing.T) {
	control := &cartridgeControl{t: t}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	program := &kitLocalProgram{}
	native := nativeLocalRuntime{runtime: runtime, program: program}
	missing := filepath.Join(t.TempDir(), strings.Repeat("ab", 32))
	err := native.LoadCartridge(context.Background(), context.Background(), missing, strings.Repeat("ab", 32), []byte("cart"))
	if err == nil || !strings.Contains(err.Error(), "the core is not installed") || strings.Contains(err.Error(), "kit-local cartridge link is unavailable") {
		t.Fatalf("missing core error %v", err)
	}
	if control.linkedCalls != 0 || control.loadCalls != 0 || control.inspectCalls != 0 || program.active() {
		t.Fatalf("missing core dispatched linked=%d loads=%d inspects=%d programmed=%v", control.linkedCalls, control.loadCalls, control.inspectCalls, program.active())
	}

	root := t.TempDir()
	install, rom := installFormat3Core(t, root)
	err = native.LoadCartridge(context.Background(), context.Background(), install, strings.Repeat("cd", 32), rom)
	if err == nil || !strings.Contains(err.Error(), "the core is not installed") {
		t.Fatalf("wrong package error %v", err)
	}
	if control.linkedCalls != 0 || program.active() {
		t.Fatal("wrong package id programmed the core")
	}

	inspection, inspectErr := corepackage.InspectPackage(install)
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	err = native.LoadCartridge(context.Background(), context.Background(), install, inspection.PackageID, nil)
	if err == nil || err.Error() != "cartridge is empty" || control.linkedCalls != 0 {
		t.Fatalf("empty cartridge error %v calls %d", err, control.linkedCalls)
	}
}
