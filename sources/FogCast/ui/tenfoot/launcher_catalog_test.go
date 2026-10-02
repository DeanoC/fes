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
