package tenfoot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/misteross/expansion"
)

func kitDisplaySession() hostclient.SessionResult {
	s := hardwareBoundSession()
	s.CoreKeyboard = true
	s.Input = &hostclient.SessionInput{State: "attached", Ready: true}
	s.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1}
	s.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.media.blob", Major: 1}, {ID: "fes.keyboard", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}
	return s
}

func writeKitDisplaySession(w http.ResponseWriter, s hostclient.SessionResult) {
	json.NewEncoder(w).Encode(map[string]any{"id": s.ID, "state": s.State, "game_id": s.GameID, "target": s.Target, "target_id": s.TargetID, "flight_id": s.FlightID, "media_name": s.MediaName, "core_package": s.CorePackage, "input": s.Input})
}

type transitionPadFeed struct {
	fakePadFeed
	closes atomic.Int32
}

func (f *transitionPadFeed) Close() { f.closes.Add(1) }

func kitDisplayApp(t *testing.T, server *httptest.Server) (*App, *transitionPadFeed) {
	t.Helper()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, liveHardwareTestScript)}), "")
	feed := &transitionPadFeed{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.session = kitDisplaySession()
	app.syncGPUParkLocked()
	app.mu.Unlock()
	t.Cleanup(app.Stop)
	return app, feed
}

func computerTapeKitSession() hostclient.SessionResult {
	s := hardwareBoundSession()
	s.CoreKeyboard = true
	s.Input = &hostclient.SessionInput{State: "attached", Ready: true}
	tape := protocol.Zx81TapeInterface()
	s.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}
	s.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor}}
	s.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{
		Unit: protocol.Zx81TapeUnit, Interface: tape, MinBytes: 1, MaxBytes: 16384, ChunkBytes: 512, State: "empty",
	}}
	return s
}

func computerTapeDisplaySession() hostclient.SessionResult {
	s := computerTapeKitSession()
	s.CorePackage.ActiveInterfaces = append(s.CorePackage.ActiveInterfaces,
		hostclient.SessionCoreInterface{ID: "fes.memory.hps-ddr", Major: 1},
		hostclient.SessionCoreInterface{ID: "fes.video.session-display", Major: 1})
	return s
}

func TestKitComputerTapeWithoutPlaneStaysHidden(t *testing.T) {
	var displays atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		displays.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session = computerTapeKitSession()
	app.syncGPUParkLocked()
	app.mu.Unlock()
	now := time.Now()
	app.HandleCommand(CmdHome, now)
	app.HandleCommand(CmdSearch, now)
	app.HandlePlayHIDScancode("f12", hidKeyboardF12, true, now)
	if snap := app.Snapshot(); snap.Session.LoadTape || snap.Session.HardwareRoom || snap.Session.ControlsOpen || snap.TapePicker.Open || displays.Load() != 0 {
		t.Fatalf("computer tape without a plane opened controls: %+v displays=%d", snap.Session, displays.Load())
	}
}

func observedKitPackage(prior hostclient.SessionResult) *protocol.CorePackageStatus {
	p := prior.CorePackage
	observed := &protocol.CorePackageStatus{PackageID: p.PackageID, Generation: p.Generation,
		ABI: protocol.RuntimeContract{ID: p.ABI.ID, Major: p.ABI.Major, Minor: p.ABI.Minor}, MediaUnits: p.MediaUnits}
	for _, i := range p.ActiveInterfaces {
		observed.ActiveInterfaces = append(observed.ActiveInterfaces, protocol.RuntimeInterface{ID: i.ID, Major: i.Major, Minor: i.Minor})
	}
	return observed
}

