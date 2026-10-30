package misterruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// machineLoadControl is a runtime that admits a pc386 package, reads its
// machine record from the golden fixture and answers the library load with
// a scripted refusal.
type machineLoadControl struct {
	pc386FixtureControl
	t        *testing.T
	refusal  Protocol2Error
	loads    int
	loadDisk *ataDiskRequest
}

func (c *machineLoadControl) InspectCore(_ context.Context, path, id string) (Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return Protocol2Response{}, err
	}
	layout := &protocol.RuntimeContract{ID: protocol.Pc386MachineInterface().ID, Major: 1}
	return Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		InspectedPackage: &Protocol2Inspection{PackageID: id, Descriptor: inspection.Descriptor, Compatible: true, PersistenceLayout: layout}}, nil
}
func (c *machineLoadControl) InspectMachineCoreData(_ context.Context, _, id, _, gameID string) (Protocol2Response, error) {
	response, err := decodeProtocol2Response([]byte(fixtureLines(c.t, "protocol-v2-pc386-responses.jsonl")[0]))
	if err != nil {
		return Protocol2Response{}, err
	}
	response.CoreData.PackageID, response.CoreData.GameID = id, gameID
	return response, nil
}
func (c *machineLoadControl) UpdateMachineSettings(context.Context, string, string, string, string, string, protocol.MachineSettings) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (c *machineLoadControl) InspectCoreData(context.Context, string, string, string) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (c *machineLoadControl) UpdateCoreSettings(context.Context, string, string, string, string, protocol.PaddleSpeed) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (c *machineLoadControl) LoadLibraryCore(context.Context, string, string, string) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (c *machineLoadControl) LoadCore(context.Context, string, string) (Protocol2Response, error) {
	return Protocol2Response{}, io.ErrUnexpectedEOF
}
func (c *machineLoadControl) LoadLibraryMachineCore(_ context.Context, _, _, _, _ string, disk *ataDiskRequest) (Protocol2Response, error) {
	c.loads++
	c.loadDisk = disk
	refusal := c.refusal
	return Protocol2Response{Protocol: 2, OK: false, State: "idle", Execution: "none", Version: "test", Error: &refusal}, nil
}

// A staged image that no longer matches its digest fails the runtime's base
// check. The agent removes it, so the next probe reports it missing and the
// host stages it again; any other refusal leaves the staged image alone.
func TestMachineLoadRemovesACorruptStagedDisk(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refusal Protocol2Error
		removed bool
	}{
		{"base mismatch", Protocol2Error{Code: "corrupt_data", Message: "ATA disk image differs from its base identity", Phase: "input"}, true},
		{"corrupt delta", Protocol2Error{Code: "corrupt_data", Message: "bad ATA delta", Phase: "media_data"}, false},
		{"io failure", Protocol2Error{Code: "io_failed", Message: "read failed", Phase: "input"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			images := filepath.Join(t.TempDir(), "disk-images")
			idle, err := decodeProtocol2Response([]byte(fixtureLines(t, "protocol-v2-pc386-responses.jsonl")[0]))
			if err != nil {
				t.Fatal(err)
			}
			idle.CoreData = nil
			control := &machineLoadControl{pc386FixtureControl: pc386FixtureControl{response: idle}, t: t, refusal: tc.refusal}
			r := NewRuntime(control, "", time.Millisecond, time.Second, WithCorePackageRoot(t.TempDir()), WithDiskImageRoot(images))
			image := bytes.Repeat([]byte{0xf6}, 1<<20)
			sum := sha256.Sum256(image)
			sha := hex.EncodeToString(sum[:])
			if _, err := r.StageDiskImage(ctx, sha, int64(len(image)), bytes.NewReader(image)); err != nil {
				t.Fatal(err)
			}
			archive := pc386FixtureArchive(t, protocol.PersistenceWordsInterface().ID, protocol.Pc386MachineInterface().ID)
			inspection, apiErr := r.InspectCore(ctx, int64(len(archive)), bytes.NewReader(archive))
			if apiErr != nil {
				t.Fatal(apiErr)
			}
			binding := protocol.LibraryMachineBinding{GameID: "ultima-vi", Disk: &protocol.StagedDisk{SHA256: sha,
				Geometry: protocol.DiskGeometry{Cylinders: 2, Heads: 16, SectorsPerTrack: 63}}}
			_, _, apiErr = r.LoadLibraryMachineCoreOwned(ctx, ctx, ctx, int64(len(archive)), bytes.NewReader(archive), inspection.PackageID, binding)
			if control.loads != 1 || control.loadDisk == nil || control.loadDisk.Path != filepath.Join(images, sha+".img") {
				t.Fatalf("load %d %+v %v", control.loads, control.loadDisk, apiErr)
			}
			if apiErr == nil || (apiErr.Message == protocol.StagedDiskCorruptMessage) != tc.removed {
				t.Fatalf("refusal %+v", apiErr)
			}
			_, statErr := os.Stat(filepath.Join(images, sha+".img"))
			if os.IsNotExist(statErr) != tc.removed {
				t.Fatalf("staged image removed=%v, want %v", os.IsNotExist(statErr), tc.removed)
			}
			if status, err := r.DiskImage(ctx, sha); err != nil || status.Staged == tc.removed {
				t.Fatalf("probe after the refusal %+v %v", status, err)
			}
		})
	}
}

