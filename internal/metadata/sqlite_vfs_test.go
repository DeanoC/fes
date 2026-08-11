package metadata

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gosqlite.org/vfs"
)

func TestContractSQLiteVFSRejectsUnownedNames(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	for _, name := range []string{
		"",
		"cache.sqlite3/../outside",
		"../cache.sqlite3",
		"/tmp/cache.sqlite3",
		"cache.sqlite3-unknown",
		"cache.sqlite3-super-journal",
		"cache.sqlite3.tmp",
	} {
		if _, _, err := cache.sqliteVFS.Open(name, vfs.OpenReadWrite); err == nil {
			t.Fatalf("unsafe VFS name %q was accepted", name)
		}
	}
	for _, name := range []string{"cache.sqlite3", "cache.sqlite3-wal", "cache.sqlite3-journal"} {
		full, err := cache.sqliteVFS.FullPathname(name)
		if err != nil {
			t.Fatalf("FullPathname(%q): %v", name, err)
		}
		if _, _, err := cache.sqliteVFS.Open(full, vfs.OpenReadWrite|vfs.OpenCreate); err != nil {
			t.Fatalf("allowed VFS name %q: %v", name, err)
		}
	}
}

func TestContractSQLiteCacheParentSwapDuringOpenLeavesExternalSentinelUntouched(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "private-parent")
	root := filepath.Join(parent, "metadata")
	if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
		t.Fatal(err)
	}

	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	externalDB := filepath.Join(external, "cache.sqlite3")
	createSQLiteSentinel(t, externalDB)
	if err := os.Chmod(externalDB, 0o644); err != nil {
		t.Fatal(err)
	}
	beforeContent := readFileBytes(t, externalDB)
	beforeMode := lstatMode(t, externalDB)

	var once sync.Once
	restore := setSQLiteVFSHooksForTest(sqliteVFSHooks{
		beforeOpen: func(name string) {
			if !strings.HasSuffix(name, "cache.sqlite3") {
				return
			}
			once.Do(func() {
				moved := parent + ".moved"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatalf("move private parent: %v", err)
				}
				if err := os.Symlink(external, parent); err != nil {
					t.Fatalf("install external parent symlink: %v", err)
				}
			})
		},
	})
	cache, openErr := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	restore()
	if cache != nil {
		_ = cache.Close()
	}
	restoreSwappedParent(t, parent, parent+".moved")

	if content := readFileBytes(t, externalDB); string(content) != string(beforeContent) {
		t.Fatalf("external sentinel content changed: before=%x after=%x", beforeContent, content)
	}
	if mode := lstatMode(t, externalDB); mode != beforeMode {
		t.Fatalf("external sentinel mode changed: before=%o after=%o", beforeMode, mode)
	}
	if openErr == nil {
		t.Fatal("parent swap during VFS open unexpectedly succeeded without an identity error")
	}
}

func TestContractSQLiteCacheDescriptorChmodCannotTouchSwappedParent(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "private-parent")
	root := filepath.Join(parent, "metadata")
	if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
		t.Fatal(err)
	}

	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	externalDB := filepath.Join(external, "cache.sqlite3")
	createSQLiteSentinel(t, externalDB)
	if err := os.Chmod(externalDB, 0o644); err != nil {
		t.Fatal(err)
	}
	beforeContent := readFileBytes(t, externalDB)
	beforeMode := lstatMode(t, externalDB)

	var once sync.Once
	restore := setSQLiteVFSHooksForTest(sqliteVFSHooks{
		afterOpen: func(name string, _ *os.File) {
			if !strings.HasSuffix(name, "cache.sqlite3") {
				return
			}
			once.Do(func() {
				moved := parent + ".moved"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatalf("move private parent: %v", err)
				}
				if err := os.Symlink(external, parent); err != nil {
					t.Fatalf("install external parent symlink: %v", err)
				}
			})
		},
	})
	cache, openErr := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	restore()
	if cache != nil {
		_ = cache.Close()
	}
	restoreSwappedParent(t, parent, parent+".moved")

	if content := readFileBytes(t, externalDB); string(content) != string(beforeContent) {
		t.Fatalf("external sentinel content changed: before=%x after=%x", beforeContent, content)
	}
	if mode := lstatMode(t, externalDB); mode != beforeMode {
		t.Fatalf("external sentinel mode changed: before=%o after=%o", beforeMode, mode)
	}
	if openErr == nil {
		t.Fatal("parent swap before descriptor chmod unexpectedly succeeded")
	}
}

