package fogcast

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
)

func TestFolderWatchRootsIncludeEveryMappedConfiguredRoot(t *testing.T) {
	snes := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/games/SNES"}
	firstMega := catalog.Root{ID: "genesis-first", System: protocol.SystemMegaDrive, Path: "/games/Genesis"}
	secondMega := catalog.Root{ID: "genesis-second", System: protocol.SystemMegaDrive, Path: "/other/Genesis"}
	gba := catalog.Root{ID: "gba-main", System: protocol.SystemGBA, Path: "/games/GBA"}
	gbc := catalog.Root{ID: "gbc-main", System: protocol.SystemGameBoyColor, Path: "/games/GBC"}
	atari2600 := catalog.Root{ID: "a2600-main", System: protocol.SystemAtari2600, Path: "/games/Atari2600"}
	coleco := catalog.Root{ID: "coleco-main", System: protocol.SystemColecoVision, Path: "/games/ColecoVision"}
	lynx := catalog.Root{ID: "lynx-main", System: protocol.SystemAtariLynx, Path: "/games/AtariLynx"}

	got := FolderWatchRoots([]catalog.Root{snes, firstMega, gba, gbc, atari2600, coleco, lynx, secondMega})
	want := []catalog.Root{snes, firstMega, gba, gbc, atari2600, coleco, lynx, secondMega}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("watch roots = %#v, want %#v", got, want)
	}
}

