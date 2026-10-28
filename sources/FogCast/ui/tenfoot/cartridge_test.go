package tenfoot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/misteross/expansion"
)

// colecoKitSession is a running Coleco native shell: controller and keypad
// ports, the session plane and one linked 16 KiB cartridge.
func colecoKitSession() hostclient.SessionResult {
	s := hardwareBoundSession()
	s.GameID = "fpga-coleco"
	s.System = string(protocol.SystemColecoVision)
	s.Input = &hostclient.SessionInput{State: "attached", Ready: true}
	s.CorePackage.CoreID = colecoCoreID
	s.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.application", Major: 1}
	s.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{
		{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.keypad.ports", Major: 1},
		{ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}
	s.CorePackage.ROMLink = &hostclient.SessionROMLink{ROMID: "cartridge-rom", SourceSize: 16384}
	return s
}

// colecoBIOSKitSession is the format-4 Coleco shell: its BIOS and a 16 KiB
// cartridge are both linked, and only the cartridge is swapped.
func colecoBIOSKitSession() hostclient.SessionResult {
	s := colecoKitSession()
	s.CorePackage.ROMLink = nil
	s.CorePackage.ROMLinks = &hostclient.SessionROMLinks{Sources: []hostclient.SessionROMSource{
		{ID: "coleco-bios", Role: "firmware", SourceSize: 8192},
		{ID: "coleco-cart", Role: "cartridge", SourceSize: 16384}}}
	composition := expansion.PartsComposition{
		PackageID: s.CorePackage.PackageID, Layout: expansion.ColecoNativeVideoLayout,
		Parts:       []expansion.PartSelection{{Role: "video", PartID: strings.Repeat("b", 64)}},
		ShellSHA256: strings.Repeat("c", 64), PayloadSHA256: strings.Repeat("d", 64), PayloadSize: 40408,
	}
	var err error
	composition.ID, err = expansion.PartsCompositionID(composition.PackageID, composition.Layout, composition.Parts, composition.PayloadSHA256)
	if err != nil {
		panic(err)
	}
	s.CorePackage.PartsComposition = &composition
	return s
}

// Guide/Search over a running Coleco opens the cartridge picker while Home/F12
// retains the Coleco Arcade title room. Choosing a cartridge imports it and
// relaunches the title through the session-bound cartridge call.
func TestKitCartridgeMachineSwapsCartridgeOverPlay(t *testing.T) {
	for name, session := range map[string]func() hostclient.SessionResult{
		"cartridge": colecoKitSession, "bios-and-cartridge": colecoBIOSKitSession} {
		t.Run(name, func(t *testing.T) { testCartridgeSwapOverPlay(t, session) })
	}
}

func testCartridgeSwapOverPlay(t *testing.T, session func() hostclient.SessionResult) {
	prior := session()
	next := session()
	next.CorePackage.Generation = prior.CorePackage.Generation + 1
	dir := t.TempDir()
	cart := bytes.Repeat([]byte{0xc0}, 16384)
	if err := os.WriteFile(filepath.Join(dir, "game.col"), cart, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "short.col"), make([]byte, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var visible []bool
	var swap protocol.CartridgeRequest
	swaps := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, bound := protocol.DevelopmentMediaHeaders(r.Header)
		switch r.URL.Path {
		case "/api/v1/session/display":
			show := decodeSessionDisplay(t, r)
			mu.Lock()
			visible = append(visible, show)
			mu.Unlock()
			writeKitDisplaySession(w, prior)
		case "/api/v1/core-media":
			body, _ := io.ReadAll(r.Body)
			json.NewEncoder(w).Encode(map[string]any{"media_id": fmt.Sprintf("%x", sha256.Sum256(body)), "size": len(body)})
		case "/api/v1/session/cartridge":
			mu.Lock()
			err := json.NewDecoder(r.Body).Decode(&swap)
			swaps++
			mu.Unlock()
			if err != nil || !bound || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation ||
				r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
				t.Error("cartridge swap lost the captured machine")
			}
			writeKitDisplaySession(w, next)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, hardwareRoomID, liveHardwareTestScript),
		testRoomPack(t, colecoArcadeRoomID, `function load() end; function draw() gfx.clear("#101820") end`),
	}), "")
	app.mu.Lock()
	app.session = prior
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "coleco", Root: dir}}
	app.syncGPUParkLocked()
	app.mu.Unlock()
	app.HandleCommand(CmdSearch, time.Now())
	waitFor(t, app, "cartridge selector over play", func(s Snapshot) bool {
		return s.Session.ControlsOpen && !s.GPUParked && s.TapePicker.Open && s.Status == "Choose a cartridge for the running machine."
	})
	app.mu.Lock()
	var gotCompositionID, gotLayout string
	if app.session.CorePackage != nil && app.session.CorePackage.PartsComposition != nil {
		gotCompositionID = app.session.CorePackage.PartsComposition.ID
		gotLayout = app.session.CorePackage.PartsComposition.Layout
	}
	app.mu.Unlock()
	if expected := prior.CorePackage.PartsComposition; expected != nil &&
		(gotCompositionID != expected.ID || gotLayout != expansion.ColecoNativeVideoLayout) {
		t.Fatalf("format-4 video-part identity was lost from the display response: id=%q layout=%q", gotCompositionID, gotLayout)
	}
	for _, row := range app.Snapshot().TapePicker.Rows {
		if row.Kind == tapePickerKindEject {
			t.Fatal("a linked cartridge offered eject")
		}
	}
	selectTapeLibraryRoot(t, app, dir)
	for _, row := range app.Snapshot().TapePicker.Rows {
		if row.Kind == tapePickerKindFile && row.Selectable != (row.Name == "game.col") {
			t.Fatalf("wrong cartridge selection: %+v", row)
		}
	}
	selectTapeNamedRow(t, app, "game.col")
	selectCartridgeFromPad(t, app)
	waitFor(t, app, "cartridge changed", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked && s.Status == "Cartridge changed."
	})
	mu.Lock()
	defer mu.Unlock()
	if swaps != 1 || swap.Name != "game.col" || swap.MediaID != fmt.Sprintf("%x", sha256.Sum256(cart)) {
		t.Fatalf("swaps=%d request=%+v", swaps, swap)
	}
	app.mu.Lock()
	generation, focused := app.session.CorePackage.Generation, app.sessionDisplay
	app.mu.Unlock()
	if generation != next.CorePackage.Generation || focused == protocol.SessionDisplayFocused {
		t.Fatalf("new load not adopted: generation=%d display=%+v", generation, focused)
	}
	// The relaunch replaced the plane, so no return request went to the old one.
	if len(visible) != 1 || !visible[0] {
		t.Fatalf("display requests = %v", visible)
	}
}

