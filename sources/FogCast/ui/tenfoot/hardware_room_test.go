package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const liveHardwareTestScript = `
focus = 1
local live = {id="host-zx81",game_id="fpga-zx81",target="dev",package_id=string.rep("a",64),generation="9"}
function load() end
resume_count = 0
function on_resume() resume_count = resume_count + 1 rooms.back() end
function on_input(cmd)
  if cmd == "right" then focus = focus + 1 return true end
  if cmd == "select" then session.launch("fpga-zx81") return true end
  if cmd == "filter-next" or cmd == "filter_next" then session.open_tape(live) return true end
  if cmd == "sort" then session.stop(live) return true end
  return false
end
function on_activate(id)
  if id == "tape" then session.open_tape(live) end
  if id == "home" then rooms.home() end
end
function draw()
  gfx.clear("#122738")
  gfx.text("Hardware room", 20, 20, {size=24})
  gfx.rect(20, 80, 200, 60, "#b8cad4")
  gfx.hit("tape", 20, 80, 200, 60)
  gfx.hit("home", 240, 80, 120, 60)
end
`

func newLiveHardwareApp(t *testing.T, h *tapeHost) *App {
	t.Helper()
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, liveHardwareTestScript)}), "")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.mu.Lock()
	app.openRoomLocked(hardwareRoomID)
	app.mu.Unlock()
	app.HandleCommand(CmdRight, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active hardware", func(s Snapshot) bool { return s.GPUParked && s.Session.LoadTape && s.Launch.Phase == "ok" })
	return app
}

type colecoCoordinatorRuntime struct {
	activation misterruntime.CoreActivation
}

func (*colecoCoordinatorRuntime) Health(string) protocol.Health { return protocol.Health{Ready: true} }
func (*colecoCoordinatorRuntime) Reconcile(context.Context) protocol.Status {
	return protocol.Status{State: protocol.StateIdle}
}
func (*colecoCoordinatorRuntime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, nil
}
func (*colecoCoordinatorRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", nil
}
func (*colecoCoordinatorRuntime) Stop(context.Context) (string, *protocol.APIError) { return "", nil }
func (r *colecoCoordinatorRuntime) LoadCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	return r.activation, true, nil
}

type coordinatorSessionAPI struct {
	coordinator *agent.Coordinator
}

func (*coordinatorSessionAPI) Games(context.Context) ([]catalog.Game, error) { return nil, nil }
func (*coordinatorSessionAPI) Search(context.Context, string) ([]catalog.Game, error) {
	return nil, nil
}
func (*coordinatorSessionAPI) Game(context.Context, string) (catalog.Game, error) {
	return catalog.Game{}, nil
}
func (*coordinatorSessionAPI) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{}, nil
}
func (s *coordinatorSessionAPI) Status(context.Context) (protocol.Status, error) {
	status := s.coordinator.Status()
	gameID, system := "fpga-coleco-dk", catalog.CorePlatform
	status.GameID, status.System = &gameID, &system
	return status, nil
}
func (*coordinatorSessionAPI) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{}, nil
}
func (*coordinatorSessionAPI) LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error) {
	return protocol.Status{}, nil
}
func (*coordinatorSessionAPI) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (*coordinatorSessionAPI) MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip) {
	return nil, nil
}

func assertLiveHardwareIdentity(t *testing.T, app *App, h *tapeHost) {
	t.Helper()
	app.mu.Lock()
	session := app.session
	app.mu.Unlock()
	h.mu.Lock()
	launches := h.launches
	h.mu.Unlock()
	if session.ID != h.sessionID || session.State != "active" || session.CorePackage == nil || session.CorePackage.Generation != 9 || launches != 1 {
		t.Fatalf("live media changed session: %+v launches=%d", session, launches)
	}
}

