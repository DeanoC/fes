package fogcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type stagingClient struct {
	held     protocol.DiskImageStatus
	probeErr error
	uploaded int
}

func (c *stagingClient) DiskImage(ctx context.Context, sha string) (protocol.DiskImageStatus, error) {
	return c.held, c.probeErr
}
func (c *stagingClient) StageDiskImage(ctx context.Context, sha string, size int64, r io.Reader) (protocol.DiskImageStatus, error) {
	data, _ := io.ReadAll(r)
	c.uploaded = len(data)
	return protocol.DiskImageStatus{SHA256: sha, Size: int64(len(data)), Staged: true}, nil
}
func (c *stagingClient) LoadLibraryMachineCore(context.Context, int64, io.Reader, string, protocol.LibraryMachineBinding) (protocol.Status, error) {
	return protocol.Status{}, nil
}

// partitionedImage is a synthetic image whose one partition a tool wrote
// for 4 heads and 17 sectors: LBA 17 (0/1/1) to 2039 (29/3/17).
func partitionedImage(size int) []byte {
	data := make([]byte, size)
	entry := data[446:462]
	entry[1], entry[2], entry[3] = 1, 1, 0
	entry[4], entry[5], entry[6], entry[7] = 0x06, 3, 17, 29
	binary.LittleEndian.PutUint32(entry[8:], 17)
	binary.LittleEndian.PutUint32(entry[12:], 2023)
	data[510], data[511] = 0x55, 0xaa
	return data
}

// linuxFdiskImage is a 16 MiB image partitioned by util-linux fdisk, which
// writes 255 heads and 63 sectors: LBA 2048 (0/32/33) to 32767 (2/10/8).
func linuxFdiskImage() []byte {
	data := make([]byte, 16<<20)
	entry := data[446:462]
	entry[1], entry[2], entry[3] = 32, 33, 0
	entry[4], entry[5], entry[6], entry[7] = 0x06, 10, 8, 2
	binary.LittleEndian.PutUint32(entry[8:], 2048)
	binary.LittleEndian.PutUint32(entry[12:], 30720)
	data[510], data[511] = 0x55, 0xaa
	return data
}

func TestHardDiskImportReportsGeometry(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Service{catalog: store}
	image := partitionedImage(1 << 20)
	media, created, err := s.ImportHardDiskMedia(ctx, int64(len(image)), bytes.NewReader(image))
	if err != nil || !created || media.Geometry == nil || *media.Geometry != (protocol.DiskGeometry{Cylinders: 30, Heads: 4, SectorsPerTrack: 17}) || media.GeometryError != "" {
		t.Fatalf("import %+v %v %v", media, created, err)
	}
	// The same bytes again report the same geometry from the stream.
	if again, created, err := s.ImportHardDiskMedia(ctx, int64(len(image)), bytes.NewReader(image)); err != nil || created || again.Geometry == nil || *again.Geometry != *media.Geometry {
		t.Fatalf("reimport %+v %v %v", again, created, err)
	}
	// A table written for more heads than the disk reports is stored, says
	// why, and refuses the launch until the entry overrides it.
	linux := linuxFdiskImage()
	refused, _, err := s.ImportHardDiskMedia(ctx, int64(len(linux)), bytes.NewReader(linux))
	if err != nil || refused.Geometry != nil || !strings.Contains(refused.GeometryError, "255 heads and 63 sectors") {
		t.Fatalf("255-head import %+v %v", refused, err)
	}
	pkg := strings.Repeat("a", 64)
	entry, err := store.CreateCoreMediaEntry(ctx, "Linux disk", "fes.pc386", pkg, protocol.HardDiskRole, refused.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	var apiErr *protocol.APIError
	if source, err := s.readHardDisk(ctx, entry); source != nil || !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest || !strings.Contains(apiErr.Message, "255 heads") {
		t.Fatalf("255-head launch %+v %v", source, err)
	}
	override := protocol.DiskGeometry{Cylinders: 2, Heads: 16, SectorsPerTrack: 63}
	if entry, err = store.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, refused.MediaID, &override); err != nil {
		t.Fatal(err)
	}
	source, err := s.readHardDisk(ctx, entry)
	if err != nil || source.disk.Geometry != override {
		t.Fatalf("override launch %+v %v", source, err)
	}
	_ = source.Close()
}