func startEmptyZX81Kit(t *testing.T, library string) *App {
	t.Helper()
	prior := computerTapeDisplaySession()
	running := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: observedKitPackage(prior)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/display":
			b, valid := protocol.DevelopmentMediaHeaders(r.Header)
			if !valid || !b.MatchesSessionDisplay(running) {
				http.Error(w, "session display identity rejected", http.StatusConflict)
				return
			}
			_ = decodeSessionDisplay(t, r)
			writeKitDisplaySession(w, prior)
		case "/api/v1/library/zx81-tapes":
			_, _ = io.WriteString(w, `{"tapes":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session = prior
	if library != "" {
		app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "zx81", Root: library}}
	}
	app.syncGPUParkLocked()
	app.mu.Unlock()
	return app
}

func assertEmptyLibrarySelector(t *testing.T, app *App, dir string) {
	t.Helper()
	waitFor(t, app, "empty library selector", func(s Snapshot) bool {
		return s.TapePicker.Open && s.TapePicker.Title == tapePickerTitle && s.Session.State == "active" && len(s.TapePicker.Rows) >= 2
	})
	rows := app.Snapshot().TapePicker.Rows
	if rows[0].Kind != tapePickerKindEject || !rows[0].Selectable || rows[1].Kind != tapePickerKindOSK {
		t.Fatalf("empty library hid eject or a typed path: %+v", rows)
	}
	selectTapeLibraryRoot(t, app, dir)
	var parent, eject, typedPath bool
	for _, row := range app.Snapshot().TapePicker.Rows {
		if row.Kind == tapePickerKindParent && row.Name == ".." {
			parent = true
		}
		if row.Kind == tapePickerKindEject && row.Selectable {
			eject = true
		}
		if row.Kind == tapePickerKindOSK && row.Selectable {
			typedPath = true
		}
		if row.Kind == tapePickerKindFile {
			t.Fatalf("folder with no media file listed a file: %+v", row)
		}
	}
	if !parent || !eject || !typedPath {
		t.Fatalf("empty folder was not entered: %+v", app.Snapshot().TapePicker.Rows)
	}
}

func TestF12OpensZX81SelectorWhenLibraryHasNoPFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := startEmptyZX81Kit(t, dir)
	now := time.Now()
	if !app.HandlePlayHIDScancode("f12", 0, true, now) {
		t.Fatal("named F12 was forwarded instead of opening the selector")
	}
	assertEmptyLibrarySelector(t, app, dir)
	if !app.HandlePlayHIDScancode("f12", hidKeyboardF12, false, time.Now()) || !app.Snapshot().TapePicker.Open {
		t.Fatal("F12 release closed or forwarded the selector")
	}
	if !app.HandlePlayHIDScancode("F12", hidKeyboardF12, true, time.Now()) {
		t.Fatal("second F12 was forwarded")
	}
	waitFor(t, app, "selector dismissed", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked && s.Session.State == "active"
	})
}

func TestKitComputerMediaPresentsFramesAndRestoresVideo(t *testing.T) {
	for _, tc := range []struct {
		name               string
		contract           protocol.RuntimeContract
		minBytes, maxBytes uint32
	}{
		{"zx81", protocol.Zx81TapeInterface(), 1, 16384},
		{"spectrum", protocol.SpectrumTapeInterface(), uint32(protocol.SpectrumTapeMinBytes), uint32(protocol.SpectrumTapeMaxBytes)},
		{"apple2", protocol.Apple2FloppyInterface(), 143360, 143360},
		{"c64", protocol.C64DiskInterface(), 174848, 174848},
		{"atari-st", protocol.AtariStFloppyInterface(), 737280, 737280},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := computerTapeDisplaySession()
			prior.CorePackage.ActiveInterfaces[0] = hostclient.SessionCoreInterface{ID: tc.contract.ID, Major: tc.contract.Major, Minor: tc.contract.Minor}
			prior.CorePackage.MediaUnits[0] = protocol.MediaUnitStatus{Unit: 0, Interface: tc.contract, MinBytes: tc.minBytes, MaxBytes: tc.maxBytes, ChunkBytes: 512, State: "empty"}
			testKitComputerMediaDisplay(t, prior)
		})
	}
}

func TestFormat4ColecoSelectorSubmitsACompositedFrame(t *testing.T) {
	prior := colecoBIOSKitSession()
	presenter := &countingSessionMenu{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		visible := decodeSessionDisplay(t, r)
		presenter.set(menudisplay.Status{
			Available: visible, Session: visible, Generation: 4,
			PackageID: prior.CorePackage.PackageID, CoreGeneration: prior.CorePackage.Generation,
			Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes,
		})
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	app.session = prior
	app.syncGPUParkLocked()
	app.mu.Unlock()

	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "format-4 Coleco selector", func(s Snapshot) bool {
		return s.Session.ControlsOpen && s.TapePicker.Open && !s.GPUParked
	})
	drawFrame(display, app.Snapshot(), map[string]gpuTexture{}, map[string]gpuTexture{})
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != 1 || n != menudisplay.FrameBytes {
		t.Fatalf("format-4 selector frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	if !presenter.visible() {
		t.Fatal("format-4 selector frame was fully transparent")
	}
	app.mu.Lock()
	composition := app.session.CorePackage.PartsComposition
	app.mu.Unlock()
	if composition == nil || composition.Layout != expansion.ColecoNativeVideoLayout {
		t.Fatalf("format-4 video part identity was not retained: %+v", composition)
	}
}

func testKitComputerMediaDisplay(t *testing.T, prior hostclient.SessionResult) {
	t.Helper()
	var mu sync.Mutex
	var visible []bool
	presenter := &countingSessionMenu{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		// Use the same admission predicate as the host service, target agent and
		// runtime adapter. A display-capable UI projection alone is insufficient.
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		p := prior.CorePackage
		observed := &protocol.CorePackageStatus{PackageID: p.PackageID, Generation: p.Generation,
			ABI: protocol.RuntimeContract{ID: p.ABI.ID, Major: p.ABI.Major, Minor: p.ABI.Minor}, MediaUnits: p.MediaUnits}
		for _, i := range p.ActiveInterfaces {
			observed.ActiveInterfaces = append(observed.ActiveInterfaces, protocol.RuntimeInterface{ID: i.ID, Major: i.Major, Minor: i.Minor})
		}
		if !valid || !b.MatchesSessionDisplay(protocol.Status{State: protocol.StateActive, Development: true, CorePackage: observed}) {
			http.Error(w, "session display identity rejected", http.StatusConflict)
			return
		}
		req := struct{ Visible bool }{decodeSessionDisplay(t, r)}
		mu.Lock()
		visible = append(visible, req.Visible)
		mu.Unlock()
		presenter.set(menudisplay.Status{
			Available: req.Visible, Session: true, Generation: map[bool]uint64{true: 4}[req.Visible],
			PackageID: prior.CorePackage.PackageID, CoreGeneration: prior.CorePackage.Generation,
			Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes,
		})
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	app.session = prior
	app.syncGPUParkLocked()
	app.mu.Unlock()
	now := time.Now()
	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "computer HDMI room", func(s Snapshot) bool {
		return s.Session.ControlsOpen && !s.GPUParked && s.TapePicker.Open
	})
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != 1 || n != menudisplay.FrameBytes {
		t.Fatalf("room frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.mu.Lock()
	app.openTapePickerLocked()
	app.mu.Unlock()
	waitFor(t, app, "computer tape picker", func(s Snapshot) bool { return s.TapePicker.Open && s.Session.ControlsOpen })
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("tape picker frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "computer video restored", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked
	})
	before, _ := presenter.snapshot()
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, _ := presenter.snapshot(); calls != before {
		t.Fatalf("return submitted another launcher frame: %d -> %d", before, calls)
	}
	app.HandleCommand(CmdSearch, now)
	waitFor(t, app, "search reopens the plane", func(s Snapshot) bool {
		return s.TapePicker.Open && s.Session.ControlsOpen && !s.GPUParked
	})
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != before+1 || n != menudisplay.FrameBytes {
		t.Fatalf("search frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.HandleCommand(CmdBack, now)
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "search return", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked
	})
	mu.Lock()
	defer mu.Unlock()
	if len(visible) != 4 || !visible[0] || visible[1] || !visible[2] || visible[3] {
		t.Fatalf("display handoff = %v", visible)
	}
}

func TestKitLiveControlsNeedObservedCapability(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); http.NotFound(w, r) }))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CorePackage.ActiveInterfaces = app.session.CorePackage.ActiveInterfaces[:2]
	app.mu.Unlock()
	if snap := app.Snapshot(); snap.Session.HardwareRoom || snap.Session.LoadTape {
		t.Fatal("kit advertised HDMI controls without an identified session display")
	}
	app.HandleCommand(CmdHome, time.Now())
	app.HandleCommand(CmdSearch, time.Now())
	if snap := app.Snapshot(); snap.Session.ControlsOpen || snap.TapePicker.Open || snap.Session.State != "active" || calls.Load() != 0 {
		t.Fatal("unsupported kit display mutated the running session")
	}
}

func TestKitDisplayOpenCloseFailureAndFreshInput(t *testing.T) {
	prior := kitDisplaySession()
	entered, gate := make(chan struct{}, 1), make(chan struct{})
	var rejectClose atomic.Bool
	rejectClose.Store(true)
	var mu sync.Mutex
	var visible []bool
	var feed *transitionPadFeed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		b, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || b.TargetID != prior.TargetID || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID || feed.closes.Load() == 0 {
			t.Error("display opened without captured identity or local-input neutralization")
		}
		req := struct{ Visible bool }{decodeSessionDisplay(t, r)}
		mu.Lock()
		visible = append(visible, req.Visible)
		mu.Unlock()
		if req.Visible {
			entered <- struct{}{}
			<-gate
		} else if rejectClose.Swap(false) {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"IO_FAILED","message":"return failed"}}`)
			return
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, source := kitDisplayApp(t, server)
	feed = source
	now := time.Now()
	dpad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonDPadRight, Action: remoteinput.ActionPress}
	app.HandleLocalPad(dpad, now)
	if !feed.saw(dpad.Code, remoteinput.ActionPress) {
		t.Fatal("kit library session did not use local input source")
	}
	app.HandleCommand(CmdHome, now)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("display mutation never began")
	}
	if snap := app.Snapshot(); snap.Session.ControlsOpen || app.ForwardsPlayHID() || !snap.GPUParked {
		t.Fatal("navigation/input enabled before physical display open succeeded")
	}
	close(gate)
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Session.ControlsOpen && !s.GPUParked })
	if !app.HandleLocalPad(dpad, now) {
		t.Fatal("held gameplay button leaked into room navigation")
	}
	dpad.Action = remoteinput.ActionRelease
	app.HandleLocalPad(dpad, now)
	dpad.Action = remoteinput.ActionPress
	if app.HandleLocalPad(dpad, now) {
		t.Fatal("fresh room D-pad press was swallowed")
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "failed physical return", func(s Snapshot) bool { return s.Session.ControlsOpen && strings.Contains(s.Status, "return failed") })
	if app.ForwardsPlayHID() {
		t.Fatal("failed close restored core input while HDMI still belonged to room")
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "physical return", func(s Snapshot) bool { return !s.Session.ControlsOpen && s.GPUParked && app.ForwardsPlayHID() })
	before := len(feed.snapshot())
	app.HandleLocalPad(dpad, now)
	if len(feed.snapshot()) != before {
		t.Fatal("held menu direction reached machine on return")
	}
	dpad.Action = remoteinput.ActionRelease
	app.HandleLocalPad(dpad, now)
	dpad.Action = remoteinput.ActionPress
	app.HandleLocalPad(dpad, now)
	if len(feed.snapshot()) != before+1 {
		t.Fatal("fresh press did not restore gameplay")
	}
	app.mu.Lock()
	same := samePlayHIDSession(app.session, prior)
	app.mu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if !same || len(visible) != 3 || !visible[0] || visible[1] || visible[2] {
		t.Fatalf("display toggle replaced session or lost close retry: %v", visible)
	}
}

