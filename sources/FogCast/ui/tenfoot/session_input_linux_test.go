//go:build linux

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
	"golang.org/x/sys/unix"
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
	app.HandlePlayHIDScancode("a", nativeHIDUsage(30), true, now)
	app.HandlePlayHIDScancode("q", nativeHIDUsage(16), true, now)
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
	if !app.playKeyHeld[playHeldKey("a", nativeHIDUsage(30))] || app.playKeyHeld[playHeldKey("q", nativeHIDUsage(16))] || app.stopPhase == "stopping" || app.sessionDisplayBusy {
		t.Fatal("disconnect navigated or cleared surviving keyboard ownership")
	}
}

func TestNativeDisconnectReleasesLostHIDKeysByUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	// The lost keyboard holds F1 and left Shift; the other holds keypad 5 and
	// right Shift. Neither pair shares a usage, though F1 and keypad 5 have no
	// play-keyboard name and both Shifts are "shift".
	lost := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{}}
	other := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{}}
	for _, hold := range []struct {
		in   *nativeInput
		code uint16
	}{{lost, 59}, {lost, 42}, {other, 76}, {other, 54}} {
		name := nativeKeyName(hold.code)
		hold.in.keysHeld[hold.code] = name
		app.HandlePlayHIDScancode(name, nativeHIDUsage(hold.code), true, now)
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
		t.Fatalf("disconnect left F1 or left Shift held, or released the other keyboard's keys: %+v", feed.snapshot())
	}
}

func TestNativeKeyboardsReleaseSharedHIDUsageAfterLastHolder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()

	openKeyboard := func() (*nativeInput, *os.File) {
		t.Helper()
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = reader.Close()
			_ = writer.Close()
		})
		fd := int(reader.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, writer
	}
	first, firstWriter := openKeyboard()
	second, secondWriter := openKeyboard()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	now := time.Now()
	usageCode, ok := hidkeys.Code(0x3a) // USB HID F1.
	if !ok {
		t.Fatal("F1 has no HID event code")
	}
	writeKey := func(writer *os.File, value int32) {
		t.Helper()
		if _, err := writer.Write(nativeTestEvent(1, 59, value)); err != nil { // Linux KEY_F1.
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, now); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	releases := func() int {
		t.Helper()
		count := 0
		for _, event := range feed.snapshot() {
			if event.Device == remoteinput.DeviceKeyboard && event.Code == usageCode && event.Action == remoteinput.ActionRelease {
				count++
			}
		}
		return count
	}

	writeKey(firstWriter, 1)
	writeKey(secondWriter, 1)
	writeKey(firstWriter, 0)
	if got := releases(); got != 0 {
		t.Fatalf("first keyboard released F1 while the second still held it: %+v", feed.snapshot())
	}
	writeKey(secondWriter, 0)
	if got := releases(); got != 1 {
		t.Fatalf("last keyboard release count = %d, want 1: %+v", got, feed.snapshot())
	}
}

func TestHandoffSuppressesEachHeldHIDKeyByUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	// F1 and keypad 5 are held through a session-display hand-off. Neither has
	// a play-keyboard name, so only their usages tell them apart.
	app.HandlePlayHIDScancode(nativeKeyName(59), nativeHIDUsage(59), true, now)
	app.HandlePlayHIDScancode(nativeKeyName(76), nativeHIDUsage(76), true, now)
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.suppressHeldPlayKeysLocked()
	app.mu.Unlock()
	app.HandlePlayHIDScancode(nativeKeyName(59), nativeHIDUsage(59), false, now)
	before := len(feed.snapshot())
	if !app.HandlePlayHIDScancode(nativeKeyName(76), nativeHIDUsage(76), true, now) {
		t.Fatal("keypad 5's repeat after F1's release was not consumed")
	}
	drainLocalKeyQueue(t, app)
	if len(feed.snapshot()) != before {
		t.Fatalf("keypad 5 reached the core before a new press: %+v", feed.snapshot()[before:])
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

func TestHeldKeyMatchesItsReleaseAcrossAHIDSessionChange(t *testing.T) {
	for _, pressedHID := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(http.NotFound))
		t.Cleanup(server.Close)
		app, _ := kitDisplayApp(t, server)
		now := time.Now()
		// Enter launches a core and is released after the session changes
		// whether it carries fes.keyboard.hid.
		app.mu.Lock()
		app.session.CoreKeyboardHID = pressedHID
		app.mu.Unlock()
		app.HandlePlayHIDScancode(nativeKeyName(28), nativeHIDUsage(28), true, now)
		app.mu.Lock()
		app.session.CoreKeyboardHID = !pressedHID
		app.mu.Unlock()
		app.HandlePlayHIDScancode(nativeKeyName(28), nativeHIDUsage(28), false, now)
		drainLocalKeyQueue(t, app)
		app.mu.Lock()
		held := len(app.playKeyHeld)
		app.suppressHeldPlayKeysLocked()
		app.session.CoreKeyboardHID = pressedHID
		app.mu.Unlock()
		if held != 0 {
			t.Fatalf("pressed in HID session %v: Enter still held after its release", pressedHID)
		}
		if app.consumeSuppressedPlayKey(nativeKeyName(28), nativeHIDUsage(28), true) {
			t.Fatalf("pressed in HID session %v: the next Enter after a hand-off was swallowed", pressedHID)
		}
	}
}

func TestNativeDisconnectKeepsSharedNameButDropsLostPhysicalKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	// Outside fes.keyboard.hid both Shifts send the core's one shift key.
	lost := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{42: nativeKeyName(42)}}
	other := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{54: nativeKeyName(54)}}
	app.HandlePlayHIDScancode(nativeKeyName(42), nativeHIDUsage(42), true, now)
	app.HandlePlayHIDScancode(nativeKeyName(54), nativeHIDUsage(54), true, now)
	drainLocalKeyQueue(t, app)
	before := len(feed.snapshot())
	inputs := &nativeInputs{devices: []*nativeInput{other}}
	inputs.releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	for _, event := range feed.snapshot()[before:] {
		if event.Device == remoteinput.DeviceKeyboard {
			t.Fatalf("disconnect released shift while right Shift is still held: %+v", event)
		}
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.playKeyHeld[playHeldKey("", nativeHIDUsage(42))] || !app.playKeyHeld[playHeldKey("", nativeHIDUsage(54))] {
		t.Fatalf("disconnect kept left Shift or dropped right Shift: %v", app.playKeyHeld)
	}
}

func TestNativeDisconnectReleasesKeyThatASuppressedKeyShares(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	// Right Shift is held through a hand-off, so the core never saw it. Left
	// Shift on the lost keyboard did reach the core, as its shift key.
	app.HandlePlayHIDScancode(nativeKeyName(54), nativeHIDUsage(54), true, now)
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.releasePlayHIDLocked()
	app.suppressHeldPlayKeysLocked()
	app.mu.Unlock()
	app.HandlePlayHIDScancode(nativeKeyName(42), nativeHIDUsage(42), true, now)
	drainLocalKeyQueue(t, app)
	before := len(feed.snapshot())
	lost := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{42: nativeKeyName(42)}}
	other := &nativeInput{kind: InputKeyboard, keysHeld: map[uint16]string{54: nativeKeyName(54)}}
	inputs := &nativeInputs{devices: []*nativeInput{other}}
	inputs.releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	for _, event := range feed.snapshot()[before:] {
		if event.Device == remoteinput.DeviceKeyboard && event.Action == remoteinput.ActionRelease {
			return
		}
	}
	t.Fatalf("disconnect left the core's shift down behind a suppressed Right Shift: %+v", feed.snapshot()[before:])
}
