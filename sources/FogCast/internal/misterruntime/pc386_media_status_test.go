package misterruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type pc386FixtureControl struct{ response Protocol2Response }

func (c pc386FixtureControl) Protocol2Status(context.Context) (Protocol2Response, error) {
	return c.response, nil
}
func (pc386FixtureControl) Protocol2Stop(context.Context) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (pc386FixtureControl) Protocol2LoadDevelopmentRBF(context.Context, string) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}

func TestPc386GoldenDiskStatusesDecodeReconcileAndPassTargetClient(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-pc386-responses.jsonl")
	if len(lines) != 4 {
		t.Fatalf("pc386 fixture rows = %d", len(lines))
	}
	archive := pc386FixtureArchive(t)
	root := t.TempDir()
	staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()

	for _, test := range []struct {
		name  string
		row   int
		state string
		bound bool
	}{
		{name: "bound disk", row: 2, state: protocol.MediaUnitReady, bound: true},
		{name: "empty disk", row: 3, state: protocol.MediaUnitEmpty},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := decodeProtocol2Response([]byte(lines[test.row]))
			if err != nil {
				t.Fatalf("decode row %d: %v", test.row+1, err)
			}
			if !reflect.DeepEqual(response.ActivePackage.Descriptor, staged.Descriptor) {
				t.Fatal("pc386 fixture descriptor differs from the staged test package")
			}
			// The frozen protocol fixture uses a placeholder package ID. Rebind only
			// that synthetic identity so reconciliation can adopt the matching test
			// package and exercise the normal target status projection.
			response.ActivePackage.PackageID = staged.PackageID
			control := pc386FixtureControl{response: response}
			runtime := NewRuntime(control, "", time.Millisecond, time.Second, WithCorePackageRoot(root))
			status := runtime.reconcileProtocol2(context.Background(), control)
			if status.State != protocol.StateActive || status.CorePackage == nil ||
				status.CorePackage.PersistenceMode != "persistent" || len(status.CorePackage.MediaUnits) != 1 {
				t.Fatalf("reconciled pc386 status = %+v", status)
			}
			unit := status.CorePackage.MediaUnits[0]
			if unit.Interface != protocol.AtaDiskInterface() || unit.State != test.state || unit.CanSave() ||
				(unit.Persistence != nil) != test.bound || protocol.PersistentMediaBound(status.CorePackage) != test.bound {
				t.Fatalf("reconciled ATA unit = %+v", unit)
			}
			if protocol.MediaDataBound(status.CorePackage) {
				t.Fatal("fixed ATA binding was exposed as an Atari ST saveable binding")
			}
			assertTargetClientAcceptsStatus(t, status)
		})
	}
}

