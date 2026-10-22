package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	json.NewEncoder(w).Encode(map[string]any{"id": s.ID, "state": s.State, "game_id": s.GameID, "target": s.Target, "target_id": s.TargetID, "flight_id": s.FlightID, "core_package": s.CorePackage, "input": s.Input})
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
	if snap := app.Snapshot(); snap.Session.LoadTape || snap.Session.HardwareRoom || snap.Room.DuringPlay || snap.TapePicker.Open || displays.Load() != 0 {
		t.Fatalf("computer tape without a plane opened controls: %+v displays=%d", snap.Session, displays.Load())
	}
}

func TestKitComputerTapePresentsFramesAndRestoresVideo(t *testing.T) {
	prior := computerTapeDisplaySession()
	var mu sync.Mutex
	var visible []bool
	presenter := &countingSessionMenu{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
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
		return s.Room.DuringPlay && !s.GPUParked && !s.TapePicker.Open
	})
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != 1 || n != menudisplay.FrameBytes {
		t.Fatalf("room frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.mu.Lock()
	app.openTapePickerLocked()
	app.mu.Unlock()
	waitFor(t, app, "computer tape picker", func(s Snapshot) bool { return s.TapePicker.Open && s.Room.DuringPlay })
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != 2 || n != menudisplay.FrameBytes {
		t.Fatalf("tape picker frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "picker closed on the plane", func(s Snapshot) bool {
		return !s.TapePicker.Open && s.Room.DuringPlay && !s.GPUParked
	})
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "computer video restored", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Room.DuringPlay && s.GPUParked
	})
	before, _ := presenter.snapshot()
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, _ := presenter.snapshot(); calls != before {
		t.Fatalf("return submitted another launcher frame: %d -> %d", before, calls)
	}
	app.HandleCommand(CmdSearch, now)
	waitFor(t, app, "search reopens the plane", func(s Snapshot) bool {
		return s.TapePicker.Open && s.Room.DuringPlay && !s.GPUParked
	})
	display.Present()
	waitSessionMenuFrame(t, display)
	if calls, n := presenter.snapshot(); calls != before+1 || n != menudisplay.FrameBytes {
		t.Fatalf("search frame was not submitted: calls=%d bytes=%d", calls, n)
	}
	app.HandleCommand(CmdBack, now)
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "search return", func(s Snapshot) bool {
		return !s.TapePicker.Open && !s.Room.DuringPlay && s.GPUParked
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
	if snap := app.Snapshot(); snap.Room.DuringPlay || snap.TapePicker.Open || snap.Session.State != "active" || calls.Load() != 0 {
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
	if snap := app.Snapshot(); snap.Room.DuringPlay || app.ForwardsPlayHID() || !snap.GPUParked {
		t.Fatal("navigation/input enabled before physical display open succeeded")
	}
	close(gate)
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Room.DuringPlay && !s.GPUParked })
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
	waitFor(t, app, "failed physical return", func(s Snapshot) bool { return s.Room.DuringPlay && strings.Contains(s.Status, "return failed") })
	if app.ForwardsPlayHID() {
		t.Fatal("failed close restored core input while HDMI still belonged to room")
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "physical return", func(s Snapshot) bool { return !s.Room.DuringPlay && s.GPUParked && app.ForwardsPlayHID() })
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
	if snap := app.Snapshot(); snap.Room.DuringPlay || snap.TapePicker.Open || !snap.GPUParked {
		t.Fatal("stale opener revived room on replacement")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !samePlayHIDSession(app.session, next) || app.sessionDisplayVisible {
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
	if snap := app.Snapshot(); snap.Session.State != "idle" || snap.Room.DuringPlay || snap.TapePicker.Open || snap.GPUParked {
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
				waitFor(t, app, "HDMI room", func(s Snapshot) bool { return s.Room.DuringPlay })
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
				return s.Room.DuringPlay
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
	if snap := app.Snapshot(); snap.Room.Notice != sessionDisplayUnavailableNotice || snap.Status != "Arming Maze…" {
		t.Fatalf("presentation warning replaced live tape status: notice=%q status=%q", snap.Room.Notice, snap.Status)
	}
	close(gate)
	waitFor(t, app, "starter armed live", func(s Snapshot) bool { return !s.TapePicker.Open && strings.Contains(s.Status, "Tape armed") })
	presenter.fail.Store(false)
	display.Present()
	waitSessionMenuFrame(t, display)
	app.Tick(time.Now())
	if snap := app.Snapshot(); display.LastError() != nil || snap.Room.Notice != "" || snap.Status != "Tape armed." {
		t.Fatalf("healthy frame retained HDMI warning: err=%v notice=%q status=%q", display.LastError(), snap.Room.Notice, snap.Status)
	}
	app.mu.Lock()
	app.roomSessionNotice = "The running machine changed. Review the refreshed setup."
	app.mu.Unlock()
	app.Tick(time.Now())
	if snap := app.Snapshot(); snap.Room.Notice == "" {
		t.Fatal("healthy presenter cleared an unrelated session notice")
	}
	if imports.Load() != 1 || arms.Load() != 1 || selections.Load() != 0 || !app.Snapshot().Room.DuringPlay {
		t.Fatal("live starter selection changed Next start or left HDMI controls")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !samePlayHIDSession(app.session, prior) {
		t.Fatal("starter arm replaced machine")
	}
}

type countingSessionMenu struct {
	mu     sync.Mutex
	status menudisplay.Status
	calls  int
	lastN  int
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
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Room.DuringPlay && !s.GPUParked })
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
	if snap.Status == sessionDisplayReturningStatus || snap.Session.State != "idle" || snap.Room.DuringPlay || snap.GPUParked {
		t.Fatalf("host stop left stale return: status=%q state=%q room=%v parked=%v", snap.Status, snap.Session.State, snap.Room.DuringPlay, snap.GPUParked)
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
	waitFor(t, app, "kit HDMI room", func(s Snapshot) bool { return s.Room.DuringPlay && !s.GPUParked })
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "return to play", func(s Snapshot) bool { return !s.Room.DuringPlay && s.GPUParked && app.ForwardsPlayHID() })
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

// decodeSessionDisplay reads a strict display request. The launcher's controls
// take launcher focus; a return closes the plane.
func decodeSessionDisplay(t *testing.T, r *http.Request) bool {
	t.Helper()
	var request protocol.SessionDisplayRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	display, ok := request.Display()
	if err != nil || !ok || display != protocol.SessionDisplayFocused && display != protocol.SessionDisplayClosed {
		t.Errorf("display request %+v: %v", request, err)
	}
	return display.Visible
}