func TestKitDisplayStaleCompletionCannotRestoreOldRoom(t *testing.T) {
	prior := kitDisplaySession()
	entered, gate := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		close(entered)
		<-gate
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	<-entered
	app.mu.Lock()
	done := app.sessionDisplayWait
	next := kitDisplaySession()
	next.CorePackage.Generation++
	app.applySessionLocked(next)
	app.mu.Unlock()
	close(gate)
	<-done
	if snap := app.Snapshot(); snap.Session.ControlsOpen || snap.TapePicker.Open || !snap.GPUParked {
		t.Fatal("stale opener revived room on replacement")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !samePlayHIDSession(app.session, next) || app.sessionDisplay.Visible {
		t.Fatal("stale display completion replaced the current machine")
	}
}

func TestKitLostOpenResponseKeepsGuardedReturnReachable(t *testing.T) {
	prior := kitDisplaySession()
	var opens, closes, stops atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/stop" {
			stops.Add(1)
		}
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		req := struct{ Visible bool }{decodeSessionDisplay(t, r)}
		if req.Visible {
			opens.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close() // Physical open may have succeeded before its reply was lost.
			return
		}
		closes.Add(1)
		b, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
			t.Error("uncertain-open close lost the original binding")
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "lost open response", func(s Snapshot) bool { return strings.Contains(s.Status, "HDMI controls unavailable") })
	if app.ForwardsPlayHID() || !app.HandlePlayHIDKey("s", true, time.Now()) {
		t.Fatal("uncertain display allowed gameplay or letter-S Stop")
	}
	app.HandlePlayHIDKey("s", false, time.Now())
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "return after lost response", func(s Snapshot) bool { return s.GPUParked && app.ForwardsPlayHID() })
	if opens.Load() != 1 || closes.Load() != 1 || stops.Load() != 0 || app.Snapshot().Session.State != "active" {
		t.Fatal("lost opener could not return without replacing the machine")
	}
}

func TestKitStopDuringDisplayOpenDropsLateCompletion(t *testing.T) {
	prior := kitDisplaySession()
	entered, gate := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/display":
			close(entered)
			<-gate
			writeKitDisplaySession(w, prior)
		case "/api/v1/session/stop":
			io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	<-entered
	app.mu.Lock()
	displayDone := app.sessionDisplayWait
	app.mu.Unlock()
	app.HandleCommand(CmdStop, time.Now())
	waitFor(t, app, "Stop during display", func(s Snapshot) bool { return s.Session.State == "idle" })
	close(gate)
	<-displayDone
	if snap := app.Snapshot(); snap.Session.State != "idle" || snap.Session.ControlsOpen || snap.TapePicker.Open || snap.GPUParked {
		t.Fatal("late display open resurrected stopped machine or room")
	}
}

