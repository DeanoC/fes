package fogcast

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
	"github.com/DeanoC/FogCast/targetclient"
)

const serviceDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestServiceCatalogAdmissionWaitHonorsCancellation(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	release, err := service.acquireCatalogAdmission(context.Background())
	if err != nil {
		t.Fatalf("acquireCatalogAdmission: %v", err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.acquireCatalogAdmission(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire error = %v, want context.Canceled", err)
	}
}

func TestProgressReaderStartsAfterBodyBytesAndForwardsClose(t *testing.T) {
	var starts int
	reader := &progressReader{
		Reader:      &scriptedReadCloser{reads: []scriptedRead{{n: 0, err: io.ErrUnexpectedEOF}, {n: 1, err: io.EOF}}},
		onFirstRead: func() { starts++ },
	}
	buffer := make([]byte, 1)
	if _, err := reader.Read(buffer); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("first read error = %v", err)
	}
	if starts != 0 {
		t.Fatalf("upload-started emitted on empty/error read: %d", starts)
	}
	if _, err := reader.Read(buffer); !errors.Is(err, io.EOF) {
		t.Fatalf("second read error = %v", err)
	}
	if starts != 1 {
		t.Fatalf("upload-started count = %d, want 1", starts)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := reader.Reader.(*scriptedReadCloser).closeCount; got != 1 {
		t.Fatalf("underlying reader close count = %d, want 1", got)
	}
}

type scriptedRead struct {
	n   int
	err error
}

type scriptedReadCloser struct {
	reads      []scriptedRead
	closeCount int
}

func (r *scriptedReadCloser) Read([]byte) (int, error) {
	if len(r.reads) == 0 {
		return 0, io.EOF
	}
	next := r.reads[0]
	r.reads = r.reads[1:]
	return next.n, next.err
}

func (r *scriptedReadCloser) Close() error {
	r.closeCount++
	return nil
}

func TestServiceLaunchMapsOnlyUnknownGameToNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code protocol.ErrorCode
	}{
		{name: "unknown", err: sql.ErrNoRows, code: protocol.CodeROMNotFound},
		{name: "catalog failure", err: errors.New("database unavailable at /private/library token-secret"), code: protocol.CodeInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeServiceCatalog{gameErr: test.err}
			client := &fakeServiceClient{}
			service := newTestService(store, &fakeServicePreparer{}, client)

			_, err := service.Launch(context.Background(), "snes-unknown", nil)
			assertServiceErrorCode(t, err, test.code)
			if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 {
				t.Fatalf("unknown game reached target: probe=%d upload=%d launch=%d", client.probeCalls, client.uploadCalls, client.launchCalls)
			}
		})
	}
}

func TestServiceLaunchRejectsInvalidGameIDBeforeCatalogAccess(t *testing.T) {
	store := &fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})}}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})

	_, err := service.Launch(context.Background(), "../private", nil)
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
	if store.gameCalls != 0 {
		t.Fatalf("catalog calls = %d, want 0", store.gameCalls)
	}
}

func TestServiceLaunchHonorsCancellationBeforeCatalogAccess(t *testing.T) {
	store := &fakeServiceCatalog{games: []catalog.Game{serviceGame(catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"})}}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.Launch(ctx, "snes-synthetic", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if store.gameCalls != 0 {
		t.Fatalf("catalog calls = %d, want 0", store.gameCalls)
	}
}

func TestThrottledReaderDelaysReads(t *testing.T) {
	reader := &throttledReader{Reader: strings.NewReader("x"), delay: 20 * time.Millisecond}
	started := time.Now()
	buf := make([]byte, 1)
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 15*time.Millisecond {
		t.Fatalf("read completed too quickly: %s", elapsed)
	}
}

func TestServiceHostOnlyExecutionUsesInjectedResolverAndAdapter(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	prepared := preparedServiceFixture(t, []byte("rom"), identity)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakeHostExecutor{}
	service := newTestServiceWithExecution(store, &fakeServicePreparer{prepared: prepared}, &fakeServiceClient{}, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return "host_only", nil }),
		Host:     adapter,
	})

	if got, err := service.SessionExecution(context.Background(), game.ID); err != nil || got != "host_only" {
		t.Fatalf("SessionExecution = %q, %v", got, err)
	}
	response, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if response.Status.State != protocol.StateActive || response.Status.GameID == nil || *response.Status.GameID != game.ID {
		t.Fatalf("host launch response = %+v", response)
	}
	if adapter.launchCalls != 1 || adapter.contentPath != "rom" {
		t.Fatalf("host adapter launch calls=%d content=%q", adapter.launchCalls, adapter.contentPath)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != game.ID {
		t.Fatalf("host status = %+v, %v", status, err)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("host adapter stop calls=%d", adapter.stopCalls)
	}
}

func TestConfiguredExecutionResolverSelectsOnlyConfiguredSystems(t *testing.T) {
	adapter := &fakeHostExecutor{}
	resolver := NewConfiguredExecutionResolver([]protocol.System{protocol.SystemSNES}, adapter)
	game := serviceGame(catalog.Content{})
	if got, err := resolver.Resolve(context.Background(), game); err != nil || got != ExecutionHostOnly {
		t.Fatalf("SNES execution = %q, %v", got, err)
	}
	game.System = protocol.SystemMegaDrive
	if got, err := resolver.Resolve(context.Background(), game); err != nil || got != ExecutionFPGANative {
		t.Fatalf("Mega Drive execution = %q, %v", got, err)
	}
}

func TestConfiguredExecutionResolverFallsBackWithoutAdapter(t *testing.T) {
	resolver := NewConfiguredExecutionResolver([]protocol.System{protocol.SystemSNES}, nil)
	if got, err := resolver.Resolve(context.Background(), serviceGame(catalog.Content{})); err != nil || got != ExecutionFPGANative {
		t.Fatalf("execution = %q, %v", got, err)
	}
}

func TestServiceHostLaunchRetainsOwnershipWhenPreparedCleanupFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepared.sfc")
	if err := os.WriteFile(path, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.Content = nil
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	prepared := &romsource.Prepared{Path: path, Content: identity}
	adapter := &fakeHostExecutor{}
	service := newTestServiceWithExecution(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{prepared: prepared}, &fakeServiceClient{}, ExecutionPolicy{
		Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
		Host:     adapter,
	})
	if _, err := service.Launch(context.Background(), game.ID, nil); err == nil {
		t.Fatal("cleanup failure was not returned")
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != game.ID {
		t.Fatalf("owned host status = %+v, %v", status, err)
	}
	if adapter.launchCalls != 1 {
		t.Fatalf("launch calls = %d", adapter.launchCalls)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.stopCalls != 1 {
		t.Fatalf("stop calls = %d", adapter.stopCalls)
	}
}

func TestServiceCatalogAndV1ControlDelegation(t *testing.T) {
	game := serviceGame(catalog.Content{})
	report := catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "snes-main", Added: 1}}}
	store := &fakeServiceCatalog{games: []catalog.Game{game}, searchGames: []catalog.Game{game}}
	scanner := &fakeServiceScanner{report: report}
	health := protocol.Health{APIVersion: "v1", Ready: true}
	status := protocol.Status{State: protocol.StateActive}
	stopped := protocol.Status{State: protocol.StateIdle}
	client := &fakeServiceClient{healthResult: health, statusResult: status, stopResult: stopped}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{}, store, scanner, &fakeServicePreparer{}, client,
	)
	ctx := context.Background()

	if got, err := service.Scan(ctx); err != nil || !reflect.DeepEqual(got, report) {
		t.Fatalf("Scan = %+v, %v", got, err)
	}
	if got, err := service.Games(ctx); err != nil || !reflect.DeepEqual(got, []catalog.Game{game}) {
		t.Fatalf("Games = %+v, %v", got, err)
	}
	if got, err := service.Game(ctx, game.ID); err != nil || !reflect.DeepEqual(got, game) {
		t.Fatalf("Game = %+v, %v", got, err)
	}
	if got, err := service.Search(ctx, "synthetic"); err != nil || !reflect.DeepEqual(got, []catalog.Game{game}) {
		t.Fatalf("Search = %+v, %v", got, err)
	}
	if store.searchQuery != "synthetic" {
		t.Fatalf("search query = %q", store.searchQuery)
	}
	if got, err := service.QueryGames(ctx, catalog.Query{}); err != nil || !reflect.DeepEqual(got.Games, []catalog.Game{game}) {
		t.Fatalf("QueryGames = %+v, %v", got, err)
	}
	if got, err := service.Platforms(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Platforms = %+v, %v", got, err)
	}
	if client.healthCalls != 0 {
		t.Fatalf("catalog reads contacted target health %d times", client.healthCalls)
	}
	if got, err := service.Health(ctx); err != nil || got != health {
		t.Fatalf("Health = %+v, %v", got, err)
	}
	if got, err := service.Status(ctx); err != nil || !reflect.DeepEqual(got, status) {
		t.Fatalf("Status = %+v, %v", got, err)
	}
	if got, err := service.Stop(ctx); err != nil || !reflect.DeepEqual(got, stopped) {
		t.Fatalf("Stop = %+v, %v", got, err)
	}
	if scanner.calls != 1 || client.healthCalls != 1 || client.statusCalls != 1 || client.stopCalls != 1 {
		t.Fatalf("delegation calls = scan:%d health:%d status:%d stop:%d", scanner.calls, client.healthCalls, client.statusCalls, client.stopCalls)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(first): %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(second): %v", err)
	}
	if store.closeCount() != 1 {
		t.Fatalf("catalog close calls = %d, want 1", store.closeCount())
	}
}

func TestServiceOpenComposesCatalogScannerAndAuthenticatedClient(t *testing.T) {
	const token = "synthetic-private-token"
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	writeServiceConfig(t, configPath, server.URL, token, library)
	indexParent := filepath.Join(dir, "state")
	staging := filepath.Join(dir, "cache", "staging")
	if err := os.Mkdir(indexParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := Paths{Config: configPath, Index: filepath.Join(indexParent, "library.sqlite3"), Staging: staging}

	service, err := Open(context.Background(), paths, server.Client())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()
	if status, err := service.Status(context.Background()); err != nil || status.State != protocol.StateIdle {
		t.Fatalf("Status = %+v, %v", status, err)
	}
	if requestCount != 1 {
		t.Fatalf("status request count = %d, want 1", requestCount)
	}
	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(report.Roots) != 1 || report.Roots[0].RootID != "snes-main" || report.Roots[0].Offline {
		t.Fatalf("scan report = %+v", report)
	}
	if games, err := service.Games(context.Background()); err != nil || len(games) != 0 {
		t.Fatalf("Games = %+v, %v", games, err)
	}
	for _, path := range []string{indexParent, staging} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %q mode = %s, want private 0700", filepath.Base(path), info.Mode())
		}
	}
	indexInfo, err := os.Lstat(paths.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !indexInfo.Mode().IsRegular() || indexInfo.Mode().Perm()&0o077 != 0 {
		t.Fatalf("index mode = %s, want private regular file", indexInfo.Mode())
	}
}

