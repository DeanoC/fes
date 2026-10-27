package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func atariLibraryPackage(t *testing.T, format3, writable, geometry bool, transforms ...func(string) string) []byte {
	t.Helper()
	archive, _ := apple2LibraryPackageFixture(t, format3, func(text string) string {
		text = strings.ReplaceAll(text, "apple2", "atari-st")
		if writable {
			text += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-write\"\nmajor = 1\nminor = 0\nrequired = true\n"
		}
		if geometry {
			text += "\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-geometry\"\nmajor = 1\nminor = 0\nrequired = true\n"
		}
		for _, transform := range transforms {
			text = transform(text)
		}
		return text
	})
	return archive
}

func newAtariDesk(t *testing.T, geometry bool) (*Service, catalog.CoreEntry) {
	return newAtariDeskPackage(t, atariLibraryPackage(t, true, true, geometry))
}

func newAtariDeskPackage(t *testing.T, raw []byte) (*Service, catalog.CoreEntry) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, root+"/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	packages, err := corepackage.NewStore(root + "/packages")
	if err != nil {
		t.Fatal(err)
	}
	client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: 10 * time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	inspection, created, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("import: %v created=%v", err, created)
	}
	entry, err := s.CreateCoreEntry(ctx, "Atari desk", inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	return s, entry
}

func TestCreateBlankAtariStDiskBindsTheTitle(t *testing.T) {
	ctx := context.Background()
	s, entry := newAtariDesk(t, false)
	got, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
		Name: "work", GameID: entry.GameID, ExpectedPackageID: entry.PackageID, ExpectedMediaID: "", Bind: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := atarist.Format("work", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(want.Bytes)
	if !got.Created || got.MediaID != hex.EncodeToString(sum[:]) || got.Size != int64(len(want.Bytes)) || got.Name != "WORK" || got.Tracks != 80 || got.Heads != 2 || got.Sectors != 9 || got.Entry == nil || got.Entry.MediaRole != protocol.DiskRole || got.Entry.MediaID != got.MediaID {
		t.Fatalf("blank disk %+v", got)
	}
	stored, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || stored.MediaID != got.MediaID || stored.MediaRole != protocol.DiskRole {
		t.Fatalf("stored %+v %v", stored, err)
	}
	again, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
		Name: "WORK", GameID: entry.GameID, ExpectedPackageID: entry.PackageID, ExpectedMediaID: got.MediaID, Bind: true,
	})
	if err != nil || again.Created || again.MediaID != got.MediaID || again.Entry == nil || again.Entry.MediaID != got.MediaID {
		t.Fatalf("rebind %+v %v", again, err)
	}
	if _, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
		Name: "other", Tracks: 80, Heads: 1, Sectors: 9, GameID: entry.GameID, ExpectedPackageID: entry.PackageID, ExpectedMediaID: got.MediaID, Bind: true,
	}); err == nil {
		t.Fatal("non-720 geometry admitted without the extension")
	}
}

func TestCreateBlankAtariStDiskHonorsExtendedGeometry(t *testing.T) {
	ctx := context.Background()
	s, entry := newAtariDesk(t, true)
	got, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
		Name: "side", Tracks: 82, Heads: 1, Sectors: 10, GameID: entry.GameID, ExpectedPackageID: entry.PackageID, ExpectedMediaID: "", Bind: true,
	})
	if err != nil || got.Size != int64(82*1*10*512) || got.Entry == nil || got.Entry.MediaRole != "disk" {
		t.Fatalf("extended %+v %v", got, err)
	}
}

func TestCreateBlankAtariStDiskRejectsOtherPackages(t *testing.T) {
	ctx := context.Background()
	s, _, entry, _ := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Standalone Pong")
	_, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
		Name: "work", GameID: entry.GameID, ExpectedPackageID: entry.PackageID, ExpectedMediaID: entry.MediaID, Bind: true,
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
		t.Fatalf("pong disk: %v", err)
	}
	if _, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{Name: "bad/name"}); err == nil {
		t.Fatal("bad name stored")
	}
}

func TestCreateBlankAtariStDiskRequiresWritableFormat3AtariFirmware(t *testing.T) {
	for _, tc := range []struct {
		name string
		pkg  []byte
	}{
		{name: "read-only floppy", pkg: atariLibraryPackage(t, true, false, false)},
		{name: "format 2", pkg: atariLibraryPackage(t, false, true, false)},
		{name: "different core", pkg: atariLibraryPackage(t, true, true, false, func(text string) string {
			return strings.ReplaceAll(text, "fes.atari-st", "fes.other")
		})},
		{name: "non-firmware ROM", pkg: atariLibraryPackage(t, true, true, false, func(text string) string {
			return strings.Replace(text, `role = "firmware"`, `role = "cartridge"`, 1)
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, entry := newAtariDeskPackage(t, tc.pkg)
			_, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{
				Name: "work", GameID: entry.GameID, ExpectedPackageID: entry.PackageID, Bind: true,
			})
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
				t.Fatalf("package admitted: %v", err)
			}
		})
	}
}

func TestCreateBlankAtariStDiskStoreOnlyDoesNotRequirePackage(t *testing.T) {
	ctx := context.Background()
	s, _ := newAtariDesk(t, false)
	s.corePackages = nil
	got, err := s.CreateBlankAtariStDisk(ctx, BlankAtariStRequest{Name: "work"})
	if err != nil || !got.Created || got.MediaID == "" || got.Entry != nil {
		t.Fatalf("store-only disk %+v: %v", got, err)
	}
}