func TestKitFailedStopKeepsDisplayReturnReachable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pending   bool
		lostStop  bool
		returnVia string
	}{
		{"open-rejected-stop-back", false, false, "back"},
		{"pending-open-rejected-stop-back", true, false, "back"},
		{"open-rejected-stop-controller", false, false, "controller"},
		{"pending-open-rejected-stop-controller", true, false, "controller"},
		{"open-lost-stop-shell-exit", false, true, "exit"},
		{"pending-open-lost-stop-shell-exit", true, true, "exit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prior := kitDisplaySession()
			entered, gate := make(chan struct{}), make(chan struct{})
			var release sync.Once
			var opens, closes, stops atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/session/display":
					b, ok := protocol.DevelopmentMediaHeaders(r.Header)
					if !ok || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation ||
						b.Target != prior.Target || b.TargetID != prior.TargetID || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
						t.Error("display return lost the captured machine identity")
					}
					var req struct{ Visible bool }
					json.NewDecoder(r.Body).Decode(&req)
					if req.Visible {
						opens.Add(1)
						close(entered)
						if tc.pending {
							<-gate
						}
					} else {
						closes.Add(1)
					}
					writeKitDisplaySession(w, prior)
				case "/api/v1/session/stop":
					var req struct {
						ReleaseIdle bool `json:"release_idle"`
					}
					json.NewDecoder(r.Body).Decode(&req)
					if req.ReleaseIdle {
						writeKitDisplaySession(w, prior)
						return
					}
					stops.Add(1)
					if tc.lostStop {
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						conn.Close()
						return
					}
					w.WriteHeader(http.StatusConflict)
					io.WriteString(w, `{"error":{"code":"BUSY","message":"display request is still running"}}`)
				case "/api/v1/session":
					writeKitDisplaySession(w, prior)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app, _ := kitDisplayApp(t, server)
			t.Cleanup(func() { release.Do(func() { close(gate) }) })
			app.HandleCommand(CmdHome, time.Now())
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("display open never began")
			}
			if !tc.pending {
				waitFor(t, app, "HDMI room", func(s Snapshot) bool { return s.Session.ControlsOpen })
			}
			app.mu.Lock()
			displayDone := app.sessionDisplayWait
			app.mu.Unlock()
			b := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton,
				Code: remoteinput.ButtonB, Action: remoteinput.ActionPress}
			if tc.returnVia == "controller" {
				app.HandleLocalPad(b, time.Now()) // Still held when Stop takes focus.
			}
			app.HandleCommand(CmdStop, time.Now())
			waitFor(t, app, "failed Stop", func(s Snapshot) bool { return s.Session.RetryStop && !s.Session.Stopping })
			if tc.pending {
				release.Do(func() { close(gate) })
				select {
				case <-displayDone:
				case <-time.After(time.Second):
					t.Fatal("late display open never completed")
				}
			}
			if app.ForwardsPlayHID() {
				t.Fatal("failed Stop returned input while HDMI may still show controls")
			}
			app.mu.Lock()
			bound := samePlayHIDSession(app.sessionDisplayBinding, prior)
			app.mu.Unlock()
			if !bound || closes.Load() != 0 {
				t.Fatal("failed Stop forgot or implicitly returned the display")
			}
			switch tc.returnVia {
			case "back":
				app.HandleCommand(CmdBack, time.Now())
			case "controller":
				app.HandleLocalPad(b, time.Now())
				if closes.Load() != 0 {
					t.Fatal("held B returned the display without release and fresh press")
				}
				b.Action = remoteinput.ActionRelease
				app.HandleLocalPad(b, time.Now())
				b.Action = remoteinput.ActionPress
				app.HandleLocalPad(b, time.Now())
			case "exit":
				app.Stop()
			}
			waitFor(t, app, "return after failed Stop", func(Snapshot) bool { return closes.Load() == 1 })
			if tc.returnVia != "exit" {
				waitFor(t, app, "confirmed display return", func(Snapshot) bool { return app.ForwardsPlayHID() })
			}
			if opens.Load() != 1 || stops.Load() != 1 || app.Snapshot().Session.State != "active" {
				t.Fatal("display return retried Stop or replaced the running machine")
			}
		})
	}
}

func TestKitSelectStartKeepsStopPriority(t *testing.T) {
	var displays, stops atomic.Int32
	var mu sync.Mutex
	var stopBody string
	prior := kitDisplaySession()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/display":
			displays.Add(1)
			writeKitDisplaySession(w, kitDisplaySession())
		case "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			stopBody = string(raw)
			mu.Unlock()
			stops.Add(1)
			io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	app.HandleLocalPad(remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonSelect, Action: remoteinput.ActionPress}, now)
	app.HandleLocalPad(remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: remoteinput.ButtonStart, Action: remoteinput.ActionPress}, now.Add(time.Millisecond))
	app.Tick(now.Add(time.Millisecond + localChordHold))
	waitFor(t, app, "chord Stop", func(s Snapshot) bool { return s.Session.State == "idle" })
	if stops.Load() != 1 || displays.Load() != 0 || feed.saw(remoteinput.ButtonSelect, remoteinput.ActionPress) || feed.saw(remoteinput.ButtonStart, remoteinput.ActionPress) {
		t.Fatal("Select/View opened controls or reached core ahead of Stop chord")
	}
	mu.Lock()
	body := stopBody
	mu.Unlock()
	assertStopReleasesCapturedPlay(t, body, prior)
	app.mu.Lock()
	held := app.retainedIdleLease
	app.mu.Unlock()
	if held {
		t.Fatal("released Select+Start still tracked a retained idle lease")
	}
}

