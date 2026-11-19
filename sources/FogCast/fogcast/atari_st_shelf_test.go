package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

// shelfKitClient is a kit that keeps saved disks by identity.
type shelfKitClient struct {
	*packageLibraryClient
	saved     map[protocol.MediaRecordIdentity][]byte
	corrupt   map[protocol.MediaRecordIdentity]bool
	discarded []protocol.MediaRecordIdentity
	fail      error
	// failDiscard refuses only discards, after any copy has been made.
	failDiscard error
	// startAfterDiscard simulates another host starting the source between
	// the record discard and the library's final target check.
	startAfterDiscard string
	batches           int
}

func savedRevision(image []byte) string {
	sum := sha256.Sum256(append([]byte("record:"), image...))
	return hex.EncodeToString(sum[:])
}

func (c *shelfKitClient) record(id protocol.MediaRecordIdentity) protocol.MediaRecord {
	image, ok := c.saved[id]
	if !ok {
		return protocol.MediaRecord{State: protocol.MediaRecordAbsent, Revision: "absent"}
	}
	if c.corrupt[id] {
		return protocol.MediaRecord{State: protocol.MediaRecordCorrupt, Revision: savedRevision(image), Bytes: int64(len(image)) + 173}
	}
	// As on a kit: changed means the saved image is not the base image.
	sum := sha256.Sum256(image)
	return protocol.MediaRecord{
		State: protocol.MediaRecordPresent, Revision: savedRevision(image), Bytes: int64(len(image)) + 173,
		Changed: hex.EncodeToString(sum[:]) != id.BaseMediaID,
	}
}

func (c *shelfKitClient) InspectMediaRecord(_ context.Context, id protocol.MediaRecordIdentity) (protocol.MediaRecord, error) {
	if c.fail != nil {
		return protocol.MediaRecord{}, c.fail
	}
	return c.record(id), nil
}

func (c *shelfKitClient) InspectMediaRecords(_ context.Context, ids []protocol.MediaRecordIdentity) ([]protocol.MediaRecord, error) {
	c.batches++
	if c.fail != nil {
		return nil, c.fail
	}
	out := make([]protocol.MediaRecord, len(ids))
	for i, id := range ids {
		out[i] = c.record(id)
	}
	return out, nil
}

func (c *shelfKitClient) ExportMediaRecord(_ context.Context, d protocol.MediaRecordDiscard) ([]byte, error) {
	if c.fail != nil {
		return nil, c.fail
	}
	if c.record(d.MediaRecordIdentity).Revision != d.ExpectedRevision || c.corrupt[d.MediaRecordIdentity] {
		return nil, &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "saved media changed"}
	}
	return append([]byte(nil), c.saved[d.MediaRecordIdentity]...), nil
}

func (c *shelfKitClient) DiscardMediaRecord(_ context.Context, d protocol.MediaRecordDiscard) (protocol.MediaRecord, error) {
	if c.fail != nil {
		return protocol.MediaRecord{}, c.fail
	}
	if c.failDiscard != nil {
		return protocol.MediaRecord{}, c.failDiscard
	}
	// As on a kit: nothing is discarded while a core runs.
	if c.statusResult.State != protocol.StateIdle {
		return protocol.MediaRecord{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the running core before discarding saved media"}
	}
	if c.record(d.MediaRecordIdentity).Revision != d.ExpectedRevision {
		return protocol.MediaRecord{}, &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "saved media changed"}
	}
	delete(c.saved, d.MediaRecordIdentity)
	c.discarded = append(c.discarded, d.MediaRecordIdentity)
	if c.startAfterDiscard != "" {
		gameID := c.startAfterDiscard
		c.statusResult = protocol.Status{State: protocol.StateActive, GameID: &gameID}
	}
	return protocol.MediaRecord{State: protocol.MediaRecordAbsent, Revision: "absent"}, nil
}