func TestKitColecoGuidePickerBackReturnsToPlay(t *testing.T) {
	prior := colecoBIOSKitSession()
	running := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: observedKitPackage(prior)}
	var mu sync.Mutex
	var displays []protocol.SessionDisplay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		binding, valid := protocol.DevelopmentMediaHeaders(r.Header)
		if !valid || !binding.MatchesSessionDisplay(running) {
			http.Error(w, "session display identity rejected", http.StatusConflict)
			return
		}
		display := readSessionDisplay(t, r)
		mu.Lock()
		displays = append(displays, display)
		mu.Unlock()
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, hardwareRoomID, liveHardwareTestScript),
		testRoomPack(t, colecoArcadeRoomID, `function load() end; function draw() error("Guide must not open the Coleco room") end`),
	}), "")
	app.mu.Lock()
	app.session = prior
	app.syncGPUParkLocked()
	app.mu.Unlock()

	guide := sessionGuideCommand(app.Snapshot().Session)
	if guide != CmdSearch {
		t.Fatalf("Coleco Guide command = %v, want cartridge picker", guide)
	}
	app.HandleCommand(guide, time.Now())
	snap := waitFor(t, app, "Coleco Guide cartridge picker", func(s Snapshot) bool {
		return s.Session.ControlsOpen && s.TapePicker.Open && !s.GPUParked
	})
	if !snap.Session.MediaSelector || snap.Session.LoadTape || snap.Room.DuringPlay || snap.Room.ID == colecoArcadeRoomID {
		t.Fatalf("Coleco picker opened under a room or used live-media state: session=%+v room=%+v", snap.Session, snap.Room)
	}
	app.HandleCommand(CmdBack, time.Now())
	returned := waitFor(t, app, "Coleco Guide picker return to play", func(s Snapshot) bool {
		return s.Session.State == "active" && !s.Session.ControlsOpen && !s.TapePicker.Open && s.GPUParked
	})
	if returned.Room.DuringPlay || returned.Session.MediaSelector {
		t.Fatalf("Coleco session controls stayed open after Back: session=%+v room=%+v", returned.Session, returned.Room)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(displays) != 2 || displays[0] != protocol.SessionDisplayFocused || displays[1] != protocol.SessionDisplayClosed {
		t.Fatalf("Guide/Back display transitions = %v", displays)
	}
}

func selectCartridgeFromPad(t *testing.T, app *App) {
	t.Helper()
	now := time.Now()
	e := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton,
		Code: remoteinput.ButtonSelect, Action: remoteinput.ActionPress}
	if app.HandleLocalPad(e, now) {
		t.Fatal("picker Select was consumed by the running keypad machine")
	}
	app.Press(app.localPadCommand(e), now)
	e.Action = remoteinput.ActionRelease
	if app.HandleLocalPad(e, now) {
		t.Fatal("picker Select release was consumed by the running keypad machine")
	}
	app.Release(app.localPadCommand(e))
}

// A keypad machine keeps Select: it is the Coleco's keypad * key. Home or
// Guide opens its controls, and the hint names Guide.
func TestKeypadMachineKeepsSelect(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.SetKitLocal(&fakeLocalCores{}, &fakePadFeed{})
	app.mu.Lock()
	app.session = colecoKitSession()
	select_ := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonSelect}
	press, release := select_, select_
	press.Action, release.Action = remoteinput.ActionPress, remoteinput.ActionRelease
	now := time.Now()
	_, down := app.sessionPadLocked(press, now)
	_, up := app.sessionPadLocked(release, now)
	opened := app.roomDuringPlay || app.sessionDisplayBusy
	app.mu.Unlock()
	if len(down) != 1 || down[0] != press || len(up) != 1 || up[0] != release || opened {
		t.Fatalf("Select did not reach the keypad machine: down=%v up=%v opened=%v", down, up, opened)
	}
	if snap := app.Snapshot(); !snap.Session.KeypadSelect {
		t.Fatal("snapshot lost the keypad Select")
	}
}