func TestUnchangedConfiguredSNESPathPreservesUserState(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })

	root := catalog.Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: rootPath}
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	if _, err := scanner.Scan(ctx, []catalog.Root{root}); err != nil {
		t.Fatalf("seed Scan: %v", err)
	}
	games, err := store.Games(ctx)
	if err != nil || len(games) != 1 {
		t.Fatalf("seed games = %+v, %v", games, err)
	}
	gameID := games[0].ID
	if err := users.SetFavorite(ctx, gameID, true); err != nil {
		t.Fatal(err)
	}

	service := newService(
		Config{
			Libraries:      []catalog.Root{root},
			Library:        LibraryConfig{WatchRoot: rootPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
	state, err := service.LibraryState(ctx, gameID)
	if err != nil || !state.Favorite {
		t.Fatalf("user state after unchanged-path reconcile = %+v, %v", state, err)
	}
	if game, err := service.Game(ctx, gameID); err != nil || game.LibraryID != root.ID {
		t.Fatalf("game identity after unchanged-path reconcile = %+v, %v", game, err)
	}
}

func TestServiceFolderWatchRootComesFromConfig(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	firstRoot := catalog.Root{ID: "snes-first", System: protocol.SystemSNES, Path: filepath.Join(first, "SNES")}
	secondRoot := catalog.Root{ID: "snes-second", System: protocol.SystemSNES, Path: filepath.Join(second, "SNES")}
	firstService := newService(
		Config{Libraries: []catalog.Root{firstRoot}, Library: LibraryConfig{WatchRoot: first}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	secondService := newService(
		Config{Libraries: []catalog.Root{secondRoot}, Library: LibraryConfig{WatchRoot: second}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if firstService.FolderWatchRoot() != first || secondService.FolderWatchRoot() != second {
		t.Fatalf("watch roots = %q and %q", firstService.FolderWatchRoot(), secondService.FolderWatchRoot())
	}
	if firstService.FolderWatchRoot() == DefaultFolderWatchRoot || secondService.FolderWatchRoot() == DefaultFolderWatchRoot {
		t.Fatal("service used the candidate UNC instead of config")
	}
	if !reflect.DeepEqual(firstService.folderWatchRoots(), []catalog.Root{firstRoot}) {
		t.Fatalf("first service roots = %#v", firstService.folderWatchRoots())
	}
}

func TestServiceScanAndWatchShareConfiguredMappedRoots(t *testing.T) {
	scanner := &fakeServiceScanner{}
	old := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/old/snes"}
	mega := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: "/mega"}
	service := newService(
		Config{
			Libraries:      []catalog.Root{old, mega},
			Library:        LibraryConfig{WatchRoot: "/new/share"},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scanner.roots, []catalog.Root{old, mega}) {
		t.Fatalf("Scan roots = %#v", scanner.roots)
	}
	if !reflect.DeepEqual(service.folderWatchRoots(), []catalog.Root{old, mega}) {
		t.Fatalf("watch roots = %#v", service.folderWatchRoots())
	}
	if service.rootsByID[old.ID] != old {
		t.Fatalf("Play root = %#v", service.rootsByID[old.ID])
	}
}

func TestChangedWatchRootCanRescanWithoutIdentityConflict(t *testing.T) {
	ctx := context.Background()
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(firstDir, "First.sfc"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondDir, "Second.sfc"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	const configuredID = "operator-snes-root"
	first := newService(
		Config{
			Libraries:      []catalog.Root{{ID: configuredID, System: protocol.SystemSNES, Path: firstDir}},
			Library:        LibraryConfig{WatchRoot: firstDir},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := first.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	second := newService(
		Config{
			Libraries:      []catalog.Root{{ID: configuredID, System: protocol.SystemSNES, Path: secondDir}},
			Library:        LibraryConfig{WatchRoot: secondDir},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if first.folderWatchRoots()[0].ID != configuredID || second.folderWatchRoots()[0].ID != configuredID {
		t.Fatal("configured library id changed with its operator path")
	}
	firstGames, err := first.Games(ctx)
	if err != nil || len(firstGames) != 1 {
		t.Fatalf("first catalog = %+v err=%v", firstGames, err)
	}
	oldID := firstGames[0].ID
	if _, err := second.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("second reconcile after watch_root change: %v", err)
	}
	games, err := second.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, game := range games {
		if game.RelativePath == "First.sfc" || game.LibraryID != configuredID {
			t.Fatalf("rebound library retained stale content: %+v", game)
		}
	}
	if len(games) != 1 || games[0].RelativePath != "Second.sfc" {
		t.Fatalf("rebound catalog = %+v", games)
	}
	if oldID == games[0].ID {
		t.Fatal("different relative path unexpectedly reused a game id")
	}
}

func TestChangedConfiguredSNESIDCanRescanSamePath(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootPath, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	first := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "old-snes-id", System: protocol.SystemSNES, Path: rootPath}},
			Library:        LibraryConfig{WatchRoot: rootPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := first.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	second := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "new-snes-id", System: protocol.SystemSNES, Path: rootPath}},
			Library:        LibraryConfig{WatchRoot: rootPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := second.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("second reconcile after id change: %v", err)
	}
	games, err := second.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].LibraryID != "new-snes-id" || games[0].RelativePath != "Axelay.sfc" {
		t.Fatalf("catalog after id change = %+v", games)
	}
}

func TestConfiguredSNESIDReuseRebindsBeforeReleasingSupersededRoots(t *testing.T) {
	ctx := context.Background()
	currentPath := t.TempDir()
	retainedPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(currentPath, "Current.sfc"), []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(retainedPath, "Retained.sfc"), []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	if _, err := scanner.Scan(ctx, []catalog.Root{
		{ID: "a-current", System: protocol.SystemSNES, Path: currentPath},
		{ID: "z-reused", System: protocol.SystemSNES, Path: retainedPath},
	}); err != nil {
		t.Fatalf("seed scan: %v", err)
	}

	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "z-reused", System: protocol.SystemSNES, Path: currentPath}},
			Library:        LibraryConfig{WatchRoot: currentPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("reconcile reused id: %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].LibraryID != "z-reused" || games[0].RelativePath != "Current.sfc" {
		t.Fatalf("catalog after reused id = %+v", games)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{{ID: "z-reused", System: protocol.SystemSNES, Path: currentPath}}) {
		t.Fatalf("libraries after reused id = %#v", libraries)
	}
}

func TestServiceScanReleasesRetiredSNESRootReclassifiedAsMegaDrive(t *testing.T) {
	ctx := context.Background()
	sharedPath := t.TempDir()
	snesPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sharedPath, "Sonic.md"), []byte("sonic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snesPath, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	oldSNES := catalog.Root{ID: "old-snes-id", System: protocol.SystemSNES, Path: sharedPath}
	if _, err := scanner.Scan(ctx, []catalog.Root{oldSNES}); err != nil {
		t.Fatalf("seed scan: %v", err)
	}

	mega := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: sharedPath}
	snes := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: snesPath}
	service := newService(
		Config{
			Libraries:      []catalog.Root{mega, snes},
			Library:        LibraryConfig{WatchRoot: snesPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.Scan(ctx); err != nil {
		t.Fatalf("Scan(reclassified): %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 {
		t.Fatalf("games after reclassification = %+v", games)
	}
	for _, game := range games {
		if game.LibraryID == oldSNES.ID {
			t.Fatalf("retired SNES identity retained a game: %+v", game)
		}
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{mega, snes}) {
		t.Fatalf("libraries after reclassification = %#v", libraries)
	}
}

func TestServiceScanReleasesMegaDriveRootReclassifiedAsSNES(t *testing.T) {
	for _, test := range []struct {
		name              string
		oldID, newID      string
		moveToNewRootPath bool
	}{
		{name: "new id", oldID: "genesis-main", newID: "snes-main"},
		{name: "reused id", oldID: "shared-main", newID: "shared-main"},
		{name: "reused id at new path", oldID: "shared-main", newID: "shared-main", moveToNewRootPath: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			oldPath := t.TempDir()
			newPath := oldPath
			if test.moveToNewRootPath {
				newPath = t.TempDir()
			}
			if err := os.WriteFile(filepath.Join(oldPath, "Sonic.md"), []byte("sonic"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(newPath, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
			oldMega := catalog.Root{ID: test.oldID, System: protocol.SystemMegaDrive, Path: oldPath}
			if _, err := scanner.Scan(ctx, []catalog.Root{oldMega}); err != nil {
				t.Fatalf("seed scan: %v", err)
			}

			snes := catalog.Root{ID: test.newID, System: protocol.SystemSNES, Path: newPath}
			service := newService(
				Config{
					Libraries:      []catalog.Root{snes},
					Library:        LibraryConfig{WatchRoot: newPath},
					RequestTimeout: time.Second, UploadTimeout: time.Second,
				},
				Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
			)
			if _, err := service.Scan(ctx); err != nil {
				t.Fatalf("Scan(reclassified): %v", err)
			}
			games, err := service.Games(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(games) != 1 || games[0].LibraryID != snes.ID || games[0].System != protocol.SystemSNES || games[0].RelativePath != "Axelay.sfc" {
				t.Fatalf("games after reclassification = %+v", games)
			}
			libraries, err := store.Libraries(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(libraries, []catalog.Root{snes}) {
				t.Fatalf("libraries after reclassification = %#v", libraries)
			}
		})
	}
}

func TestServiceScanReleasesSNESIdentityReclassifiedAsMegaDriveAtNewPath(t *testing.T) {
	ctx := context.Background()
	oldPath := t.TempDir()
	megaPath := t.TempDir()
	snesPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldPath, "Old.sfc"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(megaPath, "Sonic.md"), []byte("sonic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snesPath, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	oldSNES := catalog.Root{ID: "shared-main", System: protocol.SystemSNES, Path: oldPath}
	if _, err := scanner.Scan(ctx, []catalog.Root{oldSNES}); err != nil {
		t.Fatalf("seed scan: %v", err)
	}

	mega := catalog.Root{ID: oldSNES.ID, System: protocol.SystemMegaDrive, Path: megaPath}
	snes := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: snesPath}
	service := newService(
		Config{
			Libraries:      []catalog.Root{mega, snes},
			Library:        LibraryConfig{WatchRoot: snesPath},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.Scan(ctx); err != nil {
		t.Fatalf("Scan(reclassified): %v", err)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{mega, snes}) {
		t.Fatalf("libraries after reclassification = %#v", libraries)
	}
}

func TestServiceRunFolderWatchCountsPersistentReconcileFailures(t *testing.T) {
	scanner := &fakeServiceScanner{err: errors.New("transient smb")}
	service := newService(
		Config{Library: LibraryConfig{WatchRoot: "/snes"}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	service.folderWatchInterval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.RunFolderWatch(ctx) }()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && service.FolderWatchReconcileFailures() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if service.FolderWatchReconcileFailures() < 2 {
		t.Fatalf("failures = %d, want retries", service.FolderWatchReconcileFailures())
	}
}

func TestServiceRunFolderWatchCountsUnresolvedWatchRoot(t *testing.T) {
	scanner := &fakeServiceScanner{}
	service := newService(
		Config{
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	_, err := service.ReconcileFolderWatch(context.Background())
	if !errors.Is(err, errFolderWatchRootUnresolved) {
		t.Fatalf("ReconcileFolderWatch error = %v, want unresolved watch root", err)
	}
	assertServiceErrorCode(t, err, protocol.CodeInternal)

	service.folderWatchInterval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.RunFolderWatch(ctx) }()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && service.FolderWatchReconcileFailures() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if service.FolderWatchReconcileFailures() < 2 {
		t.Fatalf("unresolved-root failures = %d, want retries", service.FolderWatchReconcileFailures())
	}
	if scanner.calls != 0 {
		t.Fatalf("scanner calls = %d, want none without a resolved watch root", scanner.calls)
	}
}

func TestServiceReconcileFolderWatchUpdatesCatalogThenPlayUsesKitCache(t *testing.T) {
	ctx := context.Background()
	watch := t.TempDir()
	staging := t.TempDir()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	preparer := &romsource.Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	rom := []byte("snes-rom-bytes")
	if err := os.WriteFile(filepath.Join(watch, "Axelay.sfc"), rom, 0o600); err != nil {
		t.Fatal(err)
	}

	var operations []string
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		operations = append(operations, "probe")
		if system != protocol.SystemSNES {
			t.Fatalf("probe system = %q", system)
		}
		if client.uploadCalls == 0 {
			return protocol.CacheProbeResponse{Present: false}, nil
		}
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.upload = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
		operations = append(operations, "upload")
		body, err := io.ReadAll(reader)
		if err != nil || string(body) != string(rom) {
			t.Fatalf("uploaded = %q err=%v", body, err)
		}
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity}, nil
	}
	client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		operations = append(operations, "launch")
		gameID, system, coreName := request.GameID, request.System, "SNES"
		return protocol.CachedLaunchResponse{
			Status: protocol.Status{
				State: protocol.StateActive, GameID: &gameID, System: &system,
				ExpectedCore: &coreName, ObservedCore: &coreName,
			},
			Content: request.Content,
		}, nil
	}

	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: watch}},
			Library:        LibraryConfig{WatchRoot: watch},
			RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
		},
		Paths{Staging: staging}, store, scanner, preparer, client,
	)

	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].RelativePath != "Axelay.sfc" || games[0].System != protocol.SystemSNES || games[0].State != catalog.SourceStateAvailable {
		t.Fatalf("catalog after add = %+v", games)
	}
	gameID := games[0].ID

	if _, err := service.Launch(ctx, gameID, nil); err != nil {
		t.Fatalf("Launch(miss): %v", err)
	}
	if client.uploadCalls != 1 || client.launchCalls != 1 {
		t.Fatalf("miss calls upload=%d launch=%d", client.uploadCalls, client.launchCalls)
	}

	if _, err := service.Launch(ctx, gameID, nil); err != nil {
		t.Fatalf("Launch(hit): %v", err)
	}
	if client.uploadCalls != 1 || client.launchCalls != 2 || client.probeCalls != 2 {
		t.Fatalf("hit calls probe=%d upload=%d launch=%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
	if !reflect.DeepEqual(operations, []string{"probe", "upload", "launch", "probe", "launch"}) {
		t.Fatalf("operations = %v", operations)
	}

	if err := os.Remove(filepath.Join(watch, "Axelay.sfc")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch(remove): %v", err)
	}
	gone, err := service.Game(ctx, gameID)
	if err != nil {
		t.Fatal(err)
	}
	if gone.State != catalog.SourceStateMissing {
		t.Fatalf("removed state = %q", gone.State)
	}
}

func TestServiceReconcileMegaDriveFolderWatchUpdatesCatalogThenPlayUsesKitCache(t *testing.T) {
	ctx := context.Background()
	watch := t.TempDir()
	snesWatch := t.TempDir()
	staging := t.TempDir()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	preparer := &romsource.Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	rom := []byte("mega-rom-bytes")
	if err := os.WriteFile(filepath.Join(watch, "Sonic.md"), rom, 0o600); err != nil {
		t.Fatal(err)
	}

	var operations []string
	client := &fakeServiceClient{}
	client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		operations = append(operations, "probe")
		if system != protocol.SystemMegaDrive {
			t.Fatalf("probe system = %q", system)
		}
		if client.uploadCalls == 0 {
			return protocol.CacheProbeResponse{Present: false}, nil
		}
		return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
	}
	client.upload = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
		operations = append(operations, "upload")
		body, err := io.ReadAll(reader)
		if err != nil || string(body) != string(rom) {
			t.Fatalf("uploaded = %q err=%v", body, err)
		}
		return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity}, nil
	}
	client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		operations = append(operations, "launch")
		gameID, system, coreName := request.GameID, request.System, "MegaDrive"
		return protocol.CachedLaunchResponse{
			Status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &coreName, ObservedCore: &coreName},
			Content: request.Content,
		}, nil
	}

	service := newService(
		Config{Libraries: []catalog.Root{{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: watch}}, Library: LibraryConfig{WatchRoot: snesWatch}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: staging}, store, scanner, preparer, client,
	)

	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].RelativePath != "Sonic.md" || games[0].System != protocol.SystemMegaDrive || games[0].State != catalog.SourceStateAvailable {
		t.Fatalf("catalog after add = %+v", games)
	}
	gameID := games[0].ID
	if _, err := service.Launch(ctx, gameID, nil); err != nil {
		t.Fatalf("Launch(miss): %v", err)
	}
	if _, err := service.Launch(ctx, gameID, nil); err != nil {
		t.Fatalf("Launch(hit): %v", err)
	}
	if client.uploadCalls != 1 || client.launchCalls != 2 || client.probeCalls != 2 {
		t.Fatalf("calls probe=%d upload=%d launch=%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
	if !reflect.DeepEqual(operations, []string{"probe", "upload", "launch", "probe", "launch"}) {
		t.Fatalf("operations = %v", operations)
	}
}

func TestServiceReconcileNESAndSMSFolderWatchUsesKitCacheMissThenHit(t *testing.T) {
	for _, test := range []struct {
		name, file, rootID, core string
		system                   protocol.System
	}{
		{name: "NES", file: "Mario.nes", rootID: "nes-main", core: "NES", system: protocol.SystemNES},
		{name: "SMS", file: "Alex.sms", rootID: "sms-main", core: "SMS", system: protocol.SystemSMS},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			watch, staging := t.TempDir(), t.TempDir()
			store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
			preparer := &romsource.Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
			rom := []byte(test.name + "-rom-bytes")
			if err := os.WriteFile(filepath.Join(watch, test.file), rom, 0o600); err != nil {
				t.Fatal(err)
			}

			operations := []string{}
			client := &fakeServiceClient{}
			client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				operations = append(operations, "probe")
				if system != test.system {
					t.Fatalf("probe system = %q", system)
				}
				if client.uploadCalls == 0 {
					return protocol.CacheProbeResponse{Present: false}, nil
				}
				return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
			}
			client.upload = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
				operations = append(operations, "upload")
				body, err := io.ReadAll(reader)
				if err != nil || string(body) != string(rom) {
					t.Fatalf("uploaded = %q err=%v", body, err)
				}
				return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity}, nil
			}
			client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				operations = append(operations, "launch")
				gameID, system, coreName := request.GameID, request.System, test.core
				return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &coreName, ObservedCore: &coreName}, Content: request.Content}, nil
			}

			service := newService(Config{
				Libraries: []catalog.Root{{ID: test.rootID, System: test.system, Path: watch}},
				Library:   LibraryConfig{WatchRoot: watch}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
			}, Paths{Staging: staging}, store, scanner, preparer, client)
			if _, err := service.ReconcileFolderWatch(ctx); err != nil {
				t.Fatalf("ReconcileFolderWatch: %v", err)
			}
			games, err := service.Games(ctx)
			if err != nil || len(games) != 1 || games[0].System != test.system || games[0].RelativePath != test.file || games[0].State != catalog.SourceStateAvailable {
				t.Fatalf("catalog = %+v, err=%v", games, err)
			}
			if _, err := service.Launch(ctx, games[0].ID, nil); err != nil {
				t.Fatalf("Launch(miss): %v", err)
			}
			if _, err := service.Launch(ctx, games[0].ID, nil); err != nil {
				t.Fatalf("Launch(hit): %v", err)
			}
			if client.probeCalls != 2 || client.uploadCalls != 1 || client.launchCalls != 2 || !reflect.DeepEqual(operations, []string{"probe", "upload", "launch", "probe", "launch"}) {
				t.Fatalf("operations=%v calls probe=%d upload=%d launch=%d", operations, client.probeCalls, client.uploadCalls, client.launchCalls)
			}
		})
	}
}

