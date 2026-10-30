package localcores

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
	"github.com/pelletier/go-toml/v2"
)

func TestPCFloppyStartsEmptyOnlyWithExtendedComputerABI(t *testing.T) {
	iface := corepackage.Interface{ID: protocol.PcxtFloppyInterface().ID, Major: 1, Required: true}
	for _, tc := range []struct {
		minor int64
		want  bool
	}{{0, false}, {1, true}, {2, false}} {
		d := corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.computer", Major: 1, Minor: tc.minor}}
		if computerEmptyStartup(d, iface) != tc.want {
			t.Fatalf("computer 1.%d", tc.minor)
		}
	}
}

type coreOpts struct {
	rom          []byte
	romRole      string
	badROMHash   bool
	selectionSHA string
	cartridge    bool
	interfaces   []corepackage.Interface
}

func installCore(t *testing.T, packages, selections, coreID, name, abi string, opts coreOpts) string {
	t.Helper()
	payload := []byte("rbf:" + coreID)
	payloadSum := sha256.Sum256(payload)
	payloadSHA := hex.EncodeToString(payloadSum[:])
	dir := t.TempDir()
	format := 2
	role := opts.romRole
	if opts.rom != nil || opts.cartridge || role != "" {
		format = 3
		if role == "" {
			role = "firmware"
			if opts.cartridge {
				role = "cartridge"
			}
		}
	}
	manifest := fmt.Sprintf(`format = %d
[core]
id = %q
name = %q
description = "Synthetic local control test fixture"
version = "1.0.0"
[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/localcores"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "test fixture"
`, format, coreID, name, len(payload), payloadSHA, abi)
	interfaces := opts.interfaces
	if len(interfaces) == 0 {
		interfaces = []corepackage.Interface{{ID: "fes.video.fixed-720p60", Major: 1, Required: true}}
	}
	for _, iface := range interfaces {
		manifest += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = %d\nminor = %d\nrequired = %t\n", iface.ID, iface.Major, iface.Minor, iface.Required)
	}
	if format == 3 {
		// Bind a structurally valid test ROM map to these payload bytes. The marker
		// opts.rom selects a firmware requirement, not a physical bitstream fixture.
		file, err := os.Open("../../../misteross/expansion/testdata/rom/map.json.gz")
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		var mapping expansion.ROMMap
		if err := json.NewDecoder(reader).Decode(&mapping); err != nil {
			t.Fatal(err)
		}
		reader.Close()
		file.Close()
		mapping.BaseSHA256 = payloadSHA
		mapping.SourceSize = 1024
		mapping.Blocks = mapping.Blocks[:1]
		mapping.Blocks[0].SourceOffset = 0
		data, err := json.Marshal(mapping)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		manifest += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = %q\nsource_size = 1024\nfile = \"rom-map.json\"\nsize = %d\nsha256 = %q\n", role, len(data), fmt.Sprintf("%x", sha256.Sum256(data)))
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.rbf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := inspection.PackageID
	installed := filepath.Join(packages, id)
	if err := os.Rename(dir, installed); err != nil {
		t.Fatal(err)
	}
	dir = installed
	if opts.badROMHash {
		if err := os.WriteFile(filepath.Join(dir, "rom-map.json"), []byte("corrupt"), 0o644); err != nil {
			t.Fatal(err)
		}
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

func installSizedCore(t *testing.T, packages, selections, coreID, name, abi string, payloadBytes, romBytes int64, role string) string {
	t.Helper()
	dir := t.TempDir()
	payloadSHA := writeZeros(t, filepath.Join(dir, "core.rbf"), payloadBytes)
	format := 2
	if romBytes > 0 {
		format = 3
		if role == "" {
			role = "firmware"
		}
	}
	manifest := fmt.Sprintf(`format = %d
[core]
id = %q
name = %q
description = "Synthetic local control test fixture"
version = "1.0.0"
[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/localcores"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "test fixture"
[[interfaces]]
id = "fes.video.fixed-720p60"
major = 1
minor = 0
required = true
`, format, coreID, name, payloadBytes, payloadSHA, abi)
	if format == 3 {
		romSHA := writeZeros(t, filepath.Join(dir, "rom-map.json"), romBytes)
		manifest += fmt.Sprintf("\n[rom]\nid = \"machine-rom\"\nrole = %q\nsource_size = 1024\nfile = \"rom-map.json\"\nsize = %d\nsha256 = %q\n", role, romBytes, romSHA)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	id := directoryPackageID(t, dir)
	listed, err := corepackage.InspectListing(context.Background(), dir)
	if err == nil && listed.PackageID != id {
		t.Fatalf("package id %s, listing %s", id, listed.PackageID)
	}
	if err != nil && format == 2 {
		t.Fatal(err)
	}
	installed := filepath.Join(packages, id)
	if err := os.Rename(dir, installed); err != nil {
		t.Fatal(err)
	}
	stem := coreID[len("fes."):]
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n",
		coreID, id, payloadSHA, installed)
	if err := os.WriteFile(filepath.Join(selections, "fes-"+stem+".package.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

// directoryPackageID is the package identity InspectListing hashes, streamed
// so a multi-megabyte member is not buffered. Callers use it for a map whose
// digest matches but whose structure listing must reject.
func directoryPackageID(t *testing.T, dir string) string {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Format int64 `toml:"format"`
	}
	if err := toml.Unmarshal(manifest, &header); err != nil {
		t.Fatal(err)
	}
	domain := "FES-CORE-PACKAGE-2\n"
	switch header.Format {
	case 3:
		domain = "FES-CORE-PACKAGE-3\n"
	case 4:
		domain = "FES-CORE-PACKAGE-4\n"
	}
	hash := sha256.New()
	if _, err := hash.Write([]byte(domain)); err != nil {
		t.Fatal(err)
	}
	writeMember := func(data []byte, path string) {
		t.Helper()
		var length [8]byte
		var size uint64
		var reader io.Reader
		if data != nil {
			size = uint64(len(data))
			reader = bytes.NewReader(data)
		} else {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			size = uint64(info.Size())
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			reader = file
		}
		binary.LittleEndian.PutUint64(length[:], size)
		if _, err := hash.Write(length[:]); err != nil {
			t.Fatal(err)
		}
		n, err := io.Copy(hash, reader)
		if err != nil || uint64(n) != size {
			t.Fatalf("hash %s: %v", path, err)
		}
	}
	writeMember(manifest, "")
	writeMember(nil, filepath.Join(dir, "core.rbf"))
	if header.Format == 3 || header.Format == 4 {
		writeMember(nil, filepath.Join(dir, "rom-map.json"))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeZeros(t *testing.T, path string, n int64) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.TeeReader(io.LimitReader(zeroReader{}, n), file)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func byCore(cores []Core) map[string]Core {
	out := map[string]Core{}
	for _, core := range cores {
		out[core.CoreID] = core
	}
	return out
}

func TestListingInspectionMatchesFullPackage(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "core-packages")
	selections := filepath.Join(root, "selections")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	rom := []byte(`{"format":1}` + "\n")
	for _, item := range []struct {
		id, name, abi string
		opts          coreOpts
	}{
		{id: "fes.pong", name: "FES Pong", abi: "fes.simple-game"},
		{id: "fes.sms", name: "FES Master System", abi: "fes.simple-computer", opts: coreOpts{cartridge: true}},
		{id: "fes.zx81", name: "ZX81", abi: "fes.simple-computer", opts: coreOpts{rom: rom}},
	} {
		packageID := installCore(t, packages, selections, item.id, item.name, item.abi, item.opts)
		dir := filepath.Join(packages, packageID)
		full, err := corepackage.InspectPackage(dir)
		if err != nil {
			t.Fatal(err)
		}
		listed, err := corepackage.InspectListing(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if listed.PackageID != full.PackageID || listed.Descriptor.Format != full.Descriptor.Format || listed.Descriptor.Core.ID != full.Descriptor.Core.ID || listed.Descriptor.Core.Name != full.Descriptor.Core.Name || listed.Descriptor.Payload != full.Descriptor.Payload {
			t.Fatalf("%s listing id %s format %d payload %+v; full id %s format %d payload %+v", item.id, listed.PackageID, listed.Descriptor.Format, listed.Descriptor.Payload, full.PackageID, full.Descriptor.Format, full.Descriptor.Payload)
		}
		switch {
		case listed.Descriptor.ROM == nil && full.Descriptor.ROM == nil:
		case listed.Descriptor.ROM == nil || full.Descriptor.ROM == nil || listed.Descriptor.ROM.SHA256 != full.Descriptor.ROM.SHA256 || listed.Descriptor.ROM.Role != full.Descriptor.ROM.Role:
			t.Fatalf("%s ROM listing %+v full %+v", item.id, listed.Descriptor.ROM, full.Descriptor.ROM)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := corepackage.InspectListing(ctx, filepath.Join(packages, "missing")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled listing %v", err)
	}
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
	ramtest := installCore(t, packages, selections, "fes.ramtest", "FES RAM Tester", "fes.application", coreOpts{})
	pong := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	zx81 := installCore(t, packages, selections, "fes.zx81", "ZX81  (sealed)", "fes.simple-computer", coreOpts{rom: rom})
	coleco := installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{cartridge: true})
	sms := installCore(t, packages, selections, "fes.sms", "FES Master System", "fes.simple-computer", coreOpts{cartridge: true})
	sg := installCore(t, packages, selections, "fes.sg1000", "FES SG-1000", "fes.simple-computer", coreOpts{cartridge: true})
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
		{"fes.ramtest", ramtest, "FES RAM Tester", "fes.application", "none", "", true},
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

func TestMalformedDigestMatchingROMMapIsNotListed(t *testing.T) {
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	pong := installCore(t, packages, selections, "fes.pong", "FES Pong", "fes.simple-game", coreOpts{})
	if directoryPackageID(t, filepath.Join(packages, pong)) != pong {
		t.Fatal("directory identity drifted from InspectPackage")
	}
	validROM := installCore(t, packages, selections, "fes.coleco", "FES Coleco", "fes.application", coreOpts{cartridge: true})
	if directoryPackageID(t, filepath.Join(packages, validROM)) != validROM {
		t.Fatal("format-3 directory identity drifted from InspectPackage")
	}

	// 1024 zero bytes match the manifest digest and the package id, and fail
	// structural parsing. Listing and launch inspection must both reject it.
	id := installSizedCore(t, packages, selections, "fes.sms", "FES Master System", "fes.simple-computer", 64, 1024, "cartridge")
	dir := filepath.Join(packages, id)
	romMap, err := os.ReadFile(filepath.Join(dir, "rom-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(romMap) != 1024 || !bytes.Equal(romMap, make([]byte, 1024)) {
		t.Fatalf("map len %d", len(romMap))
	}
	sum := sha256.Sum256(romMap)
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte(hex.EncodeToString(sum[:]))) {
		t.Fatal("manifest digest does not match the zero map")
	}
	if _, err := corepackage.InspectPackage(dir); err == nil {
		t.Fatal("InspectPackage accepted a zero ROM map")
	}
	if _, err := corepackage.InspectListing(context.Background(), dir); err == nil {
		t.Fatal("InspectListing accepted a zero ROM map")
	}
	got := byCore(ReadInstalledCores(selections, packages))
	if _, ok := got["fes.sms"]; ok {
		t.Fatal("malformed digest-matching map was listed")
	}
	if got["fes.pong"].PackageID != pong || !got["fes.pong"].Launchable {
		t.Fatalf("pong %+v", got["fes.pong"])
	}
	if got["fes.coleco"].PackageID != validROM || got["fes.coleco"].Needs != "media" {
		t.Fatalf("coleco %+v", got["fes.coleco"])
	}
}

func TestUnverifiedROMIsOmitted(t *testing.T) {
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
	if len(got) != 0 {
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

func TestLaunchRequirementsComeFromDescriptor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		coreID     string
		format     int64
		abi        string
		abiMinor   int64
		role       string
		interfaces []corepackage.Interface
		needs      string
		block      string
		launchable bool
	}{
		{name: "new standalone application", format: 2, abi: "fes.application", needs: "none", launchable: true},
		{name: "renamed cartridge core", format: 3, abi: "fes.application", role: "cartridge", needs: "media"},
		{name: "firmware", format: 3, abi: "fes.simple-computer", role: "firmware", needs: "firmware"},
		{name: "multiple ROM sources", format: 4, abi: "fes.application", needs: "firmware"},
		{name: "native output is mandatory", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.fabric.video.native-pixels", Major: 1}}, needs: "parts"},
		{name: "cartridge cannot bypass native output", format: 3, abi: "fes.application", role: "cartridge", interfaces: []corepackage.Interface{{ID: "fes.fabric.video.native-pixels", Major: 1}}, needs: "parts"},
		{name: "blob requires host delivery", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "cartridge cannot substitute for required blob", format: 3, abi: "fes.application", role: "cartridge", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer empty drives", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "PC/XT fixed-video package", coreID: "fes.pcxt", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.video.fixed-720p60", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer spectrum tape starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.spectrum-tape", Major: 2, Required: true}}, needs: "none", launchable: true},
		{name: "computer c64 disk starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.c64-disk", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "format-2 ZX81 requires a local machine ROM splice", coreID: "fes.zx81", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.zx81-tape", Major: 1, Required: true}}, needs: "firmware", block: "Needs machine ROM"},
		{name: "computer st floppy starts empty", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.atari-st-floppy", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "computer st empty-drive extensions", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{
			{ID: "fes.media.atari-st-floppy", Major: 1, Required: true},
			{ID: "fes.media.atari-st-floppy-write", Major: 1, Required: true},
			{ID: "fes.media.atari-st-floppy-geometry", Major: 1, Required: true},
		}, needs: "none", launchable: true},
		{name: "computer blob is not an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer blob stream is not an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob-stream", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "computer blob stays blocked beside an empty drive", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{
			{ID: "fes.media.apple2-floppy", Major: 1, Required: true},
			{ID: "fes.media.blob", Major: 1, Required: true},
		}, needs: "unsupported"},
		{name: "future computer floppy is not empty startup", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 2, Required: true}}, needs: "unsupported"},
		{name: "extended computer retains existing empty drives", format: 2, abi: "fes.computer", abiMinor: 1, interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}, needs: "none", launchable: true},
		{name: "future computer abi does not inherit empty drives", format: 2, abi: "fes.computer", abiMinor: 2, interfaces: []corepackage.Interface{{ID: "fes.media.apple2-floppy", Major: 1, Required: true}}, needs: "unsupported"},
		{name: "optional computer blob", format: 2, abi: "fes.computer", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1}}, needs: "none", launchable: true},
		{name: "optional media", format: 2, abi: "fes.application", interfaces: []corepackage.Interface{{ID: "fes.media.blob", Major: 1}}, needs: "none", launchable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coreID := tc.coreID
			if coreID == "" {
				coreID = "fes.new-core"
			}
			d := corepackage.Descriptor{Format: tc.format, Core: corepackage.Core{ID: coreID, Name: "New core"}, Target: corepackage.Target{ProgrammingProfile: "fes-gp-v1"}, ABI: corepackage.Contract{ID: tc.abi, Major: 1, Minor: tc.abiMinor}, Interfaces: tc.interfaces}
			if tc.role != "" {
				d.ROM = &corepackage.ROM{Role: tc.role}
			}
			inspection := corepackage.Inspection{PackageID: fmt.Sprintf("%064x", 1), Descriptor: d}
			got := FromInspection(inspection, "/installed/package")
			if got.Needs != tc.needs || got.Launchable != tc.launchable || (got.Block == "") != tc.launchable || (tc.block != "" && got.Block != tc.block) || got.installPath != "/installed/package" || got.CoreID != d.Core.ID {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestInstalledIdentityAndHyphenatedCoreID(t *testing.T) {
	root := t.TempDir()
	packages, selections := filepath.Join(root, "pkgs"), filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(selections, 0o755); err != nil {
		t.Fatal(err)
	}
	id := installCore(t, packages, selections, "fes.atari-st", "ST", "fes.computer", coreOpts{})
	got := ReadInstalledCores(selections, packages)
	if len(got) != 1 || got[0].CoreID != "fes.atari-st" {
		t.Fatalf("hyphenated core missing: %+v", got)
	}
	path := filepath.Join(packages, id, "manifest.toml")
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A name change preserves payload SHA but changes package identity.
	manifest = bytes.Replace(manifest, []byte(`name = "ST"`), []byte(`name = "Changed"`), 1)
	if err := os.WriteFile(path, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadInstalledCores(selections, packages); len(got) != 0 {
		t.Fatalf("stale package identity admitted: %+v", got)
	}
}

func TestUnavailableCoreIsListedUntilInstalled(t *testing.T) {
	root := t.TempDir()
	packages, selections := filepath.Join(root, "pkgs"), filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	before := factoryFingerprint(selections, packages)
	if !strings.Contains(before, "unavailable absent\n") {
		t.Fatalf("fingerprint %q", before)
	}
	manifest := filepath.Join(root, "unavailable-cores.json")
	body := []byte(`{"version":1,"cores":[{"core_id":"fes.atari-st","name":"Atari ST","reason":"timing-not-met","seeds":[4],"figures":[]}]}` + "\n")
	if err := os.WriteFile(manifest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	after := factoryFingerprint(selections, packages)
	if after == before || !strings.Contains(after, "unavailable ") {
		t.Fatalf("fingerprint did not observe the manifest\nbefore %s\nafter %s", before, after)
	}
	got := ReadInstalledCores(selections, packages)
	if len(got) != 1 || got[0].CoreID != "fes.atari-st" || got[0].Name != "Atari ST" || got[0].Needs != "unavailable" || got[0].Launchable || got[0].Block != "Hardware unavailable" || got[0].PackageID != unavailablePackageID("fes.atari-st") {
		t.Fatalf("%+v", got)
	}
	if err := os.WriteFile(manifest, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cores := ReadInstalledCores(selections, packages); len(cores) != 0 {
		t.Fatalf("corrupt manifest changed the catalog: %+v", cores)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", manifest); err != nil {
		t.Fatal(err)
	}
	if cores := ReadInstalledCores(selections, packages); len(cores) != 0 {
		t.Fatalf("symlink manifest changed the catalog: %+v", cores)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	body = []byte(`{"version":2,"cores":[{"core_id":"fes.atari-st","name":"Atari ST"}]}` + "\n")
	if err := os.WriteFile(manifest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if cores := ReadInstalledCores(selections, packages); len(cores) != 0 {
		t.Fatalf("wrong version changed the catalog: %+v", cores)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	forged := []byte(`{"version":1,"cores":[{"core_id":"fes.pong","name":"Pong"},{"core_id":"fes.atari-st","name":"Atari ST","reason":"forged"}]}` + "\n")
	if err := os.WriteFile(manifest, forged, 0o644); err != nil {
		t.Fatal(err)
	}
	if cores := ReadInstalledCores(selections, packages); len(cores) != 0 {
		t.Fatalf("forged manifest changed the catalog: %+v", cores)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	installed := installCore(t, packages, selections, "fes.atari-st", "Atari ST", "fes.computer", coreOpts{})
	if err := os.WriteFile(manifest, []byte(`{"version":1,"cores":[{"core_id":"fes.atari-st","name":"Atari ST","reason":"timing-not-met"}]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = ReadInstalledCores(selections, packages)
	if len(got) != 1 || got[0].PackageID != installed || got[0].PackageID == unavailablePackageID("fes.atari-st") {
		t.Fatalf("installed package lost to the omission row: %+v", got)
	}
}

func requiredIfaces(ids ...string) []corepackage.Interface {
	out := make([]corepackage.Interface, len(ids))
	for i, id := range ids {
		out[i] = corepackage.Interface{ID: id, Major: 1, Required: true}
	}
	return out
}

// TestDescriptorInputsDistinguishesTypingKeyboardsFromJoypadMatrices covers
// the installed-package shapes for ZX81, Spectrum, ColecoVision, Master
// System and SG-1000. The Quartus ZX81 (build_fes_zx81.py) types on plain
// fes.keyboard and also requires the session-display plane. Spectrum types
// on fes.keyboard.hid. The console packages reuse plain fes.keyboard as a
// joystick, or declare gamepad ports instead.
func TestDescriptorInputsDistinguishesTypingKeyboardsFromJoypadMatrices(t *testing.T) {
	cart := &corepackage.ROM{Role: "cartridge"}
	for _, tc := range []struct {
		name string
		d    corepackage.Descriptor
		want string
	}{
		{
			name: "zx81",
			d: corepackage.Descriptor{Interfaces: requiredIfaces(
				"fes.keyboard", "fes.video.fixed-720p60", "fes.memory.hps-ddr",
				"fes.video.session-display", "fes.media.blob", "fes.audio.pcm-s16-stereo-48k",
			)},
			want: "keyboard",
		},
		{
			name: "spectrum",
			d: corepackage.Descriptor{Interfaces: requiredIfaces(
				"fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
				"fes.audio.pcm-s16-stereo-48k", "fes.media.spectrum-tape",
			)},
			want: "keyboard",
		},
		{
			name: "coleco",
			d: corepackage.Descriptor{
				ROM: cart,
				Interfaces: requiredIfaces(
					"fes.audio.pcm-s16-stereo-48k", "fes.gamepad.ports", "fes.keypad.ports",
					"fes.video.fixed-720p60",
				),
			},
			want: "",
		},
		{
			name: "coleco keyboard matrix",
			d:    corepackage.Descriptor{Interfaces: requiredIfaces("fes.keyboard", "fes.video.fixed-720p60")},
			want: "",
		},
		{
			name: "sms",
			d: corepackage.Descriptor{
				ROM:        cart,
				Interfaces: requiredIfaces("fes.keyboard", "fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k"),
			},
			want: "",
		},
		{
			name: "sg-1000",
			d: corepackage.Descriptor{
				ROM:        cart,
				Interfaces: requiredIfaces("fes.keyboard", "fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k"),
			},
			want: "",
		},
		{
			name: "sms bring-up",
			d: corepackage.Descriptor{Interfaces: requiredIfaces(
				"fes.keyboard", "fes.video.fixed-720p60", "fes.media.blob",
				"fes.media.blob-stream", "fes.audio.pcm-s16-stereo-48k",
			)},
			want: "",
		},
		{
			name: "sg-1000 bring-up",
			d: corepackage.Descriptor{Interfaces: requiredIfaces(
				"fes.keyboard", "fes.video.fixed-720p60", "fes.media.blob", "fes.audio.pcm-s16-stereo-48k",
			)},
			want: "",
		},
	} {
		got := strings.Join(descriptorInputs(tc.d), ",")
		if got != tc.want {
			t.Fatalf("%s: inputs %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDescriptorInputsNamesRequiredKeyboardAndMouse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		interfaces []corepackage.Interface
		want       string
	}{
		{"pad only", []corepackage.Interface{{ID: "fes.gamepad.ports", Major: 1, Required: true}}, ""},
		{"hid keyboard", []corepackage.Interface{{ID: "fes.keyboard.hid", Major: 1, Required: true}, {ID: "fes.gamepad.ports", Major: 1, Required: true}}, "keyboard"},
		{"keyboard and mouse", []corepackage.Interface{{ID: "fes.mouse.relative", Major: 1, Required: true}, {ID: "fes.keyboard.hid", Major: 1, Required: true}}, "keyboard,mouse"},
		// A matrix with no computer plane is the joystick, not a typing keyboard.
		{"simple-computer input matrix", []corepackage.Interface{{ID: "fes.keyboard", Major: 1, Required: true}, {ID: "fes.video.fixed-720p60", Major: 1, Required: true}}, ""},
		{"optional mouse", []corepackage.Interface{{ID: "fes.mouse.relative", Major: 1}}, ""},
		{"prefix only", []corepackage.Interface{{ID: "fes.keyboardish", Major: 1, Required: true}}, ""},
	} {
		got := strings.Join(descriptorInputs(corepackage.Descriptor{Interfaces: tc.interfaces}), ",")
		if got != tc.want {
			t.Fatalf("%s: inputs %q, want %q", tc.name, got, tc.want)
		}
	}
}
