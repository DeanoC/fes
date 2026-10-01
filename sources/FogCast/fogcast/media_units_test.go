package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaUnitServiceClient struct {
	fakeServiceClient
	inserts, ejects int
	inserted        []byte
	binding         protocol.MediaUnitBinding
	insertErr       error
	// state is reported for unit 0 after each operation.
	after func(state string) protocol.Status
}

func (c *mediaUnitServiceClient) InsertMedia(_ context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	c.inserts++
	c.binding = b
	c.inserted, _ = io.ReadAll(body)
	if c.insertErr != nil || int64(len(c.inserted)) != size {
		return c.statusResult, errors.Join(c.insertErr, errors.New("insert failed"))
	}
	c.statusResult = c.after("ready")
	return c.statusResult, nil
}

func (c *mediaUnitServiceClient) EjectMedia(_ context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	c.ejects++
	c.binding = b
	c.statusResult = c.after("empty")
	return c.statusResult, nil
}

func computerSessionStatus(id string, generation uint64, state string) protocol.Status {
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1}},
		MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: state}},
	}}
}

func TestLiveDiskInsertAndEjectUseTheFloppyUnit(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	disk := bytes.Repeat([]byte{0x96}, 143360)
	media, _, err := store.ImportCoreMediaStream(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	short, _, err := store.ImportCoreMediaStream(ctx, 1024, bytes.NewReader(disk[:1024]))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	client := &mediaUnitServiceClient{fakeServiceClient: fakeServiceClient{statusResult: computerSessionStatus(id, 4, "empty")},
		after: func(state string) protocol.Status { return computerSessionStatus(id, 4, state) }}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.activeExecution = ExecutionFPGANative
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "dev"}
	got, err := s.ReplaceLiveMedia(ctx, media.MediaID, "DOS33.DSK", b)
	if err != nil || client.inserts != 1 || !bytes.Equal(client.inserted, disk) || client.binding.Unit != 0 || client.binding.Generation != 4 {
		t.Fatalf("insert err=%v calls=%d binding=%+v", err, client.inserts, client.binding)
	}
	if unit, ok := protocol.MediaUnit(got.CorePackage, 0); !ok || unit.State != "ready" {
		t.Fatalf("status %+v", got.CorePackage)
	}
	for name, call := range map[string]func() error{
		"wrong size": func() error { _, err := s.ReplaceLiveMedia(ctx, short.MediaID, "short.dsk", b); return err },
		"prodos":     func() error { _, err := s.ReplaceLiveMedia(ctx, media.MediaID, "game.po", b); return err },
		"stale generation": func() error {
			_, err := s.ReplaceLiveMedia(ctx, media.MediaID, "game.do", protocol.DevelopmentMediaBinding{PackageID: id, Generation: 3, Target: "dev"})
			return err
		},
	} {
		if err := call(); err == nil || client.inserts != 1 {
			t.Fatalf("%s accepted: %v calls=%d", name, err, client.inserts)
		}
	}
	cleared, err := s.ClearLiveMedia(ctx, b)
	if err != nil || client.ejects != 1 || client.binding.Unit != 0 {
		t.Fatalf("eject err=%v calls=%d", err, client.ejects)
	}
	if unit, ok := protocol.MediaUnit(cleared.CorePackage, 0); !ok || unit.State != "empty" {
		t.Fatalf("eject status %+v", cleared.CorePackage)
	}
	// A ZX81 tape name never reaches the disk unit.
	if _, err := s.ReplaceLiveMedia(ctx, media.MediaID, "maze.p", b); err == nil || client.inserts != 1 {
		t.Fatal("tape name inserted into a disk unit")
	}
}

