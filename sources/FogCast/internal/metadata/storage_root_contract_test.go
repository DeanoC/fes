package metadata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gosqlite.org/vfs"
)

func TestContractMetadataRootCreatesPrivateDirectoriesAndPreservesAncestors(t *testing.T) {
	base := t.TempDir()
	ancestor := filepath.Join(base, "ancestor")
	if err := os.Mkdir(ancestor, 0o750); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(ancestor, "created", "metadata")

	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	if mode := storageFileMode(t, ancestor); mode != 0o750 {
		t.Fatalf("pre-existing ancestor mode changed: %o", mode)
	}
	for _, path := range []string{filepath.Join(ancestor, "created"), root, filepath.Join(root, "artwork")} {
		if mode := storageFileMode(t, path); mode != 0o700 {
			t.Fatalf("created private directory %s mode = %o, want 700", path, mode)
		}
	}
}

func TestContractMetadataRootParentSwapBeforeCreateCannotEscape(t *testing.T) {
	for _, replacement := range []string{"symlink", "non-directory"} {
		t.Run(replacement, func(t *testing.T) {
			base := t.TempDir()
			parent := filepath.Join(base, "private-parent")
			root := filepath.Join(parent, "metadata")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			externalSentinel := filepath.Join(external, "sentinel")
			if err := os.WriteFile(externalSentinel, []byte("must-remain"), 0o644); err != nil {
				t.Fatal(err)
			}
			moved := parent + ".moved"
			var once sync.Once
			restoreHooks := setStorageRootHooksForTest(storageRootHooks{
				beforeCreate: func(path string) {
					if filepath.Clean(path) != filepath.Clean(root) {
						return
					}
					once.Do(func() {
						if err := os.Rename(parent, moved); err != nil {
							t.Fatalf("move private parent: %v", err)
						}
						if replacement == "symlink" {
							if err := os.Symlink(external, parent); err != nil {
								t.Fatalf("install external parent symlink: %v", err)
							}
							return
						}
						if err := os.WriteFile(parent, []byte("parent sentinel"), 0o644); err != nil {
							t.Fatalf("install non-directory parent: %v", err)
						}
					})
				},
			})
			cache, openErr := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
			restoreHooks()
			if cache != nil {
				_ = cache.Close()
			}
			if opCode(openErr) != ErrStorage {
				t.Fatalf("parent swap during missing-root creation = %v, want storage", openErr)
			}
			if _, err := os.Lstat(filepath.Join(external, "metadata")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("external metadata was created: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(moved, "metadata")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed initialization left metadata in the held moved tree: %v", err)
			}
			assertFileContent(t, externalSentinel, "must-remain")
			if replacement == "symlink" {
				if err := os.Remove(parent); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(parent); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(moved, parent); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestContractMetadataRootDescriptorChmodCannotTouchReplacement(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "private-parent")
	root := filepath.Join(parent, "metadata")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	externalRoot := filepath.Join(external, "metadata")
	if err := os.Mkdir(externalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	externalSentinel := filepath.Join(externalRoot, "sentinel")
	if err := os.WriteFile(externalSentinel, []byte("must-remain"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := parent + ".moved"
	var once sync.Once
	restoreHooks := setStorageRootHooksForTest(storageRootHooks{
		beforeChmod: func(path string) {
			if filepath.Clean(path) != filepath.Clean(root) {
				return
			}
			once.Do(func() {
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
	restoreHooks()
	if cache != nil {
		_ = cache.Close()
	}
	if opCode(openErr) != ErrStorage {
		t.Fatalf("descriptor chmod after parent swap = %v, want storage", openErr)
	}
	if mode := storageFileMode(t, externalRoot); mode != 0o755 {
		t.Fatalf("external directory mode changed: %o", mode)
	}
	assertFileContent(t, externalSentinel, "must-remain")
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, parent); err != nil {
		t.Fatal(err)
	}
}

func TestContractCommittedRootRemainsConfinedAfterAmbientParentReplacement(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "private-parent")
	rootPath := filepath.Join(parent, "metadata")
	root, err := acquirePrivateRoot(rootPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.commit(); err != nil {
		_ = root.abort(false)
		t.Fatal(err)
	}
	defer root.abort(false)

	moved := parent + ".moved"
	external := t.TempDir()
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Fatal(err)
	}
	if err := root.root.Mkdir("held-child", 0o700); err != nil {
		t.Fatalf("held root operation failed after parent rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "metadata", "held-child")); err != nil {
		t.Fatalf("held root operation did not stay with moved private tree: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(external, "held-child")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("held root escaped into replacement tree: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, parent); err != nil {
		t.Fatal(err)
	}
}

func TestContractCacheRootLeaseUsesPhysicalIdentityAcrossDarwinAliases(t *testing.T) {
	privateTmp, err := os.Stat("/private/tmp")
	if err != nil {
		t.Skipf("/private/tmp unavailable: %v", err)
	}
	tmp, err := os.Stat("/tmp")
	if err != nil || !os.SameFile(tmp, privateTmp) {
		t.Skip("/tmp and /private/tmp are not the same physical directory")
	}
	base, err := os.MkdirTemp("/tmp", "fogcast-lease-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	root := filepath.Join(base, "metadata")
	alias := filepath.Join("/private/tmp", strings.TrimPrefix(base, "/tmp"), "metadata")

	first, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	if second, err := OpenCache(context.Background(), CacheConfig{Root: alias, CredentialScope: "scope"}); second != nil || opCode(err) != ErrStorage {
		if second != nil {
			_ = second.Close()
		}
		_ = first.Close()
		t.Fatalf("physical alias duplicate = cache:%v err:%v", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCache(context.Background(), CacheConfig{Root: alias, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
}

func TestContractSettingsMismatchUnregistersFailedVFSBeforeRetry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	first, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	before := sqliteVFSSequence.Load()
	second, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope-b"})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after := sqliteVFSSequence.Load()
	if after-before < 2 {
		t.Fatalf("settings mismatch did not perform a failed attempt and retry: attempts=%d", after-before)
	}
	for sequence := before + 1; sequence <= after; sequence++ {
		name := fmt.Sprintf("fogcast-cache-%d", sequence)
		registered, ok := vfs.Find(name)
		if name == second.sqliteVFSName {
			if !ok || registered == nil {
				t.Fatalf("successful retry VFS %q is absent", name)
			}
			continue
		}
		if ok || registered != nil {
			t.Fatalf("failed settings-mismatch VFS %q remained registered", name)
		}
	}
}

func storageFileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