func TestServiceOpenFailureDoesNotExposeConfiguredRootOrToken(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "private-library")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	const token = "private-token-value"
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:8182"
token = %q
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q

[[libraries]]
id = "snes-other"
system = "snes"
root = %q
`, token, root, root)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Paths{Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}, nil)
	if err == nil {
		t.Fatal("Open succeeded with duplicate roots")
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), token) {
		t.Fatalf("Open error exposed private config: %v", err)
	}
}

func TestServiceOpenClosesUserLibraryWhenMediaIndexPathIsInvalid(t *testing.T) {
	dir := t.TempDir()
	library := filepath.Join(dir, "library")
	mediaRoot := filepath.Join(dir, "covers")
	if err := os.Mkdir(library, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mediaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:9"
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q

[[library_media]]
id = "covers-main"
root = %q
`, library, mediaRoot)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	indexParent := filepath.Join(dir, "state")
	if err := os.Mkdir(indexParent, 0o700); err != nil {
		t.Fatal(err)
	}
	userLibrary := filepath.Join(indexParent, "library-user.sqlite3")
	mediaIndex := filepath.Join(indexParent, "media-index")
	if err := os.Mkdir(mediaIndex, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := Open(context.Background(), Paths{
		Config:      configPath,
		Index:       filepath.Join(indexParent, "library.sqlite3"),
		Staging:     filepath.Join(dir, "staging"),
		UserLibrary: userLibrary,
		MediaIndex:  mediaIndex,
	}, nil)
	if err == nil {
		t.Fatal("Open succeeded with a directory media index")
	}

	users, err := libraryuser.OpenContext(context.Background(), userLibrary)
	if err != nil {
		t.Fatalf("reopen user library after failed Open: %v", err)
	}
	defer users.Close()
	if err := users.SetFavorite(context.Background(), "megadrive-sonic-test", true); err != nil {
		t.Fatalf("user library write after failed Open: %v", err)
	}
}

func TestServiceOpenHonorsPreCanceledContextWithoutCreatingState(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Config: filepath.Join(dir, "missing.toml"), Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Open(ctx, paths, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want context cancellation", err)
	}
	if _, err := os.Lstat(filepath.Dir(paths.Index)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled Open created state: %v", err)
	}
}

func TestValidatePrivateDirectoryPathRejectsFilesystemRoot(t *testing.T) {
	if err := validatePrivateDirectoryPath(string(os.PathSeparator)); err == nil {
		t.Fatal("filesystem root accepted as private state directory")
	}
	if err := validatePrivateDirectoryPath(filepath.Join(t.TempDir(), "state")); err != nil {
		t.Fatalf("temporary state directory rejected: %v", err)
	}
}

type fakeServiceCatalog struct {
	games       []catalog.Game
	searchGames []catalog.Game
	gameErr     error
	gameCalls   int
	matchCalls  int
	updateCalls int
	closeMu     sync.Mutex
	closeCalls  int
	queryErr    error
	searchQuery string
	rootMatch   func(context.Context, catalog.Game, catalog.Root) (bool, error)
	match       func(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error)
	update      func(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error)
}

func (f *fakeServiceCatalog) Game(context.Context, string) (catalog.Game, error) {
	f.gameCalls++
	if f.gameErr != nil {
		return catalog.Game{}, f.gameErr
	}
	if len(f.games) == 0 {
		return catalog.Game{}, errors.New("missing fake game")
	}
	index := f.gameCalls - 1
	if index >= len(f.games) {
		index = len(f.games) - 1
	}
	return f.games[index], nil
}

func (f *fakeServiceCatalog) Games(context.Context) ([]catalog.Game, error) {
	return append([]catalog.Game(nil), f.games...), nil
}

func (f *fakeServiceCatalog) Search(_ context.Context, query string) ([]catalog.Game, error) {
	f.searchQuery = query
	return append([]catalog.Game(nil), f.searchGames...), nil
}

func (f *fakeServiceCatalog) QueryGames(_ context.Context, query catalog.Query) (catalog.Page, error) {
	if f.queryErr != nil {
		return catalog.Page{}, f.queryErr
	}
	games := f.games
	if query.Text != "" {
		games = f.searchGames
	}
	if query.Restrict {
		allowed := make(map[string]struct{}, len(query.RestrictIDs))
		for _, id := range query.RestrictIDs {
			allowed[id] = struct{}{}
		}
		filtered := make([]catalog.Game, 0, len(games))
		for _, game := range games {
			if _, ok := allowed[game.ID]; ok {
				filtered = append(filtered, game)
			}
		}
		games = filtered
	}
	limit := query.Limit
	if limit <= 0 || limit > len(games) {
		limit = len(games)
	}
	return catalog.Page{Games: append([]catalog.Game(nil), games[:limit]...)}, nil
}

func (f *fakeServiceCatalog) Platforms(context.Context) ([]catalog.PlatformInfo, error) {
	return nil, nil
}

func (f *fakeServiceCatalog) GamesByIDs(_ context.Context, ids []string) ([]catalog.Game, error) {
	byID := make(map[string]catalog.Game, len(f.games))
	for _, game := range f.games {
		byID[game.ID] = game
	}
	games := make([]catalog.Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := byID[id]; ok {
			games = append(games, game)
		}
	}
	return games, nil
}

func (f *fakeServiceCatalog) GameMatchesRoot(ctx context.Context, game catalog.Game, root catalog.Root) (bool, error) {
	if f.rootMatch != nil {
		return f.rootMatch(ctx, game, root)
	}
	return game.LibraryID == root.ID && game.System == root.System, nil
}

func (f *fakeServiceCatalog) ContentMatches(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (bool, error) {
	f.matchCalls++
	if f.match != nil {
		return f.match(ctx, game, root, content)
	}
	return true, nil
}

type fakeContentLaunchAdmission struct {
	matches bool
}

func (a *fakeContentLaunchAdmission) ContentMatches() bool { return a.matches }
func (a *fakeContentLaunchAdmission) Close() error         { return nil }

func (f *fakeServiceCatalog) BeginContentLaunchAdmission(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (catalog.ContentLaunchAdmission, error) {
	matches, err := f.ContentMatches(ctx, game, root, content)
	if err != nil {
		return nil, err
	}
	return &fakeContentLaunchAdmission{matches: matches}, nil
}

func (f *fakeServiceCatalog) CompareAndSetContent(ctx context.Context, game catalog.Game, root catalog.Root, content catalog.Content) (bool, error) {
	f.updateCalls++
	if f.update != nil {
		return f.update(ctx, game, root, content)
	}
	return true, nil
}

func (f *fakeServiceCatalog) Close() error {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	f.closeCalls++
	return nil
}

func (f *fakeServiceCatalog) closeCount() int {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	return f.closeCalls
}

type fakeServiceScanner struct {
	report     catalog.ScanReport
	err        error
	calls      int
	roots      []catalog.Root
	started    chan struct{}
	block      <-chan struct{}
	attempted  chan struct{}
	admit      func(context.Context) (func(), error)
	onAdmitted func()
	once       sync.Once
}

type serviceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f serviceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type delayedUploadResult struct {
	data     []byte
	readErr  error
	closeErr error
}

type delayedFailingUploadTransport struct {
	failure error
	started chan struct{}
	result  chan delayedUploadResult
}

func (t *delayedFailingUploadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/v1/health" {
		return serviceJSONResponse(request, protocol.Health{APIVersion: "v1", Ready: true})
	}
	switch request.Method {
	case http.MethodGet:
		return serviceJSONResponse(request, protocol.CacheProbeResponse{Present: false})
	case http.MethodPut:
		go func() {
			data := make([]byte, 0, request.ContentLength)
			buffer := make([]byte, 1)
			started := false
			for {
				n, readErr := request.Body.Read(buffer)
				if n > 0 {
					data = append(data, buffer[:n]...)
				}
				if !started {
					close(t.started)
					started = true
				}
				if readErr != nil {
					t.result <- delayedUploadResult{data: data, readErr: readErr, closeErr: request.Body.Close()}
					return
				}
				runtime.Gosched()
			}
		}()
		<-t.started
		return nil, t.failure
	default:
		return nil, fmt.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
	}
}

func serviceJSONResponse(request *http.Request, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}, nil
}

func (f *fakeServiceScanner) SetAdmissionGate(admit func(context.Context) (func(), error)) {
	f.admit = admit
}

func (f *fakeServiceScanner) Scan(ctx context.Context, roots []catalog.Root) (catalog.ScanReport, error) {
	f.calls++
	f.roots = append([]catalog.Root(nil), roots...)
	if f.attempted != nil {
		close(f.attempted)
	}
	release := func() {}
	if f.admit != nil {
		var err error
		release, err = f.admit(ctx)
		if err != nil {
			return catalog.ScanReport{}, err
		}
	}
	defer release()
	if f.onAdmitted != nil {
		f.onAdmitted()
	}
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.block != nil {
		<-f.block
	}
	return f.report, f.err
}

type fakeServicePreparer struct {
	prepared *romsource.Prepared
	err      error
	calls    int
	prepare  func(context.Context, catalog.Root, catalog.Game) (*romsource.Prepared, error)
}

func (f *fakeServicePreparer) Prepare(ctx context.Context, root catalog.Root, game catalog.Game) (*romsource.Prepared, error) {
	f.calls++
	if f.prepare != nil {
		return f.prepare(ctx, root, game)
	}
	return f.prepared, f.err
}

type fakeServiceClient struct {
	cacheIndex          func(context.Context) (protocol.CacheIndex, error)
	cacheIndexCalls     int
	probe               func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error)
	upload              func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error)
	developmentLoad     func(context.Context, int64, io.Reader) (protocol.Status, error)
	coreLoad            func(context.Context, int64, io.Reader) (protocol.Status, error)
	developmentReboot   func(context.Context) (protocol.Status, error)
	probeCalls          int
	uploadCalls         int
	launchCalls         int
	nativeLaunchCalls   int
	developmentCalls    int
	coreCalls           int
	developmentReboots  int
	developmentSize     int64
	developmentBody     []byte
	healthCalls         int
	statusCalls         int
	stopCalls           int
	activeGame          string
	healthResult        protocol.Health
	healthFn            func(context.Context) (protocol.Health, error)
	statusResult        protocol.Status
	statusFn            func(context.Context) (protocol.Status, error)
	stopResult          protocol.Status
	stopFn              func(context.Context) (protocol.Status, error)
	healthErr           error
	statusErr           error
	stopErr             error
	meshLeaseGeneration string
	meshLeaseAbandoned  bool
}

func (f *fakeServiceClient) MeshKitLease() (bool, string) {
	if f == nil || f.meshLeaseGeneration == "" {
		return false, ""
	}
	return true, f.meshLeaseGeneration
}

func (f *fakeServiceClient) MeshKitLeaseAbandoned() bool {
	return f != nil && f.meshLeaseAbandoned
}

