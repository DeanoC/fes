//go:build linux

package tenfoot

import (
	"github.com/DeanoC/FogCast/internal/testwait"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
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
	case <-time.After(testwait.Timeout):
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
	otherA := a
	otherA.Player = 1
	app.HandleLocalPad(a, now)
	app.HandleLocalPad(b, now)
	app.HandleLocalPad(otherA, now)
	app.HandlePlayHIDKey("a", true, now)
	app.HandlePlayHIDKey("q", true, now)
	drainLocalKeyQueue(t, app)
	lost := &nativeInput{kind: InputGamepad, player: 0, padHeld: map[padKey]remoteinput.Event{{0, a.Code}: a, {0, b.Code}: b}, keysHeld: map[uint16]string{30: "a", 16: "q"}}
	other := &nativeInput{kind: InputGamepad, player: 1, padHeld: map[padKey]remoteinput.Event{{1, a.Code}: otherA}, keysHeld: map[uint16]string{30: "a"}}
	inputs := &nativeInputs{devices: []*nativeInput{other}}
	inputs.releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	events := feed.snapshot()
	released := func(player uint8, code remoteinput.Code) bool {
		for _, e := range playerEvents(events, player) {
			if e.Code == code && e.Action == remoteinput.ActionRelease && e.Kind == remoteinput.KindButton {
				return true
			}
		}
		return false
	}
	if !released(0, remoteinput.ButtonA) || !released(0, remoteinput.ButtonB) || !feed.saw(zx81keys.Letter('Q'), remoteinput.ActionRelease) ||
		released(1, remoteinput.ButtonA) || feed.saw(zx81keys.Letter('A'), remoteinput.ActionRelease) {
		t.Fatalf("disconnect released a surviving source or left lost input held: %+v", events)
	}
	// The port goes back after the lost pad's releases; keyboard releases
	// queue separately and may follow it.
	departed := false
	for _, e := range events {
		if remoteinput.IsLocalPlayerDeparture(e) && e.Player == 0 {
			departed = true
		} else if departed && e.Device == remoteinput.DeviceGamepad && e.Player == 0 {
			t.Fatalf("pad release after its departure: %+v", events)
		}
	}
	if !departed {
		t.Fatalf("lost pad did not give its port back: %+v", events)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.playKeyHeld["a"] || app.playKeyHeld["q"] || app.stopPhase == "stopping" || app.sessionDisplayBusy {
		t.Fatal("disconnect navigated or cleared surviving keyboard ownership")
	}
}

func TestNativeDisconnectReleasesLostHIDKeysByUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboard = false
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	lost := &nativeInput{fd: 1, kind: InputKeyboard}
	other := &nativeInput{fd: 2, kind: InputKeyboard}
	// F1 and left Shift have no name or share a name with the other keyboard's
	// keypad 5 and right Shift. Disconnect must release only the lost usages.
	for _, hold := range []struct {
		in   *nativeInput
		code uint16
	}{{lost, 59}, {lost, 42}, {other, 76}, {other, 54}} {
		if hold.in.key(app, hold.code, 1, now) {
			t.Fatalf("key %d requested Stop", hold.code)
		}
	}
	drainLocalKeyQueue(t, app)
	inputs := &nativeInputs{devices: []*nativeInput{other}}
	inputs.releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	released := func(usage uint8) bool {
		code, _ := hidkeys.Code(usage)
		return feed.saw(code, remoteinput.ActionRelease)
	}
	if !released(0x3a) || !released(0xe1) || released(0x5d) || released(0xe5) {
		t.Fatalf("disconnect left lost keys held or released another keyboard's keys: %+v", feed.snapshot())
	}
}

func TestHandoffSuppressesEachHeldHIDKeyByUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboard = false
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	in := &nativeInput{fd: 1, kind: InputKeyboard}
	// Neither key has a play-keyboard name, so their usages distinguish the
	// held keys across a session-display hand-off.
	for _, code := range []uint16{59, 76} { // F1 and keypad 5
		if in.key(app, code, 1, now) {
			t.Fatalf("key %d requested Stop", code)
		}
	}
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.suppressHeldPlayKeysLocked()
	app.mu.Unlock()
	in.key(app, 59, 0, now)
	before := len(feed.snapshot())
	in.key(app, 76, 2, now)
	drainLocalKeyQueue(t, app)
	if len(feed.snapshot()) != before {
		t.Fatalf("keypad 5 repeat reached the core after F1 was released: %+v", feed.snapshot()[before:])
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
	waitFor(t, app, "keyboard room", func(s Snapshot) bool { return s.Session.ControlsOpen })
	if !app.HandlePlayHIDKey("a", true, now) {
		t.Fatal("held gameplay key became navigation")
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "keyboard return", func(s Snapshot) bool { return !s.Session.ControlsOpen && app.ForwardsPlayHID() })
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