func TestPc386PersistentMachineRecordAllowsDisklessStatus(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-live-computer-statuses.jsonl")
	response, err := decodeProtocol2Response([]byte(lines[1]))
	if err != nil {
		t.Fatal(err)
	}
	active := response.ActivePackage
	if active == nil || active.PersistenceMode != "persistent" {
		t.Fatalf("active package = %+v", active)
	}
	dropMediaInterfaces := func(interfaces []corepackage.Interface) []corepackage.Interface {
		filtered := interfaces[:0]
		for _, item := range interfaces {
			if item.ID != protocol.AtaDiskInterface().ID && item.ID != "fes.memory.hps-ddr" {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}
	active.Descriptor.Interfaces = dropMediaInterfaces(active.Descriptor.Interfaces)
	response.Capabilities.ActiveInterfaces = slices.DeleteFunc(response.Capabilities.ActiveInterfaces,
		func(item Protocol2Interface) bool {
			return item.ID == protocol.AtaDiskInterface().ID || item.ID == "fes.memory.hps-ddr"
		})
	for index := range response.Capabilities.ABIs {
		abi := &response.Capabilities.ABIs[index]
		if abi.ID == active.Descriptor.ABI.ID && abi.Major == uint16(active.Descriptor.ABI.Major) &&
			abi.Minor == uint16(active.Descriptor.ABI.Minor) {
			filtered := abi.Interfaces[:0]
			for _, item := range abi.Interfaces {
				if item.ID != protocol.AtaDiskInterface().ID && item.ID != "fes.memory.hps-ddr" {
					filtered = append(filtered, item)
				}
			}
			abi.Interfaces = filtered
		}
	}
	response.Capabilities.MediaUnits = nil
	if !descriptorPersistentMachineData(active.Descriptor) || descriptorAtaDisk(active.Descriptor) {
		t.Fatal("diskless pc386 machine-data descriptor was not isolated")
	}
	if !validActivePackage(*active, response.Capabilities) {
		t.Fatal("persistent pc386 machine record without an ATA unit was rejected")
	}
}

func TestAtariStGoldenStatusWithMultipleComputerABIMinorsReconciles(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-computer-responses.jsonl")
	response, err := decodeProtocol2Response([]byte(lines[7]))
	if err != nil {
		t.Fatalf("decode Atari ST status: %v", err)
	}
	archive := atariStFixtureArchive(t)
	root := t.TempDir()
	staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if !reflect.DeepEqual(response.ActivePackage.Descriptor, staged.Descriptor) {
		t.Fatal("Atari ST fixture descriptor differs from the staged test package")
	}
	response.ActivePackage.PackageID = staged.PackageID
	control := pc386FixtureControl{response: response}
	runtime := NewRuntime(control, "", time.Millisecond, time.Second, WithCorePackageRoot(root))
	status := runtime.reconcileProtocol2(context.Background(), control)
	if status.State != protocol.StateActive || status.CorePackage == nil || status.CorePackage.CoreID != "fes.atari-st" ||
		status.CorePackage.PersistenceMode != "volatile" || len(status.CorePackage.MediaUnits) != 1 {
		t.Fatalf("reconciled Atari ST status = %+v", status)
	}
	unit := status.CorePackage.MediaUnits[0]
	if unit.Interface != protocol.AtariStFloppyInterface() || unit.State != protocol.MediaUnitEmpty || unit.Saveable == nil ||
		*unit.Saveable || unit.Persistence != nil || protocol.MediaDataBound(status.CorePackage) {
		t.Fatalf("reconciled Atari ST floppy = %+v", unit)
	}
	assertTargetClientAcceptsStatus(t, status)
}

func TestLiveRuntimeComputerStatusesDecodeReconcileAndPassTargetClient(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-live-computer-statuses.jsonl")
	if len(lines) != 3 {
		t.Fatalf("live runtime status fixture rows = %d", len(lines))
	}
	for _, test := range []struct {
		name       string
		row        int
		coreID     string
		mediaState string
		bound      bool
	}{
		{name: "Atari ST empty floppy", row: 0, coreID: "fes.atari-st", mediaState: protocol.MediaUnitEmpty},
		{name: "pc386 empty fixed disk", row: 1, coreID: "fes.pc386", mediaState: protocol.MediaUnitEmpty},
		{name: "pc386 bound fixed disk", row: 2, coreID: "fes.pc386", mediaState: protocol.MediaUnitReady, bound: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := decodeProtocol2Response([]byte(lines[test.row]))
			if err != nil {
				t.Fatalf("decode live runtime row %d: %v", test.row+1, err)
			}
			var computerVersions [][2]uint16
			for _, abi := range response.Capabilities.ABIs {
				if abi.ID == "fes.computer" {
					computerVersions = append(computerVersions, [2]uint16{abi.Major, abi.Minor})
				}
			}
			if !reflect.DeepEqual(computerVersions, [][2]uint16{{1, 0}, {1, 1}}) {
				t.Fatalf("live fes.computer ABI versions = %v", computerVersions)
			}
			archive := liveRuntimeFixtureArchive(t, test.coreID)
			root := t.TempDir()
			staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			defer staged.Cleanup()
			if !reflect.DeepEqual(response.ActivePackage.Descriptor, staged.Descriptor) {
				t.Fatalf("live %s descriptor differs from the staged test package: live=%+v staged=%+v",
					test.coreID, response.ActivePackage.Descriptor, staged.Descriptor)
			}
			response.ActivePackage.PackageID = staged.PackageID
			control := pc386FixtureControl{response: response}
			runtime := NewRuntime(control, "", time.Millisecond, time.Second, WithCorePackageRoot(root))
			status := runtime.reconcileProtocol2(context.Background(), control)
			if status.State != protocol.StateActive || status.CorePackage == nil ||
				status.CorePackage.CoreID != test.coreID || len(status.CorePackage.MediaUnits) != 1 {
				t.Fatalf("reconciled live status = %+v", status)
			}
			unit := status.CorePackage.MediaUnits[0]
			if unit.State != test.mediaState {
				t.Fatalf("reconciled live media unit = %+v", unit)
			}
			if test.coreID == "fes.pc386" {
				if status.CorePackage.PersistenceMode != "persistent" || unit.Interface != protocol.AtaDiskInterface() ||
					unit.Saveable == nil || *unit.Saveable || (unit.Persistence != nil) != test.bound ||
					protocol.PersistentMediaBound(status.CorePackage) != test.bound || protocol.MediaDataBound(status.CorePackage) {
					t.Fatalf("reconciled live pc386 status = %+v", status.CorePackage)
				}
			} else if status.CorePackage.PersistenceMode != "volatile" ||
				unit.Interface != protocol.AtariStFloppyInterface() || unit.Saveable != nil ||
				unit.Persistence != nil || protocol.MediaDataBound(status.CorePackage) {
				t.Fatalf("reconciled live Atari ST status = %+v", status.CorePackage)
			}
			assertTargetClientAcceptsStatus(t, status)
		})
	}
}

func liveRuntimeFixtureArchive(t *testing.T, coreID string) []byte {
	t.Helper()
	runtimeFixtures := filepath.Join("..", "..", "..", "libmister-runtime", "tests", "fixtures", "core-bundle-v2")
	manifestPath := filepath.Join(runtimeFixtures, "manifests", "valid-basic.toml")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(manifestBytes)
	payload, err := os.ReadFile(filepath.Join(runtimeFixtures, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	switch coreID {
	case "fes.pc386":
		manifest = strings.ReplaceAll(manifest, "fes.pong", "fes.pc386")
		manifest = strings.ReplaceAll(manifest,
			"id = \"fes.simple-game\"\nmajor = 1\nminor = 0",
			"id = \"fes.computer\"\nmajor = 1\nminor = 1")
		manifest = strings.ReplaceAll(manifest, "id = \"fes.gamepad\"", "id = \"fes.keyboard.hid\"")
		for _, id := range []string{"fes.audio.pcm-s16-stereo-48k", "fes.mouse.relative",
			"fes.persistence.words", "fes.pc386.machine", "fes.rtc.wall-clock",
			"fes.memory.hps-ddr", "fes.media.ata-disk"} {
			manifest += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = true\n", id)
		}
	case "fes.atari-st":
		manifest = strings.ReplaceAll(manifest, "fes.simple-game", "fes.computer")
		manifest = strings.ReplaceAll(manifest, "id = \"fes.gamepad\"", "id = \"fes.keyboard.hid\"")
		for _, id := range []string{"fes.gamepad.ports", "fes.audio.pcm-s16-stereo-48k", "fes.media.apple2-floppy"} {
			manifest += fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = true\n", id)
		}
		manifest += "\n[[interfaces]]\nid = \"fes.expansion.apple2-bus\"\nmajor = 1\nminor = 0\nrequired = false\n"
		manifest = strings.ReplaceAll(manifest, "fes.pong", "fes.atari-st")
		manifest = strings.ReplaceAll(manifest, "fes.media.apple2-floppy", "fes.media.atari-st-floppy")
		manifest = strings.ReplaceAll(manifest, "fes.expansion.apple2-bus", "fes.expansion.atari-st-bus")
		manifest += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
	default:
		t.Fatalf("unsupported live fixture core %q", coreID)
	}
	return pc386TarArchive([]byte(manifest), payload)
}

func atariStFixtureArchive(t *testing.T) []byte {
	t.Helper()
	payloadPath := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2", "payloads", "fes-fixture.rbf")
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest := []byte(fmt.Sprintf(`format = 2

[core]
id = "fes.atari-st"
name = "Synthetic FES Atari ST"
description = "Synthetic test-only core bundle fixture; never deploy."
version = "0.1.0"

[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"

[payload]
file = "core.rbf"
size = %d
sha256 = "%x"

[abi]
id = "fes.computer"
major = 1
minor = 0

[[interfaces]]
id = "fes.video.fixed-720p60"
major = 1
minor = 0
required = true

[[interfaces]]
id = "fes.keyboard.hid"
major = 1
minor = 0
required = true

[[interfaces]]
id = "fes.media.atari-st-floppy"
major = 1
minor = 0
required = true

[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/fes-atari-st"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture generator 1.0 (test-only)"
`, len(payload), digest))
	return pc386TarArchive(manifest, payload)
}

func assertTargetClientAcceptsStatus(t *testing.T, status protocol.Status) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/library/core/load" {
			t.Errorf("target request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := targetclient.NewClient(base, "test-token", server.Client())
	got, err := client.LoadLibraryCore(context.Background(), 1, strings.NewReader("x"), status.CorePackage.PackageID)
	if err != nil {
		t.Fatalf("targetclient rejected reconciled computer status: %v", err)
	}
	if got.CorePackage == nil || len(got.CorePackage.MediaUnits) != len(status.CorePackage.MediaUnits) ||
		(len(status.CorePackage.MediaUnits) > 0 && !reflect.DeepEqual(got.CorePackage.MediaUnits, status.CorePackage.MediaUnits)) {
		t.Fatalf("targetclient changed reconciled media status: got=%+v want=%+v", got.CorePackage, status.CorePackage)
	}
}

// pc386FixtureArchive is a synthetic fes.computer 1.1 pc386 package with the
// ATA disk; extra interface IDs are added as required version 1.0.
func pc386FixtureArchive(t *testing.T, extra ...string) []byte {
	return pc386PackageArchive(t, append([]string{"fes.video.fixed-720p60", "fes.memory.hps-ddr", "fes.media.ata-disk"}, extra...)...)
}

// pc386PackageArchive is a synthetic fes.computer 1.1 package declaring the
// interfaces, each required version 1.0.
func pc386PackageArchive(t *testing.T, interfaces ...string) []byte {
	t.Helper()
	payloadPath := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2", "payloads", "fes-fixture.rbf")
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	var blocks string
	for _, id := range interfaces {
		blocks += fmt.Sprintf("[[interfaces]]\nid = %q\nmajor = 1\nminor = 0\nrequired = true\n\n", id)
	}
	manifest := []byte(fmt.Sprintf(`format = 2

[core]
id = "fes.pc386"
name = "FES pc386"
description = "Synthetic test-only core bundle fixture; never deploy."
version = "0.1.0"

[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"

[payload]
file = "core.rbf"
size = %d
sha256 = "%x"

[abi]
id = "fes.computer"
major = 1
minor = 1

%s[build]
id = "0123456789abcdef0123456789abcdef"
repository = "https://example.invalid/fes-pc386"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture generator 1.0 (test-only)"
`, len(payload), digest, blocks))
	return pc386TarArchive(manifest, payload)
}

func pc386TarArchive(manifest, payload []byte) []byte {
	var output bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:108], "0000644\x00")
		copy(header[108:116], "0000000\x00")
		copy(header[116:124], "0000000\x00")
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(entry.data)))
		copy(header[136:148], "00000000000\x00")
		for i := 148; i < 156; i++ {
			header[i] = ' '
		}
		header[156] = '0'
		copy(header[257:263], "ustar\x00")
		copy(header[263:265], "00")
		checksum := 0
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", checksum))
		output.Write(header)
		output.Write(entry.data)
		output.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	output.Write(make([]byte, 1024))
	return output.Bytes()
}
