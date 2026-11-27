package misterruntime_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// de25UploadInput is the shared mailbox-upload fixture rewritten into the
// descriptor the DE25 runtime admits for one standard machine, with a ROM of
// the sealed size. Nothing exports such a package yet.
func de25UploadInput(t *testing.T, core string, size int) corepackage.ROMInput {
	t.Helper()
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v3")
	read := func(parts ...string) []byte {
		raw, err := os.ReadFile(filepath.Join(append([]string{fixture}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	// The PC/XT's 68 KiB image has no shared fixture: it is the 64 KiB one
	// with the size, and the map's length and digest, rewritten.
	source := size
	if size == 73728 {
		source = 65536
	}
	text := string(read("manifests", fmt.Sprintf("valid-nano-upload-%d.toml", source)))
	mapping := read("maps", fmt.Sprintf("valid-nano-upload-%d.json", source))
	if source != size {
		before := fmt.Sprintf("\"source_size\":%d}\n", source)
		if !bytes.HasSuffix(mapping, []byte(before)) {
			t.Fatal("fixture map layout changed")
		}
		original := mapping
		mapping = append(bytes.TrimSuffix(append([]byte(nil), mapping...), []byte(before)), fmt.Sprintf("\"source_size\":%d}\n", size)...)
		for old, replacement := range map[string]string{
			fmt.Sprintf("source_size = %d\n", source):               fmt.Sprintf("source_size = %d\n", size),
			fmt.Sprintf("size = %d\n", len(original)):               fmt.Sprintf("size = %d\n", len(mapping)),
			fmt.Sprintf("sha256 = \"%x\"", sha256.Sum256(original)): fmt.Sprintf("sha256 = \"%x\"", sha256.Sum256(mapping)),
		} {
			if strings.Count(text, old) != 1 {
				t.Fatal("fixture manifest lacks", old)
			}
			text = strings.Replace(text, old, replacement, 1)
		}
	}
	start, end := strings.Index(text, "[[interfaces]]"), strings.Index(text, "[build]")
	if start < 0 || end < start {
		t.Fatal("fixture manifest layout changed")
	}
	interfaces := ""
	add := func(id string, major, minor int) {
		interfaces += fmt.Sprintf("[[interfaces]]\nid = %q\nmajor = %d\nminor = %d\nrequired = true\n\n", id, major, minor)
	}
	for _, id := range []string{"fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
		"fes.audio.pcm-s16-stereo-48k", "fes.video.session-display", "fes.fabric.video.display-plane"} {
		add(id, 1, 0)
	}
	add("fes.fabric.de25-scanout", 1, 3)
	if core == "fes.atari-st" {
		for _, id := range []string{"fes.mouse.relative", "fes.media.atari-st-floppy", "fes.media.atari-st-floppy-write",
			"fes.media.atari-st-floppy-geometry"} {
			add(id, 1, 0)
		}
	} else if core == "fes.pcxt-dos" {
		add("fes.media.pcxt-floppy", 1, 0)
		add("fes.media.pcxt-floppy-write", 1, 0)
	} else {
		add("fes.media.spectrum-tape", 2, 0)
	}
	text = text[:start] + interfaces + text[end:]
	if abi := corepackage.DE25ComputerABI(core); abi.Minor != 0 {
		text = strings.Replace(text, "id = \"fes.simple-game\"\nmajor = 1\nminor = 0\n", fmt.Sprintf("id = \"fes.simple-game\"\nmajor = 1\nminor = %d\n", abi.Minor), 1)
	}
	for old, replacement := range map[string]string{
		`id = "fes.pong"`:                   fmt.Sprintf("id = %q", core),
		`programming_profile = "fes-gp-v1"`: `programming_profile = "de25-linux-computer-v1"`,
		`id = "fes.simple-game"`:            `id = "fes.computer"`,
	} {
		if !strings.Contains(text, old) {
			t.Fatal("fixture manifest lacks", old)
		}
		text = strings.Replace(text, old, replacement, 1)
	}
	rom := make([]byte, size)
	for i := range rom {
		rom[i] = byte(i*13 + 7)
	}
	archive := appendROMMap(t, tarCoreArchive(t, []byte(text), read("payloads", "fes-fixture.rbf")), mapping)
	return corepackage.ROMInput{Package: archive, ROM: rom}
}

// firmwareTargetControl is a DE25 runtime: no linker, and a fes.computer ABI
// that uploads firmware when upload is set.
type firmwareTargetControl struct {
	packageControl
	t        *testing.T
	calls    int
	path     string
	root     string
	link     corepackage.ROMLinkIdentity
	initial  *misterruntime.InitialMediaRequest
	mismatch bool
}

func newFirmwareTargetControl(t *testing.T, upload bool, linking uint64) *firmwareTargetControl {
	capabilities := misterruntime.Protocol2Capabilities{ROMLinking: linking}
	if upload {
		capabilities.ABIs = []misterruntime.Protocol2ABI{{ID: "fes.computer", Major: 1,
			Interfaces: []misterruntime.Protocol2Interface{{ID: "fes.fabric.firmware-upload", Major: 1}}}}
	}
	return &firmwareTargetControl{t: t, packageControl: packageControl{status2: &misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test", Capabilities: capabilities}}}
}

func (c *firmwareTargetControl) LoadROMLinkedCore(context.Context, string, string, string, string, string, *expansion.Composition, string, corepackage.ROMLinkIdentity) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("upload package sent as a linked bitstream")
	return misterruntime.Protocol2Response{}, nil
}
func (c *firmwareTargetControl) InspectCoreData(_ context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	i, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return misterruntime.Protocol2Response{OK: true, CoreData: &protocol.CoreData{PackageID: id, CoreID: i.Descriptor.Core.ID, Mode: "volatile", Revision: "absent", PaddleSpeed: protocol.PaddleNormal}}, nil
}
func (c *firmwareTargetControl) UpdateCoreSettings(context.Context, string, string, string, string, protocol.PaddleSpeed) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("settings mutation during a firmware launch")
	return misterruntime.Protocol2Response{}, nil
}
func (c *firmwareTargetControl) LoadLibraryCore(context.Context, string, string, string) (misterruntime.Protocol2Response, error) {
	c.t.Fatal("library load without the firmware source")
	return misterruntime.Protocol2Response{}, nil
}

func (c *firmwareTargetControl) LoadFirmwareCore(ctx context.Context, path, id, root, firmwarePath string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest, _ string) (misterruntime.Protocol2Response, error) {
	c.calls++
	source, err := os.ReadFile(firmwarePath)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	base, err := os.ReadFile(filepath.Join(path, "core.rbf"))
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	// The source is the ROM itself and the receipt names the unpatched payload.
	if link.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) || link.SourceSize != int64(len(source)) ||
		link.ProgrammedSHA256 != fmt.Sprintf("%x", sha256.Sum256(base)) || link.ProgrammedSize != int64(len(base)) || link.Role != "firmware" {
		c.t.Fatal("firmware receipt does not bind the source to the unchanged payload")
	}
	if initial != nil {
		disk, err := os.ReadFile(initial.Path)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(disk)) != initial.BaseMediaID || initial.DataRoot != misterruntime.MediaDataRoot {
			c.t.Fatal("initial disk is not the sealed source", err)
		}
	}
	c.path, c.root, c.link, c.initial = firmwarePath, root, link, initial
	r, err := c.packageControl.LoadCore(ctx, path, id)
	if err != nil {
		return r, err
	}
	a := r.ActivePackage
	if c.mismatch {
		link.SourceSHA256 = strings.Repeat("0", 64)
	}
	a.ROMLink = &link
	// The runtime reports the package's own computer minor.
	a.Observed.ABI = &misterruntime.Protocol2Contract{ID: "fes.computer", Major: 1, Minor: uint16(a.Descriptor.ABI.Minor)}
	r.Capabilities.ActiveInterfaces = nil
	for _, i := range a.Descriptor.Interfaces {
		r.Capabilities.ActiveInterfaces = append(r.Capabilities.ActiveInterfaces, misterruntime.Protocol2Interface{ID: i.ID, Major: uint16(i.Major), Minor: uint16(i.Minor)})
	}
	sort.Slice(r.Capabilities.ActiveInterfaces, func(i, j int) bool {
		return r.Capabilities.ActiveInterfaces[i].ID < r.Capabilities.ActiveInterfaces[j].ID
	})
	r.Capabilities.ProgrammingProfiles = []string{a.Descriptor.Target.ProgrammingProfile}
	r.Capabilities.ABIs = []misterruntime.Protocol2ABI{{ID: "fes.computer", Major: 1, Minor: uint16(a.Descriptor.ABI.Minor), Interfaces: r.Capabilities.ActiveInterfaces}}
	if initial != nil {
		a.PersistenceMode = "persistent"
		floppy, bytes := protocol.AtariStFloppyInterface(), uint32(737280)
		if a.Descriptor.Core.ID == "fes.pcxt-dos" {
			floppy, bytes = protocol.PcxtFloppyInterface(), uint32(protocol.PcxtFloppyBytes)
		}
		r.Capabilities.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: floppy, MinBytes: bytes, MaxBytes: bytes, ChunkBytes: protocol.ComputerMediaChunkBytes, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Revision: "absent"}}}
	}
	c.status2 = &r
	return r, nil
}