func TestColecoHomeAndF12OpenColecoSelectorWithoutLiveMedia(t *testing.T) {
	coordinator := agent.New(&colecoCoordinatorRuntime{activation: misterruntime.CoreActivation{
		PackageID: "506110a2554a80e0952d50ab3d40d2ea27e41ba014d4a91703e7de31bcddfa9c",
		Descriptor: corepackage.Descriptor{
			Core:  corepackage.Core{ID: colecoCoreID},
			ABI:   corepackage.Contract{ID: "fes.application", Major: 1},
			Build: corepackage.Build{ID: "0123456789abcdef0123456789abcdef"},
		},
		Generation: 7,
		ActiveInterfaces: []misterruntime.Protocol2Interface{
			{ID: "fes.audio.pcm-s16-stereo-48k", Major: 1},
			{ID: "fes.video.session-display", Major: 1},
		},
	}}, 0, 0)
	status, apiErr := coordinator.LoadCore(context.Background(), 1, strings.NewReader("p"))
	if apiErr != nil || status.CorePackage == nil || status.CorePackage.CoreID != colecoCoreID {
		t.Fatalf("coordinator status=%+v err=%v", status, apiErr)
	}

	hostServer := httptest.NewServer(hostapi.New(&coordinatorSessionAPI{coordinator: coordinator}))
	t.Cleanup(hostServer.Close)
	installedSession, err := hostclient.NewClient(hostServer.URL, hostServer.Client()).Session(context.Background())
	if err != nil {
		t.Fatalf("read host session: %v", err)
	}
	if installedSession.System != string(catalog.CorePlatform) || installedSession.CorePackage == nil ||
		installedSession.CorePackage.CoreID != colecoCoreID ||
		sessionHasCoreInterface(installedSession, "fes.expansion.coleco-bus", 2) {
		t.Fatalf("host session lost the coordinator's Coleco identity: %+v", installedSession)
	}
	for _, key := range []string{"home", "f12"} {
		t.Run(key, func(t *testing.T) {
			app := NewApp(nil, 1280, 720, 20)
			t.Cleanup(app.Stop)
			app.SetRooms(rooms.NewIndex([]rooms.Pack{
				testRoomPack(t, hardwareRoomID, `
function load() end
function draw() error("ZX81 workbench must not draw over Coleco") end
`),
				testRoomPack(t, colecoArcadeRoomID, `
function load() end
function draw() gfx.clear("#101820") gfx.text("Coleco title selector", 20, 20, {size=24}) end
`),
			}), "")
			app.mu.Lock()
			app.applySessionLocked(installedSession)
			coleco := app.colecoSessionLocked()
			app.mu.Unlock()
			if !coleco {
				t.Fatal("Tenfoot did not recognize the coordinator-built Coleco host session")
			}

			if hint := app.Snapshot().HeaderHint(); !strings.Contains(hint, "coleco selector") {
				t.Fatalf("Coleco active-session hint = %q", hint)
			}
			now := time.Now()
			if key == "home" {
				if !app.HandlePlayHIDKey("home", true, now) {
					t.Fatal("Coleco Home key was not consumed")
				}
			} else if !app.HandlePlayHIDScancode("F12", hidKeyboardF12, true, now) {
				t.Fatal("Coleco F12 key was not consumed")
			}
			snap := app.Snapshot()
			if snap.Room.ID != colecoArcadeRoomID || !snap.Room.DuringPlay || snap.GPUParked {
				t.Fatalf("Coleco %s did not open its selector over the active session: room=%+v parked=%v", key, snap.Room, snap.GPUParked)
			}
			if snap.Session.State != "active" || snap.Status != playingColecoSelectorStatus {
				t.Fatalf("Coleco session changed or selector status missing: session=%+v status=%q", snap.Session, snap.Status)
			}
			app.mu.Lock()
			app.startLaunchGameLocked(hostclient.Game{ID: "next-coleco-title", Title: "Next title"})
			launchPhase := app.launch.Phase
			app.mu.Unlock()
			if launchPhase == "launching" || app.Snapshot().Status != colecoCartridgeSessionStatus {
				t.Fatalf("selector launched a second cartridge before Stop: phase=%q status=%q", launchPhase, app.Snapshot().Status)
			}
		})
	}
}