func TestKitStarterLiveTapeImportsThenArmsCapturedMachine(t *testing.T) {
	prior := kitDisplaySession()
	starter := hostclient.HardwareTape{ID: "maze", Name: "Maze", Filename: "maze.p", SHA256: strings.Repeat("b", 64), License: "CC0", Controls: "Cursor keys"}
	entered, gate := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	var imports, arms, selections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session/display":
			writeKitDisplaySession(w, prior)
		case "/api/v1/library/zx81-tapes":
			json.NewEncoder(w).Encode(map[string]any{"tapes": []hostclient.HardwareTape{starter}})
		case "/api/v1/library/zx81-tapes/maze/import":
			imports.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"media_id": starter.SHA256, "size": 128})
		case "/api/v1/session/live-media":
			arms.Add(1)
			close(entered)
			<-gate
			b, ok := protocol.DevelopmentMediaHeaders(r.Header)
			var req protocol.LiveMediaRequest
			json.NewDecoder(r.Body).Decode(&req)
			if !ok || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID || req.MediaID != starter.SHA256 || req.Name != starter.Filename {
				t.Error("starter live arm lost captured identity")
			}
			writeKitDisplaySession(w, prior)
		default:
			if r.Method == http.MethodPut {
				selections.Add(1)
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	presenter := &transientSessionMenu{status: menudisplay.Status{Available: true, Generation: 3, Session: true, PackageID: prior.CorePackage.PackageID, CoreGeneration: prior.CorePackage.Generation}}
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	app.openTapePickerLocked()
	app.mu.Unlock()
	waitFor(t, app, "starter rows", func(s Snapshot) bool {
		for _, row := range s.TapePicker.Rows {
			if row.Kind == tapePickerKindStarter {
				return s.Session.ControlsOpen
			}
		}
		return false
	})
	if imports.Load() != 0 || arms.Load() != 0 {
		t.Fatal("opening tape shelf imported or armed starter")
	}
	selectTapeNamedRow(t, app, starter.Name)
	app.HandleCommand(CmdDetails, time.Now())
	if !strings.Contains(app.Snapshot().TapePicker.Status, starter.Controls) || !strings.Contains(app.Snapshot().TapePicker.Status, starter.License) {
		t.Fatal("starter picker omitted controls or attribution")
	}
	app.HandleCommand(CmdSelect, time.Now())
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("live tape arm did not begin")
	}
	presenter.fail.Store(true)
	display.Present()
	waitSessionMenuFrame(t, display)
	if display.LastError() == nil {
		t.Fatal("simulated media/display overlap did not reject a frame")
	}
	app.Tick(time.Now())
	if snap := app.Snapshot(); snap.Session.ControlsNotice != sessionDisplayUnavailableNotice || snap.Status != "Arming Maze…" {
		t.Fatalf("presentation warning replaced live tape status: notice=%q status=%q", snap.Session.ControlsNotice, snap.Status)
	}
	close(gate)
	waitFor(t, app, "starter armed live", func(s Snapshot) bool { return !s.TapePicker.Open && strings.Contains(s.Status, "Tape armed") })
	presenter.fail.Store(false)
	waitFor(t, app, "starter returned to play", func(s Snapshot) bool { return !s.Session.ControlsOpen && s.GPUParked })
	if snap := app.Snapshot(); snap.Session.ControlsNotice != "" || snap.Status != "Tape armed." {
		t.Fatalf("return retained HDMI warning: notice=%q status=%q", snap.Session.ControlsNotice, snap.Status)
	}
	app.mu.Lock()
	app.roomSessionNotice = "The running machine changed. Review the refreshed setup."
	app.mu.Unlock()
	app.Tick(time.Now())
	if snap := app.Snapshot(); snap.Session.ControlsNotice == "" {
		t.Fatal("healthy presenter cleared an unrelated session notice")
	}
	if imports.Load() != 1 || arms.Load() != 1 || selections.Load() != 0 || app.Snapshot().Session.ControlsOpen {
		t.Fatal("live starter selection changed Next start or failed to return to play")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !samePlayHIDSession(app.session, prior) {
		t.Fatal("starter arm replaced machine")
	}
}

func TestKitTapePickerArmsSavedZX81ProgramWithoutReimport(t *testing.T) {
	prior := computerTapeKitSession()
	digest := strings.Repeat("c", 64)
	saved := hostclient.HardwareTape{ID: "saved-" + digest, Name: "Saved program 2026-10-24 00:00", Filename: "saved.p", License: "Saved", SHA256: digest, MediaID: digest, RAMKB: 1}
	var imports, arms atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/zx81-tapes":
			if r.URL.Query().Get("game_id") != prior.GameID {
				t.Errorf("saved cassette lookup game_id = %q, want %q", r.URL.Query().Get("game_id"), prior.GameID)
			}
			json.NewEncoder(w).Encode(map[string]any{"tapes": []hostclient.HardwareTape{saved}})
		case "/api/v1/library/zx81-tapes/" + saved.ID + "/import":
			imports.Add(1)
			http.Error(w, "saved media must not be imported again", http.StatusBadRequest)
		case "/api/v1/session/live-media":
			arms.Add(1)
			binding, ok := protocol.DevelopmentMediaHeaders(r.Header)
			var request protocol.LiveMediaRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if !ok || binding.PackageID != prior.CorePackage.PackageID || binding.Generation != prior.CorePackage.Generation || binding.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID || request.MediaID != digest || request.Name != saved.Filename {
				t.Error("saved cassette live arm lost captured identity or media ID")
			}
			writeKitDisplaySession(w, prior)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.mu.Lock()
	app.session = prior
	app.syncGPUParkLocked()
	app.openTapePickerLocked()
	app.mu.Unlock()
	t.Cleanup(app.Stop)
	waitFor(t, app, "saved ZX81 cassette row", func(s Snapshot) bool {
		for _, row := range s.TapePicker.Rows {
			if row.Starter != nil && row.Starter.MediaID == digest {
				return true
			}
		}
		return false
	})
	selectTapeNamedRow(t, app, saved.Name)
	snapshot := app.Snapshot()
	if label := tapePickerRowLabel(snapshot.TapePicker.Rows[snapshot.TapePicker.Index]); !strings.Contains(label, "Saved program") {
		t.Fatalf("saved cassette label = %q", label)
	}
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "saved ZX81 cassette armed", func(s Snapshot) bool {
		return !s.TapePicker.Open && s.Status == "Tape armed."
	})
	if imports.Load() != 0 || arms.Load() != 1 {
		t.Fatalf("saved selection imports=%d arms=%d", imports.Load(), arms.Load())
	}
}

