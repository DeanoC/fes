package tenfoot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const dataStormPlayScript = `
resumed = 0
function apply(games, err)
  if err ~= nil then
    destination.set({ kind = "game", label = "Data Storm", query = "Data Storm", missing = true })
    return
  end
  destination.set({ kind = "game", label = "Data Storm", query = "Data Storm", matches = games })
end
function load()
  library.query({ q = "Data Storm", limit = 20 }, apply)
end
function on_resume()
  resumed = resumed + 1
end
function draw()
  gfx.clear("#102030")
  gfx.text("resumed-" .. tostring(resumed), 8, 8, { size = 16 })
  gfx.hit("shelf", 0, 0, room.width, room.height)
end
`

func TestDataStormKitDirectLaunchPadStopAndReturn(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(remote.Close)
	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	rom := []byte("data-storm-fixture")
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), rom, 0o600); err != nil {
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
`, remote.URL, root)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}
	if err := os.MkdirAll(filepath.Dir(paths.Index), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Staging, 0o700); err != nil {
		t.Fatal(err)
	}
	service, _, err := fogcast.BootLocalCatalog(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })

	var launches atomic.Int32
	var inputs atomic.Int32
	handler := hostapi.New(service)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/launch" {
			launches.Add(1)
		}
		if r.URL.Path == "/api/v1/session/input/event" || r.URL.Path == "/api/v1/launcher/input" {
			inputs.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(host.Close)

	hold := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	runtime := &holdCartridge{hold: hold}
	packages := filepath.Join(dir, "pkgs")
	selections := filepath.Join(dir, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	installSMSCore(t, packages, selections)
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	cores := localcores.New(manager, runtime, localcores.Roots{Selections: selections, Packages: packages})
	t.Cleanup(cores.Close)
	sock := fmt.Sprintf("/tmp/f385-%d.sock", os.Getpid())
	t.Cleanup(func() { _ = os.Remove(sock) })
	serveCtx, serveCancel := context.WithCancel(context.Background())
	t.Cleanup(serveCancel)
	go func() { _ = localcores.Serve(serveCtx, sock, localcores.Handler(cores)) }()
	waitForSocket(t, sock)

	index := rooms.NewIndex([]rooms.Pack{testRoomPack(t, "storm", dataStormPlayScript)})
	app := NewApp(NewClient(host.URL, host.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("storm")
	app.localContent = service.LocalContentPath
	app.SetKitLocal(localcores.NewClient(sock), &fakePadFeed{})
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	snap := waitFor(t, app, "data storm ready", func(s Snapshot) bool {
		d := s.Room.Destination
		return s.Room.Open && s.Room.ID == "storm" && d.Confirm() == rooms.ConfirmLaunchKit && d.KitDirect && len(d.Matches) == 1 && d.Matches[0].System == "sms"
	})
	focus := snap.Room.Destination.Label

	app.HandleCommand(CmdSelect, time.Unix(100, 0))
	snap = app.Snapshot()
	if snap.LocalCorePhase != localPhaseLaunching || !snap.LocalCorePresentsPaused {
		t.Fatalf("confirm did not start the local cartridge phase=%s paused=%v", snap.LocalCorePhase, snap.LocalCorePresentsPaused)
	}
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), time.Unix(110, 0))
	if app.localFeed.(*fakePadFeed).count(remoteinput.ButtonA, remoteinput.ActionPress) != 0 {
		t.Fatal("launch forwarded A")
	}
	if launches.Load() != 0 || inputs.Load() != 0 {
		t.Fatalf("launch left the kit path launches=%d inputs=%d", launches.Load(), inputs.Load())
	}

	release()
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused && s.Room.Destination.Label == focus
	})
	got := runtime.roms()
	if len(got) != 1 || string(got[0]) != string(rom) || runtime.coreLoads != 0 {
		t.Fatalf("runtime roms=%q coreLoads=%d", got, runtime.coreLoads)
	}
	feed := app.localFeed.(*fakePadFeed)
	tPlay := time.Unix(200, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), tPlay)
	app.HandleLocalPad(padButton(remoteinput.ButtonA, false), tPlay.Add(20*time.Millisecond))
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 1 || feed.count(remoteinput.ButtonA, remoteinput.ActionRelease) != 1 {
		t.Fatalf("running cartridge did not see A: %+v", feed.snapshot())
	}

	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	snap = waitFor(t, app, "resume", func(s Snapshot) bool {
		return !s.LocalCorePresentsPaused && s.LocalCorePhase == "" && s.LocalCoreRedraw >= 1 && roomTextHas(s, "resumed-1")
	})
	if runtime.stops() != 1 {
		t.Fatalf("stops %d", runtime.stops())
	}
	if !snap.Room.Open || snap.Room.ID != "storm" || snap.RoomPicker.Open || snap.Room.Destination.Label != focus {
		t.Fatalf("room was not restored: %+v picker=%v", snap.Room, snap.RoomPicker.Open)
	}
	if launches.Load() != 0 || inputs.Load() != 0 {
		t.Fatalf("pad path used the host launches=%d inputs=%d", launches.Load(), inputs.Load())
	}
}

type holdCartridge struct {
	mu        sync.Mutex
	romBytes  [][]byte
	coreLoads int
	stopCount int
	hold      chan struct{}
}

func (h *holdCartridge) LoadCore(context.Context, context.Context, string, string) error {
	h.mu.Lock()
	h.coreLoads++
	h.mu.Unlock()
	return nil
}

func (h *holdCartridge) LoadCartridge(_ context.Context, _ context.Context, _, _ string, rom []byte) error {
	h.mu.Lock()
	h.romBytes = append(h.romBytes, append([]byte(nil), rom...))
	h.mu.Unlock()
	if h.hold != nil {
		<-h.hold
	}
	return nil
}

func (h *holdCartridge) Stop(context.Context, context.Context) error {
	h.mu.Lock()
	h.stopCount++
	h.mu.Unlock()
	return nil
}

func (h *holdCartridge) roms() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([][]byte, len(h.romBytes))
	for i := range h.romBytes {
		out[i] = append([]byte(nil), h.romBytes[i]...)
	}
	return out
}

func (h *holdCartridge) stops() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopCount
}

func installSMSCore(t *testing.T, packages, selections string) string {
	t.Helper()
	coreID := "fes.sms"
	sum := sha256.Sum256([]byte(coreID))
	id := hex.EncodeToString(sum[:])
	payload := []byte("rbf:" + coreID)
	payloadSum := sha256.Sum256(payload)
	payloadSHA := hex.EncodeToString(payloadSum[:])
	dir := filepath.Join(packages, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Master System"
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = "fes.computer"
major = 1
minor = 0
`, coreID, len(payload), payloadSHA)
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core.rbf"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n", coreID, id, payloadSHA, dir)
	if err := os.WriteFile(filepath.Join(selections, "fes-sms.package.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", path)
}
