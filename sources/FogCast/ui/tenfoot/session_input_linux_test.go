//go:build linux

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func drainLocalKeyQueue(t *testing.T, app *App) {
	t.Helper()
	app.mu.Lock()
	tail := app.playHIDTail
	app.mu.Unlock()
	if tail == nil {
		return
	}
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("local keyboard queue did not drain")
	}
}

func TestNativeDisconnectReleasesOnlyLostDeviceHolds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	press := func(code remoteinput.Code) remoteinput.Event {
		return remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Code: code, Action: remoteinput.ActionPress}
	}
	a, b := press(remoteinput.ButtonA), press(remoteinput.ButtonB)
	app.HandleLocalPad(a, now)
	app.HandleLocalPad(b, now)
	app.HandlePlayHIDKey("a", true, now)
	app.HandlePlayHIDKey("q", true, now)
	drainLocalKeyQueue(t, app)
	lost := &nativeInput{padHeld: map[remoteinput.Code]remoteinput.Event{a.Code: a, b.Code: b}, keysHeld: map[uint16]string{30: "a", 16: "q"}}
	other := &nativeInput{padHeld: map[remoteinput.Code]remoteinput.Event{a.Code: a}, keysHeld: map[uint16]string{30: "a"}}
	inputs := &nativeInputs{devices: []*nativeInput{other}}
	inputs.releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	if !feed.saw(remoteinput.ButtonB, remoteinput.ActionRelease) || !feed.saw(zx81keys.Letter('Q'), remoteinput.ActionRelease) ||
		feed.saw(remoteinput.ButtonA, remoteinput.ActionRelease) || feed.saw(zx81keys.Letter('A'), remoteinput.ActionRelease) {
		t.Fatalf("disconnect released a surviving source or left lost keys held: %+v", feed.snapshot())
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.playKeyHeld["a"] || app.playKeyHeld["q"] || app.stopPhase == "stopping" || app.sessionDisplayBusy {
		t.Fatal("disconnect navigated or cleared surviving keyboard ownership")
	}
}

func TestKitKeyboardIsNeutralBeforeOpeningAndNeedsNewPress(t *testing.T) {
	prior := kitDisplaySession()
	var feed *transitionPadFeed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		if !feed.saw(zx81keys.Letter('A'), remoteinput.ActionRelease) || feed.closes.Load() == 0 {
			t.Error("HDMI opened before keyboard neutral and source disconnect")
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, source := kitDisplayApp(t, server)
	feed = source
	now := time.Now()
	app.HandlePlayHIDKey("a", true, now)
	app.HandlePlayHIDKey("home", true, now)
	waitFor(t, app, "keyboard room", func(s Snapshot) bool { return s.Room.DuringPlay })
	if !app.HandlePlayHIDKey("a", true, now) {
		t.Fatal("held gameplay key became navigation")
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "keyboard return", func(s Snapshot) bool { return !s.Room.DuringPlay && app.ForwardsPlayHID() })
	before := len(feed.snapshot())
	app.HandlePlayHIDKey("a", true, now)
	drainLocalKeyQueue(t, app)
	if len(feed.snapshot()) != before {
		t.Fatal("held key replayed after returning to machine")
	}
	app.HandlePlayHIDKey("a", false, now)
	app.HandlePlayHIDKey("a", true, now)
	drainLocalKeyQueue(t, app)
	if len(feed.snapshot()) != before+1 {
		t.Fatal("fresh keyboard press did not reach the same machine")
	}
}
