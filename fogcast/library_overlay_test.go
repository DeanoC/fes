package fogcast

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

func TestLibrarySettingsPersistRootsTargetsAndRefreshScanner(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	oldRoot := filepath.Join(dir, "SNES")
	newRoot := filepath.Join(dir, "NES")
	for _, root := range []string{oldRoot, newRoot} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, "config.toml")
	configBody := "base_url = \"http://192.0.2.10:8182\"\n" +
		"token = \"test-token\"\nrequest_timeout_seconds = 2\nupload_timeout_seconds = 3\n\n" +
		"[[libraries]]\nid = \"old-snes\"\nsystem = \"snes\"\nroot = \"" + oldRoot + "\"\n"
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := &fakeServiceScanner{}
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Libraries: []catalog.Root{{ID: "old-snes", System: protocol.SystemSNES, Path: oldRoot}},
			Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
			Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: filepath.Join(dir, "staging")}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, client,
		WithConfigPath(configPath),
	)
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile selected target: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 12,
		PreferredRegions:   []string{"japan"},
		Libraries:          []catalog.Root{{System: protocol.SystemNES, Path: newRoot}},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: false},
		},
		SelectedTarget: "spare",
	}); err != nil {
		t.Fatal(err)
	}
	settings := service.LibrarySettings()
	if len(settings.Libraries) != 1 || settings.Libraries[0].ID == "" || settings.Libraries[0].Path != newRoot {
		t.Fatalf("published libraries = %#v", settings.Libraries)
	}
	if scanner.calls != 1 || len(scanner.roots) != 1 || scanner.roots[0] != settings.Libraries[0] {
		t.Fatalf("scanner calls=%d roots=%#v", scanner.calls, scanner.roots)
	}
	reloaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Libraries) != 1 || reloaded.Libraries[0] != settings.Libraries[0] || len(reloaded.Targets) != 2 || reloaded.SelectedTarget != "spare" {
		t.Fatalf("reloaded managed settings differ")
	}
	if reloaded.Targets[0].Agent != "test-token" || reloaded.Targets[1].Enabled {
		t.Fatal("target credential preservation or disabled target persistence failed")
	}
}

func TestLibrarySettingsScanDoesNotBlockTargetStop(t *testing.T) {
	ctx := context.Background()
	oldRoot := catalog.Root{ID: "old-snes", System: protocol.SystemSNES, Path: t.TempDir()}
	newRoot := catalog.Root{ID: "new-nes", System: protocol.SystemNES, Path: t.TempDir()}
	started := make(chan struct{})
	release := make(chan struct{})
	scanner := &fakeServiceScanner{started: started, block: release}
	client := &fakeServiceClient{stopResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Libraries: []catalog.Root{oldRoot},
			Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
			Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, client,
	)
	setDone := make(chan error, 1)
	go func() {
		setDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{newRoot},
			Targets: []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"}}, SelectedTarget: "dev",
		})
	}()
	<-started
	stopDone := make(chan error, 1)
	go func() {
		_, err := service.Stop(ctx)
		stopDone <- err
	}()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Stop blocked behind library scan")
	}
	close(release)
	if err := <-setDone; err != nil {
		t.Fatal(err)
	}
}

func TestLibrarySettingsReturnsScanFailure(t *testing.T) {
	for _, method := range []string{"set", "patch"} {
		t.Run(method, func(t *testing.T) {
			oldRoot := catalog.Root{ID: "old-snes", System: protocol.SystemSNES, Path: t.TempDir()}
			newRoot := catalog.Root{ID: "new-nes", System: protocol.SystemNES, Path: t.TempDir()}
			scanner := &fakeServiceScanner{err: errors.New("fixture scan failure")}
			service := newService(
				Config{
					Libraries: []catalog.Root{oldRoot},
					Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
					Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
				},
				Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
			)

			var err error
			if method == "set" {
				err = service.SetLibrarySettings(context.Background(), LibraryConfig{
					AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{newRoot},
					Targets: []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"}}, SelectedTarget: "dev",
				})
			} else {
				err = service.PatchLibrarySettings(context.Background(), LibraryConfigPatch{Libraries: &[]catalog.Root{newRoot}})
			}
			if err == nil {
				t.Fatal("settings-triggered scan failure was discarded")
			}
			assertServiceErrorCode(t, err, protocol.CodeInternal)
			if scanner.calls != 1 {
				t.Fatalf("scanner calls = %d, want 1", scanner.calls)
			}
			if got := service.LibrarySettings().Libraries; !reflect.DeepEqual(got, []catalog.Root{newRoot}) {
				t.Fatalf("published libraries = %#v", got)
			}
		})
	}
}

type serialSettingsScanner struct {
	mu           sync.Mutex
	started      chan []catalog.Root
	releaseFirst chan struct{}
	calls        [][]catalog.Root
}

