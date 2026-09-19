package tenfoot

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const testRoomScript = `
local Grid = require "widgets.grid"
grid = nil
resumed = 0
loads = 0
function load()
  loads = loads + 1
  library.query({ platform = "snes" }, function(games, err)
    assert(err == nil, err)
    grid = Grid.new{ id = "g", x = 20, y = 40, w = room.width - 40, h = room.height - 80, cell_w = 160, cell_h = 200, items = games }
    for _, g in ipairs(games) do g.cover = image.cover(g.id) end
  end)
end
function on_input(cmd)
  if grid and grid:input(cmd) then return true end
  if cmd == "select" and grid then
    local g = grid:selected()
    if g then session.launch(g.id) end
    return true
  end
  if cmd == "search" or cmd == "tab" then rooms.open_library{ platform = "megadrive", layout = "shelf" } return true end
  if cmd == "sort" then rooms.open("nested") return true end
  return false
end
function on_hover(id) if grid then grid:on_hover(id) end end
function on_activate(id) if grid and grid:on_activate(id) then on_input("select") end end
function on_resume() resumed = resumed + 1 end
function draw()
  gfx.clear("#102030")
  gfx.rect(0, 0, room.width, 30, room.theme.accent)
  gfx.text(room.title, 20, 4, { size = 20, bold = true })
  if grid then grid:draw{ cover = function(item) return item.cover end } end
end`

func testRoomPack(t *testing.T, id, script string) rooms.Pack {
	t.Helper()
	fsys := fstest.MapFS{
		rooms.ManifestName: {Data: []byte("id = \"" + id + "\"\ntitle = \"Room " + id + "\"\n")},
		"main.lua":         {Data: []byte(script)},
	}
	p := rooms.LoadPackFS(fsys, "mem:"+id)
	if p.Err != nil {
		t.Fatal(p.Err)
	}
	return p
}

type roomHost struct {
	mu       sync.Mutex
	launches []string
	state    string
	server   *httptest.Server
	// launchGate, when set, holds the launch response until it is closed so
	// tests can observe the "launching" phase.
	launchGate   chan struct{}
	launchStatus int
	launchBody   string
}

