//go:build linux

package tenfoot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
)

func TestRunMenuDisplaySmoke(t *testing.T) {
	server := framebufferFixture(t)
	client := &recordingMenu{}
	original := openMenuDisplay
	t.Cleanup(func() { openMenuDisplay = original })
	openMenuDisplay = func(string) (*gfx.MenuDisplay, error) {
		return gfx.NewMenuDisplayWithPresenter(client)
	}
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
	if calls, gen, n := client.snapshot(); calls == 0 || gen != 3 || n != menudisplay.FrameBytes {
		t.Fatalf("smoke did not present a full 720p frame: calls=%d generation=%d bytes=%d", calls, gen, n)
	}
}