func TestContractSQLiteCacheLeafSwapFailsClosedWithoutFollowingSymlink(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "private-parent")
	root := filepath.Join(parent, "metadata")
	if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
		t.Fatal(err)
	}

	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("do not follow"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeContent := readFileBytes(t, sentinel)
	beforeMode := lstatMode(t, sentinel)

	var once sync.Once
	restore := setSQLiteVFSHooksForTest(sqliteVFSHooks{
		beforeOpen: func(name string) {
			if !strings.HasSuffix(name, "cache.sqlite3") {
				return
			}
			once.Do(func() {
				if err := os.Symlink(sentinel, filepath.Join(root, "cache.sqlite3")); err != nil {
					t.Fatalf("install database symlink: %v", err)
				}
			})
		},
	})
	cache, openErr := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	restore()
	if cache != nil {
		_ = cache.Close()
	}
	if openErr == nil {
		t.Fatal("database leaf symlink unexpectedly opened")
	}
	if content := readFileBytes(t, sentinel); string(content) != string(beforeContent) {
		t.Fatalf("symlink target content changed: before=%q after=%q", beforeContent, content)
	}
	if mode := lstatMode(t, sentinel); mode != beforeMode {
		t.Fatalf("symlink target mode changed: before=%o after=%o", beforeMode, mode)
	}
}

func TestContractSQLiteCacheUsesWALWithoutPhysicalSHMAndReopens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		var operation *OpError
		if errors.As(err, &operation) {
			t.Logf("open cause: %v", operation.cause)
		}
		t.Fatal(err)
	}
	key, err := BuildCacheKey(ProviderIGDB, "58", "wal", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Put(key, Result{Outcome: OutcomeExact}, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if len(cache.sqliteVFS.openedLeavesSnapshot()) == 0 {
		t.Fatal("rooted VFS did not observe any SQLite opens")
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "cache.sqlite3-shm")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("physical SHM residue = %v", err)
	}

	reopened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		var operation *OpError
		if errors.As(err, &operation) {
			t.Logf("reopen cause: %v", operation.cause)
		}
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, ok, err := reopened.Get(key); err != nil || !ok {
		t.Fatalf("reopened cache lookup: ok=%v err=%v", ok, err)
	}
}

func TestContractSQLiteCacheRemovesRegularLegacySHMButRejectsSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "cache.sqlite3-shm")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err = OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatalf("regular legacy SHM reopen: %v", err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacy); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy SHM was not removed: %v", err)
	}

	sentinel := filepath.Join(t.TempDir(), "shm-target")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"}); err == nil {
		t.Fatal("legacy SHM symlink unexpectedly accepted")
	}
	if content := readFileBytes(t, sentinel); string(content) != "keep" {
		t.Fatalf("legacy SHM symlink target changed: %q", content)
	}
}

func createSQLiteSentinel(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sentinel(value TEXT)`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func lstatMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func restoreSwappedParent(t *testing.T, parent, moved string) {
	t.Helper()
	if info, err := os.Lstat(parent); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(parent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(parent); errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(moved, parent); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContractSQLiteCacheRejectsDuplicateRootOwnership(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	first, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"}); second != nil || opCode(err) != ErrStorage {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("duplicate cache = cache:%v err:%v", second, err)
	}
}