func newRoomHost(t *testing.T) *roomHost {
	t.Helper()
	h := &roomHost{state: "idle"}
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			games := []hostclient.Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("snes-zelda", "Zelda", "snes"),
			}
			if r.URL.Query().Get("platform") == "megadrive" {
				games = []hostclient.Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []hostclient.Platform{{ID: "snes", Label: "SNES"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []hostclient.Collection{}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
			_ = json.NewEncoder(w).Encode(hostclient.Presentation{GameID: id, State: "ready", Presentation: &hostclient.PresentationInfo{CoverArtworkID: handle}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			h.mu.Lock()
			gate := h.launchGate
			status := h.launchStatus
			body := h.launchBody
			h.mu.Unlock()
			if gate != nil {
				<-gate
			}
			h.mu.Lock()
			h.launches = append(h.launches, string(raw))
			if status == 0 {
				h.state = "active"
			}
			h.mu.Unlock()
			if status != 0 {
				if status == http.StatusOK {
					status = http.StatusInternalServerError
				}
				w.WriteHeader(status)
				if body == "" {
					body = `{"error":{"code":"TRANSFER_FAILED","message":"content transfer failed"}}`
				}
				_, _ = io.WriteString(w, body)
				return
			}
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			h.mu.Lock()
			h.state = "idle"
			h.mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			h.mu.Lock()
			state := h.state
			h.mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"`+state+`"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *roomHost) launchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.launches)
}

func newRoomApp(t *testing.T, h *roomHost, index *rooms.Index, homeRooms bool) *App {
	t.Helper()
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(t.TempDir() + "/tenfoot.json")
	app.SetRooms(index, "/tmp/rooms")
	app.SetHomeRooms(homeRooms)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func waitFor(t *testing.T, app *App, what string, pred func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if pred(snap) {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := app.Snapshot()
	t.Fatalf("timed out waiting for %s: room=%+v picker=%+v status=%q", what, snap.Room.Open, snap.RoomPicker.Open, snap.Status)
	return snap
}

func TestRoomPickerOpensRoomAndLaunchesThroughHost(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()

	snap := waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	if len(snap.RoomPicker.Rows) != 3 || !snap.RoomPicker.Rows[0].Library || snap.RoomPicker.Rows[1].ID != "arcade" {
		t.Fatalf("picker rows %+v", snap.RoomPicker.Rows)
	}
	if snap.HeaderHint() == "" {
		t.Fatal("picker hint missing")
	}
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "room open with grid", func(s Snapshot) bool {
		return s.Room.Open && len(s.Room.Frame.Hits) >= 2
	})
	if snap.Room.ID != "arcade" || snap.RoomPicker.Open {
		t.Fatalf("room %+v picker %v", snap.Room.ID, snap.RoomPicker.Open)
	}
	if snap.Room.OffsetX != snap.Grid.contentLeft() || snap.Room.Width != snap.Grid.contentWidth() {
		t.Fatalf("room geometry %+v", snap.Room)
	}
	if !strings.Contains(snap.HeaderHint(), "back") {
		t.Fatalf("room hint %q", snap.HeaderHint())
	}

	// Covers requested by the script are fetched from the host and exposed for upload.
	snap = waitFor(t, app, "room cover", func(s Snapshot) bool {
		return len(s.Room.Images) >= 2
	})
	imageOps := 0
	for _, op := range snap.Room.Frame.Ops {
		if op.Kind == rooms.OpImage {
			imageOps++
		}
	}
	if imageOps != 2 {
		t.Fatalf("expected 2 cover blits, got %d", imageOps)
	}

	// Pointer: hovering the second cell moves focus; click launches through the host.
	hit := snap.Room.Frame.Hits[1]
	px := snap.Room.OffsetX + int(hit.X+hit.W/2)
	py := snap.Room.OffsetY + int(hit.Y+hit.H/2)
	if got := HitTest(snap, px, py); got.Kind != PointerRoom || got.KeyID != "g:2" {
		t.Fatalf("hit test %+v", got)
	}
	app.PointerClick(px, py, now)
	snap = waitFor(t, app, "launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 || !strings.Contains(h.launches[0], "snes-zelda") {
		t.Fatalf("launches %v", h.launches)
	}
	if !snap.GPUParked || !snap.Room.Open {
		t.Fatalf("room must stay open under the session: parked=%v open=%v", snap.GPUParked, snap.Room.Open)
	}

	// Stopping the session resumes the room.
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "resume", func(s Snapshot) bool { return !s.GPUParked })
	waitFor(t, app, "on_resume", func(s Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.room != nil && app.room.Err() == nil && luaGlobalNumber(app, "resumed") == 1
	})

	// Nested room, then Back resumes the suspended parent (same VM: load ran
	// once, on_resume fired again), then Back again returns to the picker.
	app.mu.Lock()
	parentBefore := app.room
	app.mu.Unlock()
	app.HandleCommand(CmdSortCycle, now)
	snap = waitFor(t, app, "nested", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "nested" })
	app.HandleCommand(CmdBack, now)
	snap = waitFor(t, app, "parent", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "arcade" })
	app.mu.Lock()
	sameVM := app.room == parentBefore
	loads := luaGlobalNumber(app, "loads")
	resumed := luaGlobalNumber(app, "resumed")
	app.mu.Unlock()
	if !sameVM || loads != 1 || resumed != 2 {
		t.Fatalf("parent must be suspended, not reloaded: sameVM=%v loads=%v resumed=%v", sameVM, loads, resumed)
	}

	// A safe-area nudge resizes the live room.
	app.HandleCommand(CmdSafeAreaIn, now)
	snap = app.Snapshot()
	if snap.Room.Width != snap.Grid.contentWidth() {
		t.Fatalf("room width %d != content %d", snap.Room.Width, snap.Grid.contentWidth())
	}
	app.mu.Lock()
	if err := app.room.CheckGlobal("room.width == " + itoa(snap.Grid.contentWidth())); err != nil {
		app.mu.Unlock()
		t.Fatal(err)
	}
	app.mu.Unlock()

	// Back while a launch is in flight must not leave the room.
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(gate)
			h.mu.Lock()
			h.launchGate = nil
			h.mu.Unlock()
		})
	}
	t.Cleanup(release) // a failed assertion must not leave the fake host blocked
	h.mu.Lock()
	h.launchGate = gate
	h.mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "launching", func(s Snapshot) bool { return s.Launch.Phase == "launching" })
	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	app.mu.Lock()
	rawStatus := app.status
	app.mu.Unlock()
	if !snap.Room.Open || snap.Launch.Phase != "launching" || rawStatus != "launch in progress" {
		t.Fatalf("back during launch: open=%v phase=%q status=%q", snap.Room.Open, snap.Launch.Phase, rawStatus)
	}
	release()
	waitFor(t, app, "second launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark", func(s Snapshot) bool { return !s.GPUParked })

	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.Room.Open || !snap.RoomPicker.Open {
		t.Fatalf("expected picker after leaving root room: %+v %+v", snap.Room.Open, snap.RoomPicker.Open)
	}
	app.HandleCommand(CmdHome, now)
	if app.Snapshot().RoomPicker.Open {
		t.Fatal("home should toggle the picker closed")
	}
}