type shutdownOwnershipServiceClient struct {
	*fakeServiceClient
	hasKitGrant bool
}

func (f *shutdownOwnershipServiceClient) HasKitGrant() bool { return f.hasKitGrant }

func TestServiceShutdownCleanupRequiredUsesLocalOwnership(t *testing.T) {
	tests := []struct {
		name      string
		execution string
		target    string
		grant     bool
		want      bool
	}{
		{name: "never-owned idle"},
		{name: "post-stop idle"},
		{name: "owned native active", execution: ExecutionFPGANative, target: "dev", grant: true, want: true},
		{name: "owned development recovery", execution: ExecutionFPGADevelopment, target: "dev", grant: true, want: true},
		{name: "lost native ownership", execution: ExecutionFPGANative, target: "dev", want: false},
		{name: "lost development ownership", execution: ExecutionFPGADevelopment, target: "dev", want: false},
		{name: "active host-only", execution: ExecutionHostOnly, target: "host", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &shutdownOwnershipServiceClient{
				fakeServiceClient: &fakeServiceClient{}, hasKitGrant: tc.grant,
			}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			service.executionMu.Lock()
			service.activeExecution = tc.execution
			service.activeTarget = tc.target
			service.executionMu.Unlock()

			if got := service.ShutdownCleanupRequired(); got != tc.want {
				t.Fatalf("shutdown cleanup required = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestServiceShutdownCleanupRequiredUsesForegroundTargetGrant(t *testing.T) {
	selected := &shutdownOwnershipServiceClient{
		fakeServiceClient: &fakeServiceClient{}, hasKitGrant: true,
	}
	foreground := &shutdownOwnershipServiceClient{
		fakeServiceClient: &fakeServiceClient{}, hasKitGrant: true,
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, selected)
	service.targetMu.Lock()
	service.targetClients["other"] = foreground
	service.targetMu.Unlock()
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "other"
	service.executionMu.Unlock()

	if !service.ShutdownCleanupRequired() {
		t.Fatal("foreground target grant was not admitted for shutdown cleanup")
	}
	foreground.hasKitGrant = false
	if service.ShutdownCleanupRequired() {
		t.Fatal("selected target grant admitted cleanup after foreground ownership was lost")
	}
}

func TestServiceShutdownCleanupRequiredClearsAfterOwnedStopAndRelease(t *testing.T) {
	var stopCalls, releaseCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":"lease"}`)
		case "/v1/stop":
			if r.Header.Get(targetclient.KitLeaseHeader) != "lease" {
				t.Errorf("stop lease = %q, want lease", r.Header.Get(targetclient.KitLeaseHeader))
			}
			stopCalls++
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			if r.Header.Get(targetclient.KitLeaseHeader) != "lease" {
				t.Errorf("release lease = %q, want lease", r.Header.Get(targetclient.KitLeaseHeader))
			}
			releaseCalls++
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "game")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	claim, err := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(claim, true); err != nil {
		t.Fatalf("claim lease: %v", err)
	}
	if !lease.Held() {
		t.Fatal("claimed lease is not held")
	}

	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "dev"
	service.executionMu.Unlock()
	if !service.ShutdownCleanupRequired() {
		t.Fatal("owned active service was not admitted for shutdown cleanup")
	}
	if status, err := service.Stop(context.Background()); err != nil || status.State != protocol.StateIdle {
		t.Fatalf("owned stop = %+v, %v", status, err)
	}
	if err := service.ReleaseKitLease(context.Background()); err != nil {
		t.Fatalf("release lease: %v", err)
	}
	if lease.Held() {
		t.Fatal("released lease remains held")
	}
	if service.ShutdownCleanupRequired() {
		t.Fatal("post-stop idle service still admitted for shutdown cleanup")
	}
	if stopCalls != 1 || releaseCalls != 1 {
		t.Fatalf("target lifecycle calls = stop:%d release:%d, want 1 each", stopCalls, releaseCalls)
	}
}

func TestSoftStopKeepsSameOwnerUntilExplicitRelease(t *testing.T) {
	var stopCalls, releaseCalls int
	var stopToken, releaseToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":"same-owner"}`)
		case "/v1/stop":
			stopCalls++
			stopToken = r.Header.Get(targetclient.KitLeaseHeader)
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			releaseCalls++
			releaseToken = r.Header.Get(targetclient.KitLeaseHeader)
			fmt.Fprint(w, `{"state":"free"}`)
		case "/v1/development/reboot":
			t.Error("soft-stop armed a development reboot")
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "fogcast@sofa", "interactive game/development session")
	defer lease.Close(context.Background())
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	claim, err := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(claim, true); err != nil {
		t.Fatalf("claim lease: %v", err)
	}
	token := lease.CurrentToken()
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "dev"
	service.executionMu.Unlock()
	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("soft-stop = %+v, %v", status, err)
	}
	if stopCalls != 1 || stopToken != token || releaseCalls != 0 || !lease.Held() || lease.CurrentToken() != token {
		t.Fatalf("stopCalls=%d releaseCalls=%d stopToken=%q held=%v token=%q", stopCalls, releaseCalls, stopToken, lease.Held(), lease.CurrentToken())
	}
	if err := service.ReleaseKitLease(context.Background()); err != nil {
		t.Fatalf("explicit release: %v", err)
	}
	if releaseCalls != 1 || releaseToken != token || lease.Held() {
		t.Fatalf("releaseCalls=%d releaseToken=%q held=%v", releaseCalls, releaseToken, lease.Held())
	}
}

func TestSoftStopThenSelectedTargetChangeExplicitStopReleasesRetainedLease(t *testing.T) {
	var devStopCalls, devReleaseCalls, spareReleaseCalls int
	var devReleaseToken string
	devServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":"retained-dev"}`)
		case "/v1/stop":
			devStopCalls++
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			devReleaseCalls++
			devReleaseToken = r.Header.Get(targetclient.KitLeaseHeader)
			fmt.Fprint(w, `{"state":"free"}`)
		case "/v1/development/reboot":
			t.Error("retained-lease stop armed a development reboot")
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer devServer.Close()
	spareServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/status":
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/stop":
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			spareReleaseCalls++
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer spareServer.Close()
	devBase, err := url.Parse(devServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	spareBase, err := url.Parse(spareServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	devLease := targetclient.NewKitLease(devBase, "bearer", devServer.Client(), "fogcast@sofa", "interactive game/development session")
	defer devLease.Close(context.Background())
	devClient := targetclient.NewClient(devBase, "bearer", devServer.Client()).WithKitLease(devLease)
	spareLease := targetclient.NewKitLease(spareBase, "bearer", spareServer.Client(), "fogcast@sofa", "interactive game/development session")
	defer spareLease.Close(context.Background())
	spareClient := targetclient.NewClient(spareBase, "bearer", spareServer.Client()).WithKitLease(spareLease)
	claim, err := http.NewRequest(http.MethodPost, devServer.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := devLease.Authorize(claim, true); err != nil {
		t.Fatalf("claim lease: %v", err)
	}
	token := devLease.CurrentToken()
	ctx := context.Background()
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: devServer.URL, Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: spareServer.URL, Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, devClient,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spareClient, nil
			}
			return devClient, nil
		}),
	)
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "dev"
	service.executionMu.Unlock()

	status, err := service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("soft-stop = %+v, %v", status, err)
	}
	if devStopCalls != 1 || devReleaseCalls != 0 || !devLease.Held() || devLease.CurrentToken() != token {
		t.Fatalf("after soft-stop stop=%d release=%d held=%v token=%q", devStopCalls, devReleaseCalls, devLease.Held(), devLease.CurrentToken())
	}
	settings := LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: devServer.URL},
			{Name: "spare", Enabled: true, Address: spareServer.URL},
		},
		SelectedTarget: "spare",
	}
	if err := service.SetLibrarySettings(ctx, settings); err != nil {
		t.Fatalf("selected target change: %v", err)
	}
	if service.LibrarySettings().SelectedTarget != "spare" || service.targetClients["dev"] != nil || service.targetClients["spare"] == nil {
		t.Fatalf("selected=%q dev client dropped=%v spare present=%v", service.LibrarySettings().SelectedTarget, service.targetClients["dev"] == nil, service.targetClients["spare"] != nil)
	}
	if devReleaseCalls != 0 || !devLease.Held() {
		t.Fatalf("settings change released retained lease: releases=%d held=%v", devReleaseCalls, devLease.Held())
	}

	status, err = service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("explicit stop on spare = %+v, %v", status, err)
	}
	if devReleaseCalls != 0 || !devLease.Held() || devLease.CurrentToken() != token {
		t.Fatalf("stop before release freed retained lease: releases=%d held=%v token=%q", devReleaseCalls, devLease.Held(), devLease.CurrentToken())
	}
	if err := service.ReleaseKitLease(ctx); err != nil {
		t.Fatalf("explicit release: %v", err)
	}
	if devReleaseCalls != 1 || devReleaseToken != token || devLease.Held() || spareReleaseCalls != 0 {
		t.Fatalf("devRelease=%d token=%q held=%v spareRelease=%d", devReleaseCalls, devReleaseToken, devLease.Held(), spareReleaseCalls)
	}
}