func TestHardDiskLaunchGeometryAndStaging(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Service{catalog: store}
	image := partitionedImage(1 << 20)
	media, _, err := store.ImportHardDiskMediaStream(ctx, int64(len(image)), bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("a", 64)
	entry, err := store.CreateCoreMediaEntry(ctx, "Ultima VI", "fes.pc386", pkg, protocol.HardDiskRole, media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.readHardDisk(ctx, entry)
	if err != nil || source.disk.Geometry != (protocol.DiskGeometry{Cylinders: 30, Heads: 4, SectorsPerTrack: 17}) || source.disk.Size != 1<<20 || source.disk.SHA256 != media.MediaID {
		t.Fatalf("inferred %+v %v", source, err)
	}
	// A kit holding the digest is not sent the image again.
	held := &stagingClient{held: protocol.DiskImageStatus{SHA256: media.MediaID, Size: 1 << 20, Staged: true}}
	if err := stageHardDisk(ctx, held, source); err != nil || held.uploaded != 0 {
		t.Fatalf("reuse %v %d", err, held.uploaded)
	}
	missing := &stagingClient{held: protocol.DiskImageStatus{SHA256: media.MediaID}}
	if err := stageHardDisk(ctx, missing, source); err != nil || missing.uploaded != 1<<20 {
		t.Fatalf("upload %v %d", err, missing.uploaded)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	old := &stagingClient{probeErr: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "the kit's software predates hard-disk images"}}
	var apiErr *protocol.APIError
	if err := stageHardDisk(ctx, old, &hardDiskSource{disk: source.disk}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
		t.Fatalf("old kit %v", err)
	}
	// The entry's override wins over the partition table.
	override := protocol.DiskGeometry{Cylinders: 2, Heads: 16, SectorsPerTrack: 63}
	entry, err = store.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, media.MediaID, &override)
	if err != nil {
		t.Fatal(err)
	}
	source, err = s.readHardDisk(ctx, entry)
	if err != nil || source.disk.Geometry != override {
		t.Fatalf("override %+v %v", source, err)
	}
	_ = source.Close()
	// No selection launches with the drive empty; another role is refused.
	if source, err := s.readHardDisk(ctx, catalog.CoreEntry{GameID: entry.GameID}); source != nil || err != nil {
		t.Fatalf("diskless %+v %v", source, err)
	}
	if _, err := s.readHardDisk(ctx, catalog.CoreEntry{MediaRole: protocol.DiskRole, MediaID: media.MediaID}); err == nil {
		t.Fatal("floppy role launched as a hard disk")
	}
}

func TestMachineLoadIdentityRequiresTheBoundDisk(t *testing.T) {
	noSave := false
	sha := strings.Repeat("c", 64)
	unit := protocol.MediaUnitStatus{Unit: protocol.AtaDiskUnit, Interface: protocol.AtaDiskInterface(), MinBytes: uint32(protocol.AtaDiskMinBytes),
		MaxBytes: uint32(protocol.AtaDiskMaxBytes), ChunkBytes: protocol.AtaDiskChunkBytes, State: protocol.MediaUnitReady, Saveable: &noSave,
		Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "ultima-vi", BaseMediaID: sha, Revision: "absent"}}
	status := func(u *protocol.MediaUnitStatus) *protocol.CorePackageStatus {
		p := &protocol.CorePackageStatus{PersistenceMode: "persistent", ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1, Minor: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.AtaDiskInterface().ID, Major: protocol.AtaDiskInterface().Major}}}
		if u != nil {
			p.MediaUnits = []protocol.MediaUnitStatus{*u}
		}
		return p
	}
	withDisk := coreLoadSource{machine: &protocol.LibraryMachineBinding{GameID: "ultima-vi", Disk: &protocol.StagedDisk{SHA256: sha, Size: 1 << 20}}}
	if !withDisk.matchesLoadedIdentity(status(&unit)) {
		t.Fatal("the disk the host sent was refused")
	}
	for name, change := range map[string]func(*protocol.MediaUnitStatus){
		"other game": func(u *protocol.MediaUnitStatus) {
			u.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "wolf3d", BaseMediaID: sha, Revision: "absent"}
		},
		"other image": func(u *protocol.MediaUnitStatus) {
			u.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "ultima-vi", BaseMediaID: strings.Repeat("d", 64), Revision: "absent"}
		},
		"unbound":     func(u *protocol.MediaUnitStatus) { u.Persistence = nil },
		"empty drive": func(u *protocol.MediaUnitStatus) { u.State, u.Persistence = protocol.MediaUnitEmpty, nil },
	} {
		u := unit
		change(&u)
		if withDisk.matchesLoadedIdentity(status(&u)) {
			t.Errorf("%s accepted", name)
		}
	}
	if withDisk.matchesLoadedIdentity(status(nil)) {
		t.Error("a load without the disk unit accepted")
	}
	// With no disk sent, the drive must be empty or absent.
	diskless := coreLoadSource{machine: &protocol.LibraryMachineBinding{GameID: "ultima-vi"}}
	empty := unit
	empty.State, empty.Persistence = protocol.MediaUnitEmpty, nil
	if !diskless.matchesLoadedIdentity(status(&empty)) || !diskless.matchesLoadedIdentity(status(nil)) {
		t.Fatal("an empty drive was refused")
	}
	if diskless.matchesLoadedIdentity(status(&unit)) {
		t.Fatal("a disk the host never sent was accepted")
	}
}

func TestStagingKeepsTheKitStorageError(t *testing.T) {
	full := &stagingClient{probeErr: &protocol.APIError{Code: protocol.CodeCacheFull, Message: protocol.DiskImageStorageFullMessage, Phase: "request"}}
	var apiErr *protocol.APIError
	if err := stageHardDisk(context.Background(), full, &hardDiskSource{}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeCacheFull || apiErr.Message != protocol.DiskImageStorageFullMessage {
		t.Fatalf("full card %v", err)
	}
}
