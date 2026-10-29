package misterruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func corepackageDescriptor(id string, minor int64) corepackage.Descriptor {
	return corepackage.Descriptor{ABI: corepackage.Contract{ID: id, Major: 1, Minor: minor}}
}

const machineCoreData = `,"core_data":{"package_id":"PKG","core_id":"fes.pc386","layout":{"id":"fes.pc386.machine","major":1,"minor":0},"mode":"persistent","revision":"absent","game_id":"ultima-vi","machine":{"preset":3,"frequency":0,"code_profile":1,"turbo":0,"sb_base":576,"sb_irq":5,"sb_dma":1}}}`

func machineDataResponse(t *testing.T) string {
	t.Helper()
	base := fixtureLines(t, "protocol-v2.jsonl")[1]
	return strings.TrimSuffix(base, "}") + strings.Replace(machineCoreData, "PKG", fixturePackageID, 1)
}

// The runtime's pc386 record names its game and settings and has no Pong fields.
func TestMachineCoreDataDecodes(t *testing.T) {
	response, err := decodeProtocol2Response([]byte(machineDataResponse(t)))
	if err != nil {
		t.Fatal(err)
	}
	d := response.CoreData
	if d == nil || d.GameID != "ultima-vi" || d.Machine == nil || *d.Machine != (protocol.MachineSettings{Preset: 3, CodeProfile: 1, SBBase: 0x240, SBIRQ: 5, SBDMA: 1}) {
		t.Fatalf("decoded %+v", d)
	}
	for _, pair := range [][2]string{
		{`"sb_irq":5`, `"sb_irq":9`},
		{`"preset":3`, `"preset":8`},
		{`"frequency":0`, `"frequency":250,"extra":1`},
		{`"game_id":"ultima-vi"`, `"game_id":"Ultima VI"`},
		{`"id":"fes.pc386.machine"`, `"id":"fes.pong.progress"`},
		{`"mode":"persistent"`, `"mode":"volatile"`},
		{`"game_id":"ultima-vi",`, `"game_id":"ultima-vi","paddle_speed":1,`},
	} {
		if _, err := decodeProtocol2Response([]byte(strings.Replace(machineDataResponse(t), pair[0], pair[1], 1))); err == nil {
			t.Errorf("accepted %s", pair[1])
		}
	}
}

// The runtime's own pc386 response bytes (COMPONENT_FIXTURES copy) decode:
// a missing record's defaults, a stored record, and a running disk unit. The
// fourth line, an empty fixed disk, is TestPc386GoldenDiskStatuses's.
func TestRuntimePc386ResponsesDecode(t *testing.T) {
	lines := fixtureLines(t, "protocol-v2-pc386-responses.jsonl")
	if len(lines) != 4 {
		t.Fatalf("fixture has %d lines", len(lines))
	}
	layout := protocol.Pc386MachineInterface()
	for i, want := range []struct {
		revision string
		machine  protocol.MachineSettings
	}{
		{"absent", protocol.DefaultMachineSettings()},
		{strings.Repeat("b", 64), protocol.MachineSettings{Preset: 3, Frequency: 250, CodeProfile: 1, SBBase: 0x240, SBIRQ: 5, SBDMA: 1}},
	} {
		response, err := decodeProtocol2Response([]byte(lines[i]))
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		d := response.CoreData
		if d == nil || d.CoreID != "fes.pc386" || d.Mode != "persistent" || d.Revision != want.revision || d.GameID != "ultima-vi" ||
			d.Layout == nil || *d.Layout != layout || d.Machine == nil || *d.Machine != want.machine || d.PaddleSpeed != 0 || d.BestRally != 0 || !d.Valid() {
			t.Fatalf("line %d core_data %+v", i+1, d)
		}
	}
	response, err := decodeProtocol2Response([]byte(lines[2]))
	if err != nil {
		t.Fatalf("line 3: %v", err)
	}
	if response.State != "running_development" || response.ActivePackage == nil || response.ActivePackage.PersistenceMode != "persistent" {
		t.Fatalf("line 3 active %+v", response.ActivePackage)
	}
	units := response.Capabilities.MediaUnits
	if len(units) != 1 {
		t.Fatalf("units %+v", units)
	}
	u := units[0]
	if u.Unit != 0 || u.Interface != protocol.AtaDiskInterface() || u.MinBytes != 1<<20 || u.MaxBytes != 128<<20 || u.ChunkBytes != 65536 ||
		u.State != protocol.MediaUnitReady || u.Persistence == nil || u.Persistence.GameID != "ultima-vi" || u.Persistence.BaseMediaID != strings.Repeat("c", 64) {
		t.Fatalf("ata unit %+v", u)
	}
	status := computerMediaStatus(response).CorePackage
	if got, ok := protocol.MediaUnit(status, 0); !ok || got.Interface != protocol.AtaDiskInterface() || got.CanSave() ||
		!protocol.PersistentMediaBound(status) || protocol.MediaDataBound(status) {
		t.Fatalf("status unit %+v %v", got, ok)
	}
}

