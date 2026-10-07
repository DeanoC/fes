package tenfoot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
)

// writeCatalogConfig writes a kit catalog config like the image default:
// optional [[targets]] and one sms library at root.
func writeCatalogConfig(t *testing.T, root, targets string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf(`request_timeout_seconds = 1
upload_timeout_seconds = 2
state = %q
staging = %q
%s
[[libraries]]
id = "sms-main"
system = "sms"
root = %q
`, filepath.Join(dir, "state"), filepath.Join(dir, "staging"), targets, root)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const disabledKitTarget = `selected_target = "kit"

[[targets]]
name = "kit"
enabled = false
`

func hostLauncher(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "host fixture", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func smsRootWithGame(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func hostApp(t *testing.T, apiBase, catalog string) *App {
	t.Helper()
	app, err := configuredApp(Options{
		GFX:           "menu-display",
		APIBase:       apiBase,
		CatalogConfig: catalog,
		PrefsPath:     filepath.Join(t.TempDir(), "tenfoot.json"),
		Width:         1280,
		Height:        720,
		MaxGames:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func waitHostHit(t *testing.T, app *App, hits *atomic.Int32) {
	t.Helper()
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("launcher.json host was never asked for the library")
	}
}

// #616: the image default (disabled kit target, missing games root) must
// not replace the launcher.json host library.
func TestImageDefaultCatalogKeepsTheHostLibrary(t *testing.T) {
	host, hits := hostLauncher(t)
	missing := filepath.Join(t.TempDir(), "games", "sms")
	app := hostApp(t, host.URL, writeCatalogConfig(t, missing, disabledKitTarget))
	if app.localCatalogClose != nil {
		t.Fatal("empty kit-local catalog replaced the host library")
	}
	waitHostHit(t, app, hits)
}

// No local games (root exists but is empty) falls back to the host.
func TestEmptyLocalShelfKeepsTheHostLibrary(t *testing.T) {
	host, hits := hostLauncher(t)
	empty := filepath.Join(t.TempDir(), "sms")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	app := hostApp(t, host.URL, writeCatalogConfig(t, empty, ""))
	if app.localCatalogClose != nil {
		t.Fatal("empty local shelf replaced the host library")
	}
	waitHostHit(t, app, hits)
}

// Local games but every declared target disabled falls back to the host.
func TestDisabledLocalTargetKeepsTheHostLibrary(t *testing.T) {
	host, hits := hostLauncher(t)
	app := hostApp(t, host.URL, writeCatalogConfig(t, smsRootWithGame(t), disabledKitTarget))
	if app.localCatalogClose != nil {
		t.Fatal("catalog with only disabled targets replaced the host library")
	}
	waitHostHit(t, app, hits)
}

// An enabled local target with local games keeps the kit-local shelf (#385).
func TestEnabledLocalTargetWithGamesUsesTheLocalShelf(t *testing.T) {
	host, _ := hostLauncher(t)
	targets := `selected_target = "kit"

[[targets]]
name = "kit"
enabled = true
address = "http://127.0.0.1:1"
agent = "synthetic-agent-token"
`
	app := hostApp(t, host.URL, writeCatalogConfig(t, smsRootWithGame(t), targets))
	if app.localCatalogClose == nil {
		t.Fatal("enabled local target with games fell back to the host")
	}
	app.Stop()
}

func TestUseLocalCatalogNotices(t *testing.T) {
	path := writeCatalogConfig(t, smsRootWithGame(t), "")
	for _, notice := range []string{fogcast.ShelfEmpty, fogcast.ShelfMissing, fogcast.ShelfOffline} {
		if useLocalCatalog(path, notice) {
			t.Fatalf("notice %q chose the local shelf", notice)
		}
	}
	if !useLocalCatalog(path, "") {
		t.Fatal("playable local shelf without targets did not choose the local shelf")
	}
}
