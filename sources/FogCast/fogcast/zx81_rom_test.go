package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type fakeROM struct {
	programmed []byte
	sha        string
}

func (f fakeROM) Link(ctx context.Context, base []byte) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(base) == 0 {
		return nil, "", errTestLink
	}
	return f.programmed, f.sha, nil
}

var errTestLink = bytes.ErrTooLarge

func TestDE25ZX81CatalogLaunchUsesEmbeddedMachineROM(t *testing.T) {
	ctx := context.Background()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifestBytes, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes = []byte(strings.NewReplacer(
		"fes.pong", "fes.zx81", "de10_nano", "de25_nano",
		"5CSEBA6U23I7", "A5EB013BB23BE4SCS", "fes-gp-v1", "de25-hps-first-v1",
		"fes.simple-game", "fes.computer",
		"[[interfaces]]\nid = \"fes.gamepad\"\nmajor = 1\nminor = 0\nrequired = true\n\n", "",
	).Replace(string(manifestBytes)))
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	raw := testCorePackageArchive(t, manifestBytes, payload)
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "ZX81")
	loadCalls := 0
	client.coreLoad = func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
		loadCalls++
		loaded, err := io.ReadAll(body)
		if err != nil || size != int64(len(raw)) || !bytes.Equal(loaded, raw) {
			t.Fatalf("embedded-ROM package changed: size=%d bytes=%d err=%v", size, len(loaded), err)
		}
		core := "fes.zx81"
		return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
			CorePackage: &protocol.CorePackageStatus{PackageID: inspection.PackageID, Generation: 4,
				ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, BuildID: inspection.Descriptor.Build.ID,
				PersistenceMode: "volatile"}}, nil
	}
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("linker-configured-%t", configured), func(t *testing.T) {
			if configured {
				// This linker returns an error for empty output. It must never be used.
				service.SetMachineROMLinker(fakeROM{})
			}
			if !service.shelfComputerLaunchReady(ctx, entry, inspection) {
				t.Fatal("embedded-ROM DE25 entry is blocked on the shelf")
			}
			response, err := service.Launch(ctx, entry.GameID, nil)
			if err != nil || response.Status.CorePackage == nil || response.Status.CorePackage.PackageID != inspection.PackageID {
				t.Fatalf("normal catalog launch = %+v, %v", response.Status, err)
			}
		})
	}
	if loadCalls != 2 {
		t.Fatalf("normal package loads = %d, want 2", loadCalls)
	}
}

func TestApplyZX81MachineROMWrapsTheSealedPackage(t *testing.T) {
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	for _, member := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		archive.Write(ustarHeader(member.name, int64(len(member.data))))
		archive.Write(member.data)
		archive.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	image := sha256.Sum256([]byte("basic"))
	service := &Service{machineROM: fakeROM{programmed: bytes.Repeat([]byte{0x44}, 32), sha: hex.EncodeToString(image[:])}}
	descriptor := corepackage.Descriptor{}
	descriptor.Core.ID = "fes.zx81"
	descriptor.Target.Platform = "de10_nano"
	body, sha, err := service.applyZX81MachineROM(context.Background(), descriptor, archive.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := corepackage.ReadRomInit(body)
	if err != nil {
		t.Fatal(err)
	}
	if sha != hex.EncodeToString(image[:]) || !bytes.Equal(got.Package, archive.Bytes()) || bytes.Equal(got.Programmed, payload) {
		t.Fatalf("sha %s package %v programmed changed %v", sha, bytes.Equal(got.Package, archive.Bytes()), !bytes.Equal(got.Programmed, payload))
	}
	other := descriptor
	other.Core.ID = "fes.coleco"
	unchanged, _, err := service.applyZX81MachineROM(context.Background(), other, archive.Bytes(), nil)
	if err != nil || unchanged != nil {
		t.Fatalf("other core = %d %v", len(unchanged), err)
	}
	plain := &Service{}
	unchanged, _, err = plain.applyZX81MachineROM(context.Background(), descriptor, archive.Bytes(), nil)
	var apiErr *protocol.APIError
	if unchanged != nil || !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest || apiErr.Message == "" {
		t.Fatalf("unconfigured zx81 = %d %v", len(unchanged), err)
	}
}

func TestPythonMachineROMStopsWhenTheLaunchIsCanceled(t *testing.T) {
	directory := t.TempDir()
	script := filepath.Join(directory, "sleep.py")
	image := filepath.Join(directory, "image.hex")
	if err := os.WriteFile(script, []byte("import time\ntime.sleep(30)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte("00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := (PythonMachineROM{Script: script, Image: image, MistralCV: "/usr/bin/true"}).Link(ctx, []byte{1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled link = %v", err)
	}
}

func TestInitializedLaunchKeepsCompositionAndImageDigest(t *testing.T) {
	entry := catalog.CoreEntry{CoreID: "fes.zx81", PackageID: "pkg"}
	body := []byte("envelope")
	plain := initializedLaunchSource(entry, body, nil)
	if plain.composition != nil || plain.entry == nil || plain.entry.PackageID != "pkg" {
		t.Fatalf("plain source = %#v", plain.composition)
	}
	bundle := &corepackage.CompositionBundle{Composition: expansion.Composition{ID: "composition"}}
	composed := initializedLaunchSource(entry, body, bundle)
	if composed.composition == nil || composed.composition.ID != "composition" {
		t.Fatalf("composed source = %#v", composed.composition)
	}
	status := retainImageSHA(protocol.Status{CorePackage: &protocol.CorePackageStatus{PackageID: "pkg"}}, "digest")
	if status.CorePackage.ImageSHA256 != "digest" {
		t.Fatalf("digest = %q", status.CorePackage.ImageSHA256)
	}
	if kept := retainImageSHA(protocol.Status{}, "digest"); kept.CorePackage != nil {
		t.Fatal("digest attached without a package")
	}
}

func ustarHeader(name string, size int64) []byte {
	header := make([]byte, 512)
	copy(header[0:100], name)
	copy(header[100:108], "0000644\x00")
	copy(header[108:116], "0000000\x00")
	copy(header[116:124], "0000000\x00")
	copy(header[124:136], fmt.Sprintf("%011o\x00", size))
	copy(header[136:148], "00000000000\x00")
	for index := 148; index < 156; index++ {
		header[index] = ' '
	}
	header[156] = '0'
	copy(header[257:263], "ustar\x00")
	copy(header[263:265], "00")
	checksum := 0
	for _, value := range header {
		checksum += int(value)
	}
	copy(header[148:156], fmt.Sprintf("%06o\x00 ", checksum))
	return header
}