type countingSessionMenu struct {
	mu               sync.Mutex
	status           menudisplay.Status
	calls            int
	lastN            int
	lastFrameVisible bool
}

func (c *countingSessionMenu) set(status menudisplay.Status) {
	c.mu.Lock()
	c.status = status
	c.mu.Unlock()
}

func (c *countingSessionMenu) snapshot() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, c.lastN
}

func (c *countingSessionMenu) visible() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastFrameVisible
}

func (c *countingSessionMenu) Status(context.Context) (menudisplay.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, nil
}

func (c *countingSessionMenu) Present(_ context.Context, generation uint64, pixels []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.lastN = len(pixels)
	c.lastFrameVisible = false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			c.lastFrameVisible = true
			break
		}
	}
	return menudisplay.Result{Generation: generation}, nil
}

type transientSessionMenu struct {
	status menudisplay.Status
	fail   atomic.Bool
}

func (c *transientSessionMenu) Status(context.Context) (menudisplay.Status, error) {
	return c.status, nil
}

func (c *transientSessionMenu) Present(_ context.Context, generation uint64, _ []byte) (menudisplay.Result, error) {
	if c.fail.Load() {
		return menudisplay.Result{}, errors.New("menu request rejected: busy menu display is unavailable")
	}
	return menudisplay.Result{Generation: generation}, nil
}

func waitSessionMenuFrame(t *testing.T, display *gfx.MenuDisplay) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for display.FramePending() {
		if time.Now().After(deadline) {
			t.Fatal("menu frame did not complete")
		}
		time.Sleep(time.Millisecond)
	}
}

// #767: a host-side stop that lands while the room is returning to play drops
// that return, and must not leave its "Returning to play…" copy behind.
func TestKitHostStopClearsReturningToPlay(t *testing.T) {
	prior := kitDisplaySession()
	returning, gate := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		req := struct{ Visible bool }{decodeSessionDisplay(t, r)}
		if !req.Visible {
			close(returning)
			<-gate
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Session.ControlsOpen && !s.GPUParked })
	app.HandleCommand(CmdBack, time.Now())
	<-returning
	if s := app.Snapshot().Status; s != sessionDisplayReturningStatus {
		t.Fatalf("status during return = %q", s)
	}
	app.mu.Lock()
	done := app.sessionDisplayWait
	app.applySessionLocked(hostclient.SessionResult{State: "idle"}) // Stopped from the host.
	app.mu.Unlock()
	close(gate)
	<-done
	snap := app.Snapshot()
	if snap.Status == sessionDisplayReturningStatus || snap.Session.State != "idle" || snap.Session.ControlsOpen || snap.GPUParked {
		t.Fatalf("host stop left stale return: status=%q state=%q room=%v parked=%v", snap.Status, snap.Session.State, snap.Session.ControlsOpen, snap.GPUParked)
	}
}

// A completed return clears its copy, so a later host-side stop shows idle.
func TestKitReturnToPlayClearsStatus(t *testing.T) {
	prior := kitDisplaySession()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Session.ControlsOpen && !s.GPUParked })
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "return to play", func(s Snapshot) bool { return !s.Session.ControlsOpen && s.GPUParked && app.ForwardsPlayHID() })
	if s := app.Snapshot().Status; s == sessionDisplayReturningStatus {
		t.Fatal("completed return kept its transition copy")
	}
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{State: "idle"})
	app.mu.Unlock()
	if s := app.Snapshot().Status; s == sessionDisplayReturningStatus || s == playingHardwareRoomStatus {
		t.Fatalf("host stop left play copy %q", s)
	}
}

// decodeSessionDisplay reads a strict display request without a surround. The
// launcher's controls take launcher focus; a return closes the plane.
func decodeSessionDisplay(t *testing.T, r *http.Request) bool {
	t.Helper()
	display := readSessionDisplay(t, r)
	if display != protocol.SessionDisplayFocused && display != protocol.SessionDisplayClosed {
		t.Errorf("display request without a surround: %+v", display)
	}
	return display.Visible
}

func readSessionDisplay(t *testing.T, r *http.Request) protocol.SessionDisplay {
	t.Helper()
	var request protocol.SessionDisplayRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	display, ok := request.Display()
	if err != nil || !ok {
		t.Errorf("display request %+v: %v", request, err)
	}
	return display
}

// surroundKit serves the session display route for a ZX81-style session and
// reports each confirmed plane state to the fake runtime presenter.
type surroundKit struct {
	mu        sync.Mutex
	displays  []protocol.SessionDisplay
	refuse    func(protocol.SessionDisplay) bool
	presenter *countingSessionMenu
}

func (k *surroundKit) requests() []protocol.SessionDisplay {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]protocol.SessionDisplay(nil), k.displays...)
}