func TestServiceReconcileNewFPGAFolderWatchUsesKitCacheMissThenHit(t *testing.T) {
	for _, test := range []struct {
		name, file, rootID, core string
		system                   protocol.System
	}{
		{name: "Game Boy", file: "Tetris.gb", rootID: "gb-main", core: "GAMEBOY", system: protocol.SystemGameBoy},
		{name: "GBA", file: "Mario.gba", rootID: "gba-main", core: "GBA", system: protocol.SystemGBA},
		{name: "PC Engine", file: "Bonk.pce", rootID: "pce-main", core: "TGFX16", system: protocol.SystemPCE},
		{name: "Game Gear", file: "Sonic.gg", rootID: "gg-main", core: "SMS", system: protocol.SystemGameGear},
		{name: "Game Boy Color", file: "Zelda.gbc", rootID: "gbc-main", core: "GAMEBOY", system: protocol.SystemGameBoyColor},
		{name: "Atari 2600", file: "Adventure.a26", rootID: "a2600-main", core: "ATARI7800", system: protocol.SystemAtari2600},
		{name: "ColecoVision", file: "Zaxxon.col", rootID: "coleco-main", core: "Coleco", system: protocol.SystemColecoVision},
		{name: "Atari Lynx", file: "Chip.lnx", rootID: "lynx-main", core: "AtariLynx", system: protocol.SystemAtariLynx},
		{name: "WonderSwan", file: "Guilty Gear.ws", rootID: "ws-main", core: "WonderSwan", system: protocol.SystemWonderSwan},
		{name: "WonderSwan Color", file: "Final Fantasy.wsc", rootID: "wsc-main", core: "WonderSwan", system: protocol.SystemWonderSwanColor},
		{name: "Atari 7800", file: "Food Fight.a78", rootID: "a7800-main", core: "ATARI7800", system: protocol.SystemAtari7800},
		{name: "Intellivision", file: "Astrosmash.int", rootID: "intv-main", core: "Intellivision", system: protocol.SystemIntellivision},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			watch, staging := t.TempDir(), t.TempDir()
			store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
			preparer := &romsource.Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
			rom := []byte(test.name + "-rom-bytes")
			if err := os.WriteFile(filepath.Join(watch, test.file), rom, 0o600); err != nil {
				t.Fatal(err)
			}

			operations := []string{}
			client := &fakeServiceClient{}
			client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				operations = append(operations, "probe")
				if system != test.system {
					t.Fatalf("probe system = %q", system)
				}
				if client.uploadCalls == 0 {
					return protocol.CacheProbeResponse{Present: false}, nil
				}
				return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
			}
			client.upload = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, error) {
				operations = append(operations, "upload")
				body, err := io.ReadAll(reader)
				if err != nil || string(body) != string(rom) {
					t.Fatalf("uploaded = %q err=%v", body, err)
				}
				return protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity}, nil
			}
			client.launch = func(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				operations = append(operations, "launch")
				if request.System != test.system {
					t.Fatalf("launch system = %q", request.System)
				}
				gameID, system, coreName := request.GameID, request.System, test.core
				return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &coreName, ObservedCore: &coreName}, Content: request.Content}, nil
			}

			service := newService(Config{
				Libraries: []catalog.Root{{ID: test.rootID, System: test.system, Path: watch}},
				Library:   LibraryConfig{WatchRoot: watch}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
			}, Paths{Staging: staging}, store, scanner, preparer, client)
			if _, err := service.ReconcileFolderWatch(ctx); err != nil {
				t.Fatalf("ReconcileFolderWatch: %v", err)
			}
			games, err := service.Games(ctx)
			if err != nil || len(games) != 1 || games[0].System != test.system || games[0].RelativePath != test.file || games[0].State != catalog.SourceStateAvailable {
				t.Fatalf("catalog = %+v, err=%v", games, err)
			}
			if _, err := service.Launch(ctx, games[0].ID, nil); err != nil {
				t.Fatalf("Launch(miss): %v", err)
			}
			if _, err := service.Launch(ctx, games[0].ID, nil); err != nil {
				t.Fatalf("Launch(hit): %v", err)
			}
			if client.probeCalls != 2 || client.uploadCalls != 1 || client.launchCalls != 2 || !reflect.DeepEqual(operations, []string{"probe", "upload", "launch", "probe", "launch"}) {
				t.Fatalf("operations=%v calls probe=%d upload=%d launch=%d", operations, client.probeCalls, client.uploadCalls, client.launchCalls)
			}
		})
	}
}

