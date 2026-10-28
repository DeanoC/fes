package catalog

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

// A hard-disk image may exceed the 32 MiB limit of every other role, but only
// through the hard-disk import, and only in whole sectors.
func TestHardDiskImportBoundsAndGeometryOverride(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	image := bytes.Repeat([]byte{0xf6}, 33<<20)
	if _, _, err := s.ImportCoreMediaStream(ctx, int64(len(image)), bytes.NewReader(image)); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("33 MiB through the general import: %v", err)
	}
	if _, _, err := s.ImportHardDiskMediaStream(ctx, int64(len(image))+1, bytes.NewReader(append(image, 0))); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("partial sector: %v", err)
	}
	if _, _, err := s.ImportHardDiskMediaStream(ctx, 512, bytes.NewReader(image[:512])); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Fatalf("under 1 MiB: %v", err)
	}
	media, created, err := s.ImportHardDiskMediaStream(ctx, int64(len(image)), bytes.NewReader(image))
	if err != nil || !created || media.Size != int64(len(image)) {
		t.Fatalf("hard-disk import: %+v %v %v", media, created, err)
	}
	if _, reader, err := s.OpenCoreMedia(ctx, media.MediaID); err != nil {
		t.Fatalf("open hard disk: %v", err)
	} else if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("a", 64)
	entry, err := s.CreateCoreMediaEntry(ctx, "Ultima VI", "fes.pc386", pkg, protocol.HardDiskRole, media.MediaID)
	if err != nil || entry.DiskGeometry != nil {
		t.Fatalf("entry %+v %v", entry, err)
	}
	g := &protocol.DiskGeometry{Cylinders: 67, Heads: 16, SectorsPerTrack: 63}
	if _, err := s.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, strings.Repeat("b", 64), g); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("stale media: %v", err)
	}
	if _, err := s.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, media.MediaID, &protocol.DiskGeometry{Cylinders: 1000, Heads: 16, SectorsPerTrack: 63}); !errors.Is(err, ErrInvalidCoreEntry) {
		t.Fatalf("geometry over the image: %v", err)
	}
	set, err := s.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, media.MediaID, g)
	if err != nil || set.DiskGeometry == nil || *set.DiskGeometry != *g {
		t.Fatalf("override %+v %v", set, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.CoreEntry(ctx, entry.GameID)
	if err != nil || got.DiskGeometry == nil || *got.DiskGeometry != *g {
		t.Fatalf("reopened %+v %v", got, err)
	}
	// Selecting other media drops the override; clearing it with nil works too.
	cleared, err := reopened.SelectCoreEntryMedia(ctx, entry.GameID, pkg, media.MediaID, "", "")
	if err != nil || cleared.DiskGeometry != nil {
		t.Fatalf("media change kept geometry: %+v %v", cleared, err)
	}
	if _, err := reopened.SetCoreEntryDiskGeometry(ctx, entry.GameID, pkg, media.MediaID, g); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("geometry without the image: %v", err)
	}
}
