//go:build !wasip1 && !tamago

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
)

func TestCatalogFixtureScansThroughCurrentAPIAndCloses(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	if err := os.Mkdir(library, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(library, "Aster.sfc"), make([]byte, 32768), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.toml")
	text := fmt.Sprintf("selected_target = \"unavailable\"\nrequest_timeout_seconds = 3\nupload_timeout_seconds = 5\nstate = %q\nstaging = %q\n[[targets]]\nname = \"unavailable\"\nenabled = false\n[[libraries]]\nid = \"fixture\"\nsystem = \"snes\"\nroot = %q\n",
		filepath.Join(root, "state"), filepath.Join(root, "staging"), library)
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	paths, err := fogcast.PathsForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	host, err := serveCatalog(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	address, err := url.Parse(host.Base)
	if err != nil || address.Hostname() != "127.0.0.1" || address.Port() == "" {
		t.Fatal("fixture did not bind loopback", host.Base, err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(host.Base + "/api/v1/games")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Games []struct{ Title string }
	}
	err = json.NewDecoder(response.Body).Decode(&body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || len(body.Games) != 1 || body.Games[0].Title != "Aster" {
		t.Fatalf("scanned fixture unavailable: %+v, %v", body, err)
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	if response, err = client.Get(host.Base + "/api/v1/games"); err == nil {
		_ = response.Body.Close()
		t.Fatal("fixture listener survived Close")
	}
}
