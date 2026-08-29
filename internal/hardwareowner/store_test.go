package hardwareowner

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Store{Path: filepath.Join(dir, "hardware-owner-v1.json"), ExpectedUID: uint32(os.Getuid())}
}

func recoveringIntentAfterNormalMain() Record {
	next := recoveringIntentRecord()
	next.ActiveGeneration = 42
	next.GenerationHighWater = 43
	next.CandidateGeneration = 43
	return next
}

func TestStoreLoadMissingRecordIsTheOnlyInitializationResult(t *testing.T) {
	store := testStore(t)
	record, exists, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("missing record reported as existing")
	}
	if !reflect.DeepEqual(record, Record{}) {
		t.Fatalf("missing record = %#v, want zero record", record)
	}
}

func TestStoreReplaceLoadAndCanonicalPermissions(t *testing.T) {
	store := testStore(t)
	want := normalMainRecord()
	if err := store.Replace(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode/type = %s, want regular 0600", info.Mode())
	}
	uid, ok := statUID(info)
	if !ok || uid != uint32(os.Getuid()) {
		t.Fatalf("record uid = %d (ok=%v), want test uid %d", uid, ok, os.Getuid())
	}
	got, exists, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !exists || !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded = (%#v,%v), want (%#v,true)", got, exists, want)
	}
	wantBytes, err := want.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	gotBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(wantBytes) || !strings.HasSuffix(string(gotBytes), "\n") {
		t.Fatalf("stored bytes = %q, want canonical newline-terminated bytes %q", gotBytes, wantBytes)
	}
}

