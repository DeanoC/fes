package tenfoot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const localDataStormScript = `
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
function on_input(cmd)
  if cmd == "sort" then
    library.query({ q = "Data Storm", limit = 20 }, apply)
    return true
  end
  return false
end
function draw()
  gfx.rect(0, 0, 10, 10, "#fff")
  gfx.hit("shelf", 0, 0, room.width, room.height)
end
`

func TestLocalHostRoomBrowsesDataStormWhenRemoteIsAbsent(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "remote host is absent", http.StatusBadGateway)
	}))
	t.Cleanup(remote.Close)

	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm 1.00.sms"), []byte("data-storm-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	content := fmt.Sprintf(`base_url = %q
token = "synthetic-token"
request_timeout_seconds = 1
upload_timeout_seconds = 2

[[libraries]]
id = "sms-main"
system = "sms"
root = %q
`, remote.URL, root)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"), Staging: filepath.Join(dir, "staging")}
	if err := os.MkdirAll(filepath.Dir(paths.Index), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.Staging, 0o700); err != nil {
		t.Fatal(err)
	}
	service, notice, err := fogcast.BootLocalCatalog(context.Background(), paths)
	if err != nil {
		t.Fatalf("BootLocalCatalog: %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if notice != "" {
		t.Fatalf("ready notice = %q", notice)
	}

	var mu sync.Mutex
	var launches int
	var sawAll bool
	handler := hostapi.New(service)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/launch" {
			mu.Lock()
			launches++
			mu.Unlock()
		}
		if r.URL.Path == "/api/v1/games" && r.URL.Query().Get("availability") == "all" && r.URL.Query().Get("q") == "Data Storm" {
			mu.Lock()
			sawAll = true
			mu.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(host.Close)

	index := rooms.NewIndex([]rooms.Pack{testRoomPack(t, "local", localDataStormScript)})
	app := NewApp(NewClient(host.URL, host.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, t.TempDir())
	app.SetHomeRooms(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "data storm", func(s Snapshot) bool {
		d := s.Room.Destination
		return s.Room.Open && len(d.Matches) == 1 && d.Matches[0].Title == "Data Storm 1.00" && d.Matches[0].RootOnline && len(s.Room.Frame.Hits) > 0
	})
	if snap.Room.Destination.Availability == rooms.AvailReady || snap.Room.Destination.Confirm() == rooms.ConfirmLaunch {
		t.Fatalf("raw local title was treated as playable: %+v", snap.Room.Destination)
	}
	mu.Lock()
	queried := sawAll
	mu.Unlock()
	if !queried {
		t.Fatal("room did not query the local catalog with availability=all")
	}
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	mu.Lock()
	gotLaunches := launches
	mu.Unlock()
	if gotLaunches != 0 || snap.Launch.Phase == "launching" || snap.Launch.Phase == "ok" || len(snap.Room.Frame.Hits) == 0 {
		t.Fatalf("confirm launched or left the shelf: launch=%+v posts=%d hits=%d", snap.Launch, gotLaunches, len(snap.Room.Frame.Hits))
	}

	app.HandleCommand(CmdBack, now)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Scan(context.Background()); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := service.LocalShelfNotice(context.Background()); got != fogcast.ShelfOffline {
		t.Fatalf("offline notice = %q", got)
	}
	app.HandleCommand(CmdSortCycle, now)
	snap = waitFor(t, app, "saved row after refresh", func(s Snapshot) bool {
		d := s.Room.Destination
		return s.Room.Open && len(d.Matches) == 1 && d.Matches[0].Title == "Data Storm 1.00" && !d.Matches[0].RootOnline && len(s.Room.Frame.Hits) > 0
	})
	if snap.Room.Destination.Availability == rooms.AvailMissing || snap.Room.Destination.Confirm() == rooms.ConfirmLaunch {
		t.Fatalf("refresh hid or launched the saved title: %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	mu.Lock()
	gotLaunches = launches
	mu.Unlock()
	if gotLaunches != 0 || len(app.Snapshot().Room.Frame.Hits) == 0 {
		t.Fatalf("offline confirm launched or blanked the room: launches=%d hits=%d", gotLaunches, len(app.Snapshot().Room.Frame.Hits))
	}
}

func TestLibraryRefreshKeepsSavedRowsWhenTheHostDrops(t *testing.T) {
	mario := availableGame("snes-mario", "Mario", "snes")
	var fail bool
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			mu.Lock()
			down := fail
			mu.Unlock()
			if down {
				http.Error(w, "down", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{mario}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	app := NewApp(NewClient(srv.URL, srv.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "mario", func(s Snapshot) bool { return !s.Loading && len(s.Games) == 1 && s.Games[0].ID == mario.ID })

	mu.Lock()
	fail = true
	mu.Unlock()
	app.mu.Lock()
	app.reloadLocked()
	app.mu.Unlock()
	snap := waitFor(t, app, "kept shelf", func(s Snapshot) bool {
		return !s.Loading && s.Status == savedListOfflineCopy && len(s.Games) == 1 && s.Games[0].ID == mario.ID
	})
	if snap.LoadErr != "" {
		t.Fatalf("refresh surfaced a load error: %q", snap.LoadErr)
	}
}

func TestLibraryShowsSavedRowsWhenNothingIsReady(t *testing.T) {
	offline := availableGame("sms-data-storm", "Data Storm 1.00", "sms")
	offline.RootOnline = false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		games := []hostclient.Game{}
		notice := ""
		if r.URL.Query().Get("availability") == "all" {
			games = []hostclient.Game{offline}
			notice = savedListOfflineCopy
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"games": games, "notice": notice})
	}))
	t.Cleanup(srv.Close)
	app := NewApp(NewClient(srv.URL, srv.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "saved list", func(s Snapshot) bool {
		return !s.Loading && len(s.Games) == 1 && s.Games[0].ID == offline.ID && s.Status == savedListOfflineCopy
	})
	app.mu.Lock()
	app.startLaunchGameLocked(snap.Games[0])
	phase, message := app.launch.Phase, app.launch.Message
	app.mu.Unlock()
	if phase != "error" || message != "This game's source is offline." {
		t.Fatalf("offline launch phase=%s message=%q", phase, message)
	}
}

func TestMissingCartridgeRefusesLaunchWhileTheShelfStays(t *testing.T) {
	h := newRoomHost(t)
	app := newRoomApp(t, h, rooms.NewIndex(nil), false)
	game := availableGame("sms-data-storm", "Data Storm 1.00", "sms")
	game.ROMRequired = true
	waitFor(t, app, "shelf", func(s Snapshot) bool { return !s.Loading && len(s.Games) > 0 })
	app.mu.Lock()
	app.games = []hostclient.Game{game}
	app.grid.SetCount(1)
	app.grid.Focus = 0
	app.startLaunchGameLocked(game)
	phase, message := app.launch.Phase, app.launch.Message
	left := len(app.games)
	app.mu.Unlock()
	if phase != "error" || message != "Needs a cartridge" || h.launchCount() != 0 || left != 1 {
		t.Fatalf("phase=%s message=%q launches=%d games=%d", phase, message, h.launchCount(), left)
	}
}
