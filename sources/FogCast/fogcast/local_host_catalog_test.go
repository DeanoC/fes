package fogcast_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestBootLocalCatalogBrowsesDataStormWithoutRemoteHost(t *testing.T) {
	var remoteHits atomic.Int32
	var remotePaths atomic.Value
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteHits.Add(1)
		remotePaths.Store(r.URL.Path)
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(remote.Close)

	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rom := filepath.Join(root, "Data Storm 1.00.sms")
	if err := os.WriteFile(rom, []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := localCatalogPaths(t, dir, remote.URL, root)

	ctx := context.Background()
	service, notice, err := fogcast.BootLocalCatalog(ctx, paths)
	if err != nil {
		t.Fatalf("BootLocalCatalog: %v", err)
	}
	if notice != "" {
		t.Fatalf("ready notice = %q", notice)
	}
	if remoteHits.Load() != 0 {
		t.Fatalf("remote host received %d requests during boot", remoteHits.Load())
	}
	game := localCatalogGame(t, service, "Data Storm")
	if game.Title != "Data Storm 1.00" || game.System != protocol.SystemSMS || !game.RootOnline {
		t.Fatalf("catalog row = %+v", game)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, notice, err := fogcast.BootLocalCatalog(ctx, paths)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if notice != "" {
		t.Fatalf("reopened notice = %q", notice)
	}
	game = localCatalogGame(t, reopened, "Data Storm")
	handler := hostapi.New(reopened)
	list := serveLocalGames(t, handler, "/api/v1/games?q=Data+Storm")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Games []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			System     string `json:"system"`
			RootOnline bool   `json:"root_online"`
		} `json:"games"`
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Notice != "" || len(listed.Games) != 1 || listed.Games[0].ID != game.ID || listed.Games[0].Title != "Data Storm 1.00" || listed.Games[0].System != "sms" || !listed.Games[0].RootOnline {
		t.Fatalf("public list = %+v", listed)
	}
	detail := serveLocalGames(t, handler, "/api/v1/games/"+game.ID)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detail.Code, detail.Body.String())
	}
	var got struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		System string `json:"system"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != game.ID || got.Title != "Data Storm 1.00" || got.System != "sms" {
		t.Fatalf("detail = %+v", got)
	}
	// Catalog open, scan, and query do not dial. The games list may probe
	// the configured target's ROM cache and must still resolve the local row
	// when that target is absent.
	if hits := remoteHits.Load(); hits > 0 {
		if path, _ := remotePaths.Load().(string); path != "/v2/cache" {
			t.Fatalf("remote host received %d requests, last path %q", hits, path)
		}
	}
	beforeRemove := remoteHits.Load()

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Scan(ctx); err != nil {
		t.Fatalf("rescan after content removed: %v", err)
	}
	if got := reopened.LocalShelfNotice(ctx); got != fogcast.ShelfOffline {
		t.Fatalf("offline notice = %q", got)
	}
	offlineList := serveLocalGames(t, handler, "/api/v1/games")
	var offline struct {
		Games []struct {
			Title      string `json:"title"`
			RootOnline bool   `json:"root_online"`
		} `json:"games"`
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal(offlineList.Body.Bytes(), &offline); err != nil {
		t.Fatal(err)
	}
	if offline.Notice != fogcast.ShelfOffline || len(offline.Games) != 1 || offline.Games[0].Title != "Data Storm 1.00" || offline.Games[0].RootOnline {
		t.Fatalf("offline list = %+v", offline)
	}
	if remoteHits.Load() != beforeRemove {
		if path, _ := remotePaths.Load().(string); path != "/v2/cache" {
			t.Fatalf("rescan dialed %q", path)
		}
	}
}

func TestBootLocalCatalogExplainsEmptyAndMissingContent(t *testing.T) {
	var remotePaths atomic.Value
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remotePaths.Store(r.URL.Path)
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(remote.Close)

	emptyDir := t.TempDir()
	emptyRoot := filepath.Join(emptyDir, "sms")
	if err := os.MkdirAll(emptyRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	empty, notice, err := fogcast.BootLocalCatalog(context.Background(), localCatalogPaths(t, emptyDir, remote.URL, emptyRoot))
	if err != nil {
		t.Fatalf("empty boot: %v", err)
	}
	defer empty.Close()
	if notice != fogcast.ShelfEmpty {
		t.Fatalf("empty notice = %q", notice)
	}
	if path, ok := remotePaths.Load().(string); ok {
		t.Fatalf("empty boot dialed %s", path)
	}
	body := serveLocalGames(t, hostapi.New(empty), "/api/v1/games")
	var page struct {
		Games  []any  `json:"games"`
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal(body.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Notice != fogcast.ShelfEmpty || len(page.Games) != 0 {
		t.Fatalf("empty page = %+v", page)
	}

	missingDir := t.TempDir()
	missingRoot := filepath.Join(missingDir, "sms-missing")
	missing, notice, err := fogcast.BootLocalCatalog(context.Background(), localCatalogPaths(t, missingDir, remote.URL, missingRoot))
	if err != nil {
		t.Fatalf("missing boot: %v", err)
	}
	defer missing.Close()
	if notice != fogcast.ShelfMissing {
		t.Fatalf("missing notice = %q", notice)
	}
	if path, _ := remotePaths.Load().(string); path != "" && path != "/v2/cache" {
		t.Fatalf("missing boot dialed %s", path)
	}
}

func localCatalogPaths(t *testing.T, dir, baseURL, root string) fogcast.Paths {
	t.Helper()
	configPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = %q
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "sms-main"
system = "sms"
root = %q
`, baseURL, root)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{
		Config:  configPath,
		Index:   filepath.Join(dir, "state", "library.sqlite3"),
		Staging: filepath.Join(dir, "staging"),
	}
	if err := os.MkdirAll(filepath.Dir(paths.Index), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Staging, 0o700); err != nil {
		t.Fatal(err)
	}
	return paths
}

func serveLocalGames(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func localCatalogGame(t *testing.T, service *fogcast.Service, text string) catalog.Game {
	t.Helper()
	page, err := service.QueryGames(context.Background(), catalog.Query{Text: text, Limit: 20})
	if err != nil {
		t.Fatalf("QueryGames: %v", err)
	}
	if len(page.Games) != 1 {
		t.Fatalf("QueryGames(%q) rows = %d", text, len(page.Games))
	}
	return page.Games[0]
}