func (s *serialSettingsScanner) SetAdmissionGate(func(context.Context) (func(), error)) {}

func (s *serialSettingsScanner) Scan(_ context.Context, roots []catalog.Root) (catalog.ScanReport, error) {
	copyRoots := append([]catalog.Root(nil), roots...)
	s.mu.Lock()
	s.calls = append(s.calls, copyRoots)
	call := len(s.calls)
	s.mu.Unlock()
	s.started <- copyRoots
	if call == 1 {
		<-s.releaseFirst
	}
	return catalog.ScanReport{}, nil
}

func TestLibrarySettingsSerializesPublicationWithTriggeredScan(t *testing.T) {
	rootA := catalog.Root{ID: "root-a", System: protocol.SystemSNES, Path: t.TempDir()}
	rootB := catalog.Root{ID: "root-b", System: protocol.SystemNES, Path: t.TempDir()}
	scanner := &serialSettingsScanner{started: make(chan []catalog.Root, 2), releaseFirst: make(chan struct{})}
	service := newService(
		Config{
			Libraries: []catalog.Root{{ID: "old", System: protocol.SystemSNES, Path: t.TempDir()}},
			Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
			Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
		},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	settings := func(root catalog.Root) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}, Libraries: []catalog.Root{root},
			Targets: []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"}}, SelectedTarget: "dev",
		}
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- service.SetLibrarySettings(context.Background(), settings(rootA)) }()
	if roots := <-scanner.started; !reflect.DeepEqual(roots, []catalog.Root{rootA}) {
		t.Fatalf("first scan roots = %#v", roots)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- service.SetLibrarySettings(context.Background(), settings(rootB)) }()
	select {
	case roots := <-scanner.started:
		t.Fatalf("newer scan started before the older scan completed: %#v", roots)
	case err := <-secondDone:
		t.Fatalf("newer settings completed before the older scan: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(scanner.releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first settings: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second settings: %v", err)
	}
	if roots := <-scanner.started; !reflect.DeepEqual(roots, []catalog.Root{rootB}) {
		t.Fatalf("second scan roots = %#v", roots)
	}
	if got := service.LibrarySettings().Libraries; !reflect.DeepEqual(got, []catalog.Root{rootB}) {
		t.Fatalf("final libraries = %#v", got)
	}
}

func TestLibrarySettingsWaitsForInFlightScanBeforePublication(t *testing.T) {
	for _, test := range []struct {
		name string
		scan func(*Service) (catalog.ScanReport, error)
	}{
		{name: "direct", scan: func(service *Service) (catalog.ScanReport, error) {
			return service.Scan(context.Background())
		}},
		{name: "folder watch", scan: func(service *Service) (catalog.ScanReport, error) {
			return service.ReconcileFolderWatch(context.Background())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldRoot := catalog.Root{ID: "root-old", System: protocol.SystemSNES, Path: t.TempDir()}
			newRoot := catalog.Root{ID: "root-new", System: protocol.SystemNES, Path: t.TempDir()}
			scanner := &serialSettingsScanner{started: make(chan []catalog.Root, 2), releaseFirst: make(chan struct{})}
			service := newService(
				Config{
					Libraries: []catalog.Root{oldRoot},
					Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
					Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
				},
				Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
			)

			scanDone := make(chan error, 1)
			go func() {
				_, err := test.scan(service)
				scanDone <- err
			}()
			if roots := <-scanner.started; !reflect.DeepEqual(roots, []catalog.Root{oldRoot}) {
				t.Fatalf("in-flight scan roots = %#v", roots)
			}

			settingsDone := make(chan error, 1)
			go func() {
				settingsDone <- service.PatchLibrarySettings(context.Background(), LibraryConfigPatch{Libraries: &[]catalog.Root{newRoot}})
			}()
			select {
			case roots := <-scanner.started:
				t.Fatalf("settings scan started before the in-flight scan completed: %#v", roots)
			case err := <-settingsDone:
				t.Fatalf("settings completed before the in-flight scan: %v", err)
			case <-time.After(150 * time.Millisecond):
			}
			if got := service.LibrarySettings().Libraries; !reflect.DeepEqual(got, []catalog.Root{oldRoot}) {
				t.Fatalf("libraries published during old-root scan = %#v", got)
			}

			close(scanner.releaseFirst)
			if err := <-scanDone; err != nil {
				t.Fatalf("in-flight scan: %v", err)
			}
			if roots := <-scanner.started; !reflect.DeepEqual(roots, []catalog.Root{newRoot}) {
				t.Fatalf("settings scan roots = %#v", roots)
			}
			if err := <-settingsDone; err != nil {
				t.Fatalf("settings: %v", err)
			}
			if got := service.LibrarySettings().Libraries; !reflect.DeepEqual(got, []catalog.Root{newRoot}) {
				t.Fatalf("final libraries = %#v", got)
			}
		})
	}
}

