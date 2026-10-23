package fogcast

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func atariLibraryPackage(t *testing.T, geometry bool) []byte {
	t.Helper()
	raw := libraryPackageFixture(t, "0.1.0")
	reader := tar.NewReader(bytes.NewReader(raw))
	var out bytes.Buffer
	offset := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		canonical := bytes.Clone(raw[offset : offset+512])
		offset += 512 + (len(body)+511)/512*512
		if header.Name == "manifest.toml" {
			body = bytes.Replace(body, []byte("fes.simple-game"), []byte("fes.computer"), 1)
			body = bytes.Replace(body, []byte("fes.gamepad"), []byte("fes.media.atari-st-floppy"), 1)
			if geometry {
				body = append(body, []byte("\n[[interfaces]]\nid = \"fes.media.atari-st-floppy-geometry\"\nmajor = 1\nminor = 0\nrequired = true\n")...)
			}
		}
		copy(canonical[124:136], fmt.Sprintf("%011o\x00", len(body)))
		copy(canonical[148:156], "        ")
		sum := 0
		for _, b := range canonical {
			sum += int(b)
		}
		copy(canonical[148:156], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(canonical)
		out.Write(body)
		out.Write(make([]byte, (512-len(body)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func newAtariDesk(t *testing.T, geometry bool) (*Service, catalog.CoreEntry) {
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
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	raw := atariLibraryPackage(t, geometry)
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