func TestSoftStopRelaunchThenOtherExplicitStopKeepsLiveLease(t *testing.T) {
	var alphaStops, alphaReleases, betaStops, betaReleases int
	alphaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":"alpha-live"}`)
		case "/v1/stop":
			alphaStops++
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			alphaReleases++
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer alphaServer.Close()
	betaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/kit/claim":
			fmt.Fprint(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":"beta-stop"}`)
		case "/v1/stop":
			betaStops++
			fmt.Fprint(w, `{"state":"idle"}`)
		case "/v1/kit/release":
			betaReleases++
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer betaServer.Close()
	alphaBase, err := url.Parse(alphaServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	betaBase, err := url.Parse(betaServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	alphaLease := targetclient.NewKitLease(alphaBase, "bearer", alphaServer.Client(), "fogcast@sofa", "interactive game/development session")
	defer alphaLease.Close(context.Background())
	alphaClient := targetclient.NewClient(alphaBase, "bearer", alphaServer.Client()).WithKitLease(alphaLease)
	betaLease := targetclient.NewKitLease(betaBase, "bearer", betaServer.Client(), "fogcast@sofa", "interactive game/development session")
	defer betaLease.Close(context.Background())
	betaClient := targetclient.NewClient(betaBase, "bearer", betaServer.Client()).WithKitLease(betaLease)
	for _, claim := range []struct {
		lease *targetclient.KitLease
		raw   string
	}{
		{alphaLease, alphaServer.URL},
		{betaLease, betaServer.URL},
	} {
		req, err := http.NewRequest(http.MethodPost, claim.raw+"/v1/launch", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := claim.lease.Authorize(req, true); err != nil {
			t.Fatalf("claim lease: %v", err)
		}
	}
	if alphaLease.CurrentToken() != "alpha-live" || betaLease.CurrentToken() != "beta-stop" {
		t.Fatalf("tokens alpha=%q beta=%q", alphaLease.CurrentToken(), betaLease.CurrentToken())
	}
	ctx := context.Background()
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "alpha", Enabled: true, Address: alphaServer.URL, Agent: "alpha-fixture-token"},
				{Name: "beta", Enabled: true, Address: betaServer.URL, Agent: "beta-fixture-token"},
			},
			SelectedTarget: "alpha",
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, alphaClient,
	)
	service.targetMu.Lock()
	service.targetClients["beta"] = betaClient
	service.targetMu.Unlock()

	noteForegroundPlay(service, "alpha", "game-a")
	status, err := service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("soft-stop alpha = %+v, %v", status, err)
	}
	if alphaStops != 1 || alphaReleases != 0 || !alphaLease.Held() {
		t.Fatalf("after soft-stop stops=%d releases=%d held=%v", alphaStops, alphaReleases, alphaLease.Held())
	}

	// Relaunch reuses the same grant. The Soft-stop entry stays retained.
	noteForegroundPlay(service, "alpha", "game-a")
	noteForegroundPlay(service, "beta", "game-b")
	plays := service.PlaySessions()
	if len(plays) != 2 {
		t.Fatalf("plays after relaunch = %+v", plays)
	}

	status, err = service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("explicit stop beta = %+v, %v", status, err)
	}
	plays = service.PlaySessions()
	if len(plays) != 1 || plays[0].Target != "alpha" {
		t.Fatalf("plays after beta stop = %+v", plays)
	}
	if err := service.ReleaseKitLease(ctx); err != nil {
		t.Fatalf("explicit release: %v", err)
	}
	if alphaReleases != 0 || !alphaLease.Held() || alphaLease.CurrentToken() != "alpha-live" {
		t.Fatalf("live alpha released: releases=%d held=%v token=%q", alphaReleases, alphaLease.Held(), alphaLease.CurrentToken())
	}
	if betaStops != 1 || betaReleases != 1 || betaLease.Held() {
		t.Fatalf("beta stop=%d release=%d held=%v", betaStops, betaReleases, betaLease.Held())
	}

	// Once alpha is no longer playing, a later explicit stop still releases it.
	status, err = service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("explicit stop alpha = %+v, %v", status, err)
	}
	if err := service.ReleaseKitLease(ctx); err != nil {
		t.Fatalf("release alpha: %v", err)
	}
	if alphaStops != 2 || alphaReleases != 1 || alphaLease.Held() {
		t.Fatalf("alpha after its own stop stops=%d releases=%d held=%v", alphaStops, alphaReleases, alphaLease.Held())
	}
}

func TestReleaseKitLeaseReleasesIdleGrantsWhenPlaySurvives(t *testing.T) {
	_, clientB, leaseB := heldKitClient(t, "live-b", nil)
	_, _, leaseA := heldKitClient(t, "idle-a", nil)
	_, _, leaseC := heldKitClient(t, "idle-c", nil)
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, clientB)
	service.targetMu.Lock()
	service.targetClients["b"] = clientB
	service.targetMu.Unlock()
	noteForegroundPlay(service, "b", "game-b")
	service.executionMu.Lock()
	service.stoppedKitLeases = []*targetclient.KitLease{leaseA, leaseB, leaseC}
	service.executionMu.Unlock()

	if err := service.ReleaseKitLease(context.Background()); err != nil {
		t.Fatalf("release: %v", err)
	}
	if !leaseB.Held() || leaseB.CurrentToken() != "live-b" {
		t.Fatalf("live grant held=%v token=%q", leaseB.Held(), leaseB.CurrentToken())
	}
	if leaseA.Held() || leaseC.Held() {
		t.Fatalf("idle grants stayed held a=%v c=%v", leaseA.Held(), leaseC.Held())
	}
	if len(service.stoppedKitLeases) != 1 || service.stoppedKitLeases[0] != leaseB {
		t.Fatalf("retained grants = %#v", service.stoppedKitLeases)
	}
}

func noteForegroundPlay(service *Service, target, gameID string) {
	service.targetMu.RLock()
	defer service.targetMu.RUnlock()
	service.executionMu.Lock()
	defer service.executionMu.Unlock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = target
	service.activeGameID = gameID
	service.activeSystem = protocol.SystemSNES
	service.retainSessionTargetLocked()
}

func TestInvalidateOneTargetKeepsOtherRetainedGrants(t *testing.T) {
	_, keepClient, keepLease := heldKitClient(t, "keep-a", nil)
	_, dropClient, dropLease := heldKitClient(t, "drop-b", nil)
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, keepClient)
	service.stoppedKitLeases = []*targetclient.KitLease{keepLease, dropLease}

	service.invalidateTargetSession(dropClient)

	if !keepLease.Held() || keepLease.CurrentToken() != "keep-a" {
		t.Fatalf("retained grant after other invalidation held=%v token=%q", keepLease.Held(), keepLease.CurrentToken())
	}
	if dropLease.Held() {
		t.Fatal("invalidated target kept its grant")
	}
	if len(service.stoppedKitLeases) != 1 || service.stoppedKitLeases[0] != keepLease {
		t.Fatalf("retained grants = %#v", service.stoppedKitLeases)
	}
	if err := service.ReleaseKitLease(context.Background()); err != nil {
		t.Fatalf("explicit release: %v", err)
	}
	if keepLease.Held() || service.stoppedKitLeases != nil {
		t.Fatalf("surviving grant after release held=%v leases=%d", keepLease.Held(), len(service.stoppedKitLeases))
	}
}

func TestReleaseKitLeaseKeepsGrantWhenReleaseFails(t *testing.T) {
	var failKeep atomic.Bool
	failKeep.Store(true)
	_, _, keepLease := heldKitClient(t, "keep", func(w http.ResponseWriter, _ *http.Request) {
		if failKeep.Load() {
			http.Error(w, "transient", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"state":"free"}`)
	})
	_, _, dropLease := heldKitClient(t, "drop", nil)
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, nil)
	service.stoppedKitLeases = []*targetclient.KitLease{keepLease, dropLease}

	err := service.ReleaseKitLease(context.Background())
	if err == nil {
		t.Fatal("expected release error")
	}
	if !keepLease.Held() || keepLease.CurrentToken() != "keep" {
		t.Fatalf("failed release dropped grant held=%v token=%q", keepLease.Held(), keepLease.CurrentToken())
	}
	if dropLease.Held() {
		t.Fatal("successful release left its grant held")
	}
	if len(service.stoppedKitLeases) != 1 || service.stoppedKitLeases[0] != keepLease {
		t.Fatalf("retained grants = %#v", service.stoppedKitLeases)
	}

	failKeep.Store(false)
	if err := service.ReleaseKitLease(context.Background()); err != nil {
		t.Fatalf("retry release: %v", err)
	}
	if keepLease.Held() || service.stoppedKitLeases != nil {
		t.Fatalf("retry left held=%v leases=%d", keepLease.Held(), len(service.stoppedKitLeases))
	}
}

func heldKitClient(t *testing.T, token string, release func(http.ResponseWriter, *http.Request)) (*httptest.Server, *targetclient.Client, *targetclient.KitLease) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"generation","expires_in_ms":60000},"token":%q}`, token)
		case "/v1/kit/release":
			if got := r.Header.Get(targetclient.KitLeaseHeader); got != token {
				t.Errorf("release token = %q, want %s", got, token)
			}
			if release != nil {
				release(w, r)
				return
			}
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "fogcast@sofa", "interactive game/development session")
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
	claim, err := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(claim, true); err != nil {
		t.Fatalf("claim %s: %v", token, err)
	}
	if !lease.Held() || lease.CurrentToken() != token {
		t.Fatalf("claimed token = %q held=%v", lease.CurrentToken(), lease.Held())
	}
	return server, client, lease
}

func TestServiceCorePackageMutatesOnlyAfterTargetAdmission(t *testing.T) {
	payload := []byte("fcore")
	packageStatus := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32),
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad", Major: 1}}, Gamepad: true}}
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive},
		coreLoad: func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
			if size != int64(len(payload)) {
				t.Fatalf("size=%d", size)
			}
			got, _ := io.ReadAll(body)
			if !bytes.Equal(got, payload) {
				t.Fatalf("body=%q", got)
			}
			return packageStatus, nil
		}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution = ExecutionFPGANative
	service.activeGameID = "prior"

	status, err := service.LoadCore(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || status.CorePackage == nil || status.CorePackage.Generation != 3 || client.coreCalls != 1 {
		t.Fatalf("status=%#v calls=%d error=%v", status, client.coreCalls, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment || service.activeGameID != "" {
		t.Fatalf("execution=%q game=%q", service.activeExecution, service.activeGameID)
	}

	service.activeExecution, service.activeGameID = ExecutionFPGANative, "prior"
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid", Phase: "admission"}
	}
	_, err = service.LoadCore(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err == nil || service.activeExecution != ExecutionFPGANative || service.activeGameID != "prior" || client.stopCalls != 0 {
		t.Fatalf("execution=%q game=%q stops=%d error=%v", service.activeExecution, service.activeGameID, client.stopCalls, err)
	}
}

func TestServiceCorePackageMarksPriorStatusFailureAsPredispatch(t *testing.T) {
	client := &fakeServiceClient{statusErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable,
		Message: "status unavailable", Phase: "recovery"}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution, service.activeGameID = ExecutionHostOnly, "prior-host-game"
	_, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Phase != "request" || client.coreCalls != 0 ||
		service.activeExecution != ExecutionHostOnly || service.activeGameID != "prior-host-game" {
		t.Fatalf("error=%v core calls=%d execution=%q game=%q", err, client.coreCalls, service.activeExecution, service.activeGameID)
	}
}

func TestServiceCorePackageReconcilesLostReplyBeforePublishingExecution(t *testing.T) {
	prior := protocol.Status{State: protocol.StateIdle}
	active := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 8,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, errors.New("lost target HTTP reply")
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return active, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	if err != nil || status.CorePackage == nil || status.CorePackage.Generation != 8 ||
		service.activeExecution != ExecutionFPGADevelopment || client.coreCalls != 1 || client.statusCalls < 2 {
		t.Fatalf("status=%+v error=%v execution=%q core=%d status=%d", status, err, service.activeExecution, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackageLostReplyReconciliationHasOverallDeadline(t *testing.T) {
	prior := protocol.Status{State: protocol.StateIdle}
	launching := protocol.Status{State: protocol.StateLaunching, Development: true}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, errors.New("lost target HTTP reply")
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return launching, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.coreLoadReconcileTimeout = 40 * time.Millisecond
	started := time.Now()
	_, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	elapsed := time.Since(started)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
		elapsed > 500*time.Millisecond || client.statusCalls < 2 {
		t.Fatalf("error=%v elapsed=%s status calls=%d", err, elapsed, client.statusCalls)
	}
}

func TestServiceCorePackagePreservesConfirmedIdleFailureEvidence(t *testing.T) {
	loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	prior := protocol.Status{State: protocol.StateIdle}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return prior, nil
		}
		return protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
			Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
			Expected: loadErr.Expected, Observed: loadErr.Observed,
		}}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution, service.activeTarget = ExecutionFPGANative, "dev"
	service.activeGameID, service.activeSystem = "prior-game", protocol.SystemSNES
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != loadErr.Code || apiErr.Phase != loadErr.Phase ||
		apiErr.Expected != loadErr.Expected || apiErr.Observed != loadErr.Observed ||
		status.State != protocol.StateIdle || status.LastError == nil || client.coreCalls != 1 || client.statusCalls != 2 ||
		service.activeExecution != "" || service.activeTarget != "" || service.activeGameID != "" || service.activeSystem != "" ||
		!service.selectedTargetReconciled || service.selectedTargetRepairAllowed {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d execution=%q target=%q game=%q system=%q reconciled=%t repair=%t",
			status, err, client.coreCalls, client.statusCalls, service.activeExecution, service.activeTarget,
			service.activeGameID, service.activeSystem, service.selectedTargetReconciled, service.selectedTargetRepairAllowed)
	}
}

func TestServiceCorePackageConfirmedIdleStopsPriorHostOnlyExecution(t *testing.T) {
	for _, test := range []struct {
		name       string
		stopErr    error
		wantCode   protocol.ErrorCode
		wantPhase  string
		wantActive string
	}{
		{name: "stopped", wantCode: protocol.CodeUnrecognizedCore, wantPhase: "identity"},
		{name: "cleanup failure", stopErr: errors.New("host stop failed"), wantCode: protocol.CodeInternal,
			wantPhase: "recovery", wantActive: ExecutionHostOnly},
	} {
		t.Run(test.name, func(t *testing.T) {
			loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
				Expected: "0123", Observed: "4567"}
			idleFailure := protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
				Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
				Expected: loadErr.Expected, Observed: loadErr.Observed,
			}}
			client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
				return protocol.Status{}, loadErr
			}}
			client.statusFn = func(context.Context) (protocol.Status, error) {
				if client.statusCalls == 1 {
					return protocol.Status{State: protocol.StateIdle}, nil
				}
				return idleFailure, nil
			}
			hostExecutor := &fakeHostExecutor{stopErr: test.stopErr}
			service := newTestServiceWithExecution(&fakeServiceCatalog{}, &fakeServicePreparer{}, client,
				ExecutionPolicy{Host: hostExecutor})
			service.activeExecution, service.activeTarget = ExecutionHostOnly, "host"
			service.activeGameID, service.activeSystem = "prior-host-game", protocol.SystemSNES

			status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.wantCode || apiErr.Phase != test.wantPhase ||
				status.State != protocol.StateIdle || hostExecutor.stopCalls != 1 || service.activeExecution != test.wantActive ||
				!service.selectedTargetReconciled || service.selectedTargetRepairAllowed {
				t.Fatalf("status=%+v error=%v stops=%d execution=%q target=%q game=%q system=%q reconciled=%t repair=%t",
					status, err, hostExecutor.stopCalls, service.activeExecution, service.activeTarget,
					service.activeGameID, service.activeSystem, service.selectedTargetReconciled, service.selectedTargetRepairAllowed)
			}
			if test.wantActive == "" && (service.activeTarget != "" || service.activeGameID != "" || service.activeSystem != "") {
				t.Fatalf("retired host ownership target=%q game=%q system=%q", service.activeTarget, service.activeGameID, service.activeSystem)
			}
			if test.wantActive != "" && (service.activeTarget != "host" || service.activeGameID != "prior-host-game" || service.activeSystem != protocol.SystemSNES) {
				t.Fatalf("lost host ownership target=%q game=%q system=%q", service.activeTarget, service.activeGameID, service.activeSystem)
			}
		})
	}
}