type shelfFixture struct {
	s       *Service
	kit     *shelfKitClient
	store   *catalog.Store
	pkg     string
	game    catalog.CoreEntry
	gameRaw []byte
}

// newShelf is a library with one ST game disk, "Dungeon", from the games
// folder.
func newShelf(t *testing.T) shelfFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	users, err := libraryuser.Open(filepath.Join(root, "user.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	kit := &shelfKitClient{
		packageLibraryClient: &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}},
		saved:                map[protocol.MediaRecordIdentity][]byte{}, corrupt: map[protocol.MediaRecordIdentity]bool{},
	}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: 10 * time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, kit, WithUserLibrary(users))
	s.corePackages = packages
	raw := atariLibraryPackage(t, true, true, true)
	inspection, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	gameRaw := bytes.Repeat([]byte{0xE5}, 737280)
	copy(gameRaw, mustBlank(t, s, inspection.PackageID))
	gameRaw[4096] = 0x42
	media, _, err := s.ImportCoreMedia(ctx, int64(len(gameRaw)), bytes.NewReader(gameRaw))
	if err != nil {
		t.Fatal(err)
	}
	game, err := s.CreateCoreMediaEntry(ctx, "Dungeon", inspection.PackageID, protocol.DiskRole, media.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	return shelfFixture{s: s, kit: kit, store: store, pkg: inspection.PackageID, game: game, gameRaw: gameRaw}
}