// A full card must not fail every retry: staging sweeps unfinished uploads
// and evicts before it writes, so even a refused upload has made room.
func TestDiskImageStagingMakesRoomFirst(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disk-images")
	r := NewRuntime(nil, "", time.Millisecond, time.Second, WithDiskImageRoot(root))
	ctx := context.Background()
	var staged []string
	for fill := byte(1); fill <= diskImagesKept; fill++ {
		data := bytes.Repeat([]byte{fill}, 1<<20)
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		if _, err := r.StageDiskImage(ctx, sha, int64(len(data)), bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		used := time.Now().Add(-time.Duration(10-fill) * time.Minute)
		_ = os.Chtimes(filepath.Join(root, sha+".img"), used, used)
		staged = append(staged, sha)
	}
	leftover := filepath.Join(root, ".stage-crash")
	if err := os.WriteFile(leftover, bytes.Repeat([]byte{9}, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{9}, 1<<20)
	if _, err := r.StageDiskImage(ctx, strings.Repeat("0", 64), int64(len(data)), bytes.NewReader(data)); err == nil || err.Code != protocol.CodeDigestMismatch {
		t.Fatalf("wrong digest staged: %v", err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("an unfinished upload survived a later stage")
	}
	if _, ok := r.stagedDiskImage(staged[0]); ok {
		t.Fatal("the least recently used image was kept")
	}
	for _, sha := range staged[1:] {
		if _, ok := r.stagedDiskImage(sha); !ok {
			t.Fatal("a recent image was evicted")
		}
	}
}

func TestDiskImageFullCardIsAStorageError(t *testing.T) {
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skip("no /dev/full")
	}
	defer full.Close()
	w := &stageWriter{Writer: full}
	if _, err := io.Copy(w, bytes.NewReader(make([]byte, 4096))); err == nil || w.err == nil {
		t.Fatalf("write to a full device: %v", err)
	}
	if apiErr := diskImageWriteError(w.err); apiErr.Code != protocol.CodeCacheFull || apiErr.Message != protocol.DiskImageStorageFullMessage {
		t.Fatalf("full card reported as %+v", apiErr)
	}
	if apiErr := diskImageWriteError(os.ErrPermission); apiErr.Code != protocol.CodeInternal {
		t.Fatalf("other write failure reported as %+v", apiErr)
	}
}

// The runtime reports a pc386 package without a disk as persistent with no
// media units: its machine record is the persistence. The status is the
// runtime's golden empty-disk row with the package's own interfaces.
func TestMachineRecordAlonePersists(t *testing.T) {
	record := []string{"fes.video.fixed-720p60", "fes.memory.hps-ddr", protocol.PersistenceWordsInterface().ID, protocol.Pc386MachineInterface().ID}
	for _, tc := range []struct {
		name    string
		archive []byte
		valid   bool
	}{
		{"record without disk", pc386PackageArchive(t, record...), true},
		{"disk without its unit", pc386PackageArchive(t, append(record, protocol.AtaDiskInterface().ID)...), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			staged, err := corepackage.Stage(context.Background(), root, int64(len(tc.archive)), bytes.NewReader(tc.archive))
			if err != nil {
				t.Fatal(err)
			}
			defer staged.Cleanup()
			d := staged.Descriptor
			response, err := decodeProtocol2Response([]byte(fixtureLines(t, "protocol-v2-pc386-responses.jsonl")[3]))
			if err != nil {
				t.Fatal(err)
			}
			response.ActivePackage.PackageID, response.ActivePackage.Descriptor = staged.PackageID, d
			abi := matchingABI(response.Capabilities, d)
			if abi == nil {
				t.Fatal("the runtime advertises no ABI for the package")
			}
			response.Capabilities.ActiveInterfaces = interfaceIntersection(d, *abi)
			response.Capabilities.MediaUnits = nil
			if validActivePackage(*response.ActivePackage, response.Capabilities) != tc.valid || validMediaUnits(response) != tc.valid ||
				validProtocol2Response(response) != tc.valid {
				t.Fatalf("persistent status without media units valid=%v, want %v", !tc.valid, tc.valid)
			}
			if !tc.valid {
				return
			}
			control := pc386FixtureControl{response: response}
			status := NewRuntime(control, "", time.Millisecond, time.Second, WithCorePackageRoot(root)).reconcileProtocol2(context.Background(), control)
			if status.State != protocol.StateActive || status.CorePackage == nil || status.CorePackage.PersistenceMode != "persistent" ||
				len(status.CorePackage.MediaUnits) != 0 || protocol.PersistentMediaBound(status.CorePackage) {
				t.Fatalf("reconciled record-only status = %+v", status)
			}
			assertTargetClientAcceptsStatus(t, status)
		})
	}
}