func TestScopedDiskInsertAndEjectReachBoundKitWhenForegroundKitUnavailable(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	disk := bytes.Repeat([]byte{0x96}, 143360)
	media, _, err := store.ImportCoreMediaStream(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 64)
	clientA := &mediaUnitServiceClient{fakeServiceClient: fakeServiceClient{healthResult: protocol.Health{APIVersion: "v1"}, statusResult: computerSessionStatus(id, 4, "empty")},
		after: func(state string) protocol.Status { return computerSessionStatus(id, 4, state) }}
	service := &Service{catalog: store, uploadTimeout: time.Second, activeExecution: ExecutionFPGANative,
		selectedTarget: "kit-b", activeTarget: "kit-b", targets: []TargetConfig{{Name: "kit-a", Enabled: true, TargetID: "id-a"}, {Name: "kit-b", Enabled: true, TargetID: "id-b"}},
		targetClients: map[string]serviceClient{"kit-a": clientA, "kit-b": &fakeServiceClient{healthErr: errors.New("B unavailable")}}}
	binding := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 4, Target: "kit-a", TargetID: "id-a"}
	if _, err := service.ReplaceLiveMedia(ctx, media.MediaID, "DOS33.DSK", binding); err != nil {
		t.Fatalf("insert on bound A: %v", err)
	}
	if _, err := service.ClearLiveMedia(ctx, binding); err != nil {
		t.Fatalf("eject on bound A: %v", err)
	}
	if clientA.inserts != 1 || clientA.ejects != 1 {
		t.Fatalf("A inserts=%d ejects=%d", clientA.inserts, clientA.ejects)
	}
}

func apple2DiskLaunchFixture(t *testing.T) (*Service, *mediaUnitLaunchClient, catalog.CoreEntry, []byte) {
	t.Helper()
	ctx := context.Background()
	archive, firmware := apple2LibraryPackageFixture(t, true)
	s, base, entry, inspection := newCoreEntryLaunchFixture(t, archive, "Apple II", time.Minute)
	client := &mediaUnitLaunchClient{defaultMediaPackageClient: base}
	s.targetClients[s.selectedTarget] = client
	rom, _, err := s.ImportCoreMedia(ctx, int64(len(firmware)), bytes.NewReader(firmware))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, "apple2-firmware", "", rom.MediaID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	base.coreLoad = func(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
		staged, err := corepackage.StageROMInput(ctx, root, size, body)
		if err != nil {
			return protocol.Status{}, err
		}
		defer staged.Cleanup()
		status := apple2ActiveStatus(inspection, 9)
		status.CorePackage.ROMLink = staged.ROMLink
		base.statusResult = status
		return status, nil
	}
	return s, client, entry, bytes.Repeat([]byte{0x5a}, 143360)
}

type mediaUnitLaunchClient struct {
	*defaultMediaPackageClient
	inserts  int
	inserted []byte
	fail     bool
}

func (c *mediaUnitLaunchClient) InsertMedia(_ context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	c.inserts++
	c.inserted, _ = io.ReadAll(body)
	if c.fail {
		return c.statusResult, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media transfer failed", Phase: "transport"}
	}
	status := c.statusResult
	pkg := *status.CorePackage
	pkg.MediaUnits = []protocol.MediaUnitStatus{{Unit: b.Unit, Interface: protocol.Apple2FloppyInterface(), MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: "ready"}}
	status.CorePackage = &pkg
	c.statusResult = status
	return status, nil
}

func (c *mediaUnitLaunchClient) EjectMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.statusResult, errors.New("unexpected eject")
}

func TestLibraryDiskIsSelectedExactlyAndInsertedAfterStart(t *testing.T) {
	ctx := context.Background()
	s, client, entry, disk := apple2DiskLaunchFixture(t)
	short, _, err := s.ImportCoreMedia(ctx, 1024, bytes.NewReader(disk[:1024]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, short.MediaID); err == nil {
		t.Fatal("short disk selected")
	}
	image, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", "blob", image.MediaID); err == nil {
		t.Fatal("disk selected as startup blob")
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, image.MediaID); err != nil {
		t.Fatal(err)
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || client.inserts != 1 || client.mediaCalls != 0 || !bytes.Equal(client.inserted, disk) {
		t.Fatalf("launch err=%v inserts=%d legacy=%d", err, client.inserts, client.mediaCalls)
	}
	if unit, ok := protocol.MediaUnit(response.Status.CorePackage, 0); !ok || unit.State != "ready" ||
		response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
		t.Fatalf("session %+v", response.Status)
	}
}

func TestLibraryDiskInsertFailureStopsTheLaunch(t *testing.T) {
	ctx := context.Background()
	s, client, entry, disk := apple2DiskLaunchFixture(t)
	image, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.DiskRole, image.MediaID); err != nil {
		t.Fatal(err)
	}
	client.fail = true
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil || client.inserts != 1 || client.stopCalls == 0 {
		t.Fatalf("failed insert err=%v inserts=%d stops=%d", err, client.inserts, client.stopCalls)
	}
}