// mustBlank gives a valid ST boot sector to build a game image on.
func mustBlank(t *testing.T, s *Service, pkg string) []byte {
	t.Helper()
	disk, err := s.CreateBlankAtariStDisk(context.Background(), BlankAtariStRequest{Name: "GAME"})
	if err != nil {
		t.Fatal(err)
	}
	store := s.catalog.(coreMediaCatalog)
	_, reader, err := store.OpenCoreMedia(context.Background(), disk.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f shelfFixture) list(t *testing.T) AtariStShelf {
	t.Helper()
	shelf, err := f.s.AtariStShelf(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return shelf
}

func (f shelfFixture) write(entry catalog.CoreEntry, image []byte) string {
	f.kit.saved[shelfIdentity(entry)] = image
	return savedRevision(image)
}

func (f shelfFixture) media(t *testing.T, id string) []byte {
	t.Helper()
	_, reader, err := f.s.catalog.(coreMediaCatalog).OpenCoreMedia(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func apiCode(err error) protocol.ErrorCode {
	var api *protocol.APIError
	if errors.As(err, &api) {
		return api.Code
	}
	return ""
}

func TestAtariStShelfListsMadeDisksAndGameDisksWithChanges(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	if shelf := f.list(t); !shelf.KeptKnown || len(shelf.Disks) != 0 {
		t.Fatalf("an unwritten game disk is on the shelf: %+v", shelf)
	}
	work, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Work disk 2", PackageID: f.pkg})
	if err != nil {
		t.Fatal(err)
	}
	if !work.Made || work.Kept != ShelfKeptNone || work.Size != 737280 || work.Title != "Work disk 2" {
		t.Fatalf("new disk %+v", work)
	}
	// The name is the library title; the volume label is what GEMDOS stores.
	image := f.media(t, work.MediaID)
	root := image[(1+2*5)*512:]
	if got := string(root[:11]); got != "WORK DISK 2" {
		t.Fatalf("volume label %q", got)
	}
	// Two new disks are two media: neither can meet the other's saved changes.
	other, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Letters", PackageID: f.pkg, Tracks: 80, Heads: 1, Sectors: 9})
	if err != nil {
		t.Fatal(err)
	}
	third, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Letters 2", PackageID: f.pkg, Tracks: 80, Heads: 1, Sectors: 9})
	if err != nil {
		t.Fatal(err)
	}
	if other.Size != 368640 || other.MediaID == third.MediaID {
		t.Fatalf("blank disks share a medium or size: %+v %+v", other, third)
	}
	if _, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Letters", PackageID: f.pkg}); apiCode(err) != protocol.CodeBadRequest || !strings.Contains(err.Error(), "already has that name") {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Odd", PackageID: f.pkg, Tracks: 40, Heads: 1, Sectors: 9}); apiCode(err) != protocol.CodeBadRequest {
		t.Fatalf("bad geometry: %v", err)
	}
	// A game that was only played is saved at Stop with its image unchanged.
	// It has a saved record and is still not on the shelf.
	unwritten := f.write(f.game, append([]byte(nil), f.gameRaw...))
	if shelf := f.list(t); len(shelf.Disks) != 3 {
		t.Fatalf("a played, unwritten game disk is on the shelf: %+v", shelf)
	}
	if _, err := f.s.RevertAtariStShelfDisk(ctx, f.game.GameID, unwritten); err != nil {
		t.Fatalf("discarding an unchanged record: %v", err)
	}
	played := append([]byte(nil), f.gameRaw...)
	played[9000] = 0x17
	revision := f.write(f.game, played)
	shelf := f.list(t)
	if len(shelf.Disks) != 4 || f.kit.batches < 2 {
		t.Fatalf("shelf %+v", shelf)
	}
	titles := []string{}
	for _, disk := range shelf.Disks {
		titles = append(titles, disk.Title)
		switch disk.Title {
		case "Dungeon":
			if disk.Made || disk.Kept != ShelfKeptChanges || disk.KeptRevision != revision || disk.InDrive {
				t.Fatalf("game disk %+v", disk)
			}
		default:
			if !disk.Made || disk.Kept != ShelfKeptNone || disk.KeptRevision != "" {
				t.Fatalf("made disk %+v", disk)
			}
		}
	}
	if strings.Join(titles, "|") != "Dungeon|Letters|Letters 2|Work disk 2" {
		t.Fatalf("order %v", titles)
	}
	// A disk the kit is running is in the drive.
	running := work.GameID
	f.kit.statusResult = protocol.Status{State: protocol.StateActive, GameID: &running}
	for _, disk := range f.list(t).Disks {
		if disk.InDrive != (disk.GameID == running) {
			t.Fatalf("in drive %+v", disk)
		}
	}
	// So is a disk inserted into another game's session: the drive is bound
	// to the disk, not to the game that was launched.
	f.kit.statusResult = protocol.Status{State: protocol.StateActive, GameID: &f.game.GameID, CorePackage: &protocol.CorePackageStatus{
		MediaUnits: []protocol.MediaUnitStatus{{Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: other.GameID, BaseMediaID: other.MediaID, Revision: "absent"}}},
	}}
	inDrive := map[string]bool{}
	for _, disk := range f.list(t).Disks {
		inDrive[disk.GameID] = disk.InDrive
	}
	if !inDrive[other.GameID] || !inDrive[f.game.GameID] || inDrive[work.GameID] {
		t.Fatalf("in drive with a swapped disk: %v", inDrive)
	}
	if _, err := f.s.CopyAtariStShelfDisk(ctx, other.GameID, "Mid session"); apiCode(err) != protocol.CodeBusy {
		t.Fatalf("copy of the inserted disk: %v", err)
	}
	f.kit.statusResult = protocol.Status{State: protocol.StateIdle}
	// A launch or stop that holds admission leaves the kit unasked.
	release, err := f.s.acquireLifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if held := f.list(t); held.KeptKnown || len(held.Disks) != 3 {
		t.Fatalf("shelf during a launch: %+v", held)
	}
	release()
	// A kit that cannot be asked leaves saved changes unknown, never "none":
	// only the disks made here are listed.
	f.kit.fail = &protocol.APIError{Code: protocol.CodeBusy, Message: "runtime is busy"}
	shelf = f.list(t)
	if shelf.KeptKnown || len(shelf.Disks) != 3 {
		t.Fatalf("unknown shelf %+v", shelf)
	}
	for _, disk := range shelf.Disks {
		if !disk.Made || disk.Kept != ShelfKeptUnknown {
			t.Fatalf("unknown disk %+v", disk)
		}
	}
	f.kit.fail = nil
	f.kit.corrupt[shelfIdentity(f.game)] = true
	for _, disk := range f.list(t).Disks {
		if disk.Title == "Dungeon" && (disk.Kept != ShelfKeptDamaged || disk.KeptRevision == "") {
			t.Fatalf("damaged disk %+v", disk)
		}
	}
}

func TestAtariStShelfCopyRevertAndDelete(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	played := append([]byte(nil), f.gameRaw...)
	played[8192] = 0x99
	revision := f.write(f.game, played)

	// A copy is the disk as it stands, as a new disk made here.
	copied, err := f.s.CopyAtariStShelfDisk(ctx, f.game.GameID, "Dungeon save")
	if err != nil {
		t.Fatal(err)
	}
	if !copied.Made || copied.Kept != ShelfKeptNone || copied.MediaID == f.game.MediaID || !bytes.Equal(f.media(t, copied.MediaID), played) {
		t.Fatalf("copy %+v", copied)
	}
	if len(f.kit.saved) != 1 || len(f.kit.discarded) != 0 {
		t.Fatalf("copy changed the kit: %d saved, %v discarded", len(f.kit.saved), f.kit.discarded)
	}
	if _, err := f.s.CopyAtariStShelfDisk(ctx, f.game.GameID, "Dungeon save"); apiCode(err) != protocol.CodeBadRequest {
		t.Fatalf("copy under a taken name: %v", err)
	}
	// A disk with no changes copies its imported image.
	plain, err := f.s.CopyAtariStShelfDisk(ctx, copied.GameID, "Dungeon save 2")
	if err != nil || plain.MediaID != copied.MediaID {
		t.Fatalf("plain copy %+v %v", plain, err)
	}

	// Going back needs the changes the caller was shown.
	if _, err := f.s.RevertAtariStShelfDisk(ctx, f.game.GameID, strings.Repeat("0", 64)); apiCode(err) != protocol.CodeStaleRevision {
		t.Fatalf("revert of other changes: %v", err)
	}
	if _, err := f.s.RevertAtariStShelfDisk(ctx, copied.GameID, strings.Repeat("0", 64)); apiCode(err) != protocol.CodeStaleRevision {
		t.Fatalf("revert with nothing saved: %v", err)
	}
	running := f.game.GameID
	f.kit.statusResult = protocol.Status{State: protocol.StateActive, GameID: &running}
	for name, act := range map[string]func() error{
		"revert": func() error { _, err := f.s.RevertAtariStShelfDisk(ctx, f.game.GameID, revision); return err },
		"copy":   func() error { _, err := f.s.CopyAtariStShelfDisk(ctx, f.game.GameID, "Mid game"); return err },
	} {
		if err := act(); apiCode(err) != protocol.CodeBusy {
			t.Fatalf("%s of the disk in the drive: %v", name, err)
		}
	}
	f.kit.statusResult = protocol.Status{State: protocol.StateIdle}
	if len(f.kit.saved) != 1 {
		t.Fatal("a refused request changed the kit")
	}
	reverted, err := f.s.RevertAtariStShelfDisk(ctx, f.game.GameID, revision)
	if err != nil || reverted.Kept != ShelfKeptNone || reverted.Made || len(f.kit.saved) != 0 {
		t.Fatalf("revert %+v %v", reverted, err)
	}
	if entry, err := f.s.CoreEntry(ctx, f.game.GameID); err != nil || entry.MediaID != f.game.MediaID {
		t.Fatalf("revert touched the library entry: %+v %v", entry, err)
	}

	// Delete is for disks made here, and takes their saved changes with it.
	if err := f.s.DeleteAtariStShelfDisk(ctx, f.game.GameID, ""); apiCode(err) != protocol.CodeUnsupportedOperation {
		t.Fatalf("delete of a game disk: %v", err)
	}
	entry, err := f.s.CoreEntry(ctx, copied.GameID)
	if err != nil {
		t.Fatal(err)
	}
	written := f.write(entry, played)
	if err := f.s.DeleteAtariStShelfDisk(ctx, copied.GameID, ""); apiCode(err) != protocol.CodeStaleRevision {
		t.Fatalf("delete without the shown changes: %v", err)
	}
	f.kit.fail = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "kit is away"}
	if err := f.s.DeleteAtariStShelfDisk(ctx, copied.GameID, written); err == nil {
		t.Fatal("delete went ahead without the kit")
	}
	f.kit.fail = nil
	if _, err := f.s.CoreEntry(ctx, copied.GameID); err != nil {
		t.Fatalf("a refused delete removed the entry: %v", err)
	}
	if err := f.s.DeleteAtariStShelfDisk(ctx, copied.GameID, written); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CoreEntry(ctx, copied.GameID); apiCode(err) != protocol.CodeROMNotFound {
		t.Fatalf("deleted disk remains: %v", err)
	}
	if len(f.kit.saved) != 0 {
		t.Fatalf("delete left saved changes on the kit: %v", f.kit.saved)
	}
	titles := []string{}
	for _, disk := range f.list(t).Disks {
		titles = append(titles, disk.Title)
	}
	if strings.Join(titles, "|") != "Dungeon save 2" {
		t.Fatalf("shelf after delete %v", titles)
	}
}

