package fogcast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
)

func TestMaterializeConfinedLibraryCopiesCueSet(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tracks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "game.cue"), []byte("FILE \"tracks/track.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tracks", "track.bin"), []byte("track"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), root, "game.cue", fileFingerprint(t, filepath.Join(root, "game.cue")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if path == filepath.Join(root, "game.cue") || filepath.Base(path) != "game.cue" {
		t.Fatalf("launch path = %q", path)
	}
	copied, err := os.ReadFile(filepath.Join(filepath.Dir(path), "tracks", "track.bin"))
	if err != nil || string(copied) != "track" {
		t.Fatalf("copied companion = %q, %v", copied, err)
	}
}

func TestMaterializeConfinedLibraryRejectsEscapingCue(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "library")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.bin"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "game.cue"), []byte("FILE \"../outside.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), root, "game.cue", fileFingerprint(t, filepath.Join(root, "game.cue")))
	if err == nil && cleanup != nil {
		cleanup()
	}
	if path != "" || cleanup != nil || !errors.Is(err, catalog.ErrEscapingMediaReference) {
		t.Fatalf("escaping cue path=%q cleanup=%t err=%v", path, cleanup != nil, err)
	}
}

func TestMaterializeConfinedLibraryIgnoresLibrarySwapAfterCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "game.sfc")
	if err := os.WriteFile(source, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), root, "game.sfc", fileFingerprint(t, filepath.Join(root, "game.sfc")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	outside := filepath.Join(t.TempDir(), "swapped.sfc")
	if err := os.WriteFile(outside, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, source); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(path)
	if err != nil || string(copied) != "original" {
		t.Fatalf("copied after swap = %q, %v", copied, err)
	}
	if !strings.Contains(path, "fogcast-host-launch-") {
		t.Fatalf("launch path is not private: %q", path)
	}
}

func TestMaterializeConfinedLibraryHonorsCanceledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "game.sfc"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path, cleanup, err := materializeConfinedLibrary(ctx, root, "game.sfc", catalog.Fingerprint{})
	if err == nil && cleanup != nil {
		cleanup()
	}
	if path != "" || cleanup != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled materialize path=%q cleanup=%t err=%v", path, cleanup != nil, err)
	}
}

func TestMaterializeConfinedLibraryRejectsOversizedCue(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("A", 1<<20+32) + "\nFILE \"../outside.bin\" BINARY\n"
	if err := os.WriteFile(filepath.Join(root, "game.cue"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), root, "game.cue", fileFingerprint(t, filepath.Join(root, "game.cue")))
	if err == nil && cleanup != nil {
		cleanup()
	}
	if path != "" || cleanup != nil || !errors.Is(err, catalog.ErrUnvalidatedMediaSheet) {
		t.Fatalf("oversized cue path=%q cleanup=%t err=%v", path, cleanup != nil, err)
	}
}

func fileFingerprint(t *testing.T, path string) catalog.Fingerprint {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return catalog.Fingerprint{SourceSize: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
}

func TestMaterializeConfinedLibraryRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "library")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realRoot, "game.sfc"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "game.sfc"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), link, "game.sfc", fileFingerprint(t, filepath.Join(outside, "game.sfc")))
	if err == nil && cleanup != nil {
		cleanup()
	}
	if path != "" || cleanup != nil || err == nil {
		t.Fatalf("symlink root path=%q cleanup=%t err=%v", path, cleanup != nil, err)
	}
}

func TestMaterializeConfinedLibraryRejectsFingerprintMismatch(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "game.sfc")
	if err := os.WriteFile(source, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrong := catalog.Fingerprint{SourceSize: 3, ModifiedNS: 1}
	path, cleanup, err := materializeConfinedLibrary(context.Background(), root, "game.sfc", wrong)
	if err == nil && cleanup != nil {
		cleanup()
	}
	if path != "" || cleanup != nil || err == nil {
		t.Fatalf("mismatch path=%q cleanup=%t err=%v", path, cleanup != nil, err)
	}
}