// The kit stages the ROM source and hands it to the runtime with its receipt;
// nothing is linked and no programmed bitstream is made.
func TestFirmwareTargetSendsTheStagedSourceToTheRuntime(t *testing.T) {
	for _, machine := range []struct {
		core string
		size int
	}{{"fes.spectrum", 16384}, {"fes.spectrum", 32768}, {"fes.atari-st", 196608}} {
		t.Run(fmt.Sprint(machine.core, "-", machine.size), func(t *testing.T) {
			input := de25UploadInput(t, machine.core, machine.size)
			envelope, err := corepackage.WriteROMInput(input)
			if err != nil {
				t.Fatal(err)
			}
			control := newFirmwareTargetControl(t, true, 0)
			root := t.TempDir()
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
			active, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope))
			if apiErr != nil || !attempted || control.calls != 1 || control.root != "" || control.initial != nil ||
				active.ROMLink == nil || *active.ROMLink != control.link ||
				control.link.SourceSHA256 != fmt.Sprintf("%x", sha256.Sum256(input.ROM)) {
				t.Fatalf("activation=%#v attempted=%v error=%v calls=%d", active, attempted, apiErr, control.calls)
			}
			if filepath.Base(control.path) != "firmware.rom" {
				t.Fatal("runtime was not given the ROM source:", control.path)
			}
			if _, err := os.Stat(control.path); err != nil {
				t.Fatalf("active firmware source not retained: %v", err)
			}
			if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
				t.Fatal(apiErr)
			}
			if _, err := os.Stat(control.path); !os.IsNotExist(err) {
				t.Fatalf("stopped firmware source remains: %v", err)
			}
		})
	}
}

