package tenfoot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConfiguredAppBrowsesDataStormWhenTheRemoteHostIsAbsent(t *testing.T) {
	var launcherHits atomic.Int32
	launcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		launcherHits.Add(1)
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(launcher.Close)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "agent is absent", http.StatusBadGateway)
	}))
	t.Cleanup(agent.Close)

	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf(`base_url = %q
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "sms-main"
system = "sms"
root = %q
`, agent.URL, root)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := configuredApp(Options{
		GFX:           "menu-display",
		APIBase:       launcher.URL,
		CatalogConfig: configPath,
		PrefsPath:     filepath.Join(t.TempDir(), "tenfoot.json"),
		Width:         1280,
		Height:        720,
		MaxGames:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "local data storm", func(s Snapshot) bool {
		if s.Loading {
			return false
		}
		for _, game := range s.Games {
			if strings.Contains(game.Title, "Data Storm") && game.RootOnline {
				return true
			}
		}
		return false
	})
	if launcherHits.Load() != 0 {
		t.Fatalf("remote launcher received %d requests", launcherHits.Load())
	}
	found := false
	for _, game := range snap.Games {
		if strings.Contains(game.Title, "Data Storm") {
			found = true
		}
	}
	if !found {
		t.Fatalf("shelf = %+v", snap.Games)
	}
}

func TestReadOnlyCatalogConfigBootsTheLocalShelf(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a mode 0555 directory")
	}
	var launcherHits atomic.Int32
	launcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		launcherHits.Add(1)
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(launcher.Close)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "agent is absent", http.StatusBadGateway)
	}))
	t.Cleanup(agent.Close)

	writable := t.TempDir()
	root := filepath.Join(writable, "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(writable, "state")
	staging := filepath.Join(writable, "staging")
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	body := fmt.Sprintf(`base_url = %q
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2
state = %q
staging = %q

[[libraries]]
id = "sms-main"
system = "sms"
root = %q
`, agent.URL, state, staging, root)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDir, 0o755) })
	if err := os.Chmod(configDir, 0o555); err != nil {
		t.Fatal(err)
	}

	app, err := configuredApp(Options{
		GFX:           "menu-display",
		APIBase:       launcher.URL,
		CatalogConfig: configPath,
		PrefsPath:     filepath.Join(t.TempDir(), "tenfoot.json"),
		Width:         1280,
		Height:        720,
		MaxGames:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.localCatalogClose == nil {
		t.Fatal("read-only catalog config fell back to the host")
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "local data storm from a read-only config", func(s Snapshot) bool {
		if s.Loading {
			return false
		}
		for _, game := range s.Games {
			if strings.Contains(game.Title, "Data Storm") && game.RootOnline {
				return true
			}
		}
		return false
	})
	if launcherHits.Load() != 0 {
		t.Fatalf("remote launcher received %d requests", launcherHits.Load())
	}
	found := false
	for _, game := range snap.Games {
		if strings.Contains(game.Title, "Data Storm") {
			found = true
		}
	}
	if !found {
		t.Fatalf("shelf = %+v", snap.Games)
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("read-only config dir = %v", entries)
	}
	if _, err := os.Stat(filepath.Join(state, "library.sqlite3")); err != nil {
		t.Fatalf("state index: %v", err)
	}
}