func TestServiceRetiresSupersededSNESLibraryFromCatalog(t *testing.T) {
	ctx := context.Background()
	oldDir := t.TempDir()
	newDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "Old.sfc"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "New.sfc"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	old := catalog.Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: oldDir}
	first := newService(
		Config{Libraries: []catalog.Root{old}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := first.Scan(ctx); err != nil {
		t.Fatalf("Scan(old): %v", err)
	}
	oldGames, err := first.Games(ctx)
	if err != nil || len(oldGames) != 1 || oldGames[0].State != catalog.SourceStateAvailable {
		t.Fatalf("old catalog = %+v err=%v", oldGames, err)
	}
	oldID := oldGames[0].ID

	second := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "operator-snes-new", System: protocol.SystemSNES, Path: newDir}},
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, ok := second.rootsByID[old.ID]; ok {
		t.Fatal("Play still resolved the superseded SNES library")
	}
	if _, err := second.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
	if _, err := second.Game(ctx, oldID); err == nil {
		t.Fatal("UI can still select the retired SNES id")
	}
	if _, err := second.Launch(ctx, oldID, nil); err == nil {
		t.Fatal("Launch accepted a retired SNES id")
	}
	games, err := second.Games(ctx)
	if err != nil || len(games) != 1 || games[0].RelativePath != "New.sfc" || games[0].LibraryID == old.ID {
		t.Fatalf("catalog after retire = %+v err=%v", games, err)
	}
}