func luaGlobalNumber(app *App, name string) float64 {
	n, _ := app.room.GlobalNumber(name)
	return n
}

func TestRoomOpenLibraryAndBrokenRoom(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "broken", "function draw() error('boom') end"),
	})
	app := newRoomApp(t, h, index, false)
	now := time.Now()
	waitFor(t, app, "library", func(s Snapshot) bool { return len(s.Games) == 2 })
	if app.Snapshot().RoomPicker.Open {
		t.Fatal("home=library must not open the picker")
	}

	app.HandleCommand(CmdHome, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "arcade", func(s Snapshot) bool { return s.Room.Open && len(s.Room.Frame.Hits) >= 2 })
	app.HandleCommand(CmdTab, now)
	snap := waitFor(t, app, "library from room", func(s Snapshot) bool {
		return !s.Room.Open && s.PlatformID == "megadrive" && len(s.Games) == 1
	})
	if snap.Grid.Mode != LayoutShelf {
		t.Fatalf("layout %v", snap.Grid.Mode)
	}

	app.HandleCommand(CmdHome, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "broken room error", func(s Snapshot) bool { return s.Room.Open && s.Room.Err != "" })
	if !strings.Contains(snap.Room.Err, "boom") {
		t.Fatalf("err %q", snap.Room.Err)
	}
	rec := gfx.NewRecorder()
	textures := map[string]gpuTexture{}
	labels := map[string]gpuTexture{}
	presentFrame(rec, snap, textures, labels, false)
	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.Room.Open || !snap.RoomPicker.Open {
		t.Fatalf("back from a failed room should return to the picker: %+v", snap.RoomPicker)
	}
}

func TestDrawRoomReplaysOpsAndReapsTextures(t *testing.T) {
	rec := gfx.NewRecorder()
	textures := map[string]gpuTexture{}
	labels := map[string]gpuTexture{}
	img := testDrawRGBA(4, 4)
	grid := testDrawGrid()
	snap := Snapshot{
		Grid: grid,
		Room: RoomSnapshot{
			Open: true, ID: "r", Title: "R",
			OffsetX: grid.contentLeft(), OffsetY: grid.contentTop(),
			Width: grid.contentWidth(), Height: grid.contentHeight(),
			Images: map[string]*image.RGBA{"cover:x": img},
			Frame: rooms.Frame{
				HasClear: true, Clear: gfx.RGB(1, 2, 3),
				Ops: []rooms.Op{
					{Kind: rooms.OpRect, X: 10, Y: 10, W: 100, H: 50, Color: gfx.RGB(9, 9, 9)},
					{Kind: rooms.OpClipPush, X: 0, Y: 0, W: 50, H: 50},
					{Kind: rooms.OpImage, Image: "cover:x", X: 0, Y: 0, W: 100, H: 100},
					{Kind: rooms.OpRect, X: 200, Y: 200, W: 10, H: 10, Color: gfx.RGB(1, 1, 1)}, // fully clipped
					{Kind: rooms.OpClipPop},
					{Kind: rooms.OpText, Text: "hello", X: 10, Y: 80, Size: 18, Color: gfx.RGB(255, 255, 255)},
					{Kind: rooms.OpImage, Image: "missing", X: 0, Y: 0, W: 10, H: 10},
				},
			},
		},
	}
	presentFrame(rec, snap, textures, labels, false)
	if _, ok := textures[roomTexturePrefix+"cover:x"]; !ok {
		t.Fatal("room cover texture not uploaded")
	}
	fills := 0
	draws := 0
	var clipped *gfx.Rect
	for _, call := range rec.Calls {
		switch call.Op {
		case "FillRect":
			fills++
		case "Draw":
			draws++
			if call.Src != nil {
				clipped = call.Src
			}
		}
	}
	if fills != 1 {
		t.Fatalf("expected one visible rect fill, got %d", fills)
	}
	if draws != 2 { // clipped cover + text label
		t.Fatalf("expected cover and text draws, got %d", draws)
	}
	if clipped == nil || clipped.W != 2 || clipped.H != 2 {
		t.Fatalf("clipped cover should crop the source proportionally, got %+v", clipped)
	}
	snap.Room = RoomSnapshot{}
	presentFrame(rec, snap, textures, labels, false)
	if _, ok := textures[roomTexturePrefix+"cover:x"]; ok {
		t.Fatal("room textures must be destroyed when the room closes")
	}
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