// A library ST launch carries its drive-A disk in the same atomic request.
func TestFirmwareTargetLibraryLaunchCarriesTheInitialDisk(t *testing.T) {
	input := de25UploadInput(t, "fes.atari-st", 196608)
	disk := bytes.Repeat([]byte{0xa5}, corepackage.InitialMediaBytes)
	input.InitialMedia = &corepackage.InitialMedia{GameID: "desk", BaseMediaID: fmt.Sprintf("%x", sha256.Sum256(disk)), Bytes: disk}
	transport, err := corepackage.PrepareROMInput(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.StageROMInputBytes(context.Background(), t.TempDir(), transport.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	control := newFirmwareTargetControl(t, true, 0)
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(t.TempDir()))
	defer runtime.Stop(context.Background())
	active, attempted, apiErr := runtime.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), transport.Size(), transport.Reader(), staged.PackageID)
	if apiErr != nil || !attempted || control.calls != 1 || control.root != misterruntime.CoreDataRoot || control.initial == nil ||
		control.initial.GameID != "desk" || control.initial.Size != corepackage.InitialMediaBytes || active.ROMLink == nil {
		t.Fatalf("activation=%#v attempted=%v error=%v calls=%d initial=%+v", active, attempted, apiErr, control.calls, control.initial)
	}
}