func TestServiceUNCWatchRootKeepsUnmountedSNESIdentityAndRecovers(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	mount := filepath.Join(parent, "SNES")
	if err := os.Mkdir(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	snes := catalog.Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: mount}
	watchedSNES := snes
	first := newService(
		Config{
			Libraries:      []catalog.Root{snes},
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := first.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch(online): %v", err)
	}
	online, err := first.Games(ctx)
	if err != nil || len(online) != 1 || online[0].LibraryID != watchedSNES.ID {
		t.Fatalf("online catalog = %+v err=%v", online, err)
	}
	gameID := online[0].ID

	offlinePath := mount + "-offline"
	if err := os.Rename(mount, offlinePath); err != nil {
		t.Fatal(err)
	}
	second := newService(
		Config{
			Libraries:      []catalog.Root{snes},
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if !reflect.DeepEqual(second.folderWatchRoots(), []catalog.Root{watchedSNES}) {
		t.Fatalf("unmounted Open dropped SNES identity: %#v", second.folderWatchRoots())
	}
	if _, ok := second.rootsByID[watchedSNES.ID]; !ok {
		t.Fatal("unmounted Open retired the configured SNES mount")
	}
	offline, err := second.ReconcileFolderWatch(ctx)
	if err != nil {
		t.Fatalf("ReconcileFolderWatch(offline): %v", err)
	}
	if len(offline.Roots) != 1 || !offline.Roots[0].Offline || offline.Roots[0].RootID != watchedSNES.ID {
		t.Fatalf("offline report = %+v", offline)
	}
	kept, err := second.Game(ctx, gameID)
	if err != nil || kept.LibraryID != watchedSNES.ID {
		t.Fatalf("unmounted Open retired catalog games: %+v err=%v", kept, err)
	}

	if err := os.Rename(offlinePath, mount); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch(remount): %v", err)
	}
	recovered, err := second.Games(ctx)
	if err != nil || len(recovered) != 1 || recovered[0].ID != gameID || recovered[0].State != catalog.SourceStateAvailable {
		t.Fatalf("remount catalog = %+v err=%v", recovered, err)
	}
}

func TestServiceOpenRetiresMappedCatalogWhenNoMappedRootsRemain(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mount := filepath.Join(dir, "SNES")
	if err := os.Mkdir(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	paths := Paths{
		Config:  configPath,
		Index:   filepath.Join(dir, "state", "library.sqlite3"),
		Staging: filepath.Join(dir, "staging"),
	}
	writeServiceConfig(t, configPath, "http://fogcast.invalid", "synthetic-token", mount)
	seed, err := Open(ctx, paths, nil)
	if err != nil {
		t.Fatalf("Open(seed): %v", err)
	}
	if _, err := seed.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch(seed): %v", err)
	}
	seeded, err := seed.Games(ctx)
	if err != nil || len(seeded) != 1 {
		t.Fatalf("seeded catalog = %+v err=%v", seeded, err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("Close(seed): %v", err)
	}

	unresolvedConfig := `base_url = "http://fogcast.invalid"
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2
`
	if err := os.WriteFile(configPath, []byte(unresolvedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := Open(ctx, paths, nil)
	if err != nil {
		t.Fatalf("Open(unresolved): %v", err)
	}
	defer service.Close()
	if service.FolderWatchRoot() != DefaultFolderWatchRoot {
		t.Fatalf("default watch root = %q, want %q", service.FolderWatchRoot(), DefaultFolderWatchRoot)
	}
	if _, err := service.ReconcileFolderWatch(ctx); !errors.Is(err, errFolderWatchRootUnresolved) {
		t.Fatalf("ReconcileFolderWatch(unresolved) error = %v", err)
	}
	remaining, err := service.Games(ctx)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("catalog after mapped roots removed = %+v err=%v", remaining, err)
	}
	if _, err := service.Game(ctx, seeded[0].ID); err == nil {
		t.Fatal("retired mapped game remains selectable")
	}
}

func TestServiceUNCWatchRootUsesMountedSNESLibrary(t *testing.T) {
	ctx := context.Background()
	mount := t.TempDir()
	if err := os.WriteFile(filepath.Join(mount, "Axelay.sfc"), []byte("axelay"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	snes := catalog.Root{ID: "operator-snes-root", System: protocol.SystemSNES, Path: mount}
	watchedSNES := snes
	service := newService(
		Config{
			Libraries:      []catalog.Root{snes},
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if service.FolderWatchRoot() != DefaultFolderWatchRoot {
		t.Fatalf("documented SoT = %q", service.FolderWatchRoot())
	}
	if !reflect.DeepEqual(service.folderWatchRoots(), []catalog.Root{watchedSNES}) {
		t.Fatalf("runtime mount = %#v", service.folderWatchRoots())
	}
	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatalf("ReconcileFolderWatch: %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil || len(games) != 1 || games[0].LibraryID != watchedSNES.ID || games[0].RelativePath != "Axelay.sfc" {
		t.Fatalf("indexed via mount = %+v err=%v", games, err)
	}
}

func TestServiceCloseDoesNotCloseCatalogUnderLiveScan(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	catalogStore := &fakeServiceCatalog{}
	scanner := &fakeServiceScanner{started: started, block: block}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}}, Library: LibraryConfig{WatchRoot: DefaultFolderWatchRoot}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, catalogStore, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	service.catalogCloseWait = 40 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		done <- func() error { _, err := service.ReconcileFolderWatch(context.Background()); return err }()
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if catalogStore.closeCount() != 0 {
		t.Fatal("closed catalog under a live scan")
	}
	close(block)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scan did not finish")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && catalogStore.closeCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if catalogStore.closeCount() != 1 {
		t.Fatalf("eventual catalog close calls = %d", catalogStore.closeCount())
	}
}

func TestServiceCloseWaitsForScanThenClosesCatalog(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	catalogStore := &fakeServiceCatalog{}
	scanner := &fakeServiceScanner{started: started, block: block}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}}, Library: LibraryConfig{WatchRoot: DefaultFolderWatchRoot}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, catalogStore, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	service.catalogCloseWait = time.Second
	done := make(chan error, 1)
	go func() {
		done <- func() error { _, err := service.ReconcileFolderWatch(context.Background()); return err }()
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("scan did not start")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(block)
	}()
	if err := service.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if catalogStore.closeCount() != 1 {
		t.Fatalf("catalog close calls = %d", catalogStore.closeCount())
	}
	<-done
}

func TestServiceRunFolderWatchCountsOfflineRoots(t *testing.T) {
	scanner := &fakeServiceScanner{report: catalog.ScanReport{Roots: []catalog.RootReport{{
		RootID: "snes-main", System: protocol.SystemSNES, Offline: true,
	}}}}
	service := newService(
		Config{Library: LibraryConfig{WatchRoot: t.TempDir()}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	service.folderWatchInterval = 15 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.RunFolderWatch(ctx) }()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && service.FolderWatchReconcileFailures() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if service.FolderWatchReconcileFailures() < 2 {
		t.Fatalf("offline failures = %d, want retries", service.FolderWatchReconcileFailures())
	}
}

func TestServiceReconcileFolderWatchScansEveryMappedRoot(t *testing.T) {
	scanner := &fakeServiceScanner{}
	snes := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/snes"}
	mega := catalog.Root{ID: "genesis-main", System: protocol.SystemMegaDrive, Path: "/mega"}
	secondMega := catalog.Root{ID: "genesis-second", System: protocol.SystemMegaDrive, Path: "/mega-two"}
	nes := catalog.Root{ID: "nes-main", System: protocol.SystemNES, Path: "/nes"}
	sms := catalog.Root{ID: "sms-main", System: protocol.SystemSMS, Path: "/sms"}
	service := newService(
		Config{
			Libraries:      []catalog.Root{snes, mega, nes, sms, secondMega},
			Library:        LibraryConfig{WatchRoot: DefaultFolderWatchRoot},
			RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.ReconcileFolderWatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scanner.roots, []catalog.Root{snes, mega, nes, sms, secondMega}) {
		t.Fatalf("watch scanned %#v", scanner.roots)
	}
}