func TestServiceCorePackagePreservesRepeatedConfirmedIdleFailureEvidence(t *testing.T) {
	loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	retained := &protocol.APIError{Code: loadErr.Code, Message: "public identity mismatch", Phase: loadErr.Phase,
		Expected: loadErr.Expected, Observed: loadErr.Observed}
	prior := protocol.Status{State: protocol.StateIdle, LastError: retained}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}, statusResult: prior}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != loadErr.Code || apiErr.Phase != loadErr.Phase ||
		apiErr.Expected != loadErr.Expected || apiErr.Observed != loadErr.Observed || status.State != protocol.StateIdle ||
		status.LastError == nil || client.coreCalls != 1 || client.statusCalls != 2 {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackageRejectsMismatchedIdleFailureEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*protocol.APIError)
	}{
		{name: "code", mutate: func(err *protocol.APIError) { err.Code = protocol.CodeMiSTerUnavailable }},
		{name: "phase", mutate: func(err *protocol.APIError) { err.Phase = "video" }},
		{name: "expected", mutate: func(err *protocol.APIError) { err.Expected = "different" }},
		{name: "observed", mutate: func(err *protocol.APIError) { err.Observed = "different" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			loadErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
				Expected: "0123", Observed: "4567"}
			retained := *loadErr
			test.mutate(&retained)
			client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
				return protocol.Status{}, loadErr
			}}
			client.statusFn = func(context.Context) (protocol.Status, error) {
				if client.statusCalls == 1 {
					return protocol.Status{State: protocol.StateIdle}, nil
				}
				return protocol.Status{State: protocol.StateIdle, LastError: &retained}, nil
			}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
				status != (protocol.Status{}) || client.coreCalls != 1 || client.statusCalls != 2 {
				t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
			}
		})
	}
}

func TestServiceCorePackageDoesNotTreatAmbiguousTransferAsConfirmedIdleFailure(t *testing.T) {
	loadAPIError := &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "target reply lost", Phase: "transfer"}
	loadErr := &ambiguousPackageLoadError{cause: loadAPIError}
	client := &fakeServiceClient{coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
		return protocol.Status{}, loadErr
	}}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		if client.statusCalls == 1 {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{
			Code: loadAPIError.Code, Message: "retained transfer failure", Phase: loadAPIError.Phase,
		}}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" ||
		status != (protocol.Status{}) || client.coreCalls != 1 || client.statusCalls != 2 {
		t.Fatalf("status=%+v error=%v core=%d status_calls=%d", status, err, client.coreCalls, client.statusCalls)
	}
}

func TestServiceCorePackageReportsTargetWhileRetainingHostCleanupOwner(t *testing.T) {
	active := protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 8,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle},
		coreLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) { return active, nil }}
	hostExecutor := &fakeHostExecutor{stopErr: errors.New("host stop failed")}
	service := newTestServiceWithExecution(&fakeServiceCatalog{}, &fakeServicePreparer{}, client, ExecutionPolicy{Host: hostExecutor})
	service.activeExecution, service.activeGameID = ExecutionHostOnly, "prior-host-game"
	status, err := service.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
	if err == nil || status.CorePackage == nil || status.LastError == nil || service.activeExecution != ExecutionHostOnly || service.activeGameID != "prior-host-game" {
		t.Fatalf("status=%+v error=%v execution=%q game=%q", status, err, service.activeExecution, service.activeGameID)
	}
	client.statusResult = active
	status, err = service.Status(context.Background())
	if err != nil || status.CorePackage == nil || status.LastError == nil || status.GameID != nil {
		t.Fatalf("target observation lost during pending host cleanup: %+v %v", status, err)
	}
}

func TestServiceDevelopmentRBFUsesSelectedTargetAndStops(t *testing.T) {
	payload := []byte("development-rbf")
	observed := "DEVCORE"
	client := &fakeServiceClient{
		developmentLoad: func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
			if size != int64(len(payload)) {
				t.Fatalf("development RBF size = %d", size)
			}
			got, err := io.ReadAll(body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("development RBF body = %q", got)
			}
			return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}, nil
		},
		statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
	}
	client.developmentReboot = func(context.Context) (protocol.Status, error) {
		client.statusResult = protocol.Status{State: protocol.StateIdle}
		return protocol.Status{}, io.EOF
	}
	healthChecks := 0
	client.healthFn = func(context.Context) (protocol.Health, error) {
		healthChecks++
		if healthChecks < 3 {
			return protocol.Health{Ready: true, BootID: "boot-before"}, nil
		}
		return protocol.Health{Ready: true, BootID: "boot-after"}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || !status.Development {
		t.Fatalf("development load status = %+v", status)
	}
	if service.activeExecution != ExecutionFPGADevelopment || service.activeGameID != "" || service.activeSystem != "" {
		t.Fatalf("active development execution = %q game = %q system = %q", service.activeExecution, service.activeGameID, service.activeSystem)
	}

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || !status.Development {
		t.Fatalf("development status = %+v", status)
	}
	status, err = service.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateIdle || client.stopCalls != 1 || client.developmentReboots != 1 || healthChecks < 3 || service.activeExecution != "" {
		t.Fatalf("stop status = %+v calls = %d reboots = %d health checks = %d execution = %q", status, client.stopCalls, client.developmentReboots, healthChecks, service.activeExecution)
	}
}

func TestServiceDevelopmentRecoveryAcceptsIdleWithoutWaitingForReboot(t *testing.T) {
	payload := []byte("development-rbf")
	observed := "DEVCORE"
	client := &fakeServiceClient{
		developmentLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
			return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}, nil
		},
		statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
		developmentReboot: func(context.Context) (protocol.Status, error) {
			return protocol.Status{State: protocol.StateIdle}, nil
		},
		healthResult: protocol.Health{Ready: true, BootID: "boot-before"},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	if _, err := service.LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || client.developmentReboots != 1 || client.healthCalls != 1 || service.activeExecution != "" {
		t.Fatalf("stop=%+v err=%v reboots=%d health=%d execution=%q", status, err, client.developmentReboots, client.healthCalls, service.activeExecution)
	}
}

