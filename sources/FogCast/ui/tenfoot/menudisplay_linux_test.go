//go:build linux

package tenfoot

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRunMenuDisplaySmoke(t *testing.T) {
	server := framebufferFixture(t)
	err := Run(t.Context(), Options{
		GFX:          "menu-display",
		MenuSocket:   filepath.Join(t.TempDir(), "missing.sock"),
		Input:        "none",
		Smoke:        true,
		SmokeTimeout: 5 * time.Second,
		APIBase:      server.URL,
		Width:        640,
		Height:       480,
		PrefsPath:    filepath.Join(t.TempDir(), "prefs.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
}
