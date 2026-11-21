package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func mediaSetFixture(t *testing.T, s *Store, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		media, _, err := s.ImportCoreMedia(context.Background(), []byte("disk image "+string(rune('A'+i))))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, media.MediaID)
	}
	return ids
}

// named gives each disk an admitted .st name, "DISK2.ST" onwards.
func named(ids ...string) []SetMedia {
	out := []SetMedia{}
	for i, id := range ids {
		out = append(out, SetMedia{MediaID: id, MediaName: "DISK" + string(rune('2'+i)) + ".ST"})
	}
	return out
}

func TestCoreEntryMediaSetReplacesInOrder(t *testing.T) {
	ctx := context.Background()
	s, path := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	disks := mediaSetFixture(t, s, 4)
	entry, err := s.CreateCoreMediaEntry(ctx, "Two-disk game", "fes.atari-st", pkg, "disk", disks[0])
	if err != nil {
		t.Fatal(err)
	}
	if entry.MoreMedia != nil {
		t.Fatalf("new entry has a set: %v", entry.MoreMedia)
	}
	set, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(disks[2], disks[1]))
	if err != nil || !slices.Equal(SetMediaIDs(set.MoreMedia), []string{disks[2], disks[1]}) {
		t.Fatalf("set: %+v, %v", set, err)
	}
	// Every reader carries the set in order, across a reopen.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || !slices.Equal(got.MoreMedia, named(disks[2], disks[1])) {
		t.Fatalf("read: %+v, %v", got, err)
	}
	all, err := s.CoreEntries(ctx)
	if err != nil || len(all) != 1 || !slices.Equal(all[0].MoreMedia, got.MoreMedia) {
		t.Fatalf("list: %+v, %v", all, err)
	}
	// A stale caller is a conflict and changes nothing.
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(disks[3])); !errors.Is(err, ErrCoreEntryConflict) {
		t.Fatalf("stale set: %v", err)
	}
	reordered, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], SetMediaIDs(got.MoreMedia), named(disks[1], disks[2], disks[3]))
	if err != nil || !slices.Equal(SetMediaIDs(reordered.MoreMedia), []string{disks[1], disks[2], disks[3]}) {
		t.Fatalf("reorder: %+v, %v", reordered, err)
	}
	single, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], SetMediaIDs(reordered.MoreMedia), named())
	if err != nil || single.MoreMedia != nil {
		t.Fatalf("clear: %+v, %v", single, err)
	}
}

func TestCoreEntryMediaSetRefusesBadSets(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	disks := mediaSetFixture(t, s, 2)
	entry, err := s.CreateCoreMediaEntry(ctx, "Game", "fes.atari-st", pkg, "disk", disks[0])
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, MaxCoreEntryDisks)
	for i := range tooMany {
		tooMany[i] = strings.Repeat(string("0123456789abcdef"[i]), 64)
	}
	for name, more := range map[string][]string{
		"disk 1 again": {disks[0]},
		"twice":        {disks[1], disks[1]},
		"not a digest": {"disk"},
		"too many":     tooMany,
	} {
		if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(more...)); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"", "notes.txt", "dir/DISK2.ST", "DI\nSK2.ST"} {
		if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, []SetMedia{{MediaID: disks[1], MediaName: name}}); !errors.Is(err, ErrInvalidCoreMedia) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(strings.Repeat("f", 64))); err == nil {
		t.Error("a disk not in the library was accepted")
	}
	work, err := s.CreateCoreMediaEntry(ctx, "Work disk", "fes.atari-st", pkg, "disk", disks[1])
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkShelfDisk(ctx, work.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, work.GameID, pkg, disks[1], nil, named(disks[0])); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Errorf("shelf disk set: %v", err)
	}
	blob, err := s.CreateCoreMediaEntry(ctx, "Blob", "fes.test", pkg, "blob", disks[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, blob.GameID, pkg, disks[0], nil, named(disks[1])); !errors.Is(err, ErrInvalidCoreMedia) {
		t.Errorf("blob set: %v", err)
	}
}

func TestCoreEntryMediaSetEndsWhenDiskOneChanges(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	disks := mediaSetFixture(t, s, 3)
	entry, err := s.CreateCoreMediaEntry(ctx, "Game", "fes.atari-st", pkg, "disk", disks[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(disks[1])); err != nil {
		t.Fatal(err)
	}
	// Selecting disk 1 again keeps the set; another disk 1 ends it.
	same, err := s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, disks[0], "disk", disks[0])
	if err != nil || !slices.Equal(SetMediaIDs(same.MoreMedia), []string{disks[1]}) {
		t.Fatalf("reselect: %+v, %v", same, err)
	}
	other, err := s.SelectCoreEntryMedia(ctx, entry.GameID, pkg, disks[0], "disk", disks[2])
	if err != nil || other.MoreMedia != nil {
		t.Fatalf("new disk 1: %+v, %v", other, err)
	}
	// Removing the title removes its set.
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[2], nil, named(disks[1])); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, entry.GameID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM core_entry_media_set`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("set rows after removal: %d, %v", left, err)
	}
}

func TestCoreEntryMediaSetProtectsRemoteCachedDisk(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	first := mediaSetFixture(t, s, 1)[0]
	cached := remoteCacheImport(t, s, "remote disk two")
	entry, err := s.CreateCoreMediaEntry(ctx, "Game", "fes.zx-spectrum", pkg, "cassette", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, first, nil, named(cached.MediaID)); err != nil {
		t.Fatal(err)
	}
	// The insert trigger adopted it. Mark it remote again, as an older
	// client's reimport could, so the cleanup clause itself is tested.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO remote_cached_media(media_id) VALUES (?)`, cached.MediaID); err != nil {
		t.Fatal(err)
	}
	usage, err := s.RemoteCassetteCache(ctx, "", RemoteCachePageLimit)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TrackedObjects != 1 {
		t.Fatalf("tracked %d remote objects, want the set member", usage.TrackedObjects)
	}
	for _, c := range usage.Candidates {
		if c.MediaID == cached.MediaID {
			t.Fatal("a set member is offered for remote-cache cleanup")
		}
	}
}

func TestCoreEntryMediaSetDetachesKitHostEntry(t *testing.T) {
	ctx := context.Background()
	s, _ := mediaTestStore(t)
	pkg := strings.Repeat("a", 64)
	disks := mediaSetFixture(t, s, 2)
	identity := strings.Repeat("e", 64)
	entry, err := s.CreateKitHostEntry(ctx, "Generated", "fes.atari-st", pkg, "disk", disks[0], KitHostEntryIdentity{Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCoreEntryMediaSet(ctx, entry.GameID, pkg, disks[0], nil, named(disks[1])); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.KitHostEntry(ctx, identity); err != nil || found {
		t.Fatalf("kit-host entry still generated after a set edit: found=%v err=%v", found, err)
	}
}