func TestAtariStShelfRenameKeepsTheDisk(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	work, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Work", PackageID: f.pkg})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := f.s.CoreEntry(ctx, work.GameID)
	if err != nil {
		t.Fatal(err)
	}
	written := append([]byte(nil), f.media(t, work.MediaID)...)
	written[6000] = 7
	revision := f.write(entry, written)
	if _, err := f.s.RenameAtariStShelfDisk(ctx, f.game.GameID, "", "Other"); apiCode(err) != protocol.CodeUnsupportedOperation {
		t.Fatalf("rename of a game disk: %v", err)
	}
	if _, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Dungeon"); apiCode(err) != protocol.CodeBadRequest {
		t.Fatalf("rename onto a game: %v", err)
	}
	if _, err := f.s.CoreEntry(ctx, work.GameID); err != nil || len(f.kit.saved) != 1 {
		t.Fatalf("a refused rename changed the disk: %v", err)
	}
	// Saved changes cannot be discarded while a core runs, so a rename then
	// is refused before anything is copied.
	elsewhere := f.game.GameID
	f.kit.statusResult = protocol.Status{State: protocol.StateActive, GameID: &elsewhere}
	if _, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Letters"); apiCode(err) != protocol.CodeBusy {
		t.Fatalf("rename while a core runs: %v", err)
	}
	if err := f.s.DeleteAtariStShelfDisk(ctx, work.GameID, revision); apiCode(err) != protocol.CodeBusy {
		t.Fatalf("delete while a core runs: %v", err)
	}
	if shelf := f.list(t); len(shelf.Disks) != 1 || shelf.Disks[0].GameID != work.GameID || len(f.kit.saved) != 1 {
		t.Fatalf("a refused rename left a second disk: %+v", shelf)
	}
	f.kit.statusResult = protocol.Status{State: protocol.StateIdle}
	renamed, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Letters")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "Letters" || !renamed.Made || !bytes.Equal(f.media(t, renamed.MediaID), written) {
		t.Fatalf("renamed %+v", renamed)
	}
	if _, err := f.s.CoreEntry(ctx, work.GameID); apiCode(err) != protocol.CodeROMNotFound {
		t.Fatalf("old name remains: %v", err)
	}
	if len(f.kit.saved) != 0 {
		t.Fatalf("old saved changes remain: %v", f.kit.saved)
	}
	// A disk made again under a removed disk's name starts as its own
	// image, not with what the earlier disk left on the kit.
	if err := f.s.RemoveCoreEntry(ctx, renamed.GameID); err != nil {
		t.Fatal(err)
	}
	leftover := protocol.MediaRecordIdentity{CoreID: atariStCoreID, GameID: renamed.GameID, BaseMediaID: f.game.MediaID}
	f.kit.saved[leftover] = written
	again, err := f.s.CopyAtariStShelfDisk(ctx, f.game.GameID, "Letters")
	if err != nil {
		t.Fatal(err)
	}
	if again.GameID != renamed.GameID || len(f.kit.saved) != 0 || len(f.kit.discarded) == 0 {
		t.Fatalf("copy inherited saved changes: %+v %v", again, f.kit.saved)
	}
}