func TestColecoHomeReportsUnavailableSelector(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{
		System: string(protocol.SystemColecoVision),
		State:  "active",
	}
	app.mu.Unlock()

	if !app.HandlePlayHIDKey("home", true, time.Now()) {
		t.Fatal("Coleco Home key was not consumed when its selector was unavailable")
	}
	if got := app.Snapshot().Status; got != colecoSelectorUnavailableStatus {
		t.Fatalf("unavailable selector status = %q, want %q", got, colecoSelectorUnavailableStatus)
	}
}

func TestColecoF12ReportsUnavailableSelector(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{
		System: string(protocol.SystemColecoVision),
		State:  "active",
	}
	app.mu.Unlock()

	if !app.HandlePlayHIDScancode("F12", hidKeyboardF12, true, time.Now()) {
		t.Fatal("Coleco F12 key was not consumed when its selector was unavailable")
	}
	if got := app.Snapshot().Status; got != colecoSelectorUnavailableStatus {
		t.Fatalf("unavailable selector status = %q, want %q", got, colecoSelectorUnavailableStatus)
	}
}

func TestColecoSelectorShowsStopGuidanceAndCannotLaunchWhileRunning(t *testing.T) {
	var launchCalls atomic.Int32
	game := hostclient.Game{
		ID:         "coleco-donkey-kong",
		Title:      "Donkey Kong",
		System:     "coleco",
		State:      "available",
		RootOnline: true,
		Launchable: true,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/games":
			games := []hostclient.Game{}
			if r.URL.Query().Get("q") == "Donkey Kong" {
				games = append(games, game)
			}
			_ = json.NewEncoder(w).Encode(hostclient.GamePage{Games: games})
		case "/api/v1/session/launch":
			launchCalls.Add(1)
			http.Error(w, "a running Coleco session must not launch another cartridge", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex(rooms.Examples()), "") // Uses the production embedded coleco-arcade/main.lua.
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{
		ID:     "coleco-session",
		System: string(protocol.SystemColecoVision),
		State:  "active",
		CorePackage: &hostclient.SessionCorePackage{
			PackageID:        strings.Repeat("a", 64),
			Generation:       7,
			ABI:              hostclient.SessionCoreABI{ID: "fes.application", Major: 1},
			ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.expansion.coleco-bus", Major: 2}},
		},
	}
	app.openRoomLocked(colecoArcadeRoomID)
	app.roomDuringPlay = true
	app.status = playingColecoSelectorStatus
	app.mu.Unlock()

	snap := waitFor(t, app, "production Coleco title", func(s Snapshot) bool {
		return s.Room.ID == colecoArcadeRoomID && s.Room.Destination.Label == "Donkey Kong" && s.Room.Destination.Availability == rooms.AvailReady
	})
	if !roomTextHas(snap, "COLECOVISION") || !roomTextHas(snap, "Donkey Kong") {
		t.Fatalf("production Coleco room did not draw its selected cabinet: ops=%+v", snap.Room.Frame.Ops)
	}
	if snap.Room.Destination.Status != colecoRunningDestinationStatus || snap.Room.Destination.Action != colecoRunningDestinationAction {
		t.Fatalf("active-session destination = status %q action %q", snap.Room.Destination.Status, snap.Room.Destination.Action)
	}
	if !strings.Contains(snap.HeaderHint(), "Stop") {
		t.Fatalf("active-session selector hint omits Stop guidance: %q", snap.HeaderHint())
	}
	if !roomTextHas(snap, "Stop before opening the library") || roomTextHas(snap, "Tab opens the Coleco shelf") {
		t.Fatalf("active-session room still advertises Tab as opening the library: ops=%+v", snap.Room.Frame.Ops)
	}
	if got := renderRoomLabelSource(t, snap, "rd-status"); got != colecoRunningDestinationStatus {
		t.Fatalf("rendered destination status = %q, want %q", got, colecoRunningDestinationStatus)
	}
	if got := renderRoomLabelSource(t, snap, "rd-action"); got != colecoRunningDestinationAction || strings.Contains(got, "Play") {
		t.Fatalf("rendered destination action = %q, want Stop guidance", got)
	}

	app.HandleCommand(CmdTab, time.Now())
	tabbed := app.Snapshot()
	if !tabbed.Room.DuringPlay || tabbed.Room.ID != colecoArcadeRoomID || tabbed.Status != runningMachineLibraryBlockedStatus {
		t.Fatalf("Tab did not visibly refuse library navigation during play: room=%+v status=%q", tabbed.Room, tabbed.Status)
	}
	if got := renderRoomLabelSource(t, tabbed, "room-status"); got != runningMachineLibraryBlockedStatus {
		t.Fatalf("rendered Tab refusal = %q, want %q", got, runningMachineLibraryBlockedStatus)
	}

	app.HandleCommand(CmdDetails, time.Now())
	details := app.Snapshot()
	if !details.Detail.Open || details.DetailPlay.Status != colecoRunningDestinationStatus {
		t.Fatalf("running Coleco Details copy = open %v destination %+v", details.Detail.Open, details.DetailPlay)
	}
	if copy := detailReadiness(details.DetailPlay); copy.State != heroStateUnavailable || copy.Headline != colecoRunningDestinationStatus {
		t.Fatalf("running Coleco Details readiness = %+v, want unavailable Stop guidance", copy)
	}
	app.HandleCommand(CmdBack, time.Now())

	app.HandleCommand(CmdSelect, time.Now())
	confirmed := app.Snapshot()
	if confirmed.Session.State != "active" || confirmed.Launch.Phase == "launching" || launchCalls.Load() != 0 {
		t.Fatalf("Confirm changed or relaunched the Coleco session: session=%+v launch=%+v requests=%d", confirmed.Session, confirmed.Launch, launchCalls.Load())
	}
	if confirmed.Status != colecoCartridgeSessionStatus {
		t.Fatalf("Confirm refusal status = %q, want %q", confirmed.Status, colecoCartridgeSessionStatus)
	}
	if got := renderRoomLabelSource(t, confirmed, "rd-status"); got != colecoRunningDestinationStatus {
		t.Fatalf("rendered status after Confirm = %q, want %q", got, colecoRunningDestinationStatus)
	}
	if got := renderRoomLabelSource(t, confirmed, "room-status"); got != colecoCartridgeSessionStatus {
		t.Fatalf("rendered Confirm refusal = %q, want %q", got, colecoCartridgeSessionStatus)
	}
}

func TestColecoBackendChoiceShowsStopGuidanceWhileRunning(t *testing.T) {
	pack := testRoomPack(t, colecoArcadeRoomID, `
local matches = {
  { id = "coleco-fpga", title = "Donkey Kong", system = "coleco", launchable = true, state = "available", execution = "fpga_native" },
  { id = "coleco-host", title = "Donkey Kong", system = "coleco", launchable = true, state = "available", execution = "host_only" },
}
function load()
  destination.set({ kind = "game", label = "Donkey Kong", query = "Donkey Kong", platform = "coleco", system = "coleco", matches = matches })
end
function draw() gfx.rect(0, 0, 10, 10, "#fff") end
`)
	app := NewApp(nil, 1280, 720, 20)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), "")
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{ID: "coleco-session", System: string(protocol.SystemColecoVision), State: "active"}
	app.openRoomLocked(colecoArcadeRoomID)
	app.roomDuringPlay = true
	dest := app.roomDestinationLocked()
	if dest.Choice != rooms.ChoiceBackend || dest.Action != colecoRunningDestinationAction {
		app.mu.Unlock()
		t.Fatalf("running Coleco backend choice copy = %+v", dest)
	}
	app.openRoomChoiceLocked(dest)
	app.mu.Unlock()

	snap := app.Snapshot()
	if snap.Room.Choice.Hint != colecoRunningDestinationAction {
		t.Fatalf("running Coleco backend choice hint = %q", snap.Room.Choice.Hint)
	}
	if got := renderRoomChoiceLabelSource(t, snap, "rchoice-title"); got != colecoRunningDestinationAction {
		t.Fatalf("running Coleco backend choice title = %q, want %q", got, colecoRunningDestinationAction)
	}
}