func TestServiceDevelopmentStopUsesNativeRecoveryStatusOverHTTP(t *testing.T) {
	var stopCalls, rebootCalls, healthCalls, statusCalls int
	rebooted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" && r.Header.Get("Authorization") != "Bearer target-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/stop":
			stopCalls++
			_ = json.NewEncoder(w).Encode(protocol.Status{
				State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/development/reboot":
			rebootCalls++
			rebooted = true
			_ = json.NewEncoder(w).Encode(protocol.Status{
				State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/health":
			healthCalls++
			bootID := "boot-before"
			if rebooted {
				bootID = "boot-after"
			}
			_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true, BootID: bootID})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			statusCalls++
			if !rebooted {
				_ = json.NewEncoder(w).Encode(protocol.Status{
					State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := targetclient.NewClient(baseURL, "target-token", server.Client())
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.activeExecution = ExecutionFPGADevelopment

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("HTTP native recovery stop = %#v, %v", status, err)
	}
	if stopCalls != 1 || rebootCalls != 1 || healthCalls < 2 || statusCalls != 1 || service.activeExecution != "" {
		t.Fatalf("HTTP native recovery calls = stop:%d reboot:%d health:%d status:%d execution:%q", stopCalls, rebootCalls, healthCalls, statusCalls, service.activeExecution)
	}
}

type noDevelopmentRecoveryClient struct {
	serviceClient
}

func TestServiceDevelopmentStopRejectsTargetWithoutRecoveryCapability(t *testing.T) {
	target := &fakeServiceClient{
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &noDevelopmentRecoveryClient{serviceClient: target})
	service.activeExecution = ExecutionFPGADevelopment

	_, err := service.Stop(context.Background())
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("missing recovery capability error = %v", err)
	}
	if target.stopCalls != 1 || target.developmentReboots != 0 || service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("missing recovery calls = stop:%d reboot:%d execution:%q", target.stopCalls, target.developmentReboots, service.activeExecution)
	}
}

func TestServiceDevelopmentStopSurfacesRecoveryErrorWhenRebootDidNotStart(t *testing.T) {
	for _, test := range []struct {
		name    string
		remote  *protocol.APIError
		want    protocol.ErrorCode
		message string
	}{
		{
			name:    "busy",
			remote:  &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"},
			want:    protocol.CodeBusy,
			message: "another launch or stop transition is running",
		},
		{
			name:    "recover idle transport",
			remote:  &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime recovery is required", Phase: "recovery"},
			want:    protocol.CodeMiSTerUnavailable,
			message: "Target recovery is required; use Stop to recover before launching again.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			healthCalls := 0
			target := &fakeServiceClient{
				stopResult: protocol.Status{
					State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
				},
				developmentReboot: func(context.Context) (protocol.Status, error) {
					return protocol.Status{}, test.remote
				},
				healthFn: func(context.Context) (protocol.Health, error) {
					healthCalls++
					return protocol.Health{Ready: true, BootID: "boot-before"}, nil
				},
			}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, target)
			service.activeExecution = ExecutionFPGADevelopment
			service.uploadTimeout = time.Second

			started := time.Now()
			_, err := service.Stop(context.Background())
			if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
				t.Fatalf("stop waited %s for a reboot that did not start", elapsed)
			}
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.want || apiErr.Message != test.message {
				t.Fatalf("stop error = %v", err)
			}
			if healthCalls != 1 || target.developmentReboots != 1 || target.stopCalls != 1 || service.activeExecution != ExecutionFPGADevelopment {
				t.Fatalf("calls health=%d reboot=%d stop=%d execution=%q", healthCalls, target.developmentReboots, target.stopCalls, service.activeExecution)
			}
		})
	}
}

func TestServiceDevelopmentStopBoundsFailedRecoveryHandshake(t *testing.T) {
	target := &fakeServiceClient{
		stopResult: protocol.Status{
			State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
		},
		developmentReboot: func(context.Context) (protocol.Status, error) {
			return protocol.Status{}, errors.New("reboot command failed")
		},
		healthFn: func(context.Context) (protocol.Health, error) {
			return protocol.Health{Ready: true, BootID: "boot-before"}, nil
		},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, target)
	service.activeExecution = ExecutionFPGADevelopment
	service.uploadTimeout = 50 * time.Millisecond

	_, err := service.Stop(context.Background())
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("failed recovery error = %v", err)
	}
	if target.stopCalls != 1 || target.developmentReboots != 1 || service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("failed recovery calls = stop:%d reboot:%d execution:%q", target.stopCalls, target.developmentReboots, service.activeExecution)
	}
}

func TestServiceDevelopmentRBFLostResponseUsesStatusOnlyOnce(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     protocol.Status
		cancelLoop bool
		wantCode   protocol.ErrorCode
	}{
		{name: "active development", status: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: serviceStringPtr("DEVCORE")}},
		{name: "retained operation error", status: protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "runtime operation failed"}}, wantCode: protocol.CodeCoreTimeout},
		{name: "wrong terminal state", status: protocol.Status{State: protocol.StateActive, ObservedCore: serviceStringPtr("MegaDrive")}, wantCode: protocol.CodeMiSTerUnavailable},
		{name: "caller cancellation", status: protocol.Status{State: protocol.StateIdle}, cancelLoop: true, wantCode: protocol.CodeMiSTerUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			uploads, statuses := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
				case "/v1/development/rbf":
					_, _ = io.ReadAll(r.Body)
					mu.Lock()
					uploads++
					mu.Unlock()
					<-r.Context().Done()
				case "/v1/status":
					mu.Lock()
					statuses++
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(test.status)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			baseURL, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := targetclient.NewClient(baseURL, "test-token", server.Client())
			service := newService(
				Config{RequestTimeout: 20 * time.Millisecond, UploadTimeout: 30 * time.Millisecond},
				Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
			)
			parent, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			if test.cancelLoop {
				cancel()
				parent, cancel = context.WithTimeout(context.Background(), 90*time.Millisecond)
			}
			defer cancel()
			status, loadErr := service.LoadDevelopmentRBF(parent, 3, strings.NewReader("rbf"))
			mu.Lock()
			gotUploads, gotStatuses := uploads, statuses
			mu.Unlock()
			if gotUploads != 1 {
				t.Fatalf("development uploads = %d, want exactly one", gotUploads)
			}
			if test.wantCode == "" {
				if loadErr != nil || status.State != protocol.StateActive || !status.Development || gotStatuses == 0 {
					t.Fatalf("status=%+v err=%v status calls=%d", status, loadErr, gotStatuses)
				}
				return
			}
			var apiErr *protocol.APIError
			if !errors.As(loadErr, &apiErr) || apiErr.Code != test.wantCode || gotStatuses == 0 {
				t.Fatalf("error=%v status calls=%d, want %s after observation", loadErr, gotStatuses, test.wantCode)
			}
		})
	}
}

func serviceStringPtr(value string) *string { return &value }

func TestServiceDevelopmentRBFLostResponseBoundsEachStatusCall(t *testing.T) {
	var mu sync.Mutex
	uploads, statuses := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/development/rbf":
			_, _ = io.ReadAll(r.Body)
			mu.Lock()
			uploads++
			mu.Unlock()
			<-r.Context().Done()
		case "/v1/status":
			mu.Lock()
			statuses++
			mu.Unlock()
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	service := newService(
		Config{RequestTimeout: 25 * time.Millisecond, UploadTimeout: 30 * time.Millisecond},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetclient.NewClient(baseURL, "test-token", server.Client()),
	)
	parent, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := service.LoadDevelopmentRBF(parent, 3, strings.NewReader("rbf"))
	elapsed := time.Since(started)
	mu.Lock()
	gotUploads, gotStatuses := uploads, statuses
	mu.Unlock()
	if err == nil || gotUploads != 1 || gotStatuses != 1 || elapsed < 45*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("err=%v uploads=%d statuses=%d elapsed=%s", err, gotUploads, gotStatuses, elapsed)
	}
}

func TestServiceRejectsCatalogLaunchWhileDevelopmentRBFIsActive(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{games: []catalog.Game{{ID: "snes-replacement", System: protocol.SystemSNES}}}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.activeExecution = ExecutionFPGADevelopment

	_, err := service.Launch(context.Background(), "snes-replacement", nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
		t.Fatalf("launch error = %v", err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("active execution = %q", service.activeExecution)
	}
}

func TestServiceDevelopmentActiveReconstructsAfterHostRestart(t *testing.T) {
	observed := "DEVCORE"
	client := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	active, err := service.DevelopmentActive(context.Background())
	if err != nil || !active {
		t.Fatalf("development active = %t, %v", active, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("reconstructed execution = %q", service.activeExecution)
	}
}

func TestServiceDevelopmentSessionStateReconstructsNativeGameAfterHostRestart(t *testing.T) {
	gameID, system, core := "megadrive-reconstructed", protocol.SystemMegaDrive, "MegaDrive"
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core,
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	development, execution, err := service.DevelopmentSessionState(context.Background())
	if err != nil || development || execution != ExecutionFPGANative {
		t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
	}
	if client.statusCalls != 1 || service.activeExecution != ExecutionFPGANative {
		t.Fatalf("status calls=%d active execution=%q", client.statusCalls, service.activeExecution)
	}
}

func TestServiceDevelopmentActiveReconstructsStoppingRecoveryAfterHostRestart(t *testing.T) {
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired,
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	active, err := service.DevelopmentActive(context.Background())
	if err != nil || !active {
		t.Fatalf("development active = %t, %v", active, err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("reconstructed execution = %q", service.activeExecution)
	}
}

func TestStartupReconciliationBlocksNamedTargetSwitchBeforeStatus(t *testing.T) {
	ctx := context.Background()
	gameID := "snes-startup-active"
	system := protocol.SystemSNES
	dev := &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("unreconciled target switch error = %v", err)
	}
	if dev.statusCalls != 0 || service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("rejected switch queried or changed target: status=%d selected=%q", dev.statusCalls, service.LibrarySettings().SelectedTarget)
	}
	status, err := service.Status(ctx)
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("startup status = %+v, %v", status, err)
	}
	if _, err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if dev.stopCalls != 1 || spare.stopCalls != 0 {
		t.Fatalf("stop calls dev=%d spare=%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestIdleStartupReconciliationAllowsOneNamedTargetSwitch(t *testing.T) {
	ctx := context.Background()
	dev := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	settingsFor := func(selected string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: selected,
		}
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err == nil {
		t.Fatal("unreconciled startup switch succeeded")
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err != nil {
		t.Fatalf("idle-reconciled switch: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("dev")); err == nil {
		t.Fatal("new selected target switched again without reconciliation")
	}
	if dev.statusCalls != 1 || spare.statusCalls != 0 {
		t.Fatalf("status calls dev=%d spare=%d", dev.statusCalls, spare.statusCalls)
	}
}

func TestFailedStatusDoesNotAllowNamedTargetRecoverySwitch(t *testing.T) {
	ctx := context.Background()
	statusCalls := 0
	dev := &fakeServiceClient{statusFn: func(context.Context) (protocol.Status, error) {
		statusCalls++
		if statusCalls == 1 {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{}, errors.New("fixture target unavailable")
	}}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	settingsFor := func(selected string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: selected,
		}
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile dev: %v", err)
	}
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("later unreachable dev status succeeded")
	} else {
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("unreachable dev status error = %v", err)
		}
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("spare")); err == nil {
		t.Fatal("unreachable target switched without successful reconciliation")
	}
	if dev.statusCalls != 2 || spare.statusCalls != 0 || service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("status calls dev=%d spare=%d selected=%q", dev.statusCalls, spare.statusCalls, service.LibrarySettings().SelectedTarget)
	}
}

func TestFailedStartupStatusAllowsOneSelectedTargetConnectionRepair(t *testing.T) {
	ctx := context.Background()
	unreachable := &fakeServiceClient{statusErr: errors.New("fixture target unavailable")}
	repaired := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, unreachable,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Address == "http://192.0.2.20:8182" {
				return repaired, nil
			}
			return unreachable, nil
		}),
	)
	settingsFor := func(address string) LibraryConfig {
		return LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets:            []TargetConfig{{Name: "dev", Enabled: true, Address: address}},
			SelectedTarget:     "dev",
		}
	}
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("unreachable startup status succeeded")
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("http://192.0.2.20:8182")); err != nil {
		t.Fatalf("repair selected target connection: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, settingsFor("http://192.0.2.21:8182")); err == nil {
		t.Fatal("second connection repair succeeded without reconciling repaired target")
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("status through repaired connection: %v", err)
	}
	if unreachable.statusCalls != 1 || repaired.statusCalls != 1 {
		t.Fatalf("status calls unreachable=%d repaired=%d", unreachable.statusCalls, repaired.statusCalls)
	}
}

