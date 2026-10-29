package tenfoot

import (
	"context"
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

func TestRoomLaunchSelectsRequestedPackageBeforeOrdinaryLaunch(t *testing.T) {
	gameID, oldPackage, nextPackage := "zx81-workshop", strings.Repeat("a", 64), strings.Repeat("b", 64)
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries/" + gameID:
			if r.Method != http.MethodPut {
				t.Errorf("package selection method %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["expected_package_id"] != oldPackage || body["package_id"] != nextPackage {
				t.Errorf("package selection body %+v err=%v", body, err)
				http.Error(w, "wrong package selection", http.StatusBadRequest)
				return
			}
			calls = append(calls, "select")
			_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: gameID, CoreID: "fes.zx81", PackageID: nextPackage})
		case "/api/v1/session/launch":
			calls = append(calls, "launch")
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active", "game_id": gameID})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{client: NewClient(server.URL, server.Client()), launch: LaunchSnapshot{GameID: gameID, Phase: "launching"}}
	app.doLaunch(context.Background(), hostclient.Game{ID: gameID, Title: "Workshop ZX81"}, ClientStamp{}, nextPackage, oldPackage)
	if len(calls) != 2 || calls[0] != "select" || calls[1] != "launch" {
		t.Fatalf("launch order %v", calls)
	}
	if app.launch.Phase != "ok" {
		t.Fatalf("launch snapshot %+v", app.launch)
	}
}

func TestWorkshopLaunchRetryRepeatsFailedPackageSelection(t *testing.T) {
	gameID, oldPackage, nextPackage := "zx81-workshop", strings.Repeat("a", 64), strings.Repeat("b", 64)
	var mu sync.Mutex
	selections, launches := 0, 0
	currentPackage := oldPackage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries/" + gameID:
			if r.Method == http.MethodGet {
				mu.Lock()
				current := currentPackage
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: gameID, CoreID: "fes.zx81", PackageID: current})
				return
			}
			mu.Lock()
			selections++
			attempt := selections
			mu.Unlock()
			var body map[string]string
			if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&body) != nil || body["expected_package_id"] != oldPackage || body["package_id"] != nextPackage {
				t.Errorf("package selection request %s body %+v", r.Method, body)
				http.Error(w, "wrong package selection", http.StatusBadRequest)
				return
			}
			if attempt == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"code":"TEMPORARY","message":"package selection unavailable"}}`)
				return
			}
			mu.Lock()
			currentPackage = nextPackage
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: gameID, CoreID: "fes.zx81", PackageID: nextPackage})
		case "/api/v1/session/launch":
			mu.Lock()
			launches++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active", "game_id": gameID})
		case "/api/v1/debug/ui-events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	game := availableGame(gameID, "Workshop ZX81", "zx81")
	app := &App{client: NewClient(server.URL, server.Client()), room: &rooms.Instance{}, games: []hostclient.Game{game}}
	app.mu.Lock()
	app.startLaunchGameWithPackageLocked(game, nextPackage, oldPackage)
	app.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		phase := app.launch.Phase
		app.mu.Unlock()
		if phase == "error" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	if app.launch.Phase != "error" || !strings.Contains(app.launch.Message, "package selection failed") {
		app.mu.Unlock()
		t.Fatalf("initial package selection = %+v", app.launch)
	}
	app.handleLaunchOverlayLocked(CmdSelect) // Enter on "Select retry".
	app.mu.Unlock()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		phase := app.launch.Phase
		app.mu.Unlock()
		if phase == "ok" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	phase := app.launch.Phase
	app.mu.Unlock()
	mu.Lock()
	selectionCount, launchCount := selections, launches
	mu.Unlock()
	if phase != "ok" || selectionCount != 2 || launchCount != 1 {
		t.Fatalf("retry phase=%q selections=%d launches=%d", phase, selectionCount, launchCount)
	}
}

func TestWorkshopLaunchRetryRecoversCommittedSelectionWithLostReply(t *testing.T) {
	gameID, oldPackage, nextPackage := "zx81-workshop", strings.Repeat("a", 64), strings.Repeat("b", 64)
	var mu sync.Mutex
	selections, entryReads, launches := 0, 0, 0
	currentPackage := oldPackage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries/" + gameID:
			if r.Method == http.MethodGet {
				mu.Lock()
				entryReads++
				read := entryReads
				current := currentPackage
				mu.Unlock()
				if read == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"code":"TEMPORARY","message":"selection confirmation unavailable"}}`)
					return
				}
				_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: gameID, CoreID: "fes.zx81", PackageID: current})
				return
			}
			mu.Lock()
			selections++
			attempt := selections
			if attempt == 1 {
				currentPackage = nextPackage // The PUT committed, but its response is lost.
			}
			current := currentPackage
			mu.Unlock()
			var body map[string]string
			if r.Method != http.MethodPut || json.NewDecoder(r.Body).Decode(&body) != nil || body["expected_package_id"] != oldPackage || body["package_id"] != nextPackage {
				t.Errorf("package selection request %s body %+v", r.Method, body)
				http.Error(w, "wrong package selection", http.StatusBadRequest)
				return
			}
			if attempt > 1 || current == nextPackage {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":{"code":"STALE_REVISION","message":"core entry selection changed"}}`)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"TEMPORARY","message":"package selection response lost"}}`)
		case "/api/v1/session/launch":
			mu.Lock()
			launches++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active", "game_id": gameID})
		case "/api/v1/debug/ui-events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	game := availableGame(gameID, "Workshop ZX81", "zx81")
	app := &App{client: NewClient(server.URL, server.Client()), room: &rooms.Instance{}, games: []hostclient.Game{game}}
	app.mu.Lock()
	app.startLaunchGameWithPackageLocked(game, nextPackage, oldPackage)
	app.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		phase := app.launch.Phase
		app.mu.Unlock()
		if phase == "error" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	if app.launch.Phase != "error" || app.launchPackageID != nextPackage || app.launchExpectedPackageID != oldPackage {
		app.mu.Unlock()
		t.Fatalf("lost selection did not remain retryable: launch=%+v package=%q expected=%q", app.launch, app.launchPackageID, app.launchExpectedPackageID)
	}
	app.handleLaunchOverlayLocked(CmdSelect)
	app.mu.Unlock()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		phase := app.launch.Phase
		app.mu.Unlock()
		if phase == "ok" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.mu.Lock()
	phase := app.launch.Phase
	app.mu.Unlock()
	mu.Lock()
	selectionCount, readCount, launchCount := selections, entryReads, launches
	mu.Unlock()
	if phase != "ok" || selectionCount != 2 || readCount != 2 || launchCount != 1 {
		t.Fatalf("retry phase=%q selections=%d entry reads=%d launches=%d", phase, selectionCount, readCount, launchCount)
	}
}