func TestLibrarySettingsCancellationWhileScanHoldsAdmission(t *testing.T) {
	for _, method := range []string{"set", "patch"} {
		t.Run(method, func(t *testing.T) {
			root := catalog.Root{ID: "root-old", System: protocol.SystemSNES, Path: t.TempDir()}
			scanStarted := make(chan struct{})
			releaseScan := make(chan struct{})
			scanner := &fakeServiceScanner{started: scanStarted, block: releaseScan}
			service := newService(
				Config{
					Libraries: []catalog.Root{root},
					Targets:   []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "test-token"}}, SelectedTarget: "dev",
					Library: LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}}, RequestTimeout: time.Second, UploadTimeout: time.Second,
				},
				Paths{Staging: t.TempDir()}, &fakeServiceCatalog{}, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
			)

			scanDone := make(chan error, 1)
			go func() {
				_, err := service.Scan(context.Background())
				scanDone <- err
			}()
			<-scanStarted

			ctx, cancel := context.WithCancel(context.Background())
			settingsDone := make(chan error, 1)
			go func() {
				if method == "set" {
					settingsDone <- service.SetLibrarySettings(ctx, service.LibrarySettings())
					return
				}
				idle := 12
				settingsDone <- service.PatchLibrarySettings(ctx, LibraryConfigPatch{AttractIdleSeconds: &idle})
			}()
			select {
			case err := <-settingsDone:
				t.Fatalf("settings returned before cancellation: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-settingsDone:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("settings error = %v, want context.Canceled", err)
				}
			case <-time.After(250 * time.Millisecond):
				t.Fatal("canceled settings remained blocked behind scan")
			}

			close(releaseScan)
			if err := <-scanDone; err != nil {
				t.Fatalf("scan: %v", err)
			}
		})
	}
}

func TestLibrarySettingsRebindsCatalogOnlyLibraryPath(t *testing.T) {
	ctx := context.Background()
	oldPath := t.TempDir()
	newPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldPath, "Old.chd"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newPath, "New.chd"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	oldRoot := catalog.Root{ID: "psx-main", System: "psx", Path: oldPath}
	service := newService(
		Config{Libraries: []catalog.Root{oldRoot}, RequestTimeout: time.Second, UploadTimeout: time.Second},
		Paths{Staging: t.TempDir()}, store, scanner, &fakeServicePreparer{}, &fakeServiceClient{},
	)
	if _, err := service.Scan(ctx); err != nil {
		t.Fatalf("seed scan: %v", err)
	}

	newRoot := catalog.Root{ID: oldRoot.ID, System: oldRoot.System, Path: newPath}
	if err := service.PatchLibrarySettings(ctx, LibraryConfigPatch{Libraries: &[]catalog.Root{newRoot}}); err != nil {
		t.Fatalf("path patch: %v", err)
	}
	games, err := service.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].LibraryID != newRoot.ID || games[0].RelativePath != "New.chd" {
		t.Fatalf("catalog after path patch = %+v", games)
	}
	libraries, err := store.Libraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(libraries, []catalog.Root{newRoot}) {
		t.Fatalf("libraries after path patch = %#v", libraries)
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

func TestPatchLibrarySettingsMergesConcurrentFieldUpdates(t *testing.T) {
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

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	librarySettingsPatchStartHook = func() {
		entered <- struct{}{}
		<-release
	}
	t.Cleanup(func() { librarySettingsPatchStartHook = nil })

	idle := 8
	regions := []string{"japan"}
	idleDone := make(chan error, 1)
	regionDone := make(chan error, 1)
	go func() {
		idleDone <- service.PatchLibrarySettings(ctx, LibraryConfigPatch{AttractIdleSeconds: &idle})
	}()
	go func() {
		regionDone <- service.PatchLibrarySettings(ctx, LibraryConfigPatch{PreferredRegions: &regions})
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("patch did not reach the start hook")
		}
	}
	close(release)
	if err := <-idleDone; err != nil {
		t.Fatalf("idle patch: %v", err)
	}
	if err := <-regionDone; err != nil {
		t.Fatalf("regions patch: %v", err)
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
	if settings.AttractIdleSeconds != 8 || strings.Join(settings.PreferredRegions, ",") != "japan" {
		t.Fatalf("merged settings = %+v", settings)
	}
	if fileIdle != settings.AttractIdleSeconds || strings.Join(file.PreferredRegions, ",") != strings.Join(settings.PreferredRegions, ",") {
		t.Fatalf("memory %+v != file idle=%d regions=%v", settings, fileIdle, file.PreferredRegions)
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

func TestCanonicalConfigFailureRestoresPreviousLibraryOverlay(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "library-settings.json")
	previous := []byte("{\"attract_idle_seconds\":12,\"preferred_regions\":[\"japan\"]}\n")
	if err := os.WriteFile(overlay, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: false}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, nil,
		WithLibraryOverlayPath(overlay),
		WithConfigPath(filepath.Join(dir, "missing-config.toml")),
	)
	err := service.PatchLibrarySettings(context.Background(), LibraryConfigPatch{
		AttractIdleSeconds: intPointer(8),
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("settings error = %v", err)
	}
	body, readErr := os.ReadFile(overlay)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !reflect.DeepEqual(body, previous) {
		t.Fatalf("restored overlay = %q, want exact previous bytes %q", body, previous)
	}
	if settings := service.LibrarySettings(); settings.AttractIdleSeconds != 12 || !reflect.DeepEqual(settings.PreferredRegions, []string{"japan"}) {
		t.Fatalf("failed save published memory: %+v", settings)
	}
	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "restore-") {
			t.Fatalf("restore temporary remains: %s", entry.Name())
		}
	}
}