// The runtime advertises fes.computer at 1.0 and 1.1; each package matches
// the entry of its own minor exactly.
func TestComputerAdvertisedAtTwoMinors(t *testing.T) {
	base := fixtureLines(t, "protocol-v2.jsonl")[1]
	computer := func(minor int) string {
		return `{"id":"fes.computer","major":1,"minor":` + string(rune('0'+minor)) + `,"interfaces":[{"id":"fes.video.fixed-720p60","major":1,"minor":0}]}`
	}
	with := func(abis ...string) string {
		return strings.Replace(base, `"abis":[`, `"abis":[`+strings.Join(abis, ",")+",", 1)
	}
	response, err := decodeProtocol2Response([]byte(with(computer(0), computer(1))))
	if err != nil {
		t.Fatal(err)
	}
	for minor := int64(0); minor <= 1; minor++ {
		abi := matchingABI(response.Capabilities, corepackageDescriptor("fes.computer", minor))
		if abi == nil || int64(abi.Minor) != minor {
			t.Fatalf("minor %d matched %+v", minor, abi)
		}
	}
	for name, line := range map[string]string{"duplicate": with(computer(1), computer(1)), "unsorted": with(computer(1), computer(0))} {
		if _, err := decodeProtocol2Response([]byte(line)); err == nil {
			t.Errorf("%s ABI list accepted", name)
		}
	}
}