func TestFailedStatusDoesNotAllowRecoveryFromKnownActiveExecution(t *testing.T) {
	ctx := context.Background()
	dev := &fakeServiceClient{statusErr: errors.New("fixture target unavailable")}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-fixture-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-fixture-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(TargetConfig) (serviceClient, error) { return &fakeServiceClient{}, nil }),
	)
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "dev"
	service.executionMu.Unlock()
	if _, err := service.Status(ctx); err == nil {
		t.Fatal("unreachable active target status succeeded")
	}
	service.executionMu.Lock()
	repairAllowed := service.selectedTargetRepairAllowed
	service.executionMu.Unlock()
	if repairAllowed {
		t.Fatal("failed status granted repair while an execution was active")
	}
	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active target recovery switch error = %v", err)
	}
	if service.LibrarySettings().SelectedTarget != "dev" {
		t.Fatalf("active target changed to %q", service.LibrarySettings().SelectedTarget)
	}
}

func TestStatusReconciliationPinsActiveNamedTargetBeforeSettingsSwitch(t *testing.T) {
	ctx := context.Background()
	gameID := "snes-reconciled"
	system := protocol.SystemSNES
	statusEntered := make(chan struct{})
	statusRelease := make(chan struct{})
	dev := &fakeServiceClient{
		statusFn: func(context.Context) (protocol.Status, error) {
			close(statusEntered)
			<-statusRelease
			return protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}, nil
		},
		stopResult: protocol.Status{State: protocol.StateIdle},
	}
	spare := &fakeServiceClient{}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	statusDone := make(chan error, 1)
	go func() {
		status, err := service.Status(ctx)
		if err == nil && status.State != protocol.StateActive {
			err = errors.New("reconciled status was not active")
		}
		statusDone <- err
	}()
	<-statusEntered
	settingsDone := make(chan error, 1)
	go func() {
		settingsDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: "spare",
		})
	}()
	select {
	case err := <-settingsDone:
		t.Fatalf("settings switch completed before status reconciliation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(statusRelease)
	if err := <-statusDone; err != nil {
		t.Fatal(err)
	}
	err := <-settingsDone
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("active reconciled target switch error = %v", err)
	}
	if _, err := service.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if dev.stopCalls != 1 || spare.stopCalls != 0 {
		t.Fatalf("stop calls dev=%d spare=%d", dev.stopCalls, spare.stopCalls)
	}
}

func TestStopPinsReconciledNamedTargetThroughSettingsSwitch(t *testing.T) {
	ctx := context.Background()
	stopEntered := make(chan struct{})
	stopRelease := make(chan struct{})
	dev := &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateIdle},
		stopFn: func(context.Context) (protocol.Status, error) {
			close(stopEntered)
			<-stopRelease
			return protocol.Status{State: protocol.StateIdle}, nil
		},
	}
	spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RequestTimeout: time.Second,
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile dev idle: %v", err)
	}

	stopDone := make(chan error, 1)
	go func() {
		_, err := service.Stop(ctx)
		stopDone <- err
	}()
	<-stopEntered
	settingsDone := make(chan error, 1)
	go func() {
		settingsDone <- service.SetLibrarySettings(ctx, LibraryConfig{
			AttractIdleSeconds: 60,
			PreferredRegions:   []string{"usa"},
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
			},
			SelectedTarget: "spare",
		})
	}()
	select {
	case err := <-settingsDone:
		t.Fatalf("settings switch completed before Stop reconciliation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(stopRelease)
	if err := <-stopDone; err != nil {
		t.Fatalf("stop dev: %v", err)
	}
	if err := <-settingsDone; err != nil {
		t.Fatalf("switch to spare: %v", err)
	}

	err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "dev",
	})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBadRequest {
		t.Fatalf("unreconciled spare switch error = %v", err)
	}
	if spare.statusCalls != 0 {
		t.Fatalf("spare status calls = %d, want 0", spare.statusCalls)
	}
	if _, err := service.Status(ctx); err != nil {
		t.Fatalf("reconcile spare idle: %v", err)
	}
	if err := service.SetLibrarySettings(ctx, LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "dev",
	}); err != nil {
		t.Fatalf("switch after spare reconciliation: %v", err)
	}
}

func TestIdleNamedTargetSwitchAllowedWithStartupBoundInputAndMedia(t *testing.T) {
	for _, test := range []struct {
		name   string
		config func(*Config)
	}{
		{name: "remote input", config: func(config *Config) { config.RemoteInput.Enabled = true }},
		{name: "media", config: func(config *Config) { config.Media.Enabled = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dev := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
			spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
			config := Config{
				Targets: []TargetConfig{
					{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
				},
				SelectedTarget: "dev",
				Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			}
			test.config(&config)
			service := newService(config, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
				withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
					if target.Name == "spare" {
						return spare, nil
					}
					return dev, nil
				}),
			)
			if _, err := service.Status(context.Background()); err != nil {
				t.Fatalf("reconcile selected target: %v", err)
			}
			if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", PreviousName: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
				},
				SelectedTarget: "den",
			}); err != nil {
				t.Fatalf("selected target rename: %v", err)
			}
			if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.12:8182"},
				},
				SelectedTarget: "den",
			}); err != nil {
				t.Fatalf("unselected target edit: %v", err)
			}
			if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
				AttractIdleSeconds: 60,
				PreferredRegions:   []string{"usa"},
				Targets: []TargetConfig{
					{Name: "den", Enabled: true, Address: "http://192.0.2.10:8182"},
					{Name: "spare", Enabled: true, Address: "http://192.0.2.12:8182"},
				},
				SelectedTarget: "spare",
			}); err != nil {
				t.Fatalf("idle target switch: %v", err)
			}
			if got := service.LibrarySettings().SelectedTarget; got != "spare" {
				t.Fatalf("selected target = %q", got)
			}
			name, _ := service.SessionTarget()
			if name != "spare" {
				t.Fatalf("session target = %q", name)
			}
		})
	}
}

func TestSelectedTargetIdentityChangeRebindsOriginAndInvalidatesInput(t *testing.T) {
	dev := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	spare := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	service := newService(
		Config{
			Targets: []TargetConfig{
				{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "dev-test-token"},
				{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182", Agent: "spare-test-token"},
			},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
			RemoteInput:    RemoteInputConfig{Enabled: true},
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, dev,
		withTargetClientFactory(func(target TargetConfig) (serviceClient, error) {
			if target.Name == "spare" {
				return spare, nil
			}
			return dev, nil
		}),
	)
	var origins []string
	resets := 0
	service.SetTargetReset(func() { resets++ })
	service.SetTargetOrigin(func(target TargetConfig) { origins = append(origins, target.Name+":"+target.Address) })
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatalf("reconcile selected target: %v", err)
	}
	if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182"},
			{Name: "spare", Enabled: true, Address: "http://192.0.2.11:8182"},
		},
		SelectedTarget: "spare",
	}); err != nil {
		t.Fatalf("idle target switch: %v", err)
	}
	if resets != 1 {
		t.Fatalf("target resets = %d", resets)
	}
	if len(origins) != 1 || origins[0] != "spare:http://192.0.2.11:8182" {
		t.Fatalf("origins = %#v", origins)
	}
}

func TestNamedTargetRenameRetainsStoredAgentWithoutEcho(t *testing.T) {
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182", Agent: "stored-test-token"}},
			SelectedTarget: "dev",
			Library:        LibraryConfig{AttractIdleSeconds: 60, PreferredRegions: []string{"usa"}},
		},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		withTargetClientFactory(func(TargetConfig) (serviceClient, error) { return &fakeServiceClient{}, nil }),
	)
	if err := service.SetLibrarySettings(context.Background(), LibraryConfig{
		AttractIdleSeconds: 60,
		PreferredRegions:   []string{"usa"},
		Targets: []TargetConfig{{
			Name: "den", PreviousName: "dev", Enabled: true, Address: "http://192.0.2.10:8182",
		}},
		SelectedTarget: "den",
	}); err != nil {
		t.Fatal(err)
	}
	settings := service.LibrarySettings()
	if len(settings.Targets) != 1 || settings.Targets[0].Name != "den" || settings.Targets[0].Agent != "stored-test-token" {
		t.Fatalf("renamed target = %#v", settings.Targets)
	}
}

func TestMergeTargetAgentsUsesExplicitRenameIdentityBeforeDestinationName(t *testing.T) {
	service := &Service{targets: []TargetConfig{
		{Name: "dev", Agent: "dev-test-token"},
		{Name: "spare", Agent: "spare-test-token"},
	}}
	for _, test := range []struct {
		name string
		next []TargetConfig
		want map[string]string
	}{
		{
			name: "swap",
			next: []TargetConfig{
				{Name: "spare", PreviousName: "dev"},
				{Name: "dev", PreviousName: "spare"},
			},
			want: map[string]string{"spare": "dev-test-token", "dev": "spare-test-token"},
		},
		{
			name: "rename into removed existing name",
			next: []TargetConfig{{Name: "spare", PreviousName: "dev"}},
			want: map[string]string{"spare": "dev-test-token"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged, err := service.mergeTargetAgentsLocked(test.next)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range merged {
				if target.Agent != test.want[target.Name] {
					t.Fatalf("target %q agent = %q", target.Name, target.Agent)
				}
			}
		})
	}
	if _, err := service.mergeTargetAgentsLocked([]TargetConfig{
		{Name: "dev"},
		{Name: "den", PreviousName: "dev"},
	}); err == nil {
		t.Fatal("duplicate current target identity was accepted")
	}
}

func (f *fakeServiceClient) CacheIndex(ctx context.Context) (protocol.CacheIndex, error) {
	f.cacheIndexCalls++
	if f.cacheIndex == nil {
		return protocol.CacheIndex{}, errors.New("unexpected cache index")
	}
	return f.cacheIndex(ctx)
}

func (f *fakeServiceClient) ProbeContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	f.probeCalls++
	if f.probe == nil {
		return protocol.CacheProbeResponse{}, errors.New("unexpected probe")
	}
	return f.probe(ctx, system, content)
}

func (f *fakeServiceClient) UploadContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, error) {
	f.uploadCalls++
	if f.upload == nil {
		return protocol.CacheUploadResponse{}, errors.New("unexpected upload")
	}
	return f.upload(ctx, system, content, body)
}

