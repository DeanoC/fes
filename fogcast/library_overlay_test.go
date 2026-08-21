package fogcast

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type capturingCatalog struct {
	*fakeServiceCatalog
	last catalog.Query
}

func (c *capturingCatalog) QueryGames(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	c.last = query
	return c.fakeServiceCatalog.QueryGames(ctx, query)
}

func TestLibrarySettingsOverlayAppliesImmediatelyAndSurvivesReload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	overlay := filepath.Join(dir, "library-settings.json")
	store := &capturingCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{{
		ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}}}}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa", "world"}},
			RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
		},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithLibraryOverlayPath(overlay),
	)
	if got := service.AttractIdleSeconds(); got != 60 {
		t.Fatalf("initial idle = %d", got)
	}
	if _, err := service.QueryGames(ctx, catalog.Query{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(store.last.PreferredRegions, ","); got != "usa,world" {
		t.Fatalf("initial regions = %s", got)
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 12,
		PreferredRegions:   []string{"Japan", "europe"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := service.AttractIdleSeconds(); got != 12 {
		t.Fatalf("updated idle = %d", got)
	}
	if _, err := service.QueryGames(ctx, catalog.Query{Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(store.last.PreferredRegions, ","); got != "japan,europe" {
		t.Fatalf("updated regions = %s", got)
	}
	body, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"token", "client_secret", "host_emulator", "libraries"} {
		if strings.Contains(string(body), leaked) {
			t.Fatalf("overlay leaked %q: %s", leaked, body)
		}
	}

	reopened := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa", "world"}},
			RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: store.games}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithLibraryOverlayPath(overlay),
	)
	settings := reopened.LibrarySettings()
	if settings.AttractIdleSeconds != 12 || strings.Join(settings.PreferredRegions, ",") != "japan,europe" {
		t.Fatalf("reopened settings = %+v", settings)
	}
	if reopened.AttractIdleSeconds() != 12 {
		t.Fatalf("reopened idle = %d", reopened.AttractIdleSeconds())
	}
}

func TestSetLibrarySettingsSerializesPersistAndPublish(t *testing.T) {
	ctx := context.Background()
	overlay := filepath.Join(t.TempDir(), "library-settings.json")
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
		},
		Paths{Staging: "/private/staging"},
		&fakeServiceCatalog{games: []catalog.Game{{
			ID: "snes-mario-test", Title: "Mario", System: protocol.SystemSNES,
			Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
		}}},
		&fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithLibraryOverlayPath(overlay),
	)

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstSave atomic.Bool
	libraryOverlaySaveHook = func() {
		if firstSave.CompareAndSwap(false, true) {
			close(firstEntered)
			<-releaseFirst
		}
	}
	t.Cleanup(func() { libraryOverlaySaveHook = nil })

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 12,
			PreferredRegions:   []string{"japan"},
		})
	}()
	select {
	case <-firstEntered:
	case err := <-firstDone:
		t.Fatalf("first write finished before persist hook: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("first write did not reach persist hook")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 8,
			PreferredRegions:   []string{"europe"},
		})
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("overlapping write completed while the first persist was still unpublished: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second write: %v", err)
	}

	settings := service.LibrarySettings()
	body, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	var file libraryOverlayFile
	if err := json.Unmarshal(body, &file); err != nil {
		t.Fatal(err)
	}
	fileIdle := 0
	if file.AttractIdleSeconds != nil {
		fileIdle = *file.AttractIdleSeconds
	}
	if settings.AttractIdleSeconds != fileIdle {
		t.Fatalf("memory idle %d != file idle %d", settings.AttractIdleSeconds, fileIdle)
	}
	if strings.Join(settings.PreferredRegions, ",") != strings.Join(file.PreferredRegions, ",") {
		t.Fatalf("memory regions %v != file regions %v", settings.PreferredRegions, file.PreferredRegions)
	}
	if settings.AttractIdleSeconds != 8 || strings.Join(settings.PreferredRegions, ",") != "europe" {
		t.Fatalf("expected later write to win consistently, got %+v file=%+v", settings, file)
	}
}

func TestLibrarySettingsRejectInvalidOverlay(t *testing.T) {
	ctx := context.Background()
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	if err := service.SetLibrarySettings(ctx, LibraryConfig{AttractIdleSeconds: -3}); err == nil {
		t.Fatal("expected invalid idle to fail")
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{PreferredRegions: []string{"usa", "usa"}}); err == nil {
		t.Fatal("expected duplicate region to fail")
	}
	if service.AttractIdleSeconds() != 60 {
		t.Fatalf("rejected write mutated idle = %d", service.AttractIdleSeconds())
	}
}

func TestLoadLibraryOverlayTreatsEmptyFileAsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library-settings.json")
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, err := loadLibraryOverlay(path)
	if err != nil || ok {
		t.Fatalf("empty overlay = ok=%v err=%v", ok, err)
	}
}

func TestLibraryOverlayPathPrefersExplicitSibling(t *testing.T) {
	got := libraryOverlayPath(Paths{
		UserLibrary:     "/private/share/library-user.sqlite3",
		LibrarySettings: "/private/share/library-settings.json",
	})
	if got != "/private/share/library-settings.json" {
		t.Fatalf("path = %q", got)
	}
	derived := libraryOverlayPath(Paths{UserLibrary: "/private/share/library-user.sqlite3"})
	if derived != "/private/share/library-settings.json" {
		t.Fatalf("derived = %q", derived)
	}
}