func TestStoreReplaceIsAtomicAndLeavesNoTemporaryFiles(t *testing.T) {
	store := testStore(t)
	if err := store.Replace(normalMainRecord()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(recoveringIntentAfterNormalMain()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("replace reused the old inode instead of renaming a temporary file")
	}
	entries, err := os.ReadDir(filepath.Dir(store.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(store.Path) {
		t.Fatalf("temporary files remain: %#v", entries)
	}
}

func TestStoreReplaceRejectsGenerationHighWaterRegression(t *testing.T) {
	store := testStore(t)
	old := normalMainRecord()
	if err := store.Replace(old); err != nil {
		t.Fatal(err)
	}
	next := normalMainStartingRecord()
	next.GenerationHighWater = 41
	next.ActiveGeneration = 41
	if err := store.Replace(next); err == nil {
		t.Fatal("generation high-water regression was accepted")
	}
	got, exists, err := store.Load()
	if err != nil || !exists || !reflect.DeepEqual(got, old) {
		t.Fatalf("record after rejected regression = (%#v,%v,%v), want old record", got, exists, err)
	}
}

func TestStoreReplaceFsyncsFileAndParentBeforeSuccess(t *testing.T) {
	store := testStore(t)
	var fileSyncs, parentSyncs int
	store.syncFile = func(*os.File) error {
		fileSyncs++
		return nil
	}
	store.syncParent = func(*os.File) error {
		parentSyncs++
		return nil
	}
	if err := store.Replace(normalMainRecord()); err != nil {
		t.Fatal(err)
	}
	if fileSyncs == 0 || parentSyncs == 0 {
		t.Fatalf("fsync calls = file %d, parent %d; want both", fileSyncs, parentSyncs)
	}
}

func TestStoreReplacePreservesOldRecordWhenRenameFails(t *testing.T) {
	store := testStore(t)
	old := normalMainRecord()
	if err := store.Replace(old); err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	store.rename = func(string, string) error { return syscall.EIO }
	if err := store.Replace(recoveringIntentAfterNormalMain()); err == nil {
		t.Fatal("injected rename failure was ignored")
	}
	newBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(newBytes) != string(oldBytes) {
		t.Fatalf("old record changed after failed rename: got %q, want %q", newBytes, oldBytes)
	}
	loaded, exists, err := store.Load()
	if err != nil || !exists || !reflect.DeepEqual(loaded, old) {
		t.Fatalf("load after failed rename = (%#v,%v,%v), want old record", loaded, exists, err)
	}
}

func TestStoreReplacePreservesOldRecordWhenParentFsyncFails(t *testing.T) {
	store := testStore(t)
	old := normalMainRecord()
	if err := store.Replace(old); err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	store.syncParent = func(*os.File) error { return syscall.EIO }
	if err := store.Replace(recoveringIntentAfterNormalMain()); err == nil {
		t.Fatal("injected parent fsync failure was ignored")
	}
	newBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(newBytes) != string(oldBytes) {
		t.Fatalf("old record changed after failed parent fsync: got %q, want %q", newBytes, oldBytes)
	}
}

func TestStoreReplaceRejectsRecordSymlinkWithoutTouchingTarget(t *testing.T) {
	store := testStore(t)
	oldPath := filepath.Join(filepath.Dir(store.Path), "old.json")
	old := normalMainRecord()
	oldBytes, err := old.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, oldBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(oldPath), store.Path); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace(normalMainStartingRecord()); err == nil {
		t.Fatal("record symlink accepted")
	}
	got, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(oldBytes) {
		t.Fatal("symlink target was modified")
	}
}

func TestStoreLoadRejectsWrongModeAndNonRegularRecord(t *testing.T) {
	store := testStore(t)
	bytes, err := normalMainRecord().MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("wrong mode record was accepted")
	}
	if err := os.Remove(store.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("directory record was accepted")
	}
}

func TestStoreLoadRejectsUnsafeParent(t *testing.T) {
	root := t.TempDir()
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(unsafe, "hardware-owner-v1.json"), ExpectedUID: uint32(os.Getuid())}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("world-accessible parent accepted")
	}
	if err := os.Chmod(unsafe, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(unsafe, link); err != nil {
		t.Fatal(err)
	}
	store.Path = filepath.Join(link, "hardware-owner-v1.json")
	if _, _, err := store.Load(); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestProductionConstructorsRequireRootOwnership(t *testing.T) {
	store := NewProductionStore()
	if store == nil || store.ExpectedUID != 0 || store.Path != OwnerPath {
		t.Fatalf("production store = %#v, want path %q and uid 0", store, OwnerPath)
	}
	locker := NewProductionLocker()
	if locker == nil || locker.ExpectedUID != 0 || locker.Path != LockPath {
		t.Fatalf("production locker = %#v, want path %q and uid 0", locker, LockPath)
	}
}

func TestStoreLoadTreatsOnlyExactMissingPathAsAbsent(t *testing.T) {
	store := testStore(t)
	if err := os.Remove(filepath.Dir(store.Path)); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.Load(); err == nil || exists {
		t.Fatalf("missing parent load = exists %v, err %v; want non-initialization failure", exists, err)
	}
}

func TestStoreReplaceErrorDoesNotLeaveTemporaryRecord(t *testing.T) {
	store := testStore(t)
	store.syncFile = func(*os.File) error { return fmt.Errorf("injected sync failure") }
	if err := store.Replace(normalMainRecord()); err == nil {
		t.Fatal("injected file fsync failure was ignored")
	}
	entries, err := os.ReadDir(filepath.Dir(store.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary record remains after failed replace: %#v", entries)
	}
}

func TestFixRound4PostRenameFailureDurablyRestoresOldRecord(t *testing.T) {
	store := testStore(t)
	old := normalMainRecord()
	if err := store.Replace(old); err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}

	var fileSyncs, renames, parentSyncs int
	store.syncFile = func(file *os.File) error {
		fileSyncs++
		return file.Sync()
	}
	store.rename = func(oldPath, newPath string) error {
		renames++
		return os.Rename(oldPath, newPath)
	}
	store.syncParent = func(parent *os.File) error {
		parentSyncs++
		if parentSyncs == 1 {
			return syscall.EIO
		}
		return parent.Sync()
	}

	if err := store.Replace(recoveringIntentAfterNormalMain()); err == nil {
		t.Fatal("injected post-rename failure was ignored")
	}
	gotBytes, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBytes) != string(oldBytes) {
		t.Fatalf("reported failure did not restore old bytes: got %q, want %q", gotBytes, oldBytes)
	}
	if fileSyncs != 3 || renames != 2 || parentSyncs != 2 {
		t.Fatalf("durability calls = file fsync %d, rename %d, parent fsync %d; want 3, 2, 2", fileSyncs, renames, parentSyncs)
	}
	entries, err := os.ReadDir(filepath.Dir(store.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(store.Path) {
		t.Fatalf("rollback temporary files remain: %#v", entries)
	}
}