func newSurroundKit(t *testing.T, prior hostclient.SessionResult) (*surroundKit, *App, *gfx.MenuDisplay) {
	t.Helper()
	kit := &surroundKit{presenter: &countingSessionMenu{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		display := readSessionDisplay(t, r)
		kit.mu.Lock()
		kit.displays = append(kit.displays, display)
		refuse := kit.refuse != nil && kit.refuse(display)
		kit.mu.Unlock()
		if refuse {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		status := menudisplay.Status{Available: display.Visible, Session: true, PackageID: prior.CorePackage.PackageID,
			CoreGeneration: prior.CorePackage.Generation, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes,
			Picture: []int{312, 90, 656, 540}}
		if display.Visible {
			status.Generation = 4
		}
		kit.presenter.set(status)
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	display, err := gfx.NewMenuDisplayWithPresenter(kit.presenter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.SetSurround(true)
	app.mu.Lock()
	app.session = prior
	app.syncGPUParkLocked()
	app.mu.Unlock()
	return kit, app, display
}

// A surround shows the plane while the machine keeps input. The media
// selector moves focus on that plane and returning gives input back with the
// surround still shown.
func TestKitSurroundKeepsMachineInputWhileTheSelectorMovesFocus(t *testing.T) {
	kit, app, display := newSurroundKit(t, computerTapeDisplaySession())
	waitFor(t, app, "surround", func(s Snapshot) bool { return s.Surround.Active && s.GPUParked })
	if got := kit.requests(); len(got) != 1 || got[0] != protocol.SessionDisplayShown {
		t.Fatalf("surround requests = %+v", got)
	}
	if !app.coreOwnsPadInput() || !app.ForwardsPlayHID() {
		t.Fatal("surround took input from the machine")
	}
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := kit.presenter.snapshot(); calls != 1 || n != menudisplay.FrameBytes {
		t.Fatalf("surround frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	if got := display.SessionPicture(); got != image.Rect(312, 90, 968, 630) {
		t.Fatalf("picture = %v", got)
	}
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "media selector over the surround", func(s Snapshot) bool {
		return s.Session.ControlsOpen && s.TapePicker.Open && !s.GPUParked && !s.Surround.Active
	})
	if app.coreOwnsPadInput() {
		t.Fatal("media selector left input with the machine")
	}
	// Focus moves keep the binding, so the selector frame needs no new picture.
	if got := display.SessionPicture(); got != image.Rect(312, 90, 968, 630) {
		t.Fatalf("focus move rebound the plane: %v", got)
	}
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "surround after the selector", func(s Snapshot) bool { return !s.Session.ControlsOpen && s.Surround.Active })
	if !app.coreOwnsPadInput() {
		t.Fatal("return kept input from the machine")
	}
	want := []protocol.SessionDisplay{protocol.SessionDisplayShown, protocol.SessionDisplayFocused, protocol.SessionDisplayShown}
	if got := kit.requests(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("display requests = %+v", got)
	}
}

// A plane that refuses the surround is closed, which also releases any input
// fence the kit kept, and the session does not ask again.
func TestKitSurroundFailureClosesThePlaneOnce(t *testing.T) {
	kit, app, _ := newSurroundKit(t, computerTapeDisplaySession())
	kit.mu.Lock()
	kit.refuse = func(d protocol.SessionDisplay) bool { return d == protocol.SessionDisplayShown }
	kit.mu.Unlock()
	waitFor(t, app, "closed after refusal", func(s Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.surroundFailed && !app.sessionDisplayBusy && !app.sessionDisplayUncertain
	})
	for i := 0; i < 5; i++ {
		app.Tick(time.Now())
	}
	if got := kit.requests(); len(got) != 2 || got[0] != protocol.SessionDisplayShown || got[1] != protocol.SessionDisplayClosed {
		t.Fatalf("display requests = %+v", got)
	}
	if snap := app.Snapshot(); snap.Surround.Active || !app.coreOwnsPadInput() {
		t.Fatalf("refused surround kept the plane or input: %+v", snap.Surround)
	}
	// The media selector still opens the plane and returns it closed.
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "media selector", func(s Snapshot) bool { return s.Session.ControlsOpen && s.TapePicker.Open && !s.GPUParked })
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "machine", func(s Snapshot) bool { return !s.Session.ControlsOpen && s.GPUParked && !s.Surround.Active })
	if got := kit.requests(); len(got) != 4 || got[2] != protocol.SessionDisplayFocused || got[3] != protocol.SessionDisplayClosed {
		t.Fatalf("display requests after refusal = %+v", got)
	}
}

// When the fallback close is unconfirmed too, the plane stays uncertain:
// machine input is held back and Back retries the close until it is confirmed.
func TestKitSurroundUnconfirmedCloseKeepsARetry(t *testing.T) {
	kit, app, _ := newSurroundKit(t, computerTapeDisplaySession())
	closes := 0
	kit.mu.Lock()
	kit.refuse = func(d protocol.SessionDisplay) bool {
		if d == protocol.SessionDisplayClosed {
			closes++
			return closes == 1
		}
		return true
	}
	kit.mu.Unlock()
	waitFor(t, app, "unconfirmed close", func(s Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.surroundFailed && !app.sessionDisplayBusy && len(kit.requests()) == 2
	})
	app.mu.Lock()
	uncertain := app.sessionDisplayUncertain
	app.mu.Unlock()
	if !uncertain || app.ForwardsPlayHID() {
		t.Fatal("unconfirmed close released machine input or lost its retry")
	}
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "confirmed close", func(s Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.sessionDisplayBusy && !app.sessionDisplayUncertain
	})
	want := []protocol.SessionDisplay{protocol.SessionDisplayShown, protocol.SessionDisplayClosed, protocol.SessionDisplayClosed}
	if got := kit.requests(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("display requests = %+v", got)
	}
	if !app.ForwardsPlayHID() {
		t.Fatal("confirmed close kept machine input")
	}
}

