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

// A partitioned synthetic image: its MBR says 4 heads and 17 sectors.
func partitionedImage(size int) []byte {
	data := make([]byte, size)
	entry := data[446:462]
	entry[4], entry[5], entry[6] = 0x06, 3, 17
	binary.LittleEndian.PutUint32(entry[12:], 2000)
	data[510], data[511] = 0x55, 0xaa
	return data
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
