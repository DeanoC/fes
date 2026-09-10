package kitlauncher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
)

func TestDiskStoreCatalogRoundTrip(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	handle := strings.Repeat("ab", 32)
	snap := CatalogSnapshot{
		Games: []tenfoot.Game{
			{ID: "sonic", Title: "Sonic 2", System: "megadrive", Cover: handle, Launchable: true},
			{ID: "mario", Title: "Mario", System: "snes", Launchable: true},
		},
		Strip: []tenfoot.Game{
			{ID: "sonic", Title: "Sonic 2", System: "megadrive", Cover: handle, Launchable: true},
		},
		StripLabel: "Recent",
		Recents: []tenfoot.Game{
			{ID: "sonic", Title: "Sonic 2", System: "megadrive", Cover: handle, Launchable: true},
		},
	}
	if err := store.SaveCatalog(snap); err != nil {
		t.Fatal(err)
	}
	got, ok := store.LoadCatalog()
	if !ok {
		t.Fatal("expected catalog snapshot")
	}
	if len(got.Games) != 2 || got.Games[0].ID != "sonic" || got.Games[0].Cover != handle || !got.Games[0].Launchable {
		t.Fatalf("games %#v", got.Games)
	}
	if got.StripLabel != "Recent" || len(got.Strip) != 1 || got.Strip[0].ID != "sonic" {
		t.Fatalf("strip %#v label %q", got.Strip, got.StripLabel)
	}
	if len(got.Recents) != 1 || got.Recents[0].ID != "sonic" {
		t.Fatalf("recents %#v", got.Recents)
	}
}

func TestDiskStoreCatalogSkipsUnchangedRewrite(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	snap := CatalogSnapshot{Games: []tenfoot.Game{{ID: "pong", Title: "Pong", System: "pong", Launchable: true}}}
	if err := store.SaveCatalog(snap); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.root, catalogFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCatalog(snap); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unchanged catalog was rewritten")
	}
}

func TestDiskStoreCoverRoundTrip(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	handle := strings.Repeat("cd", 32)
	blob := []byte("cover-bytes-not-an-image")
	if err := store.SaveArtwork(handle, blob); err != nil {
		t.Fatal(err)
	}
	got, ok := store.LoadArtwork(handle)
	if !ok {
		t.Fatal("expected cover blob")
	}
	if string(got) != string(blob) {
		t.Fatalf("blob %q", got)
	}
	info, err := os.Lstat(filepath.Join(store.root, coversDirName, handle))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("cover mode %o", info.Mode().Perm())
	}
}

func TestDiskStoreRejectsInvalidHandle(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	if err := store.SaveArtwork("../escape", []byte("nope")); err == nil {
		t.Fatal("accepted traversal handle")
	}
	if err := store.SaveArtwork("zz", []byte("nope")); err == nil {
		t.Fatal("accepted short handle")
	}
	matches, err := filepath.Glob(filepath.Join(store.root, coversDirName, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("wrote invalid cover %v", matches)
	}
	if _, ok := store.LoadArtwork("../escape"); ok {
		t.Fatal("loaded traversal handle")
	}
}

func TestDiskStoreIgnoresMissingAndCorruptCatalog(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	if _, ok := store.LoadCatalog(); ok {
		t.Fatal("empty store should miss")
	}
	path := filepath.Join(store.root, catalogFileName)
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadCatalog(); ok {
		t.Fatal("corrupt catalog should miss")
	}
	if err := os.WriteFile(path+".tmp", []byte(`{"format":1,"games":[{"id":"torn"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadCatalog(); ok {
		t.Fatal("tmp catalog should not publish")
	}
}

func TestDiskStoreLoadCatalogIgnoresTornReplace(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	snap := CatalogSnapshot{Games: []tenfoot.Game{{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}}}
	if err := store.SaveCatalog(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.root, catalogFileName+".tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	got, ok := store.LoadCatalog()
	if !ok || len(got.Games) != 1 || got.Games[0].ID != "sonic" {
		t.Fatalf("torn tmp replaced published catalog %#v ok=%v", got, ok)
	}
}

func TestOpenDiskStoreRejectsRelativeRoot(t *testing.T) {
	t.Parallel()
	if _, err := OpenDiskStore("launcher-cache"); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestDefaultCacheRootIsFATBesideLauncherJSON(t *testing.T) {
	t.Parallel()
	if DefaultCacheRoot != "/media/fat/fogcast/launcher-cache" {
		t.Fatalf("root %q", DefaultCacheRoot)
	}
	dir := t.TempDir()
	cfg := writeKitConfig(t, dir, "http://127.0.0.1:8789")
	root := cacheRoot(cfg)
	if filepath.Base(root) != "launcher-cache" {
		t.Fatalf("root %q", root)
	}
	if filepath.Dir(root) != filepath.Dir(cfg.path) {
		t.Fatalf("root %q config %q", root, cfg.path)
	}
}

func TestDiskStoreCoverLRURespectsSeparateBudget(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	store.SetCoverMaxBytes(12)
	now := time.Unix(1000, 0)
	store.clock = func() time.Time { return now }
	first := strings.Repeat("aa", 32)
	second := strings.Repeat("bb", 32)
	if err := store.SaveArtwork(first, []byte("12345678")); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(2000, 0)
	if err := store.SaveArtwork(second, []byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadArtwork(first); ok {
		t.Fatal("oldest cover was not evicted")
	}
	if _, ok := store.LoadArtwork(second); !ok {
		t.Fatal("newest cover was evicted")
	}
	status := store.Status()
	if status.CoverUsedBytes != 8 || status.CoverMaxBytes != 12 || status.CoverFreeBytes != 4 {
		t.Fatalf("status %#v", status)
	}
}

func TestDiskStoreCoverLRUTouchesOnLoad(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	store.SetCoverMaxBytes(20)
	now := time.Unix(1000, 0)
	store.clock = func() time.Time { return now }
	first := strings.Repeat("aa", 32)
	second := strings.Repeat("bb", 32)
	third := strings.Repeat("cc", 32)
	if err := store.SaveArtwork(first, []byte("12345678")); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(2000, 0)
	if err := store.SaveArtwork(second, []byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(3000, 0)
	if _, ok := store.LoadArtwork(first); !ok {
		t.Fatal("first cover missing before third save")
	}
	now = time.Unix(4000, 0)
	if err := store.SaveArtwork(third, []byte("zzzzzzzz")); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadArtwork(second); ok {
		t.Fatal("untouched cover was not evicted")
	}
	if _, ok := store.LoadArtwork(first); !ok {
		t.Fatal("touched cover was evicted")
	}
}

func TestCatalogFileUsesFormat1(t *testing.T) {
	t.Parallel()
	store := mustOpenStore(t)
	if err := store.SaveCatalog(CatalogSnapshot{Games: []tenfoot.Game{{ID: "pong", Launchable: true}}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(store.root, catalogFileName))
	if err != nil {
		t.Fatal(err)
	}
	var file catalogFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if file.Format != 1 || len(file.Games) != 1 || file.Games[0].ID != "pong" {
		t.Fatalf("file %#v", file)
	}
}

func mustOpenStore(t *testing.T) *DiskStore {
	t.Helper()
	store, err := OpenDiskStore(filepath.Join(t.TempDir(), "launcher-cache"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