func TestFirmwareTargetRefusesBeforeReplacement(t *testing.T) {
	input := de25UploadInput(t, "fes.spectrum", 16384)
	envelope, err := corepackage.WriteROMInput(input)
	if err != nil {
		t.Fatal(err)
	}
	corrupted := bytes.Replace(envelope, []byte(fmt.Sprintf("%x", sha256.Sum256(input.ROM))), []byte(strings.Repeat("0", 64)), 1)
	if bytes.Equal(corrupted, envelope) {
		t.Fatal("source digest fixture not found")
	}
	for _, test := range []struct {
		name    string
		upload  bool
		linking uint64
		body    []byte
	}{
		{"runtime without the upload interface", false, 0, envelope},
		// A linker cannot serve an upload package: its payload is not patched.
		{"linker without the upload interface", false, 1, envelope},
		{"source digest mismatch", true, 0, corrupted},
		{"bare format 3", true, 0, input.Package},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := newFirmwareTargetControl(t, test.upload, test.linking)
			barrier := &replacementBarrier{}
			root := t.TempDir()
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(test.body)), bytes.NewReader(test.body))
			if apiErr == nil || attempted || control.calls != 0 || barrier.begins != 0 {
				t.Fatalf("attempted=%v error=%v calls=%d barriers=%d", attempted, apiErr, control.calls, barrier.begins)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatal("refused launch left staging:", len(entries))
			}
		})
	}
	// A daemon client with no firmware operation cannot take the package.
	control := newROMTargetControl(t, 0)
	control.status2.Capabilities.ABIs = newFirmwareTargetControl(t, true, 0).status2.Capabilities.ABIs
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	if _, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope)); apiErr == nil || attempted || control.linkedCalls != 0 {
		t.Fatal("upload package dispatched without a firmware operation", attempted, apiErr)
	}
}

func TestFirmwareTargetRejectsAnotherSourceInTheReply(t *testing.T) {
	envelope, err := corepackage.WriteROMInput(de25UploadInput(t, "fes.spectrum", 16384))
	if err != nil {
		t.Fatal(err)
	}
	control := newFirmwareTargetControl(t, true, 0)
	control.mismatch = true
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(t.TempDir()))
	defer runtime.Stop(context.Background())
	if _, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(envelope)), bytes.NewReader(envelope)); apiErr == nil || !attempted || control.calls != 1 {
		t.Fatal("reply naming another ROM confirmed", attempted, apiErr)
	}
}

// A library DE25 PC/XT launch carries its start disk beside its uploaded
// firmware in the same atomic request, and the kit confirms it.
func TestFirmwareTargetPcxtLaunchCarriesItsStartDisk(t *testing.T) {
	input := de25UploadInput(t, "fes.pcxt-dos", 73728)
	disk := bytes.Repeat([]byte{0xf6}, int(protocol.PcxtFloppyBytes))
	input.InitialMedia = &corepackage.InitialMedia{InterfaceID: protocol.PcxtFloppyInterface().ID, GameID: "freedos", BaseMediaID: fmt.Sprintf("%x", sha256.Sum256(disk)), Bytes: disk}
	transport, err := corepackage.PrepareROMInput(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.StageROMInputBytes(context.Background(), t.TempDir(), transport.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	control := newFirmwareTargetControl(t, true, 0)
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(t.TempDir()))
	defer runtime.Stop(context.Background())
	active, attempted, apiErr := runtime.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), transport.Size(), transport.Reader(), staged.PackageID)
	if apiErr != nil || !attempted || control.calls != 1 || control.initial == nil || control.initial.GameID != "freedos" ||
		control.initial.Size != uint32(protocol.PcxtFloppyBytes) || active.ROMLink == nil || active.PersistenceMode != "persistent" {
		t.Fatalf("activation=%#v attempted=%v error=%v calls=%d initial=%+v", active, attempted, apiErr, control.calls, control.initial)
	}
}
