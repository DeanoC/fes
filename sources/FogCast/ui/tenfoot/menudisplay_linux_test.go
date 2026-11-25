//go:build linux

package tenfoot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
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

// Keep the pixel observations independent of the present call count, so the
// Run-level test proves the changed view really reached the presenter.
type runRecordingMenu struct {
	recordingMenu
	framesMu sync.Mutex
	frames   [][sha256.Size]byte
}

func (c *runRecordingMenu) Present(ctx context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	digest := sha256.Sum256(pixels)
	c.framesMu.Lock()
	c.frames = append(c.frames, digest)
	c.framesMu.Unlock()
	return c.recordingMenu.Present(ctx, generation, pixels)
}

func (c *runRecordingMenu) frameDigests() [][sha256.Size]byte {
	c.framesMu.Lock()
	defer c.framesMu.Unlock()
	return append([][sha256.Size]byte(nil), c.frames...)
}

// Test the actual Run path, including its app sizing and change-driven
// menu-display selection. The gated host keeps the rendered state stable
// until the test explicitly releases a changed library result.
func TestRunMenuDisplayChangeDrivenAndGridSize(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(gate) })
	arrived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/games":
			select {
			case arrived <- struct{}{}:
			default:
			}
			select {
			case <-gate:
				_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("sms-one", "SMS test one", "sms")}})
			case <-r.Context().Done():
			}
		case "/api/v1/core-catalog":
			_, _ = io.WriteString(w, `{"cores":[{"source_id":"fixture","core_id":"fes.sms","label":"SMS","artifact_state":"installed"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	traceDir := t.TempDir()
	t.Setenv("FOGCAST_FRAME_TRACE_DIR", traceDir)
	// Keep SIGUSR1 safe during startup and teardown even before or after the
	// display loop's own listener exists.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	defer signal.Stop(signals)
	client := &runRecordingMenu{recordingMenu: recordingMenu{presentEntered: make(chan struct{}, 8)}}
	opened := make(chan *gfx.MenuDisplay, 1)
	openedSocket := make(chan string, 1)
	original := openMenuDisplay
	defer func() { openMenuDisplay = original }()
	openMenuDisplay = func(socket string) (*gfx.MenuDisplay, error) {
		openedSocket <- socket
		display, err := gfx.NewMenuDisplayWithPresenter(client)
		if err == nil {
			opened <- display
		}
		return display, err
	}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	prefs := filepath.Join(t.TempDir(), "prefs.json")
	menuSocket := filepath.Join(t.TempDir(), "menu.sock")
	go func() {
		result <- Run(ctx, Options{
			GFX: "menu-display", MenuSocket: menuSocket, Input: "none",
			APIBase: server.URL, Width: 640, Height: 480, Smoke: true,
			SmokeTimeout: 20 * time.Second, PrefsPath: prefs,
		})
	}()
	defer func() {
		release.Do(func() { close(gate) })
		cancel()
		select {
		case err := <-result:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("Run returned: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("Run did not stop after cancellation")
		}
	}()
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("library request did not reach the gated host")
	}
	select {
	case <-client.presentEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not present its first frame")
	}
	var display *gfx.MenuDisplay
	select {
	case display = <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not open the menu display")
	}
	if got := <-openedSocket; got != menuSocket {
		t.Fatalf("Run opened menu socket %q, want %q", got, menuSocket)
	}

	dumpCount := 0
	dump := func() []frameTraceRecord {
		t.Helper()
		dumpCount++
		if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			pattern := fmt.Sprintf("fogcast-frame-trace-%d-%d-*.jsonl", os.Getpid(), dumpCount)
			files, err := filepath.Glob(filepath.Join(traceDir, pattern))
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 1 {
				file, err := os.Open(files[0])
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				var records []frameTraceRecord
				decoder := json.NewDecoder(file)
				for {
					var record frameTraceRecord
					if err := decoder.Decode(&record); err == io.EOF {
						break
					} else if err != nil {
						t.Fatal(err)
					}
					records = append(records, record)
				}
				return records
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("frame trace dump did not complete")
		return nil
	}
	// A completed trace tick is a barrier after Present, not a sleep-based
	// assumption that the render or presentation worker has run.
	deadline := time.Now().Add(10 * time.Second)
	for {
		records := dump()
		ticks := 0
		for _, record := range records {
			if record.Kind == "tick" {
				ticks++
				if record.GridWidth != 1280 || record.GridHeight != 720 {
					t.Fatalf("Run app grid %dx%d, want 1280x720", record.GridWidth, record.GridHeight)
				}
			}
		}
		if ticks >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not complete enough stable ticks")
		}
		time.Sleep(20 * time.Millisecond)
	}
	idleCtx, idleCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer idleCancel()
	if err := display.WaitPending(idleCtx); err != nil {
		t.Fatalf("menu worker did not drain the stable frames: %v", err)
	}
	if calls, _, _ := client.snapshot(); calls != 1 {
		t.Fatalf("unchanged frames submitted %d times, want once", calls)
	}
	release.Do(func() { close(gate) })
	select {
	case <-client.presentEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("changed library did not submit another frame")
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		calls, _, _ := client.snapshot()
		if calls >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("changed library made %d calls, want at least two", calls)
		}
		time.Sleep(time.Millisecond)
	}
	frames := client.frameDigests()
	if len(frames) < 2 || frames[0] == frames[1] {
		t.Fatalf("changed library did not present changed pixels: frame count %d", len(frames))
	}
}