// A kit running a disk machine opens Change media over play, imports a D64
// through the paired listener and inserts it. The fake host applies the real
// display admission and the paired import bound.
func TestKitC64DiskMediaSwapsAndEjectsOverPlay(t *testing.T) {
	disk := protocol.C64DiskInterface()
	prior := computerTapeDisplaySession()
	prior.CorePackage.ActiveInterfaces[0] = hostclient.SessionCoreInterface{ID: disk.ID, Major: disk.Major, Minor: disk.Minor}
	prior.CorePackage.MediaUnits[0] = protocol.MediaUnitStatus{Unit: 0, Interface: disk, MinBytes: uint32(protocol.C64DiskBytes),
		MaxBytes: uint32(protocol.C64DiskBytes), ChunkBytes: 512, State: "empty"}
	raw, err := json.Marshal(prior.CorePackage)
	var observed protocol.CorePackageStatus
	if err != nil || json.Unmarshal(raw, &observed) != nil {
		t.Fatal("observed package projection failed")
	}
	running := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &observed}
	current := prior
	currentJSON, err := json.Marshal(prior)
	if err != nil {
		t.Fatalf("marshal session projection: %v", err)
	}
	if err := json.Unmarshal(currentJSON, &current); err != nil {
		t.Fatalf("clone session projection: %v", err)
	}
	dir := t.TempDir()
	image := bytes.Repeat([]byte{0x64}, int(protocol.C64DiskBytes))
	if err := os.WriteFile(filepath.Join(dir, "game.d64"), image, 0o600); err != nil {
		t.Fatal(err)
	}
	secondImage := bytes.Repeat([]byte{0x65}, int(protocol.C64DiskBytes))
	if err := os.WriteFile(filepath.Join(dir, "second.d64"), secondImage, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.dsk"), make([]byte, protocol.Apple2FloppyBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var visible []bool
	var inserted []protocol.LiveMediaRequest
	var imports, inserts, ejects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, bound := protocol.DevelopmentMediaHeaders(r.Header)
		switch r.URL.Path {
		case "/api/v1/session/display":
			if !bound || !b.MatchesSessionDisplay(running) {
				http.Error(w, "session display identity rejected", http.StatusConflict)
				return
			}
			show := decodeSessionDisplay(t, r)
			mu.Lock()
			visible = append(visible, show)
			writeKitDisplaySession(w, current)
			mu.Unlock()
		case "/api/v1/core-media":
			if !protocol.AdmitRemovableMediaSize(r.ContentLength) {
				http.Error(w, "paired import bound", http.StatusBadRequest)
				return
			}
			body, _ := io.ReadAll(r.Body)
			imports.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"media_id": fmt.Sprintf("%x", sha256.Sum256(body)), "size": len(body)})
		case "/api/v1/session/live-media":
			var request protocol.LiveMediaRequest
			err := json.NewDecoder(r.Body).Decode(&request)
			if err != nil || !bound || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
				http.Error(w, "disk insert lost the captured machine", http.StatusConflict)
				return
			}
			mu.Lock()
			inserted = append(inserted, request)
			current.MediaName = request.Name
			current.CorePackage.MediaUnits[0].State = protocol.MediaUnitReady
			inserts.Add(1)
			writeKitDisplaySession(w, current)
			mu.Unlock()
		case "/api/v1/session/live-media/clear":
			if !bound || b.PackageID != prior.CorePackage.PackageID || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
				http.Error(w, "disk eject lost the captured machine", http.StatusConflict)
				return
			}
			mu.Lock()
			ejects.Add(1)
			current.MediaName = ""
			current.CorePackage.MediaUnits[0].State = protocol.MediaUnitEmpty
			writeKitDisplaySession(w, current)
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session = prior
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "c64", Root: dir}}
	app.syncGPUParkLocked()
	app.mu.Unlock()
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "disk selector over play", func(s Snapshot) bool {
		return s.Session.ControlsOpen && !s.GPUParked && s.TapePicker.Open && s.TapePicker.Title == tapePickerTitle
	})
	insertDisk := func(name, label string) {
		t.Helper()
		selectTapeLibraryRoot(t, app, dir)
		found := map[string]bool{}
		for _, row := range app.Snapshot().TapePicker.Rows {
			wantSelectable := row.Name == "game.d64" || row.Name == "second.d64"
			if row.Kind == tapePickerKindFile && row.Selectable != wantSelectable {
				t.Fatalf("wrong disk selection: %+v", row)
			}
			if row.Kind == tapePickerKindFile && wantSelectable && row.Selectable {
				found[row.Name] = true
			}
		}
		if !found["game.d64"] || !found["second.d64"] {
			t.Fatalf("admitted D64s were not listed: %+v", app.Snapshot().TapePicker.Rows)
		}
		selectTapeNamedRow(t, app, name)
		app.HandleCommand(CmdSelect, time.Now())
		waitFor(t, app, label, func(s Snapshot) bool {
			return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked && s.Status == "Media inserted."
		})
	}
	insertDisk("game.d64", "first disk inserted and play restored")
	app.HandleCommand(CmdHome, time.Now())
	snap := waitFor(t, app, "disk selector reopened", func(s Snapshot) bool { return s.TapePicker.Open && s.Session.ControlsOpen })
	if snap.TapePicker.Current != "Inserted: game.d64" {
		t.Fatalf("reopened selector current disk = %q", snap.TapePicker.Current)
	}
	insertDisk("second.d64", "second disk swapped and play restored")
	app.HandleCommand(CmdHome, time.Now())
	snap = waitFor(t, app, "selector reopened after swap", func(s Snapshot) bool { return s.TapePicker.Open && s.Session.ControlsOpen })
	if snap.TapePicker.Current != "Inserted: second.d64" {
		t.Fatalf("selector current disk after swap = %q", snap.TapePicker.Current)
	}
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "disk ejected and play restored", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked && s.Status == "Media ejected."
	})
	app.HandleCommand(CmdHome, time.Now())
	snap = waitFor(t, app, "empty disk selector reopened", func(s Snapshot) bool { return s.TapePicker.Open && s.Session.ControlsOpen })
	if snap.TapePicker.Current != "No media inserted" {
		t.Fatalf("selector after eject current disk = %q", snap.TapePicker.Current)
	}
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "media selector closed", func(s Snapshot) bool { return !s.TapePicker.Open && !s.Session.ControlsOpen && s.GPUParked })
	mu.Lock()
	visibleSnapshot := append([]bool(nil), visible...)
	insertedSnapshot := append([]protocol.LiveMediaRequest(nil), inserted...)
	mu.Unlock()
	if imports.Load() != 2 || inserts.Load() != 2 || ejects.Load() != 1 || len(insertedSnapshot) != 2 ||
		insertedSnapshot[0].Name != "game.d64" || insertedSnapshot[0].MediaID != fmt.Sprintf("%x", sha256.Sum256(image)) ||
		insertedSnapshot[1].Name != "second.d64" || insertedSnapshot[1].MediaID != fmt.Sprintf("%x", sha256.Sum256(secondImage)) {
		t.Fatalf("imports=%d inserts=%d ejects=%d inserted=%+v", imports.Load(), inserts.Load(), ejects.Load(), insertedSnapshot)
	}
	wantVisible := []bool{true, false, true, false, true, false, true, false}
	if len(visibleSnapshot) != len(wantVisible) {
		t.Fatalf("display handoff = %v", visibleSnapshot)
	}
	for i := range wantVisible {
		if visibleSnapshot[i] != wantVisible[i] {
			t.Fatalf("display handoff = %v", visibleSnapshot)
		}
	}
}
