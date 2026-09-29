package tenfoot

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
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

type recordingMenu struct {
	mu      sync.Mutex
	calls   int
	lastGen uint64
	lastLen int
}

func (c *recordingMenu) Status(context.Context) (menudisplay.Status, error) {
	return menudisplay.Status{Available: true, Generation: 3, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes}, nil
}

func (c *recordingMenu) Present(_ context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	c.calls++
	c.lastGen = generation
	c.lastLen = len(pixels)
	c.mu.Unlock()
	return menudisplay.Result{Generation: generation}, nil
}

func (c *recordingMenu) snapshot() (calls int, gen uint64, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.lastGen, c.lastLen
}

func TestTenfootMenuDisplayIsChangeDrivenAnd720p(t *testing.T) {
	client := &recordingMenu{}
	orig := openMenuDisplay
	t.Cleanup(func() { openMenuDisplay = orig })
	openMenuDisplay = func(string) (*gfx.MenuDisplay, error) {
		return gfx.NewMenuDisplayWithPresenter(client)
	}
	dev, err := openChangeDrivenMenu("ignored")
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	opts := Options{Width: 640, Height: 480, APIBase: "http://127.0.0.1:9", PrefsPath: filepath.Join(t.TempDir(), "prefs.json")}.normalized()
	opts = sizedOptions(opts, dev)
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	snap := app.Snapshot()
	if snap.Grid.Width != 1280 || snap.Grid.Height != 720 {
		t.Fatalf("grid %dx%d despite -width/-height", snap.Grid.Width, snap.Grid.Height)
	}
	dev.Clear(gfx.RGB(1, 2, 3))
	dev.Present()
	deadline := time.Now().Add(time.Second)
	for {
		calls, _, n := client.snapshot()
		if calls == 1 && n == menudisplay.FrameBytes && !dev.FramePending() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first frame calls=%d bytes=%d pending=%v", calls, n, dev.FramePending())
		}
		time.Sleep(time.Millisecond)
	}
	dev.Present()
	if dev.FramePending() {
		t.Fatal("identical frame was queued")
	}
	if calls, gen, n := client.snapshot(); calls != 1 || gen != 3 || n != menudisplay.FrameBytes {
		t.Fatalf("calls=%d gen=%d bytes=%d", calls, gen, n)
	}
}
