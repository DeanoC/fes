package tenfoot

import (
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestNormalizedMenuSocketDefault(t *testing.T) {
	opts := Options{PrefsPath: filepath.Join(t.TempDir(), "missing.json")}.normalized()
	if opts.MenuSocket != defaultMenuSocket {
		t.Fatalf("socket %q", opts.MenuSocket)
	}
	opts = Options{MenuSocket: "/tmp/custom.sock", PrefsPath: filepath.Join(t.TempDir(), "missing.json")}.normalized()
	if opts.MenuSocket != "/tmp/custom.sock" {
		t.Fatalf("socket %q", opts.MenuSocket)
	}
}

func TestMenuDisplayForces720p(t *testing.T) {
	dev, err := gfx.NewMenuDisplay(filepath.Join(t.TempDir(), "missing.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	cfg := dev.Config()
	if dev.BackendName() != gfx.BackendMenuDisplay || cfg.Width != 1280 || cfg.Height != 720 || cfg.Stride != 5120 || cfg.BPP != 32 {
		t.Fatalf("backend %q config %+v", dev.BackendName(), cfg)
	}
	opts := Options{Width: 640, Height: 480, APIBase: "http://127.0.0.1:9", PrefsPath: filepath.Join(t.TempDir(), "prefs.json")}.normalized()
	opts = sizedOptions(opts, dev)
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	snap := app.Snapshot()
	if snap.Grid.Width != 1280 || snap.Grid.Height != 720 {
		t.Fatalf("grid %dx%d", snap.Grid.Width, snap.Grid.Height)
	}
}