func TestPlayingRoomLabelUsesColecoSessionPredicate(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	app.mu.Lock()
	app.session = hostclient.SessionResult{
		System: "other",
		State:  "active",
		CorePackage: &hostclient.SessionCorePackage{
			ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.expansion.coleco-bus", Major: 2}},
		},
	}
	session := app.sessionSnapshotLocked()
	app.mu.Unlock()
	if !session.Coleco || playingRoomLabel(session) != "Coleco selector" {
		t.Fatalf("Coleco expansion session label = %+v / %q", session, playingRoomLabel(session))
	}
}

func renderRoomLabelSource(t *testing.T, snap Snapshot, slot string) string {
	t.Helper()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{}
	drawRoom(dev, snap, map[string]gpuTexture{}, labels, map[string]struct{}{})
	for key, label := range labels {
		if strings.HasPrefix(key, slot+"\x1f") {
			return label.labelSource
		}
	}
	return ""
}

func renderRoomChoiceLabelSource(t *testing.T, snap Snapshot, slot string) string {
	t.Helper()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{}
	drawRoomChoice(dev, snap, labels, map[string]struct{}{})
	for key, label := range labels {
		if strings.HasPrefix(key, slot+"\x1f") {
			return label.labelSource
		}
	}
	return ""
}

func TestHardwareRoomCanReturnDuringPlayWithoutRelaunch(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.mu.Lock()
	original := app.room
	beforeResumes := luaGlobalNumber(app, "resume_count")
	app.mu.Unlock()
	if !strings.Contains(app.Snapshot().HeaderHint(), "hardware room") {
		t.Fatalf("missing visible route: %q", app.Snapshot().HeaderHint())
	}
	app.HandleCommand(CmdHome, time.Now())
	app.Tick(time.Now())
	snap := app.Snapshot()
	if snap.GPUParked || !snap.Room.DuringPlay || snap.Room.ID != hardwareRoomID || snap.RoomPicker.Open {
		t.Fatalf("room did not resume: %+v", snap.Room)
	}
	app.mu.Lock()
	same := app.room == original
	focus := luaGlobalNumber(app, "focus")
	resumes := luaGlobalNumber(app, "resume_count")
	app.mu.Unlock()
	if !same || focus != 2 || resumes != beforeResumes+1 {
		t.Fatalf("resume lost room/focus: same=%v focus=%v", same, focus)
	}
	app.HandleCommand(CmdSelect, time.Now())
	assertLiveHardwareIdentity(t, app, h)
	app.HandleCommand(CmdBack, time.Now())
	if !app.Snapshot().GPUParked {
		t.Fatal("Back must return to the running session")
	}
	button, ok := hardwareRoomButton(app.Snapshot())
	if !ok || HitTest(app.Snapshot(), button.X+2, button.Y+2).Kind != PointerHardwareRoom {
		t.Fatal("hardware room pointer route is missing")
	}
	app.PointerClick(button.X+2, button.Y+2, time.Now())
	if !app.Snapshot().Room.DuringPlay {
		t.Fatal("pointer did not return to hardware room")
	}
	app.HandleCommand(CmdLayoutCycle, time.Now())
	if !app.Snapshot().GPUParked {
		t.Fatal("Select/View must return to session")
	}
	app.HandleCommand(CommandFromButton(ButtonBack), time.Now())
	if !app.Snapshot().Room.DuringPlay {
		t.Fatal("single controller Select/View must return to hardware room")
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomOpensFromLibraryDuringPlay(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.mu.Lock()
	app.closeAllRoomsLocked()
	app.mu.Unlock()
	app.HandleCommand(CmdHome, time.Now())
	if snap := app.Snapshot(); !snap.Room.DuringPlay || snap.Room.ID != hardwareRoomID || snap.GPUParked {
		t.Fatalf("library launch cannot open hardware room: %+v", snap.Room)
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomHomeKeepsActiveRoomForIdleOnlyChoices(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdHome, time.Now())
	app.mu.Lock()
	original := app.room
	app.homeRecents = []hostclient.Game{h.zx81()}
	app.mu.Unlock()
	for _, kind := range []string{HomeKindLibrary, HomeKindRecents, HomeKindRecent} {
		app.HandleCommand(CmdHome, time.Now())
		app.mu.Lock()
		rows := app.roomPickerRowsLocked()
		found := false
		for _, row := range rows {
			if row.Kind == kind {
				found = true
				if !row.Invalid {
					t.Errorf("%s must explain that Stop is needed", kind)
				}
				app.activateHomeRowLocked(row)
				break
			}
		}
		same := app.room == original
		app.mu.Unlock()
		if !found || !same || !app.Snapshot().Room.DuringPlay {
			t.Fatalf("%s discarded live room", kind)
		}
		app.HandleCommand(CmdBack, time.Now())
		if app.Snapshot().RoomPicker.Open || !app.Snapshot().Room.DuringPlay {
			t.Fatal("Home Back should return to room")
		}
		assertLiveHardwareIdentity(t, app, h)
	}
	app.mu.Lock()
	app.openLibraryFromRoomLocked(rooms.Action{Kind: rooms.ActionOpenLibrary})
	same := app.room == original
	app.mu.Unlock()
	if !same || !app.Snapshot().Room.DuringPlay {
		t.Fatal("script Library discarded live room")
	}
	app.HandleCommand(CmdBack, time.Now())
	assertLiveHardwareIdentity(t, app, h)
	if !app.Snapshot().GPUParked {
		t.Fatal("Back should resume the same session")
	}
}

func TestHardwareRoomTapeUsesExistingPickerAndKeepsFocus(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdHome, time.Now())
	app.Tick(time.Now())
	app.HandleCommand(CmdFilterNext, time.Now())
	snap := app.Snapshot()
	if !snap.TapePicker.Open || !snap.Room.DuringPlay {
		t.Fatalf("room did not open tape picker: %+v", snap.TapePicker)
	}
	panel, ok := tapePickerPanel(snap)
	if !ok {
		t.Fatal("missing tape picker")
	}
	x, y := panel.X+20, panel.rowY(0)+panel.RowH/2
	if hit := HitTest(snap, x, y); hit.Kind != PointerTapePicker {
		t.Fatalf("tape picker did not receive pointer: %+v", hit)
	}
	app.PointerClick(x, y, time.Now())
	waitFor(t, app, "ejected in room", func(s Snapshot) bool { return !s.TapePicker.Open && strings.Contains(s.Status, "Tape ejected") })
	app.mu.Lock()
	focus := luaGlobalNumber(app, "focus")
	app.mu.Unlock()
	if !app.Snapshot().Room.DuringPlay || focus != 2 || h.clearCount() != 1 {
		t.Fatalf("tape return lost room: focus=%v clear=%d room=%+v", focus, h.clearCount(), app.Snapshot().Room)
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomTapeFailuresKeepSessionAndNavigation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		busy        int
		unavailable bool
	}{
		{name: "busy", busy: 4},
		{name: "unavailable", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTapeHost(t)
			h.clearBusyLeft, h.clearUnavailable = tc.busy, tc.unavailable
			app := newLiveHardwareApp(t, h)
			app.HandleCommand(CmdHome, time.Now())
			app.HandleCommand(CmdFilterNext, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			snap := waitFor(t, app, "live tape rejection", func(s Snapshot) bool { return s.TapePicker.Open && !s.TapePicker.Busy && h.clearCount() == 3 })
			if !strings.Contains(strings.ToLower(snap.TapePicker.Status), tc.name) {
				t.Fatalf("missing failure: %q", snap.TapePicker.Status)
			}
			app.HandleCommand(CmdBack, time.Now())
			if snap := app.Snapshot(); snap.TapePicker.Open || !snap.Room.DuringPlay || snap.GPUParked {
				t.Fatalf("failure did not return to room: %+v", snap.Room)
			}
			assertLiveHardwareIdentity(t, app, h)
		})
	}
}

func TestParkedTapePickerIsPaintedAndPointerAccessible(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdSearch, time.Now())
	snap := app.Snapshot()
	panel, ok := tapePickerPanel(snap)
	if !snap.GPUParked || !ok || HitTest(snap, panel.X+10, panel.rowY(0)+10).Kind != PointerTapePicker {
		t.Fatal("parked tape picker is not pointer accessible")
	}
	rec := gfx.NewRecorder()
	labels := map[string]gpuTexture{}
	presentFrame(rec, snap, map[string]gpuTexture{}, labels, false)
	painted := false
	for key := range labels {
		painted = painted || strings.HasPrefix(key, "tape-title\x1f")
	}
	if !painted {
		t.Fatal("parked tape controls not painted")
	}
}

func TestHardwareRoomEntryReleasesHeldKeysBeforeMenu(t *testing.T) {
	var mu sync.Mutex
	var events []remoteinput.Event
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Event.Action == remoteinput.ActionPress {
			<-gate
		}
		mu.Lock()
		events = append(events, body.Event)
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, liveHardwareTestScript)}), "")
	app.mu.Lock()
	app.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	app.syncGPUParkLocked()
	app.mu.Unlock()
	if !app.HandlePlayHIDScancode("a", 0x04, true, time.Now()) {
		t.Fatal("core did not accept key")
	}
	if !app.HandlePlayHIDScancode("home", 0x4a, true, time.Now()) || !app.Snapshot().Room.DuringPlay {
		t.Fatal("Home did not enter live room")
	}
	if app.ForwardsPlayHID() || app.HandlePlayHIDScancode("d", 0x07, true, time.Now()) {
		t.Fatal("menu key forwarded to running machine")
	}
	release()
	waitFor(t, app, "held key release", func(Snapshot) bool { mu.Lock(); defer mu.Unlock(); return len(events) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if events[0].Action != remoteinput.ActionPress || events[1].Action != remoteinput.ActionRelease || events[0].Code != events[1].Code {
		t.Fatalf("held key was not released in order: %+v", events)
	}
	app.mu.Lock()
	app.closeAllRoomsLocked()
	app.mu.Unlock()
}

func TestHardwareRoomQueuedInputDoesNotReachReplacementSession(t *testing.T) {
	first := make(chan struct{}, 1)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	var mu sync.Mutex
	var events []remoteinput.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		events = append(events, body.Event)
		mu.Unlock()
		if body.Event.Code == remoteinput.KeyA {
			first <- struct{}{}
			select {
			case <-gate:
			case <-r.Context().Done():
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{ID: "same-host", State: "active", GameID: "first", Input: &hostclient.SessionInput{Ready: true}, CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 1}}
	app.mu.Unlock()
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyA, Action: remoteinput.ActionPress})
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first key not sent")
	}
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyLeft, Action: remoteinput.ActionPress})
	app.mu.Lock()
	app.releasePlayHIDLocked()
	app.applySessionLocked(hostclient.SessionResult{ID: "same-host", State: "active", GameID: "second", Input: &hostclient.SessionInput{Ready: true}, CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 2}})
	app.mu.Unlock()
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyRight, Action: remoteinput.ActionPress})
	waitFor(t, app, "replacement key", func(Snapshot) bool { mu.Lock(); defer mu.Unlock(); return len(events) >= 2 })
	release()
	app.mu.Lock()
	tail := app.playHIDTail
	app.mu.Unlock()
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("input queue did not drain")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0].Code != remoteinput.KeyA || events[1].Code != remoteinput.KeyRight {
		t.Fatalf("old queued input reached replacement session: %+v", events)
	}
}
