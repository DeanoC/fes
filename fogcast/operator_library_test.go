package fogcast_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
)

func TestOperatorSNESRootScanMakesActRaiserFindableOnPublicGamesAPI(t *testing.T) {
	dir := t.TempDir()
	root := operatorSNESTestRoot(t, dir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"ActRaiser.smc":         []byte("actraiser"),
		"ActRaiser (USA).sfc":   []byte("actraiser-usa"),
		"ActRaiser 2 (USA).sfc": []byte("actraiser-2"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:8182"
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "operator-snes-root"
system = "snes"
root = %q
`, root)
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

	service, err := fogcast.Open(context.Background(), paths, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()

	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	const watchedLibraryID = "operator-snes-root"
	if len(report.Roots) != 1 || report.Roots[0].RootID != watchedLibraryID || report.Roots[0].Offline || report.Roots[0].Added != 3 {
		t.Fatalf("scan report = %+v", report)
	}

	handler := hostapi.New(service)
	for _, query := range []string{"ActRaiser", "actraiser"} {
		page, err := service.QueryGames(context.Background(), catalog.Query{Text: query, Grouped: true, Limit: 20})
		if err != nil {
			t.Fatalf("QueryGames(%q): %v", query, err)
		}
		if !pageHasExactActRaiser(page.Games, watchedLibraryID) {
			t.Fatalf("QueryGames(%q) missing exact ActRaiser: %+v", query, titlesOf(page.Games))
		}
		response := serveOperatorGames(t, handler, "/api/v1/games?q="+query)
		if response.Code != http.StatusOK {
			t.Fatalf("GET /api/v1/games?q=%s status = %d body=%s", query, response.Code, response.Body.String())
		}
		body := response.Body.String()
		if strings.Contains(body, "operator-snes-root") || strings.Contains(body, watchedLibraryID) || strings.Contains(body, root) {
			t.Fatalf("public games leaked library id or root: %s", body)
		}
		var result struct {
			Games []struct {
				Title          string `json:"title"`
				CanonicalTitle string `json:"canonical_title"`
			} `json:"games"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, game := range result.Games {
			if catalog.ExactActRaiserTitle(game.CanonicalTitle, game.Title) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("public games q=%q missing ActRaiser: %s", query, body)
		}
	}

	detailPage, err := service.QueryGames(context.Background(), catalog.Query{Text: "ActRaiser 2", Grouped: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(detailPage.Games) != 1 || detailPage.Games[0].LibraryID != watchedLibraryID || detailPage.Games[0].CanonicalTitle != "ActRaiser 2" {
		t.Fatalf("sequel search = %+v", titlesOf(detailPage.Games))
	}
}

func TestLoadConfigRejectsFogCastMountsStyleSymlinkLibraryRoot(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "Games", "Games", "SNES")
	if err := os.MkdirAll(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	mounts := filepath.Join(dir, "FogCastMounts")
	if err := os.Mkdir(mounts, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(mounts, "SNES")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = "http://127.0.0.1:8182"
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "snes-main"
system = "snes"
root = %q
`, link)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := fogcast.LoadConfig(path)
	if err == nil {
		t.Fatal("symlink FogCastMounts-style root was accepted")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v", err)
	}
}

func pageHasExactActRaiser(games []catalog.Game, libraryID string) bool {
	for _, game := range games {
		if catalog.ExactActRaiserTitle(game.CanonicalTitle, game.Title) && game.LibraryID == libraryID {
			return true
		}
	}
	return false
}

func operatorSNESTestRoot(t *testing.T, parent string) string {
	t.Helper()
	// FTS intentionally searches game-id tokens. Choose a bounded fixture path
	// whose derived exact-ActRaiser ids cannot add an unrelated 2* token to the
	// "ActRaiser 2" query exercised below.
	for i := 0; i < 256; i++ {
		root := filepath.Join(parent, fmt.Sprintf("root-%03d", i), "Games", "Games", "SNES")
		const libraryID = "operator-snes-root"
		collides := false
		for _, game := range []struct {
			relativePath string
			title        string
		}{
			{relativePath: "ActRaiser.smc", title: "ActRaiser"},
			{relativePath: "ActRaiser (USA).sfc", title: "ActRaiser (USA)"},
		} {
			id := catalog.GameID("snes", libraryID, game.relativePath, game.title)
			digest := id[strings.LastIndexByte(id, '-')+1:]
			if strings.HasPrefix(digest, "2") {
				collides = true
				break
			}
		}
		if !collides {
			return root
		}
	}
	t.Fatal("could not select a noncolliding operator SNES fixture root")
	return ""
}

func titlesOf(games []catalog.Game) []string {
	titles := make([]string, 0, len(games))
	for _, game := range games {
		titles = append(titles, game.Title)
	}
	return titles
}

func serveOperatorGames(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