func TestMachineRequestsCarryGameAndDisk(t *testing.T) {
	status := fixtureLines(t, "protocol-v2.jsonl")[1] + "\n"
	fixture := newSequenceSocketFixture(t, []string{machineDataResponse(t) + "\n", machineDataResponse(t) + "\n", status, ""})
	client := NewClient(fixture.path)
	ctx := context.Background()
	settings := protocol.MachineSettings{Preset: 3, CodeProfile: 1, SBBase: 0x240, SBIRQ: 5, SBDMA: 1}
	if _, err := client.InspectMachineCoreData(ctx, "/tmp/package", fixturePackageID, CoreDataRoot, "ultima-vi"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateMachineSettings(ctx, "/tmp/package", fixturePackageID, CoreDataRoot, "ultima-vi", "absent", settings); err != nil {
		t.Fatal(err)
	}
	disk := &ataDiskRequest{Path: DiskImageRoot + "/" + strings.Repeat("c", 64) + ".img", Size: 64 << 20, BaseMediaID: strings.Repeat("c", 64),
		DataRoot: MediaDataRoot, Cylinders: 130, Heads: 16, SectorsPerTrack: 63}
	ctxShort, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, _ = client.LoadLibraryMachineCore(ctxShort, "/tmp/package", fixturePackageID, CoreDataRoot, "ultima-vi", disk)
	requests := fixture.wait(t)
	if len(requests) < 4 {
		t.Fatalf("requests %v", requests)
	}
	var inspect, update, load map[string]any
	for i, target := range []*map[string]any{&inspect, &update, nil, &load} {
		if target != nil && json.Unmarshal([]byte(requests[i]), target) != nil {
			t.Fatal(requests[i])
		}
	}
	if inspect["operation"] != "inspect_core_data" || inspect["game_id"] != "ultima-vi" || inspect["data_root"] != CoreDataRoot {
		t.Fatalf("inspect %v", inspect)
	}
	machine, _ := update["machine"].(map[string]any)
	if update["operation"] != "update_core_settings" || update["game_id"] != "ultima-vi" || update["expected_revision"] != "absent" ||
		update["paddle_speed"] != nil || machine["sb_base"] != float64(0x240) || machine["sb_irq"] != float64(5) || len(machine) != 7 {
		t.Fatalf("update %v", update)
	}
	ata, _ := load["ata_disk"].(map[string]any)
	if load["operation"] != "load_library_core" || load["game_id"] != "ultima-vi" || ata["path"] != disk.Path || ata["size"] != float64(64<<20) ||
		ata["base_media_id"] != disk.BaseMediaID || ata["data_root"] != MediaDataRoot || ata["cylinders"] != float64(130) ||
		ata["heads"] != float64(16) || ata["sectors_per_track"] != float64(63) || len(ata) != 7 {
		t.Fatalf("load %v", load)
	}
	// A request the runtime would refuse is never sent.
	for _, bad := range []*ataDiskRequest{
		{Path: disk.Path, Size: 64<<20 + 1, BaseMediaID: disk.BaseMediaID, DataRoot: MediaDataRoot, Cylinders: 130, Heads: 16, SectorsPerTrack: 63},
		{Path: disk.Path, Size: 64 << 20, BaseMediaID: disk.BaseMediaID, DataRoot: MediaDataRoot, Cylinders: 131, Heads: 16, SectorsPerTrack: 63},
	} {
		if _, err := client.LoadLibraryMachineCore(ctx, "/tmp/package", fixturePackageID, CoreDataRoot, "ultima-vi", bad); err != errInvalidRuntimeRequest {
			t.Errorf("sent %+v: %v", bad, err)
		}
	}
	if _, err := client.InspectMachineCoreData(ctx, "/tmp/package", fixturePackageID, CoreDataRoot, ""); err != errInvalidRuntimeRequest {
		t.Error("inspected without a game")
	}
}

func TestDiskImageStoreVerifiesReusesAndEvicts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk-images")
	r := NewRuntime(nil, "", time.Millisecond, time.Second, WithDiskImageRoot(root))
	ctx := context.Background()
	image := func(fill byte) ([]byte, string) {
		data := bytes.Repeat([]byte{fill}, 1<<20)
		sum := sha256.Sum256(data)
		return data, hex.EncodeToString(sum[:])
	}
	data, sha := image(1)
	if status, err := r.DiskImage(ctx, sha); err != nil || status.Staged || status.SHA256 != sha {
		t.Fatalf("absent probe %+v %v", status, err)
	}
	if _, err := r.StageDiskImage(ctx, strings.Repeat("0", 64), int64(len(data)), bytes.NewReader(data)); err == nil || err.Code != protocol.CodeDigestMismatch {
		t.Fatalf("wrong digest staged: %v", err)
	}
	if _, err := r.StageDiskImage(ctx, sha, int64(len(data))-1, bytes.NewReader(data)); err == nil {
		t.Fatal("partial sector staged")
	}
	if status, err := r.StageDiskImage(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil || !status.Staged || status.Size != 1<<20 {
		t.Fatalf("stage %+v %v", status, err)
	}
	if status, err := r.DiskImage(ctx, sha); err != nil || !status.Staged || status.Size != 1<<20 {
		t.Fatalf("staged probe %+v %v", status, err)
	}
	binding := &protocol.LibraryMachineBinding{GameID: "ultima-vi", Disk: &protocol.StagedDisk{SHA256: sha, Geometry: protocol.DiskGeometry{Cylinders: 2, Heads: 16, SectorsPerTrack: 63}}}
	request, apiErr := r.resolveStagedDisk(binding)
	if apiErr != nil || request.Path != filepath.Join(root, sha+".img") || request.Size != 1<<20 || binding.Disk.Size != 1<<20 || request.DataRoot != MediaDataRoot {
		t.Fatalf("resolve %+v %v", request, apiErr)
	}
	binding.Disk.Geometry.Cylinders = 3 // 3 x 16 x 63 sectors exceed 1 MiB
	if _, apiErr := r.resolveStagedDisk(binding); apiErr == nil {
		t.Fatal("geometry larger than the staged image admitted")
	}
	// Staging more images keeps the most recently used ones.
	hourAgo := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(root, sha+".img"), hourAgo, hourAgo)
	var names []string
	for fill := byte(2); fill <= 6; fill++ {
		data, sha := image(fill)
		names = append(names, sha)
		if _, err := r.StageDiskImage(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Duration(10-fill) * time.Minute)
		_ = os.Chtimes(filepath.Join(root, sha+".img"), old, old)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != diskImagesKept {
		t.Fatalf("kept %d images", len(entries))
	}
	if _, ok := r.stagedDiskImage(names[len(names)-1]); !ok {
		t.Fatal("the image just staged was evicted")
	}
	if _, ok := r.stagedDiskImage(sha); ok {
		t.Fatal("the least recently used image was kept")
	}
}
