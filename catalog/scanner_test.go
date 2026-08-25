package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestScannerTraversesIncrementallyWithoutFollowingSymlinks(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "RPGs", "Chrono Trigger.SFC"), []byte("chrono"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "plain.BIN"), []byte("plain"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "unreadable.smc"), []byte("closed"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "gone.sfc"), []byte("gone"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "manual.txt"), []byte("ignore"))
	mustWriteScannerFile(t, filepath.Join(rootPath, ".DS_Store"), []byte("ignore"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "._plain.BIN"), []byte("ignore"))

	outside := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(outside, "outside.sfc"), []byte("outside"))
	if err := os.Symlink(filepath.Join(outside, "outside.sfc"), filepath.Join(rootPath, "linked.sfc")); err != nil {
		t.Fatalf("create file symlink: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, "linked-dir")); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}

	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	offline := Root{ID: "snes-offline", System: protocol.SystemSNES, Path: filepath.Join(t.TempDir(), "missing")}
	scanner := Scanner{Store: store}
	scanner.openFile = func(root *os.Root, name string) (scannerSourceFile, error) {
		switch filepath.Base(name) {
		case "unreadable.smc":
			return nil, fs.ErrPermission
		default:
			file, err := root.Open(name)
			if err != nil {
				return nil, err
			}
			return &failOnReadFile{File: file}, nil
		}
	}
	scanner.lstat = func(name string) (fs.FileInfo, error) {
		if filepath.Base(name) == "gone.sfc" {
			return nil, fs.ErrNotExist
		}
		return os.Lstat(name)
	}

	first, err := scanner.Scan(ctx, []Root{root, offline})
	if err != nil {
		t.Fatalf("Scan(first): %v", err)
	}
	wantFirst := ScanReport{Roots: []RootReport{
		{RootID: "snes-main", System: protocol.SystemSNES, Added: 4, Invalid: 2},
		{RootID: "snes-offline", System: protocol.SystemSNES, Offline: true, Reason: reasonRootOffline},
	}}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("first report = %+v, want %+v", first, wantFirst)
	}

	games := scannerGames(t, store)
	paths := scannerGamePaths(games)
	wantPaths := []string{"RPGs/Chrono Trigger.SFC", "gone.sfc", "plain.BIN", "unreadable.smc"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("relative paths = %v, want %v", paths, wantPaths)
	}
	for _, game := range games {
		if strings.Contains(game.RelativePath, `\`) || filepath.IsAbs(game.RelativePath) {
			t.Fatalf("relative path is not normalized: %q", game.RelativePath)
		}
		wantTitle := strings.TrimSuffix(filepath.Base(game.RelativePath), filepath.Ext(game.RelativePath))
		if game.Title != wantTitle {
			t.Errorf("title for %q = %q, want %q", game.RelativePath, game.Title, wantTitle)
		}
		if wantID := GameID(root.System, root.ID, game.RelativePath, wantTitle); game.ID != wantID {
			t.Errorf("ID for %q = %q, want %q", game.RelativePath, game.ID, wantID)
		}
	}
	assertScannerGameState(t, games, "unreadable.smc", SourceStateInvalid, reasonSourceUnreadable)
	assertScannerGameState(t, games, "gone.sfc", SourceStateInvalid, reasonSourceDisappeared)
	assertReasonsPathFree(t, rootPath, games)

	chronoPath := filepath.Join(rootPath, "RPGs", "Chrono Trigger.SFC")
	mustWriteScannerFile(t, chronoPath, []byte("chrono changed"))
	changedTime := time.Unix(1_800_000_000, 123)
	if err := os.Chtimes(chronoPath, changedTime, changedTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if err := os.Remove(filepath.Join(rootPath, "plain.BIN")); err != nil {
		t.Fatalf("remove raw source: %v", err)
	}
	second, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan(second): %v", err)
	}
	wantSecond := ScanReport{Roots: []RootReport{
		{RootID: "snes-main", System: protocol.SystemSNES, Updated: 1, Unchanged: 2, Invalid: 2, Missing: 1},
	}}
	if !reflect.DeepEqual(second, wantSecond) {
		t.Fatalf("second report = %+v, want %+v", second, wantSecond)
	}
	plain := scannerGameByPath(t, store, "plain.BIN")
	if plain.State != SourceStateMissing {
		t.Fatalf("removed source state = %q, want %q", plain.State, SourceStateMissing)
	}

	offlinePath := rootPath + "-offline"
	if err := os.Rename(rootPath, offlinePath); err != nil {
		t.Fatalf("take root offline: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(offlinePath) })
	third, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan(offline): %v", err)
	}
	if got := third.Roots; !reflect.DeepEqual(got, []RootReport{{
		RootID: "snes-main", System: protocol.SystemSNES, Offline: true, Reason: reasonRootOffline,
	}}) {
		t.Fatalf("offline report = %+v", got)
	}
	if game := scannerGameByPath(t, store, "RPGs/Chrono Trigger.SFC"); game.RootOnline || game.State != SourceStateAvailable {
		t.Fatalf("offline root changed game availability: %+v", game)
	}
}

func TestScannerDoesNotPOSIXOpenUNCRoot(t *testing.T) {
	ctx := context.Background()
	store := openScannerStore(t)
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{{
		ID: "folder-watch-unc", System: protocol.SystemSNES, Path: "//deano-clawz/Games/Games/SNES",
	}})
	if err != nil {
		t.Fatalf("Scan(UNC): %v", err)
	}
	if got := report.Roots; !reflect.DeepEqual(got, []RootReport{{
		RootID: "folder-watch-unc", System: protocol.SystemSNES, Offline: true, Reason: reasonRootOffline,
	}}) {
		t.Fatalf("UNC report = %+v", got)
	}
	if games := scannerGames(t, store); len(games) != 0 {
		t.Fatalf("UNC root indexed %v", scannerGamePaths(games))
	}
	if !isRemoteUNCPath("//deano-clawz/Games/Games/SNES") || isRemoteUNCPath("/absolute/snes") {
		t.Fatal("UNC detection drifted")
	}
}

func TestScannerDoesNotHoldCatalogTransactionDuringTraversal(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "Axelay.sfc"), []byte("axelay"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}

	started := make(chan struct{})
	block := make(chan struct{})
	var once sync.Once
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		once.Do(func() {
			close(started)
			<-block
		})
		return fs.WalkDir(rootFS, walkRoot, fn)
	}

	done := make(chan error, 1)
	go func() {
		_, err := scanner.Scan(ctx, []Root{root})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("traversal did not start")
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := store.Games(ctx)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("catalog read during traversal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("catalog read blocked during traversal")
	}
	close(block)
	if err := <-done; err != nil {
		t.Fatalf("Scan: %v", err)
	}
}

func TestScannerSerializesOverlappingCollectionsPerRoot(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "Older.sfc"), []byte("older"))
	databasePath := filepath.Join(t.TempDir(), "catalog.sqlite3")
	olderStore, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = olderStore.Close() })
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}

	olderCollected := make(chan struct{})
	releaseOlder := make(chan struct{})
	older := Scanner{Store: olderStore, Platforms: DefaultPlatforms()}
	walks := 0
	older.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		if err := fs.WalkDir(rootFS, walkRoot, fn); err != nil {
			return err
		}
		walks++
		if walks == 2 {
			close(olderCollected)
			<-releaseOlder
		}
		return nil
	}
	olderDone := make(chan error, 1)
	go func() {
		_, err := older.Scan(ctx, []Root{root})
		olderDone <- err
	}()
	select {
	case <-olderCollected:
	case <-time.After(time.Second):
		t.Fatal("older scan did not finish collecting")
	}
	mustWriteScannerFile(t, filepath.Join(rootPath, "Newer.sfc"), []byte("newer"))
	newerStore, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open concurrent CLI store during traversal: %v", err)
	}
	t.Cleanup(func() { _ = newerStore.Close() })
	newerTraversal := make(chan struct{})
	newer := Scanner{Store: newerStore, Platforms: DefaultPlatforms()}
	newer.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		select {
		case <-newerTraversal:
		default:
			close(newerTraversal)
		}
		return fs.WalkDir(rootFS, walkRoot, fn)
	}

	newerDone := make(chan error, 1)
	go func() {
		_, err := newer.Scan(ctx, []Root{root})
		newerDone <- err
	}()
	select {
	case <-newerTraversal:
		t.Fatal("newer scan traversed before the older cross-process lease was released")
	case err := <-newerDone:
		t.Fatalf("newer scan completed before the older cross-process lease was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseOlder)
	if err := <-olderDone; err != nil {
		t.Fatalf("older Scan: %v", err)
	}
	select {
	case err := <-newerDone:
		if err != nil {
			t.Fatalf("newer Scan: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("newer scan remained blocked after the older cross-process lease was released")
	}

	games := scannerGames(t, newerStore)
	if got, want := scannerGamePaths(games), []string{"Newer.sfc", "Older.sfc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("games after overlapping scans = %v, want %v", got, want)
	}
	if game := scannerGameByPath(t, newerStore, "Newer.sfc"); game.State != SourceStateAvailable {
		t.Fatalf("newer ROM was wiped by older collection: %+v", game)
	}
}

func TestScannerLeasesDoNotSerializeDifferentRoots(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "catalog.sqlite3")
	firstStore, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	secondStore, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	firstRoot := Root{ID: "snes-first", System: protocol.SystemSNES, Path: t.TempDir()}
	secondRoot := Root{ID: "snes-second", System: protocol.SystemSNES, Path: t.TempDir()}
	mustWriteScannerFile(t, filepath.Join(firstRoot.Path, "First.sfc"), []byte("first"))
	mustWriteScannerFile(t, filepath.Join(secondRoot.Path, "Second.sfc"), []byte("second"))

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	first := Scanner{Store: firstStore, Platforms: DefaultPlatforms()}
	first.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		once.Do(func() {
			close(firstStarted)
			<-releaseFirst
		})
		return fs.WalkDir(rootFS, walkRoot, fn)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Scan(ctx, []Root{firstRoot})
		firstDone <- err
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first root traversal did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := (Scanner{Store: secondStore, Platforms: DefaultPlatforms()}).Scan(ctx, []Root{secondRoot})
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second root Scan: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("different root was blocked by the first root's scan lease")
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first root Scan: %v", err)
	}
}

func TestScannerZIPClassificationUsesOnlyCentralDirectory(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	body := []byte("body that will be corrupted")
	validBytes := makeScannerZIP(t, []scannerZIPEntry{
		{name: "Games/HERO.SFC", body: body, method: zip.Store},
		{name: "README.txt", body: []byte("notes")},
	})
	marker := bytes.Index(validBytes, body)
	if marker < 0 {
		t.Fatal("stored ZIP body marker not found")
	}
	validBytes[marker] ^= 0xff // CRC now fails if Scanner opens and reads the selected member.
	mustWriteScannerFile(t, filepath.Join(rootPath, "Hero Collection.ZIP"), validBytes)
	mustWriteScannerFile(t, filepath.Join(rootPath, "zero.zip"), makeScannerZIP(t, []scannerZIPEntry{{name: "README.txt", body: []byte("notes")}}))
	mustWriteScannerFile(t, filepath.Join(rootPath, "multiple.zip"), makeScannerZIP(t, []scannerZIPEntry{{name: "one.sfc"}, {name: "two.SMC"}}))
	mustWriteScannerFile(t, filepath.Join(rootPath, "nested.zip"), makeScannerZIP(t, []scannerZIPEntry{{name: "game.sfc"}, {name: "inner.zip"}}))
	mustWriteScannerFile(t, filepath.Join(rootPath, "empty.zip"), makeScannerZIP(t, nil))
	mustWriteScannerFile(t, filepath.Join(rootPath, "directories.zip"), makeScannerZIP(t, []scannerZIPEntry{{name: "games/", directory: true}}))
	encrypted := makeScannerZIP(t, []scannerZIPEntry{{name: "secret.sfc", body: []byte("secret")}})
	setScannerZIPEncryptedFlag(t, encrypted)
	mustWriteScannerFile(t, filepath.Join(rootPath, "encrypted.zip"), encrypted)
	mustWriteScannerFile(t, filepath.Join(rootPath, "corrupt.zip"), []byte("not a ZIP central directory"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "limit.zip"), makeScannerLimitZIP(t, 4096))
	mustWriteScannerFile(t, filepath.Join(rootPath, "too-many.zip"), makeScannerLimitZIP(t, 4097))

	store := openScannerStore(t)
	root := Root{ID: "snes-zips", System: protocol.SystemSNES, Path: rootPath}
	report, err := (Scanner{Store: store}).Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got, want := report.Roots, []RootReport{{
		RootID: "snes-zips", System: protocol.SystemSNES, Added: 10, Invalid: 8,
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("report = %+v, want %+v", got, want)
	}

	valid := scannerGameByPath(t, store, "Hero Collection.ZIP")
	info, err := os.Lstat(filepath.Join(rootPath, "Hero Collection.ZIP"))
	if err != nil {
		t.Fatalf("Lstat(valid ZIP): %v", err)
	}
	wantFingerprint := Fingerprint{
		SourceSize: int64(len(validBytes)), ModifiedNS: info.ModTime().UnixNano(),
		ZIPMember: "Games/HERO.SFC", ZIPSize: int64(len(body)), ZIPCRC32: crc32.ChecksumIEEE(body), ZIPEntryCount: 2,
	}
	if valid.Kind != SourceKindZIP || valid.State != SourceStateAvailable || valid.Title != "Hero Collection" || valid.Fingerprint != wantFingerprint {
		t.Fatalf("valid ZIP = %+v, want title/kind/state/fingerprint %+v", valid, wantFingerprint)
	}
	if wantID := GameID(root.System, root.ID, "Hero Collection.ZIP", "Hero Collection"); valid.ID != wantID {
		t.Fatalf("valid ZIP ID = %q, want %q", valid.ID, wantID)
	}

	limit := scannerGameByPath(t, store, "limit.zip")
	if limit.State != SourceStateAvailable || limit.Fingerprint.ZIPEntryCount != 4096 || limit.Fingerprint.ZIPMember != "selected.sfc" {
		t.Fatalf("4,096-entry ZIP = %+v", limit)
	}
	wantReasons := map[string]string{
		"zero.zip":        reasonZIPNoROM,
		"multiple.zip":    reasonZIPMultipleROMs,
		"nested.zip":      reasonZIPNested,
		"empty.zip":       reasonZIPNoROM,
		"directories.zip": reasonZIPNoROM,
		"encrypted.zip":   reasonZIPEncrypted,
		"corrupt.zip":     reasonZIPCorrupt,
		"too-many.zip":    reasonZIPTooManyEntries,
	}
	for relativePath, reason := range wantReasons {
		game := scannerGameByPath(t, store, relativePath)
		if game.Kind != SourceKindZIP || game.State != SourceStateInvalid || game.Reason != reason {
			t.Errorf("%s = kind %q state %q reason %q, want ZIP invalid %q", relativePath, game.Kind, game.State, game.Reason, reason)
		}
		if game.Fingerprint.SourceSize == 0 || game.Fingerprint.ModifiedNS == 0 {
			t.Errorf("%s lost source stat fingerprint: %+v", relativePath, game.Fingerprint)
		}
		if strings.Contains(game.Reason, rootPath) || strings.ContainsAny(game.Reason, `/\\`) {
			t.Errorf("%s reason leaks a path: %q", relativePath, game.Reason)
		}
	}
	if got := scannerGameByPath(t, store, "too-many.zip").Fingerprint.ZIPEntryCount; got != 4097 {
		t.Fatalf("too-many ZIP entry count = %d, want 4097", got)
	}
}

func TestScannerRollsBackRootWhenTraversalCannotComplete(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "original.sfc"), []byte("original"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	mustWriteScannerFile(t, filepath.Join(rootPath, "new.sfc"), []byte("new"))
	scanner.walkDir = func(rootFS fs.FS, root string, fn fs.WalkDirFunc) error {
		if err := fs.WalkDir(rootFS, root, fn); err != nil {
			return err
		}
		return errors.New("injected traversal failure")
	}
	if _, err := scanner.Scan(ctx, []Root{root}); err == nil || !strings.Contains(err.Error(), "injected traversal failure") {
		t.Fatalf("Scan(traversal failure) error = %v", err)
	}
	if got := scannerGamePaths(scannerGames(t, store)); !reflect.DeepEqual(got, []string{"original.sfc"}) {
		t.Fatalf("games after rolled-back scan = %v, want original only", got)
	}
	if game := scannerGameByPath(t, store, "original.sfc"); !game.RootOnline || game.State != SourceStateAvailable {
		t.Fatalf("rolled-back scan changed existing game: %+v", game)
	}
}

func TestScannerTreatsConfiguredRootSymlinkAsOfflineWithoutReconciling(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "library")
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.sfc"), []byte("game"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}

	realPath := filepath.Join(parent, "library-real")
	if err := os.Rename(rootPath, realPath); err != nil {
		t.Fatalf("rename configured root: %v", err)
	}
	if err := os.Symlink(realPath, rootPath); err != nil {
		t.Fatalf("replace configured root with symlink: %v", err)
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan(root symlink): %v", err)
	}
	if got, want := report.Roots, []RootReport{{
		RootID: "snes-main", System: protocol.SystemSNES, Offline: true, Reason: reasonRootOffline,
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root-symlink report = %+v, want %+v", got, want)
	}
	if game := scannerGameByPath(t, store, "game.sfc"); game.RootOnline || game.State != SourceStateAvailable {
		t.Fatalf("root symlink reconciled existing game: %+v", game)
	}
}

func TestScannerRejectsZIPLimitsOutsideHardMaximumBeforeChangingCatalog(t *testing.T) {
	for _, limit := range []int{-1, 4097} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			store := openScannerStore(t)
			rootPath := t.TempDir()
			mustWriteScannerFile(t, filepath.Join(rootPath, "game.sfc"), []byte("game"))
			scanner := Scanner{Store: store, MaxZIPEntries: limit}
			if _, err := scanner.Scan(context.Background(), []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}}); err == nil {
				t.Fatalf("Scan with MaxZIPEntries %d succeeded", limit)
			}
			if got := scannerLibraryCount(t, store); got != 0 {
				t.Fatalf("invalid scanner configuration created %d libraries", got)
			}
			if got := scannerGames(t, store); len(got) != 0 {
				t.Fatalf("invalid scanner configuration changed catalog: %+v", got)
			}
		})
	}
}

func TestScannerRootSwapAfterPreflightRollsBackAndMarksOffline(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "library")
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.sfc"), []byte("game"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	seeded := scannerGameByPath(t, store, "game.sfc")
	content := Content{SHA256: strings.Repeat("a", 64), Size: 4, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, seeded.ID, seeded.Fingerprint, content); err != nil || !updated {
		t.Fatalf("UpdateContent = %v, %v", updated, err)
	}

	external := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(external, "outside.sfc"), []byte("outside"))
	realPath := filepath.Join(parent, "library-real")
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		if err := os.Rename(rootPath, realPath); err != nil {
			return err
		}
		if err := os.Symlink(external, rootPath); err != nil {
			return err
		}
		return fs.WalkDir(rootFS, walkRoot, fn)
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan(swapped root): %v", err)
	}
	if got, want := report.Roots, []RootReport{{
		RootID: root.ID, System: root.System, Offline: true, Reason: reasonRootOffline,
	}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("swapped-root report = %+v, want %+v", got, want)
	}
	game := scannerGameByPath(t, store, "game.sfc")
	if game.RootOnline || game.State != SourceStateAvailable || game.Content == nil || *game.Content != content {
		t.Fatalf("swapped root changed prior game/content: %+v", game)
	}
	if got := scannerGamePaths(scannerGames(t, store)); !reflect.DeepEqual(got, []string{"game.sfc"}) {
		t.Fatalf("swapped root accepted external paths: %v", got)
	}
}

func TestScannerDirectorySwapBeforeDescentRollsBackWithoutMissing(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	nestedPath := filepath.Join(rootPath, "nested")
	mustWriteScannerFile(t, filepath.Join(nestedPath, "game.sfc"), []byte("game"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	seeded := scannerGameByPath(t, store, "nested/game.sfc")
	content := Content{SHA256: strings.Repeat("b", 64), Size: 4, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, seeded.ID, seeded.Fingerprint, content); err != nil || !updated {
		t.Fatalf("UpdateContent = %v, %v", updated, err)
	}

	external := t.TempDir()
	realNestedPath := filepath.Join(rootPath, "nested-real")
	swapped := false
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(rootFS, walkRoot, func(walkPath string, entry fs.DirEntry, walkErr error) error {
			callbackErr := fn(walkPath, entry, walkErr)
			if callbackErr != nil || walkErr != nil || swapped || walkPath != "nested" || !entry.IsDir() {
				return callbackErr
			}
			swapped = true
			if err := os.Rename(nestedPath, realNestedPath); err != nil {
				return err
			}
			if err := os.Symlink(external, nestedPath); err != nil {
				return err
			}
			return nil
		})
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err == nil {
		t.Fatalf("Scan(directory swap) = %+v, nil, want traversal error", report)
	}
	if !swapped {
		t.Fatal("directory swap hook did not run")
	}
	game := scannerGameByPath(t, store, "nested/game.sfc")
	if !game.RootOnline || game.State != SourceStateAvailable || game.Content == nil || *game.Content != content {
		t.Fatalf("directory swap reconciled prior game/content: %+v", game)
	}
}

func TestScannerRealDirectorySwapBeforeDescentRollsBackWithoutMissing(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "library")
	nestedPath := filepath.Join(rootPath, "nested")
	mustWriteScannerFile(t, filepath.Join(nestedPath, "game.sfc"), []byte("game"))
	replacementPath := filepath.Join(parent, "replacement")
	if err := os.MkdirAll(replacementPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(replacement): %v", err)
	}
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	seeded := scannerGameByPath(t, store, "nested/game.sfc")
	content := Content{SHA256: strings.Repeat("e", 64), Size: 4, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, seeded.ID, seeded.Fingerprint, content); err != nil || !updated {
		t.Fatalf("UpdateContent = %v, %v", updated, err)
	}

	realNestedPath := filepath.Join(parent, "nested-real")
	swapped := false
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(rootFS, walkRoot, func(walkPath string, entry fs.DirEntry, walkErr error) error {
			callbackErr := fn(walkPath, entry, walkErr)
			if callbackErr != nil || walkErr != nil || swapped || walkPath != "nested" || !entry.IsDir() {
				return callbackErr
			}
			if err := os.Rename(nestedPath, realNestedPath); err != nil {
				return err
			}
			if err := os.Rename(replacementPath, nestedPath); err != nil {
				return err
			}
			swapped = true
			return nil
		})
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err == nil {
		t.Fatalf("Scan(real directory swap) = %+v, nil, want traversal error", report)
	}
	if !swapped {
		t.Fatal("real directory swap hook did not run")
	}
	game := scannerGameByPath(t, store, "nested/game.sfc")
	if !game.RootOnline || game.State != SourceStateAvailable || game.Content == nil || *game.Content != content {
		t.Fatalf("real directory swap reconciled prior game/content: %+v", game)
	}
}

func TestScannerInternalDirectorySwapBeforeDescentRollsBackWithoutMissing(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	nestedPath := filepath.Join(rootPath, "nested")
	mustWriteScannerFile(t, filepath.Join(nestedPath, "game.sfc"), []byte("game"))
	emptyInternalPath := filepath.Join(rootPath, "empty-internal")
	if err := os.MkdirAll(emptyInternalPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(empty internal): %v", err)
	}
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	seeded := scannerGameByPath(t, store, "nested/game.sfc")
	content := Content{SHA256: strings.Repeat("c", 64), Size: 4, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, seeded.ID, seeded.Fingerprint, content); err != nil || !updated {
		t.Fatalf("UpdateContent = %v, %v", updated, err)
	}

	realNestedPath := filepath.Join(rootPath, "nested-real")
	swapped := false
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(rootFS, walkRoot, func(walkPath string, entry fs.DirEntry, walkErr error) error {
			callbackErr := fn(walkPath, entry, walkErr)
			if callbackErr != nil || walkErr != nil || swapped || walkPath != "nested" || !entry.IsDir() {
				return callbackErr
			}
			swapped = true
			if err := os.Rename(nestedPath, realNestedPath); err != nil {
				return err
			}
			if err := os.Symlink("empty-internal", nestedPath); err != nil {
				return err
			}
			return nil
		})
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err == nil {
		t.Fatalf("Scan(internal directory swap) = %+v, nil, want traversal error", report)
	}
	if !swapped {
		t.Fatal("internal directory swap hook did not run")
	}
	game := scannerGameByPath(t, store, "nested/game.sfc")
	if !game.RootOnline || game.State != SourceStateAvailable || game.Content == nil || *game.Content != content {
		t.Fatalf("internal directory swap reconciled prior game/content: %+v", game)
	}
}

func TestScannerInternalAncestorSwapBeforeDescentRollsBackWithoutMissing(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	nestedPath := filepath.Join(rootPath, "nested")
	mustWriteScannerFile(t, filepath.Join(nestedPath, "child", "game.sfc"), []byte("game"))
	emptyInternalPath := filepath.Join(rootPath, "empty-internal", "child")
	if err := os.MkdirAll(emptyInternalPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(empty internal child): %v", err)
	}
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	seeded := scannerGameByPath(t, store, "nested/child/game.sfc")
	content := Content{SHA256: strings.Repeat("d", 64), Size: 4, Extension: "sfc"}
	if updated, err := store.UpdateContent(ctx, seeded.ID, seeded.Fingerprint, content); err != nil || !updated {
		t.Fatalf("UpdateContent = %v, %v", updated, err)
	}

	realNestedPath := filepath.Join(rootPath, "nested-real")
	swapped := false
	scanner.walkDir = func(rootFS fs.FS, walkRoot string, fn fs.WalkDirFunc) error {
		return fs.WalkDir(rootFS, walkRoot, func(walkPath string, entry fs.DirEntry, walkErr error) error {
			callbackErr := fn(walkPath, entry, walkErr)
			if callbackErr != nil || walkErr != nil || swapped || walkPath != "nested/child" || !entry.IsDir() {
				return callbackErr
			}
			swapped = true
			if err := os.Rename(nestedPath, realNestedPath); err != nil {
				return err
			}
			if err := os.Symlink("empty-internal", nestedPath); err != nil {
				return err
			}
			return nil
		})
	}
	report, err := scanner.Scan(ctx, []Root{root})
	if err == nil {
		t.Fatalf("Scan(internal ancestor swap) = %+v, nil, want traversal error", report)
	}
	if !swapped {
		t.Fatal("internal ancestor swap hook did not run")
	}
	game := scannerGameByPath(t, store, "nested/child/game.sfc")
	if !game.RootOnline || game.State != SourceStateAvailable || game.Content == nil || *game.Content != content {
		t.Fatalf("internal ancestor swap reconciled prior game/content: %+v", game)
	}
}

func TestScannerRejectsRawFinalSymlinkSwap(t *testing.T) {
	rootPath := t.TempDir()
	sourcePath := filepath.Join(rootPath, "game.sfc")
	mustWriteScannerFile(t, sourcePath, []byte("inside"))
	external := filepath.Join(t.TempDir(), "outside.sfc")
	mustWriteScannerFile(t, external, []byte("outside"))

	store := openScannerStore(t)
	scanner := Scanner{Store: store}
	scanner.lstat = swapScannerCandidateAfterLstat(t, sourcePath, external)
	if _, err := scanner.Scan(context.Background(), []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}}); err != nil {
		t.Fatalf("Scan(raw swap): %v", err)
	}
	game := scannerGameByPath(t, store, "game.sfc")
	if game.State != SourceStateInvalid || game.Reason == "" || strings.Contains(game.Reason, external) {
		t.Fatalf("raw final-component swap = %+v, want path-free invalid", game)
	}
}

func TestScannerRejectsZIPFinalSymlinkSwapWithoutReadingExternalCentralDirectory(t *testing.T) {
	rootPath := t.TempDir()
	sourcePath := filepath.Join(rootPath, "game.zip")
	mustWriteScannerFile(t, sourcePath, makeScannerZIP(t, []scannerZIPEntry{{name: "inside.sfc", body: []byte("inside")}}))
	external := filepath.Join(t.TempDir(), "outside.zip")
	mustWriteScannerFile(t, external, makeScannerZIP(t, []scannerZIPEntry{{name: "outside.sfc", body: []byte("outside")}}))

	store := openScannerStore(t)
	scanner := Scanner{Store: store}
	scanner.lstat = swapScannerCandidateAfterLstat(t, sourcePath, external)
	if _, err := scanner.Scan(context.Background(), []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}}); err != nil {
		t.Fatalf("Scan(ZIP swap): %v", err)
	}
	game := scannerGameByPath(t, store, "game.zip")
	if game.State != SourceStateInvalid || game.Fingerprint.ZIPMember != "" || game.Reason == "" || strings.Contains(game.Reason, external) {
		t.Fatalf("ZIP final-component swap = %+v, want path-free invalid without external member", game)
	}
}

func TestScannerRejectsParentDirectorySymlinkSwap(t *testing.T) {
	rootPath := t.TempDir()
	parentPath := filepath.Join(rootPath, "nested")
	sourcePath := filepath.Join(parentPath, "game.sfc")
	mustWriteScannerFile(t, sourcePath, []byte("inside"))
	externalParent := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(externalParent, "game.sfc"), []byte("outside"))

	store := openScannerStore(t)
	scanner := Scanner{Store: store}
	swapped := false
	scanner.lstat = func(name string) (fs.FileInfo, error) {
		info, err := os.Lstat(name)
		if err != nil || swapped || name != sourcePath {
			return info, err
		}
		swapped = true
		if err := os.Rename(parentPath, parentPath+"-original"); err != nil {
			t.Fatalf("rename candidate parent: %v", err)
		}
		if err := os.Symlink(externalParent, parentPath); err != nil {
			t.Fatalf("replace candidate parent with symlink: %v", err)
		}
		return info, nil
	}
	if _, err := scanner.Scan(context.Background(), []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}}); err != nil {
		t.Fatalf("Scan(parent swap): %v", err)
	}
	game := scannerGameByPath(t, store, "nested/game.sfc")
	if game.State != SourceStateInvalid || game.Reason == "" || strings.Contains(game.Reason, externalParent) {
		t.Fatalf("parent-directory swap = %+v, want path-free invalid", game)
	}
}

type failOnReadFile struct{ *os.File }

func (f *failOnReadFile) Read([]byte) (int, error) {
	return 0, errors.New("raw ROM bytes must not be read during scan")
}

func (f *failOnReadFile) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("raw ROM bytes must not be read during scan")
}

func swapScannerCandidateAfterLstat(t *testing.T, sourcePath, externalPath string) func(string) (fs.FileInfo, error) {
	t.Helper()
	swapped := false
	return func(name string) (fs.FileInfo, error) {
		info, err := os.Lstat(name)
		if err != nil || swapped || name != sourcePath {
			return info, err
		}
		swapped = true
		if err := os.Rename(sourcePath, sourcePath+"-original"); err != nil {
			t.Fatalf("rename candidate: %v", err)
		}
		if err := os.Symlink(externalPath, sourcePath); err != nil {
			t.Fatalf("replace candidate with symlink: %v", err)
		}
		return info, nil
	}
}

type scannerZIPEntry struct {
	name      string
	body      []byte
	method    uint16
	directory bool
}

func makeScannerZIP(t *testing.T, entries []scannerZIPEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: entry.method}
		if entry.directory {
			header.SetMode(fs.ModeDir | 0o755)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", entry.name, err)
		}
		if _, err := file.Write(entry.body); err != nil {
			t.Fatalf("write ZIP member %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP writer: %v", err)
	}
	return buffer.Bytes()
}

func makeScannerLimitZIP(t *testing.T, count int) []byte {
	t.Helper()
	entries := make([]scannerZIPEntry, 0, count)
	entries = append(entries, scannerZIPEntry{name: "selected.sfc", body: []byte("selected"), method: zip.Store})
	for index := 1; index < count; index++ {
		entries = append(entries, scannerZIPEntry{name: fmt.Sprintf("notes/%04d.txt", index)})
	}
	return makeScannerZIP(t, entries)
}

func setScannerZIPEncryptedFlag(t *testing.T, archive []byte) {
	t.Helper()
	if len(archive) < 8 || !bytes.Equal(archive[:4], []byte{'P', 'K', 3, 4}) {
		t.Fatal("ZIP local header not found")
	}
	archive[6] |= 1
	central := bytes.Index(archive, []byte{'P', 'K', 1, 2})
	if central < 0 || central+10 > len(archive) {
		t.Fatal("ZIP central header not found")
	}
	archive[central+8] |= 1
}

func mustWriteScannerFile(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", name, err)
	}
	if err := os.WriteFile(name, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
}

func openScannerStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close store: %v", err)
		}
	})
	return store
}

func scannerGames(t *testing.T, store *Store) []Game {
	t.Helper()
	games, err := store.Games(context.Background())
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	return games
}

func scannerLibraryCount(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM libraries").Scan(&count); err != nil {
		t.Fatalf("count libraries: %v", err)
	}
	return count
}

func scannerGamePaths(games []Game) []string {
	paths := make([]string, 0, len(games))
	for _, game := range games {
		paths = append(paths, game.RelativePath)
	}
	sort.Strings(paths)
	return paths
}

func scannerGameByPath(t *testing.T, store *Store, relativePath string) Game {
	t.Helper()
	for _, game := range scannerGames(t, store) {
		if game.RelativePath == relativePath {
			return game
		}
	}
	t.Fatalf("game %q not found", relativePath)
	return Game{}
}

func assertScannerGameState(t *testing.T, games []Game, relativePath string, state SourceState, reason string) {
	t.Helper()
	for _, game := range games {
		if game.RelativePath == relativePath {
			if game.State != state || game.Reason != reason {
				t.Fatalf("game %q = state %q reason %q, want %q %q", relativePath, game.State, game.Reason, state, reason)
			}
			return
		}
	}
	t.Fatalf("game %q not found", relativePath)
}

func TestScannerSkipsCueReferencedCompanions(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "other.img"), []byte("standalone"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.cue"), []byte("FILE \"game.img\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.img"), []byte("track"))
	store := openScannerStore(t)
	root := Root{ID: "psx-main", System: "psx", Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 2 {
		t.Fatalf("report = %+v", report)
	}
	paths := scannerGamePaths(scannerGames(t, store))
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, []string{"game.cue", "other.img"}) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestScannerKeepsUnrelatedBasenameWhenCueNamesSubdirectory(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "tracks", "track.img"), []byte("referenced"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "track.img"), []byte("standalone"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.cue"), []byte("FILE \"tracks/track.img\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n"))
	store := openScannerStore(t)
	root := Root{ID: "psx-main", System: "psx", Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 2 {
		t.Fatalf("report = %+v", report)
	}
	paths := scannerGamePaths(scannerGames(t, store))
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, []string{"game.cue", "track.img"}) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestScannerDoesNotSkipROMsReferencedByUnsupportedCue(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.sfc"), []byte("rom"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "other.sfc"), []byte("other"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.cue"), []byte("FILE \"game.sfc\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n"))
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 2 {
		t.Fatalf("report = %+v", report)
	}
	paths := scannerGamePaths(scannerGames(t, store))
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, []string{"game.sfc", "other.sfc"}) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestScannerSkipsCaseMismatchedCueCompanions(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.img"), []byte("track"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "other.img"), []byte("standalone"))
	if _, err := os.Lstat(filepath.Join(rootPath, "GAME.IMG")); err != nil {
		t.Skip("filesystem is case-sensitive")
	}
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.cue"), []byte("FILE \"GAME.IMG\" BINARY\nTRACK 01 MODE1/2352\nINDEX 01 00:00:00\n"))
	store := openScannerStore(t)
	root := Root{ID: "psx-main", System: "psx", Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 2 {
		t.Fatalf("report = %+v", report)
	}
	paths := scannerGamePaths(scannerGames(t, store))
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, []string{"game.cue", "other.img"}) {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestScannerOperatorSNESRootIndexesActRaiserForHostSearch(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "ActRaiser.smc"), []byte("actraiser"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "ActRaiser (USA).sfc"), []byte("actraiser-usa"))
	mustWriteScannerFile(t, filepath.Join(rootPath, "ActRaiser 2 (USA).sfc"), []byte("actraiser-2"))
	store := openScannerStore(t)
	root := Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].RootID != "operator-snes-root" || report.Roots[0].Offline || report.Roots[0].Added != 3 {
		t.Fatalf("report = %+v", report)
	}

	page, err := store.QueryGames(ctx, Query{Text: "actraiser", Grouped: true, Limit: 20})
	if err != nil {
		t.Fatalf("QueryGames: %v", err)
	}
	var foundExact bool
	for _, game := range page.Games {
		if game.LibraryID != "operator-snes-root" {
			t.Fatalf("library = %q", game.LibraryID)
		}
		if ExactActRaiserTitle(game.CanonicalTitle, game.Title) {
			foundExact = true
			if game.SearchAliases != SeededActRaiserAlias {
				t.Fatalf("aliases = %q", game.SearchAliases)
			}
		}
	}
	if !foundExact {
		t.Fatalf("ActRaiser missing from %+v", page.Games)
	}

	detail := scannerGameByPath(t, store, "ActRaiser.smc")
	if !detail.RootOnline || detail.State != SourceStateAvailable || !ExactActRaiserTitle(detail.CanonicalTitle, detail.Title) {
		t.Fatalf("plain ActRaiser = %+v", detail)
	}
}

func assertReasonsPathFree(t *testing.T, rootPath string, games []Game) {
	t.Helper()
	for _, game := range games {
		if game.Reason != "" && (strings.Contains(game.Reason, rootPath) || strings.ContainsAny(game.Reason, `/\\`)) {
			t.Errorf("game %q reason leaks a path: %q", game.RelativePath, game.Reason)
		}
	}
}