func (f *fakeServiceClient) LoadDevelopmentRBF(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	f.developmentCalls++
	f.developmentSize = size
	if f.developmentLoad == nil {
		return protocol.Status{}, errors.New("unexpected development RBF load")
	}
	return f.developmentLoad(ctx, size, body)
}

func (f *fakeServiceClient) LoadCore(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	f.coreCalls++
	if f.coreLoad == nil {
		return protocol.Status{}, errors.New("unexpected core package load")
	}
	return f.coreLoad(ctx, size, body)
}

func (f *fakeServiceClient) RebootDevelopment(ctx context.Context) (protocol.Status, error) {
	f.developmentReboots++
	if f.developmentReboot == nil {
		return protocol.Status{}, errors.New("unexpected development reboot")
	}
	return f.developmentReboot(ctx)
}

func (f *fakeServiceClient) Health(ctx context.Context) (protocol.Health, error) {
	f.healthCalls++
	if f.healthFn != nil {
		return f.healthFn(ctx)
	}
	return f.healthResult, f.healthErr
}

func (f *fakeServiceClient) Status(ctx context.Context) (protocol.Status, error) {
	f.statusCalls++
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return f.statusResult, f.statusErr
}

func (f *fakeServiceClient) Stop(ctx context.Context) (protocol.Status, error) {
	f.stopCalls++
	if f.stopFn != nil {
		return f.stopFn(ctx)
	}
	return f.stopResult, f.stopErr
}

type fakeHostExecutor struct {
	launchCalls int
	stopCalls   int
	statusCalls int
	contentPath string
	stopErr     error
}

type ambiguousPackageLoadError struct {
	cause *protocol.APIError
}

func (e *ambiguousPackageLoadError) Error() string           { return e.cause.Error() }
func (e *ambiguousPackageLoadError) Unwrap() error           { return e.cause }
func (e *ambiguousPackageLoadError) AmbiguousMutation() bool { return true }

func (f *fakeHostExecutor) ID() string { return "fake" }
func (f *fakeHostExecutor) Capabilities() []hostexec.Capability {
	return []hostexec.Capability{hostexec.HostOnly}
}
func (f *fakeHostExecutor) Launch(_ context.Context, content io.Reader, identity protocol.ContentIdentity) (hostexec.Status, error) {
	f.launchCalls++
	if content == nil || identity.Size == 0 {
		return hostexec.Status{}, errors.New("missing host content")
	}
	body, err := io.ReadAll(content)
	if err != nil {
		return hostexec.Status{}, err
	}
	f.contentPath = string(body)
	return hostexec.Status{State: hostexec.Active}, nil
}
func (f *fakeHostExecutor) Stop(context.Context) error { f.stopCalls++; return f.stopErr }
func (f *fakeHostExecutor) Status(context.Context) (hostexec.Status, error) {
	f.statusCalls++
	return hostexec.Status{State: hostexec.Active}, nil
}

func newTestService(store *fakeServiceCatalog, preparer *fakeServicePreparer, client serviceClient) *Service {
	return newTestServiceWithExecution(store, preparer, client, ExecutionPolicy{})
}

func newTestServiceWithExecution(store *fakeServiceCatalog, preparer *fakeServicePreparer, client serviceClient, policy ExecutionPolicy) *Service {
	root := catalog.Root{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}
	return newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, preparer, client,
		WithExecutionPolicy(policy),
	)
}

func serviceGame(content catalog.Content) catalog.Game {
	return catalog.Game{
		ID: "snes-synthetic", Title: "Synthetic", LibraryID: "snes-main", RelativePath: "game.sfc",
		System: protocol.SystemSNES, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 123}, Content: &content,
	}
}

func contentIdentity(content catalog.Content) protocol.ContentIdentity {
	return protocol.ContentIdentity{SHA256: content.SHA256, Size: content.Size, Extension: content.Extension}
}

func absentProbe(_ context.Context, _ protocol.System, _ protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	return protocol.CacheProbeResponse{Present: false}, nil
}

func TestCanonicalRemoteErrorPreservesUnsupportedOperation(t *testing.T) {
	t.Parallel()
	private := errors.New("private target detail")
	err := canonicalRemoteError(errors.Join(&protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "target wording"}, private), protocol.CodeTransferFailed)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation || apiErr.Message != "requested operation is unsupported" {
		t.Fatalf("canonical error = %#v", err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "target wording") {
		t.Fatalf("remote detail leaked: %v", err)
	}
}

type fakePathHostExecutor struct {
	fakeHostExecutor
	pathCalls  int
	lastPath   string
	lastSystem protocol.System
	cleanup    func()
}

func (f *fakePathHostExecutor) LaunchPath(_ context.Context, system protocol.System, sourcePath string) (hostexec.Status, error) {
	return f.LaunchOwnedPath(context.Background(), system, sourcePath, nil)
}

func (f *fakePathHostExecutor) LaunchOwnedPath(_ context.Context, system protocol.System, sourcePath string, cleanup func()) (hostexec.Status, error) {
	f.pathCalls++
	f.lastSystem = system
	f.lastPath = sourcePath
	f.cleanup = cleanup
	return hostexec.Status{State: hostexec.Active}, nil
}

func TestServiceFPGALaunchRejectsCueWithoutTargetUpload(t *testing.T) {
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		t.Fatal("cue set probed the target")
		return protocol.CacheProbeResponse{}, nil
	}
	service := newTestService(store, &fakeServicePreparer{}, client)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeUnsupportedOperation)
	if client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 {
		t.Fatalf("target calls = probe:%d upload:%d launch:%d", client.probeCalls, client.uploadCalls, client.launchCalls)
	}
}

func TestServiceHostOnlyCueLaunchUsesConfinedPath(t *testing.T) {
	dir := t.TempDir()
	cuePath := filepath.Join(dir, "game.cue")
	binPath := filepath.Join(dir, "game.bin")
	if err := os.WriteFile(cuePath, []byte("FILE \"game.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("track-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, cuePath)
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.upload = func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error) {
		t.Fatal("host cue launch uploaded to the target")
		return protocol.CacheUploadResponse{}, nil
	}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	response, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		if adapter.cleanup != nil {
			adapter.cleanup()
		}
	})
	if response.Status.State != protocol.StateActive || adapter.pathCalls != 1 || adapter.lastSystem != protocol.SystemSNES {
		t.Fatalf("path launch response=%+v adapter=%#v", response, adapter)
	}
	if adapter.lastPath == "" || adapter.lastPath == cuePath || filepath.Base(adapter.lastPath) != "game.cue" || !strings.Contains(adapter.lastPath, "fogcast-host-launch-") {
		t.Fatalf("launched path = %q, want private copy of game.cue", adapter.lastPath)
	}
	copiedBin, err := os.ReadFile(filepath.Join(filepath.Dir(adapter.lastPath), "game.bin"))
	if err != nil || string(copiedBin) != "track-bytes" {
		t.Fatalf("copied companion = %q, %v", copiedBin, err)
	}
	if _, err := os.Stat(cuePath); err != nil {
		t.Fatalf("library cue removed: %v", err)
	}
	if client.uploadCalls != 0 || adapter.launchCalls != 0 {
		t.Fatalf("unexpected snapshot/upload launch=%d upload=%d", adapter.launchCalls, client.uploadCalls)
	}
}

func TestServiceHostOnlyCueLaunchRejectsEscapingReference(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "library")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "secret.bin")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte("FILE \"../secret.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	client := &fakeServiceClient{}
	client.probe = func(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
		t.Fatal("escaping cue probed the target")
		return protocol.CacheProbeResponse{}, nil
	}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if adapter.pathCalls != 0 || client.probeCalls != 0 || client.uploadCalls != 0 {
		t.Fatalf("escaping cue caused I/O adapter=%d probe=%d upload=%d", adapter.pathCalls, client.probeCalls, client.uploadCalls)
	}
}

func TestServiceHostOnlyCueLaunchRejectsOversizedSheet(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("A", 1<<20+32) + "\nFILE \"../outside.bin\" BINARY\n"
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeInvalidArchive)
	if adapter.pathCalls != 0 {
		t.Fatalf("oversized cue launched %q", adapter.lastPath)
	}
}

func TestServiceHostOnlyCueLaunchRejectsMissingCompanion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.cue"), []byte("FILE \"game.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(dir, "game.cue"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
	if adapter.pathCalls != 0 {
		t.Fatalf("missing companion launched %q", adapter.lastPath)
	}
}

func TestServiceHostOnlyLaunchRejectsReplacedSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "library")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(realRoot, "game.chd")
	if err := os.WriteFile(rom, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "game.chd"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(realRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, realRoot); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.chd"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, filepath.Join(outside, "game.chd"))
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: realRoot}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	_, err := service.Launch(context.Background(), game.ID, nil)
	assertServiceErrorCode(t, err, protocol.CodeSourceUnavailable)
	if adapter.pathCalls != 0 {
		t.Fatalf("symlink root launched %q", adapter.lastPath)
	}
}
func fileServiceFingerprint(t *testing.T, path string) catalog.Fingerprint {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return catalog.Fingerprint{SourceSize: info.Size(), ModifiedNS: info.ModTime().UnixNano()}
}
func assertServiceErrorCode(t *testing.T, err error, want protocol.ErrorCode) {
	t.Helper()
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != want {
		t.Fatalf("error = %T %v, want %s", err, err, want)
	}
	if strings.Contains(err.Error(), "/private/") {
		t.Fatalf("error exposes private path: %v", err)
	}
}

func preparedServiceFixture(t *testing.T, body []byte, identity protocol.ContentIdentity) *romsource.Prepared {
	t.Helper()
	prepared, err := romsource.NewPreparedSnapshot(body, identity.Extension)
	if err != nil {
		t.Fatalf("NewPreparedSnapshot: %v", err)
	}
	prepared.Content = identity
	return prepared
}

func assertPreparedRemoved(t *testing.T, prepared *romsource.Prepared) {
	t.Helper()
	if file, err := prepared.Open(); err == nil {
		_ = file.Close()
		t.Fatal("prepared content remained openable after service cleanup")
	}
	if prepared.Path != "" {
		if _, err := os.Lstat(prepared.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("staging path still exists or cannot be inspected: %v", err)
		}
	}
	if err := prepared.Remove(); err != nil {
		t.Fatalf("second Remove = %v, want idempotent success", err)
	}
}

func assertProgressStages(t *testing.T, progress []Progress, want []string) {
	t.Helper()
	stages := make([]string, len(progress))
	for index, event := range progress {
		stages[index] = event.Stage
		if event.Message == "" {
			t.Fatalf("progress %d has empty message", index)
		}
	}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("progress stages = %v, want %v", stages, want)
	}
}

func writeServiceConfig(t *testing.T, path, baseURL, token, root string) {
	t.Helper()
	content := fmt.Sprintf(`base_url = %q
token = %q
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q
`, baseURL, token, root)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
