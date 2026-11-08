package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

// fixedVideoKitSession is a host session whose package has no session-display
// plane, as fes.pcxt-dos 87fd8149 (fes.video.fixed-720p60 only, #1561).
func fixedVideoKitSession() hostclient.SessionResult {
	s := hardwareBoundSession()
	s.CoreKeyboardHID = true
	s.Input = &hostclient.SessionInput{State: "attached", Ready: true}
	s.CorePackage.ABI = hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}
	s.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{
		{ID: "fes.keyboard.hid", Major: 1},
		{ID: "fes.video.fixed-720p60", Major: 1},
	}
	return s
}

type stopRecorder struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (r *stopRecorder) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/v1/session/display" {
			writeKitDisplaySession(w, kitDisplaySession())
			return
		}
		if req.URL.Path != "/api/v1/session/stop" {
			http.NotFound(w, req)
			return
		}
		data, _ := io.ReadAll(req.Body)
		body := map[string]any{}
		_ = json.Unmarshal(data, &body)
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"idle"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func (r *stopRecorder) stops() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]any(nil), r.bodies...)
}

// A keyboard alone must stop a session no playing room can open: F12 and Home
// take the Select+Start exit, releasing the kit lease.
func TestKitNoSessionDisplayKeyboardStops(t *testing.T) {
	for _, key := range []struct {
		name  string
		usage uint8
	}{{"F12", hidKeyboardF12}, {"home", 0x4a}} {
		t.Run(key.name, func(t *testing.T) {
			var rec stopRecorder
			app, _ := kitDisplayApp(t, rec.server(t))
			app.mu.Lock()
			app.session = fixedVideoKitSession()
			app.syncGPUParkLocked()
			app.mu.Unlock()
			if snap := app.Snapshot(); snap.Session.HardwareRoom {
				t.Fatalf("fixed-video package offered a playing room: %+v", snap.Session)
			}
			if !app.HandlePlayHIDScancode(key.name, key.usage, true, time.Now()) {
				t.Fatalf("%s was not consumed", key.name)
			}
			waitFor(t, app, key.name+" stop", func(s Snapshot) bool { return len(rec.stops()) == 1 && s.Session.State != "active" })
			if !app.HandlePlayHIDScancode(key.name, key.usage, false, time.Now()) {
				t.Fatalf("%s release was not consumed", key.name)
			}
			snap := app.Snapshot()
			if snap.Room.DuringPlay || snap.Session.ControlsOpen {
				t.Fatalf("%s opened controls instead of stopping: %+v", key.name, snap.Session)
			}
			body := rec.stops()[0]
			expected, _ := body["expected_session"].(map[string]any)
			if retain, ok := body["retain_lease"].(bool); !ok || retain || expected["id"] != "same-host" {
				t.Fatalf("%s stop body = %+v, want the bound Select+Start release", key.name, body)
			}
			if got := len(rec.stops()); got != 1 {
				t.Fatalf("%s posted %d stops", key.name, got)
			}
		})
	}
}

// A package with the session-display plane keeps Home and F12 on the playing
// room; neither stops the machine.
func TestKitSessionDisplayKeyboardOpensRoomNotStop(t *testing.T) {
	for _, key := range []struct {
		name  string
		usage uint8
	}{{"F12", hidKeyboardF12}, {"home", 0x4a}} {
		t.Run(key.name, func(t *testing.T) {
			var rec stopRecorder
			app, _ := kitDisplayApp(t, rec.server(t))
			if !app.HandlePlayHIDScancode(key.name, key.usage, true, time.Now()) {
				t.Fatalf("%s was not consumed", key.name)
			}
			snap := waitFor(t, app, key.name+" playing controls", func(s Snapshot) bool { return s.Session.ControlsOpen })
			if snap.Session.Stopping || snap.Session.State != "active" {
				t.Fatalf("%s stopped instead of opening controls: %+v", key.name, snap.Session)
			}
			if got := len(rec.stops()); got != 0 {
				t.Fatalf("%s posted %d stops", key.name, got)
			}
		})
	}
}

// Transient states never turn Home into Stop: a stop already in flight, and a
// busy session-display transition.
func TestKitNoSessionDisplayHomeIgnoresTransientStates(t *testing.T) {
	var rec stopRecorder
	app, _ := kitDisplayApp(t, rec.server(t))
	app.mu.Lock()
	app.session = fixedVideoKitSession()
	app.sessionDisplayBusy = true
	app.syncGPUParkLocked()
	app.mu.Unlock()
	app.HandlePlayHIDScancode("home", 0x4a, true, time.Now())
	time.Sleep(50 * time.Millisecond)
	if got := len(rec.stops()); got != 0 || app.Snapshot().Session.Stopping {
		t.Fatalf("Home stopped during a display transition: stops=%d", got)
	}
}

// A pad Guide press takes the same Stop on a roomless session, and leaves a
// session with a playing room alone.
func TestKitGuideRoomlessStop(t *testing.T) {
	var rec stopRecorder
	app, _ := kitDisplayApp(t, rec.server(t))
	if app.RequestRoomlessStop(time.Now()) || app.Snapshot().Session.Stopping {
		t.Fatal("Guide stopped a session that has a playing room")
	}
	app.mu.Lock()
	app.session = fixedVideoKitSession()
	app.syncGPUParkLocked()
	app.mu.Unlock()
	if !app.RequestRoomlessStop(time.Now()) {
		t.Fatal("Guide did not stop the roomless session")
	}
	waitFor(t, app, "Guide stop", func(s Snapshot) bool { return len(rec.stops()) == 1 && s.Session.State != "active" })
}