// A rename is a copy and a delete. When the delete is refused after the copy
// is made, the copy is taken back: the shelf keeps one disk under its old
// name, and the new name is free for a retry.
func TestAtariStShelfRenameTakesBackItsCopyWhenTheDeleteFails(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	work, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Work", PackageID: f.pkg})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := f.s.CoreEntry(ctx, work.GameID)
	if err != nil {
		t.Fatal(err)
	}
	written := append([]byte(nil), f.media(t, work.MediaID)...)
	written[6000] = 7
	revision := f.write(entry, written)
	f.kit.failDiscard = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "kit is away"}
	if _, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Letters"); err == nil {
		t.Fatal("rename succeeded although the old disk could not be deleted")
	}
	shelf := f.list(t)
	if len(shelf.Disks) != 1 || shelf.Disks[0].GameID != work.GameID || shelf.Disks[0].Title != "Work" {
		t.Fatalf("a failed rename left %+v", shelf.Disks)
	}
	f.kit.failDiscard = nil
	if renamed, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Letters"); err != nil || renamed.Title != "Letters" {
		t.Fatalf("retried rename: %+v %v", renamed, err)
	}
}

func TestAtariStShelfRenameKeepsSavedImageWhenTargetStartsAfterDiscard(t *testing.T) {
	ctx := context.Background()
	f := newShelf(t)
	work, err := f.s.NewAtariStShelfDisk(ctx, AtariStShelfNew{Title: "Work", PackageID: f.pkg})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := f.s.CoreEntry(ctx, work.GameID)
	if err != nil {
		t.Fatal(err)
	}
	written := append([]byte(nil), f.media(t, work.MediaID)...)
	written[6000] = 7
	revision := f.write(entry, written)
	f.s.targets = []TargetConfig{{Name: f.s.selectedTarget, Enabled: true}}
	f.kit.startAfterDiscard = work.GameID

	if _, err := f.s.RenameAtariStShelfDisk(ctx, work.GameID, revision, "Letters"); apiCode(err) != protocol.CodeBusy {
		t.Fatalf("rename after the target started: %v", err)
	}
	shelf := f.list(t)
	if len(shelf.Disks) != 2 {
		t.Fatalf("rename discarded the only copy of saved changes: %+v", shelf.Disks)
	}
	var renamed *AtariStShelfDisk
	for i := range shelf.Disks {
		if shelf.Disks[i].Title == "Letters" {
			renamed = &shelf.Disks[i]
		}
	}
	if renamed == nil || !bytes.Equal(f.media(t, renamed.MediaID), written) {
		t.Fatalf("saved image was not retained in the copied disk: %+v", shelf.Disks)
	}
	if _, err := f.s.CoreEntry(ctx, work.GameID); err != nil {
		t.Fatalf("source entry should remain when its removal is refused: %v", err)
	}
	if len(f.kit.saved) != 0 {
		t.Fatalf("source saved record was expected to have been discarded: %v", f.kit.saved)
	}
}