func TestWorkshopLaunchDoesNotOverwriteAChangedPackageSelection(t *testing.T) {
	gameID, oldPackage, nextPackage, otherPackage := "zx81-workshop", strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	selections, launches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries/" + gameID:
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: gameID, CoreID: "fes.zx81", PackageID: otherPackage})
				return
			}
			selections++
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"STALE_REVISION","message":"core entry selection changed"}}`)
		case "/api/v1/session/launch":
			launches++
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active", "game_id": gameID})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := &App{
		client:          NewClient(server.URL, server.Client()),
		launch:          LaunchSnapshot{GameID: gameID, Phase: "launching"},
		launchPackageID: nextPackage, launchExpectedPackageID: oldPackage,
	}
	app.doLaunch(context.Background(), availableGame(gameID, "Workshop ZX81", "zx81"), ClientStamp{}, nextPackage, oldPackage)
	if app.launch.Phase != "error" || !strings.Contains(app.launch.Message, "saved package selection changed") {
		t.Fatalf("changed package was not reported as a conflict: %+v", app.launch)
	}
	if app.launchPackageID != nextPackage || app.launchExpectedPackageID != oldPackage || selections != 1 || launches != 0 {
		t.Fatalf("conflicting selection state: package=%q expected=%q PUTs=%d launches=%d", app.launchPackageID, app.launchExpectedPackageID, selections, launches)
	}
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
	stopStatus   int
	stopBody     string
	stops        int
	games        []hostclient.Game
	platforms    []hostclient.Platform
	recents      []hostclient.Game
	recentsGate  chan struct{}
	prefs        map[string]hostclient.EditionPreference
	uiPosts      []UIEvent
	pairedLease  bool
	// pairedLeaseBody is the paired kit-lease reply; empty means free.
	pairedLeaseBody string
	// inputs counts pad posts that must stay on the kit socket.
	inputs int
}

func newRoomHost(t *testing.T) *roomHost {
	t.Helper()
	h := &roomHost{state: "idle", prefs: map[string]hostclient.EditionPreference{}}
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/launcher/kit-lease" && h.pairedLease:
			body := h.pairedLeaseBody
			if body == "" {
				body = `{"state":"free"}`
			}
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			if r.URL.Query().Get("collection") == "recents" {
				h.mu.Lock()
				gate := h.recentsGate
				recents := append([]hostclient.Game(nil), h.recents...)
				h.mu.Unlock()
				if gate != nil {
					<-gate
				}
				if recents == nil {
					recents = []hostclient.Game{}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"games": recents})
				return
			}
			h.mu.Lock()
			games := append([]hostclient.Game(nil), h.games...)
			h.mu.Unlock()
			if games == nil {
				games = []hostclient.Game{
					availableGame("snes-mario", "Mario", "snes"),
					availableGame("snes-zelda", "Zelda", "snes"),
				}
				if r.URL.Query().Get("platform") == "megadrive" {
					games = []hostclient.Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			h.mu.Lock()
			platforms := append([]hostclient.Platform(nil), h.platforms...)
			h.mu.Unlock()
			if platforms == nil {
				platforms = []hostclient.Platform{{ID: "snes", Label: "SNES"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": platforms})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []hostclient.Collection{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/edition-preferences":
			h.mu.Lock()
			listed := make([]hostclient.EditionPreference, 0, len(h.prefs))
			for _, pref := range h.prefs {
				listed = append(listed, pref)
			}
			h.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"preferences": listed})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/edition-preferences":
			var body hostclient.EditionPreference
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.TrimSpace(body.Query) == "" || strings.TrimSpace(body.GameID) == "" {
				http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"edition preference request is invalid"}}`, http.StatusBadRequest)
				return
			}
			h.mu.Lock()
			if h.prefs == nil {
				h.prefs = map[string]hostclient.EditionPreference{}
			}
			key := rooms.DestinationPreferenceKey(rooms.Destination{Query: body.Query, Platform: body.Platform})
			body.Query = strings.TrimSpace(body.Query)
			body.ChosenAt = time.Now().UnixNano()
			h.prefs[key] = body
			h.mu.Unlock()
			_ = json.NewEncoder(w).Encode(body)
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
			h.stops++
			status := h.stopStatus
			body := h.stopBody
			if status == 0 {
				h.state = "idle"
			}
			h.mu.Unlock()
			if status != 0 {
				if status == http.StatusOK {
					status = http.StatusInternalServerError
				}
				w.WriteHeader(status)
				if body == "" {
					body = `{"error":{"code":"SAVE_FAILED","message":"cartridge save could not be written"}}`
				}
				_, _ = io.WriteString(w, body)
				return
			}
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			h.mu.Lock()
			state := h.state
			h.mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"`+state+`"}`)
		case r.Method == http.MethodPost && (r.URL.Path == "/api/v1/launcher/input" || r.URL.Path == "/api/v1/session/input/event"):
			h.mu.Lock()
			h.inputs++
			h.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/debug/ui-events":
			var ev UIEvent
			_ = json.NewDecoder(r.Body).Decode(&ev)
			h.mu.Lock()
			h.uiPosts = append(h.uiPosts, ev)
			h.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *roomHost) uiPostsSnapshot() []UIEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]UIEvent(nil), h.uiPosts...)
}

func countUIPosts(posts []UIEvent, kind string) int {
	n := 0
	for _, ev := range posts {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func waitForUIPost(t *testing.T, h *roomHost, kind string) UIEvent {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range h.uiPostsSnapshot() {
			if ev.Kind == kind {
				return ev
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; posts %#v", kind, h.uiPostsSnapshot())
	return UIEvent{}
}

func (h *roomHost) launchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.launches)
}

func (h *roomHost) inputCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inputs
}

func (h *roomHost) preferenceCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.prefs)
}

func waitForPrefs(t *testing.T, h *roomHost, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.preferenceCount() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d edition preferences, have %d", n, h.preferenceCount())
}

func newRoomApp(t *testing.T, h *roomHost, index *rooms.Index, homeRooms bool, paired ...bool) *App {
	t.Helper()
	client := NewClient(h.server.URL, h.server.Client())
	if len(paired) > 0 && paired[0] {
		client.paired = true
	}
	app := NewApp(client, 1280, 720, 50)
	app.SetPrefsPath(t.TempDir() + "/tenfoot.json")
	app.SetRooms(index, "")
	app.SetHomeRooms(homeRooms)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func TestBuiltInListRoomsKeepStatusBelowListRows(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.games = make([]hostclient.Game, 24)
	for i := range h.games {
		game := availableGame(fmt.Sprintf("zx81-%02d", i), fmt.Sprintf("Computer %02d", i), "fpga")
		game.CoreID = "fes.zx81"
		h.games[i] = game
	}
	h.platforms = []hostclient.Platform{{ID: "fpga", Label: "FES", Tags: []string{"vdp:tms9918-family"}}}
	h.mu.Unlock()

	app := newRoomApp(t, h, rooms.NewIndex(rooms.Examples()), false)
	for _, id := range []string{"example.computers", "example.coleco-arcade"} {
		app.mu.Lock()
		app.openRoomLocked(id)
		app.mu.Unlock()
		snap := waitFor(t, app, id, func(s Snapshot) bool {
			if s.Room.ID != id {
				return false
			}
			if id == "example.coleco-arcade" {
				return roomFrameHasClip(s.Room.Frame.Ops)
			}
			return roomFrameHasTextInsideClip(s.Room.Frame.Ops)
		})
		statusY, _ := roomChromeFooterRows(snap.Grid)
		clips := 0
		for _, op := range snap.Room.Frame.Ops {
			if op.Kind != rooms.OpClipPush {
				continue
			}
			clips++
			bottom := snap.Room.OffsetY + int(op.Y+op.H)
			if bottom > statusY {
				t.Fatalf("%s list clip ends at %d, through status row at %d", id, bottom, statusY)
			}
		}
		if clips == 0 {
			t.Fatalf("%s drew no list viewport", id)
		}
	}
}

func roomFrameHasTextInsideClip(ops []rooms.Op) bool {
	depth := 0
	for _, op := range ops {
		switch op.Kind {
		case rooms.OpClipPush:
			depth++
		case rooms.OpClipPop:
			if depth > 0 {
				depth--
			}
		case rooms.OpText:
			if depth > 0 {
				return true
			}
		}
	}
	return false
}

func roomFrameHasClip(ops []rooms.Op) bool {
	for _, op := range ops {
		if op.Kind == rooms.OpClipPush {
			return true
		}
	}
	return false
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
	t.Fatalf("timed out waiting for %s: room=%+v picker=%+v fw=%+v launch=%+v loading=%v status=%q", what, snap.Room.Open, snap.RoomPicker.Open, snap.FirmwarePicker.Open, snap.Launch, snap.Loading, snap.Status)
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
	if !homeHasRow(snap, HomeKindRecents, "recents") || !homeHasRow(snap, HomeKindRoom, "arcade") || !homeHasRow(snap, HomeKindLibrary, "") {
		t.Fatalf("home rows %+v", snap.RoomPicker.Rows)
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

func homeHasRow(snap Snapshot, kind, id string) bool {
	for _, row := range snap.RoomPicker.Rows {
		if row.Kind == kind && row.ID == id {
			return true
		}
	}
	return false
}

func focusHomeRow(t *testing.T, app *App, kind, id string) RoomPickerRow {
	t.Helper()
	app.mu.Lock()
	defer app.mu.Unlock()
	rows := app.roomPickerRowsLocked()
	for i, row := range rows {
		if row.Kind == kind && row.ID == id {
			app.roomPickerIndex = i
			return row
		}
	}
	t.Fatalf("home row kind=%s id=%q missing from %+v", kind, id, rows)
	return RoomPickerRow{}
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
	fills, draws, texts := 0, 0, 0
	authoredText := labels[roomTextKey(snap.Room.Frame.Ops[5])]
	var hintText gpuTexture
	for _, label := range labels {
		if label.labelSource == snap.HeaderHint() {
			hintText = label
		}
	}
	hintPainted, hintBand := false, false
	var clipped *gfx.Rect
	for _, call := range rec.Calls {
		switch call.Op {
		case "FillRect":
			if call.Color == gfx.RGB(9, 9, 9) {
				fills++
			}
			hintBand = hintBand || (call.Color == heroText && call.Dst.H == 28)
		case "Draw":
			draws++
			if call.Src != nil {
				clipped = call.Src
			}
		case "DrawMask":
			if call.Mask == hintText.mask {
				hintPainted = call.Color == heroOnAccent && call.Y >= grid.footerY()-30 && call.Y+hintText.h < grid.footerY()
				continue
			}
			texts++
			if call.Mask != authoredText.mask || call.Color != gfx.RGB(255, 255, 255) || call.X != 10 || call.Y < 80 {
				t.Fatalf("text drawn %+v", call)
			}
		}
	}
	if fills != 1 {
		t.Fatalf("expected one visible rect fill, got %d", fills)
	}
	if draws != 1 || texts != 1 { // clipped cover; text label
		t.Fatalf("expected a cover draw and a text draw, got %d and %d", draws, texts)
	}
	if !hintBand || !hintPainted || hintText.mask == nil {
		t.Fatal("room controls need cached proportional text on a separate opaque band")
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