func TestCanonicalConfigFailureRemovesNewLibraryOverlay(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "library-settings.json")
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: false}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, nil,
		WithLibraryOverlayPath(overlay),
		WithConfigPath(filepath.Join(dir, "missing-config.toml")),
	)
	idle := 8
	err := service.PatchLibrarySettings(context.Background(), LibraryConfigPatch{AttractIdleSeconds: &idle})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("settings error = %v", err)
	}
	if _, statErr := os.Stat(overlay); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("new overlay survived failed canonical save: %v", statErr)
	}
	if settings := service.LibrarySettings(); settings.AttractIdleSeconds != 60 {
		t.Fatalf("failed save published memory: %+v", settings)
	}
}

func TestSettingsPersistenceCompensatesPostPublishFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		hook func(t *testing.T)
	}{
		{
			name: "overlay",
			hook: func(t *testing.T) {
				libraryOverlayPublishErrorHook = func() error { return errors.New("fixture overlay publish failure") }
				t.Cleanup(func() { libraryOverlayPublishErrorHook = nil })
			},
		},
		{
			name: "canonical config",
			hook: func(t *testing.T) {
				canonicalConfigPublishErrorHook = func() error { return errors.New("fixture config publish failure") }
				t.Cleanup(func() { canonicalConfigPublishErrorHook = nil })
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			overlay := filepath.Join(dir, "library-settings.json")
			configPath := filepath.Join(dir, "config.toml")
			previousOverlay := []byte("{\"attract_idle_seconds\":12,\"preferred_regions\":[\"japan\"]}\n")
			previousConfig := []byte("selected_target = \"dev\"\n\n[[targets]]\nname = \"dev\"\nenabled = false\n")
			if err := os.WriteFile(overlay, previousOverlay, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, previousConfig, 0o600); err != nil {
				t.Fatal(err)
			}
			service := newService(
				Config{
					Targets:        []TargetConfig{{Name: "dev", Enabled: false}},
					SelectedTarget: "dev",
					Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
				},
				Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, nil,
				WithLibraryOverlayPath(overlay), WithConfigPath(configPath),
			)
			test.hook(t)
			idle := 8
			err := service.PatchLibrarySettings(context.Background(), LibraryConfigPatch{AttractIdleSeconds: &idle})
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
				t.Fatalf("settings error = %v", err)
			}
			assertFileBytes := func(path string, expected []byte) {
				t.Helper()
				body, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if !reflect.DeepEqual(body, expected) {
					t.Fatalf("%s = %q, want exact previous bytes %q", filepath.Base(path), body, expected)
				}
			}
			assertFileBytes(overlay, previousOverlay)
			assertFileBytes(configPath, previousConfig)
			if settings := service.LibrarySettings(); settings.AttractIdleSeconds != 12 {
				t.Fatalf("failed save published memory: %+v", settings)
			}
		})
	}
}

func intPointer(value int) *int { return &value }

func TestLoadLibraryOverlayRejectsNonObjectJSON(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{"null\n", "[]\n", "true\n", "12\n", "\"idle\"\n"} {
		path := filepath.Join(dir, "overlay.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, ok, err := loadLibraryOverlay(path)
		if err == nil || ok {
			t.Fatalf("body %q = ok=%v err=%v", body, ok, err)
		}
	}
}

func TestNullLibraryOverlayDoesNotOverrideConfigLibrary(t *testing.T) {
	overlay := filepath.Join(t.TempDir(), "library-settings.json")
	if err := os.WriteFile(overlay, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
			Library:        LibraryConfig{AttractIdleSeconds: 12, PreferredRegions: []string{"japan"}},
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
	settings := service.LibrarySettings()
	if settings.AttractIdleSeconds != 12 || strings.Join(settings.PreferredRegions, ",") != "japan" {
		t.Fatalf("null overlay overrode config library: %+v", settings)
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
