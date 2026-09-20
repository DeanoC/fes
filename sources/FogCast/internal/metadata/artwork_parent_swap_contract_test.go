package metadata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContractOrphanArtworkGCFailsClosedOnParentSwap(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	digest := strings.Repeat("a", 64)
	artwork := filepath.Join(root, "artwork")
	if err := os.WriteFile(filepath.Join(artwork, digest), []byte("private orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalArtwork := filepath.Join(t.TempDir(), "artwork")
	if err := os.MkdirAll(externalArtwork, 0o700); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(externalArtwork, digest)
	if err := os.WriteFile(externalPath, []byte("external sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := installArtworkParentSwap(t, artwork, externalArtwork)
	err = cache.PurgeProvider(ProviderIGDB)
	restore()
	if opCode(err) != ErrStorage {
		t.Fatalf("orphan GC after parent swap = %v, want storage failure", err)
	}
	assertFileContent(t, externalPath, "external sentinel")

	if err := os.Remove(filepath.Join(artwork, digest)); err != nil {
		t.Fatal(err)
	}
	if err := cache.gcOrphanArtworkFiles(); err != nil {
		t.Fatalf("normal orphan GC after restoring private root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(artwork, digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private orphan remains after normal GC: %v", err)
	}
}

func TestContractArtworkTempCleanupFailsClosedOnParentSwapDuringReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	cache, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	artwork := filepath.Join(root, "artwork")
	tempName := ".tmp-parent-swap"
	if err := os.WriteFile(filepath.Join(artwork, tempName), []byte("private temp"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalArtwork := filepath.Join(t.TempDir(), "artwork")
	if err := os.MkdirAll(externalArtwork, 0o700); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(externalArtwork, tempName)
	if err := os.WriteFile(externalPath, []byte("external sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := installArtworkParentSwap(t, artwork, externalArtwork)
	opened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	restore()
	if opened != nil {
		_ = opened.Close()
	}
	if opCode(err) != ErrStorage {
		t.Fatalf("temp cleanup after parent swap = %v, want storage failure", err)
	}
	assertFileContent(t, externalPath, "external sentinel")

	opened, err = OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatalf("normal cache reopen after restoring private root: %v", err)
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(artwork, tempName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private temporary file remains after normal reopen cleanup: %v", err)
	}
}

func TestContractArtworkPublicationFailsClosedOnParentSwap(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	content := []byte("new private artwork")
	digest := digestBytes(content)
	artwork := filepath.Join(root, "artwork")
	externalArtwork := filepath.Join(t.TempDir(), "artwork")
	if err := os.MkdirAll(externalArtwork, 0o700); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(externalArtwork, digest)
	if err := os.WriteFile(externalPath, []byte("external sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := installArtworkParentSwapAndMoveTemp(t, artwork, externalArtwork)
	err = fetcher.publish(digest, content)
	restore()
	if err == nil {
		t.Fatal("publication after parent swap unexpectedly succeeded")
	}
	assertFileContent(t, externalPath, "external sentinel")

	normalContent := []byte("normal private artwork")
	normalDigest := digestBytes(normalContent)
	if err := fetcher.publish(normalDigest, normalContent); err != nil {
		t.Fatalf("normal artwork publication after restoring private root: %v", err)
	}
	opened, err := fetcher.OpenDigest(normalDigest)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Reader.Close()
	stored, err := io.ReadAll(opened.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, normalContent) {
		t.Fatalf("normal artwork content = %q, want %q", stored, normalContent)
	}
}

func TestContractArtworkServingFailsClosedOnParentSwap(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	fetcher, err := NewArtworkFetcher(ArtworkFetcherConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()

	content := []byte("private artwork")
	digest := digestBytes(content)
	if err := fetcher.publish(digest, content); err != nil {
		t.Fatal(err)
	}
	artwork := filepath.Join(root, "artwork")
	externalArtwork := filepath.Join(t.TempDir(), "artwork")
	if err := os.MkdirAll(externalArtwork, 0o700); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(externalArtwork, digest)
	if err := os.WriteFile(externalPath, []byte("external sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := installArtworkParentSwap(t, artwork, externalArtwork)
	opened, err := fetcher.OpenDigest(digest)
	if opened.Reader != nil {
		_ = opened.Reader.Close()
	}
	restore()
	if opCode(err) != ErrStorage {
		t.Fatalf("artwork serving after parent swap = %v, want storage failure", err)
	}
	assertFileContent(t, externalPath, "external sentinel")

	opened, err = fetcher.OpenDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Reader.Close()
	stored, err := io.ReadAll(opened.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, content) {
		t.Fatalf("normal artwork serving content = %q, want %q", stored, content)
	}
}

func installArtworkParentSwap(t *testing.T, artwork, externalArtwork string) func() {
	t.Helper()
	return installArtworkParentSwapWith(t, artwork, externalArtwork, nil)
}

func installArtworkParentSwapAndMoveTemp(t *testing.T, artwork, externalArtwork string) func() {
	t.Helper()
	return installArtworkParentSwapWith(t, artwork, externalArtwork, func(moved string) {
		entries, err := os.ReadDir(moved)
		if err != nil {
			t.Fatalf("read moved artwork directory: %v", err)
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".tmp-") {
				continue
			}
			if err := os.Rename(filepath.Join(moved, entry.Name()), filepath.Join(externalArtwork, entry.Name())); err != nil {
				t.Fatalf("move temporary file into redirected artwork directory: %v", err)
			}
			return
		}
		t.Fatal("publication hook did not find temporary artwork file")
	})
}

func installArtworkParentSwapWith(t *testing.T, artwork, externalArtwork string, afterMove func(string)) func() {
	t.Helper()
	moved := artwork + ".moved"
	swapped := false
	restoreHook := setPurgeBeforeDeleteHookForTest(func() {
		if err := os.Rename(artwork, moved); err != nil {
			t.Fatalf("move validated artwork directory: %v", err)
		}
		if err := os.Symlink(externalArtwork, artwork); err != nil {
			t.Fatalf("swap validated artwork directory: %v", err)
		}
		swapped = true
		if afterMove != nil {
			afterMove(moved)
		}
	})
	return func() {
		restoreHook()
		if !swapped {
			return
		}
		if info, err := os.Lstat(artwork); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(artwork); err != nil {
				t.Fatalf("remove swapped artwork symlink: %v", err)
			}
		}
		if err := os.Rename(moved, artwork); err != nil {
			t.Fatalf("restore private artwork directory: %v", err)
		}
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("%s content = %q, want %q", path, content, want)
	}
}
