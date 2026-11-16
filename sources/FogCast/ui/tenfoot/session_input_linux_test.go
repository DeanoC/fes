//go:build linux

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
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
	app.handlePlayHIDScancode(100, "a", nativeHIDUsage(30), true, now)
	app.handlePlayHIDScancode(101, "a", nativeHIDUsage(30), true, now)
	app.handlePlayHIDScancode(100, "q", nativeHIDUsage(16), true, now)
	drainLocalKeyQueue(t, app)
	lost := &nativeInput{fd: 100, kind: InputGamepad, player: 0, padHeld: map[padKey]remoteinput.Event{{0, a.Code}: a, {0, b.Code}: b}, keysHeld: map[uint16]string{30: "a", 16: "q"}}
	other := &nativeInput{fd: 101, kind: InputGamepad, player: 1, padHeld: map[padKey]remoteinput.Event{{1, a.Code}: otherA}, keysHeld: map[uint16]string{30: "a"}}
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
	if !app.playKeyHeld[physicalPlayKey{source: 101, key: playHeldKey("a", nativeHIDUsage(30))}] || app.playKeyHeld[physicalPlayKey{source: 100, key: playHeldKey("q", nativeHIDUsage(16))}] || app.stopPhase == "stopping" || app.sessionDisplayBusy {
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
	lost := &nativeInput{fd: 100, kind: InputKeyboard, keysHeld: map[uint16]string{}}
	other := &nativeInput{fd: 101, kind: InputKeyboard, keysHeld: map[uint16]string{}}
	for _, hold := range []struct {
		in   *nativeInput
		code uint16
	}{{lost, 59}, {lost, 42}, {other, 76}, {other, 54}} {
		name := nativeKeyName(hold.code)
		hold.in.keysHeld[hold.code] = name
		app.handlePlayHIDScancode(hold.in.fd, name, nativeHIDUsage(hold.code), true, now)
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
	writeKey := func(writer *os.File, code uint16, value int32) {
		t.Helper()
		if _, err := writer.Write(nativeTestEvent(1, code, value)); err != nil {
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

	writeKey(firstWriter, 59, 1) // Linux KEY_F1.
	writeKey(secondWriter, 59, 1)
	writeKey(firstWriter, 59, 0)
	if got := releases(); got != 0 {
		t.Fatalf("first keyboard released F1 while the second still held it: %+v", feed.snapshot())
	}
	writeKey(secondWriter, 59, 0)
	if got := releases(); got != 1 {
		t.Fatalf("last keyboard release count = %d, want 1: %+v", got, feed.snapshot())
	}

	writeKey(firstWriter, 42, 1) // Linux KEY_LEFTSHIFT.
	writeKey(secondWriter, 42, 1)
	writeKey(firstWriter, 42, 0)
	writeKey(secondWriter, 42, 0)
	app.mu.Lock()
	app.session.State = "idle"
	app.session.CoreKeyboardHID = false
	app.session.Input = nil
	app.mu.Unlock()
	app.HandleCommand(CmdSearch, now)
	writeKey(firstWriter, 2, 1) // Linux KEY_1 must not inherit either Shift hold.
	if got := app.Snapshot().OSK.Buffer; got != "1" {
		t.Fatalf("menu text after shared Shift releases = %q, want 1", got)
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
	lost := &nativeInput{fd: 100, kind: InputKeyboard, keysHeld: map[uint16]string{42: nativeKeyName(42)}}
	other := &nativeInput{fd: 101, kind: InputKeyboard, keysHeld: map[uint16]string{54: nativeKeyName(54)}}
	app.handlePlayHIDScancode(100, nativeKeyName(42), nativeHIDUsage(42), true, now)
	app.handlePlayHIDScancode(101, nativeKeyName(54), nativeHIDUsage(54), true, now)
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
	if app.playKeyHeld[physicalPlayKey{source: 100, key: playHeldKey("", nativeHIDUsage(42))}] || !app.playKeyHeld[physicalPlayKey{source: 101, key: playHeldKey("", nativeHIDUsage(54))}] {
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
	app.handlePlayHIDScancode(101, nativeKeyName(54), nativeHIDUsage(54), true, now)
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.releasePlayHIDLocked()
	app.suppressHeldPlayKeysLocked()
	app.mu.Unlock()
	app.handlePlayHIDScancode(100, nativeKeyName(42), nativeHIDUsage(42), true, now)
	drainLocalKeyQueue(t, app)
	before := len(feed.snapshot())
	lost := &nativeInput{fd: 100, kind: InputKeyboard, keysHeld: map[uint16]string{42: nativeKeyName(42)}}
	other := &nativeInput{fd: 101, kind: InputKeyboard, keysHeld: map[uint16]string{54: nativeKeyName(54)}}
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

func TestNativeKeyboardsKeepSuppressionPerDeviceDuringSurroundTransition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	open := func() (*nativeInput, *os.File) {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		fd := int(r.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, w
	}
	first, a := open()
	second, b := open()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	now := time.Now()
	send := func(w *os.File, code uint16, value int32) {
		t.Helper()
		if _, err := w.Write(nativeTestEvent(1, code, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, now); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	count := func(usage uint8, action remoteinput.Action) int {
		t.Helper()
		code, ok := hidkeys.Code(usage)
		if !ok {
			t.Fatalf("missing HID usage %#x", usage)
		}
		n := 0
		for _, e := range feed.snapshot() {
			if e.Device == remoteinput.DeviceKeyboard && e.Code == code && e.Action == action {
				n++
			}
		}
		return n
	}
	// The machine retains focus while the surround is shown/hidden. One
	// keyboard holds F1 throughout; the second starts holding it while the
	// display transition is busy and stays suppressed until its own release.
	send(a, 59, 1)
	app.mu.Lock()
	app.sessionDisplayBusy = true
	app.mu.Unlock()
	send(b, 59, 1)
	app.mu.Lock()
	app.sessionDisplayBusy = false
	app.mu.Unlock()
	send(b, 59, 2)
	if got := count(0x3a, remoteinput.ActionPress); got != 1 {
		t.Fatalf("suppressed F1 reached core: %d presses, events=%+v", got, feed.snapshot())
	}
	send(a, 59, 0)
	if got := count(0x3a, remoteinput.ActionRelease); got != 1 {
		t.Fatalf("suppressed other keyboard stranded F1: %d releases, events=%+v", got, feed.snapshot())
	}
	send(b, 59, 0)
	if got := count(0x3a, remoteinput.ActionRelease); got != 1 {
		t.Fatalf("suppressed release reached the core: %d", got)
	}
	send(b, 59, 1)
	if got := count(0x3a, remoteinput.ActionPress); got != 2 {
		t.Fatalf("fresh F1 after physical release was lost: %d", got)
	}
	send(b, 59, 0)
	if got := count(0x3a, remoteinput.ActionRelease); got != 2 {
		t.Fatalf("fresh F1 release count = %d", got)
	}

	// Reverse the release order for keypad 5 and Shift. A suppressed
	// keyboard must clear its own hold even when another one still drives
	// the same core usage, and a later press from it must work normally.
	for _, tc := range []struct {
		code  uint16
		usage uint8
	}{{76, 0x5d}, {42, 0xe1}} {
		send(a, tc.code, 1)
		app.mu.Lock()
		app.sessionDisplayBusy = true
		app.mu.Unlock()
		send(b, tc.code, 1)
		app.mu.Lock()
		app.sessionDisplayBusy = false
		app.mu.Unlock()
		send(b, tc.code, 0)
		if got := count(tc.usage, remoteinput.ActionRelease); got != 0 {
			t.Fatalf("suppressed release for %#x reached core: %d", tc.usage, got)
		}
		send(a, tc.code, 0)
		if got := count(tc.usage, remoteinput.ActionRelease); got != 1 {
			t.Fatalf("last unsuppressed release for %#x = %d", tc.usage, got)
		}
		send(b, tc.code, 1)
		send(b, tc.code, 0)
		if got := count(tc.usage, remoteinput.ActionPress); got != 2 {
			t.Fatalf("fresh press for %#x = %d", tc.usage, got)
		}
		if got := count(tc.usage, remoteinput.ActionRelease); got != 2 {
			t.Fatalf("fresh release for %#x = %d", tc.usage, got)
		}
	}

	// The shared release path must clear a previously armed menu command
	// while retaining the second keyboard's core hold.
	send(a, 103, 1) // Up
	send(b, 103, 1)
	first.commands = map[uint16]Command{103: CmdUp}
	app.repeat.Arm(CmdUp, now)
	send(a, 103, 0)
	if app.repeat.Held() != CmdNone || len(first.commands) != 0 {
		t.Fatalf("first keyboard's menu command remained armed: %+v", first.commands)
	}
	if got := count(0x52, remoteinput.ActionRelease); got != 0 {
		t.Fatalf("shared Up released early: %d", got)
	}
	send(b, 103, 0)
	if got := count(0x52, remoteinput.ActionRelease); got != 1 {
		t.Fatalf("last Up release count = %d", got)
	}

	// An evdev hold that was never admitted to the app (for example while
	// host preparation blocked input) cannot keep the core key down.
	send(a, 59, 1)
	second.keysHeld[59] = nativeKeyName(59)
	send(a, 59, 0)
	if got := count(0x3a, remoteinput.ActionRelease); got != 3 {
		t.Fatalf("untracked peer stranded F1: %d", got)
	}
	delete(second.keysHeld, 59)

	// A suppressed surviving device cannot strand a core key when the
	// unsuppressed keyboard disconnects.
	send(a, 59, 1)
	app.mu.Lock()
	app.sessionDisplayBusy = true
	app.mu.Unlock()
	send(b, 59, 1)
	app.mu.Lock()
	app.sessionDisplayBusy = false
	app.mu.Unlock()
	inputs.devices = []*nativeInput{second}
	inputs.releaseLostPlayInput(app, first)
	drainLocalKeyQueue(t, app)
	if got := count(0x3a, remoteinput.ActionRelease); got != 4 {
		t.Fatalf("disconnect left F1 held behind a suppressed peer: %d", got)
	}
	send(b, 59, 0)
	send(b, 59, 1)
	send(b, 59, 0)
	if got := count(0x3a, remoteinput.ActionRelease); got != 5 {
		t.Fatalf("surviving keyboard did not recover after disconnect: %d", got)
	}
}

func TestNativeKeyboardReleaseDuringActualNonHandoffSurroundTransition(t *testing.T) {
	entered := make(chan struct{})
	allow := make(chan struct{})
	var allowOnce sync.Once
	unblock := func() { allowOnce.Do(func() { close(allow) }) }
	var prior hostclient.SessionResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		if got := readSessionDisplay(t, r); got == protocol.SessionDisplayShown {
			close(entered)
			select {
			case <-allow:
			case <-r.Context().Done():
				return
			}
		} else if got != protocol.SessionDisplayClosed {
			t.Errorf("got display %+v", got)
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	t.Cleanup(unblock)
	app.mu.Lock()
	app.frameTrace = newFrameTrace()
	app.frameTrace.armKeyRouting(time.Now())
	app.session.CoreKeyboardHID = true
	prior = app.session
	app.mu.Unlock()
	open := func() (*nativeInput, *os.File) {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		fd := int(r.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, w
	}
	first, a := open()
	second, b := open()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	now := time.Now()
	send := func(w *os.File, code uint16, value int32) {
		t.Helper()
		if _, err := w.Write(nativeTestEvent(1, code, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, now); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	code, _ := hidkeys.Code(0x3a)
	count := func(action remoteinput.Action) int {
		n := 0
		for _, e := range feed.snapshot() {
			if e.Device == remoteinput.DeviceKeyboard && e.Code == code && e.Action == action {
				n++
			}
		}
		return n
	}
	send(a, 59, 1)
	app.mu.Lock()
	app.beginSessionDisplayLocked(protocol.SessionDisplayShown, false)
	app.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(testwait.Timeout):
		t.Fatal("display request did not start")
	}
	send(a, 59, 2) // existing admitted key repeats during non-handoff
	send(b, 59, 1) // same key newly held during the transition is suppressed
	send(a, 59, 0) // core must receive its neutral release before transition ends
	if count(remoteinput.ActionPress) != 1 || count(remoteinput.ActionRelease) != 1 {
		t.Fatalf("non-handoff transition lost neutral release or forwarded a blocked press: %+v", feed.snapshot())
	}
	unblock()
	waitFor(t, app, "surround transition", func(Snapshot) bool { app.mu.Lock(); defer app.mu.Unlock(); return !app.sessionDisplayBusy })
	send(b, 59, 0)
	if count(remoteinput.ActionRelease) != 1 {
		t.Fatalf("suppressed B release reached core: %+v", feed.snapshot())
	}
	var routes []string
	var trace []frameTraceRecord
	for _, record := range app.frameTrace.records() {
		if record.Kind == "key-route" {
			routes = append(routes, record.Route)
			trace = append(trace, record)
		}
	}
	want := []string{"admitted", "display-begin", "suppressed", "transition-release", "display-end"}
	if !slices.Equal(routes, want) {
		t.Fatalf("routing trace = %v, want %v", routes, want)
	}
	if trace[0].Source != first.fd || trace[2].Source != second.fd || trace[3].Source != first.fd ||
		trace[1].Epoch == 0 || trace[2].Epoch != trace[1].Epoch || trace[3].Epoch != trace[1].Epoch || trace[4].Epoch != trace[1].Epoch ||
		!trace[1].NonHandoff || !trace[2].NonHandoff || !trace[3].NonHandoff || !trace[4].NonHandoff {
		t.Fatalf("routing source or transition binding is wrong: %+v", trace)
	}
}

func TestNativeKeyboardsCoalesceOnlyAdmittedLegacyKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	open := func() (*nativeInput, *os.File) {
		t.Helper()
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		fd := int(r.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, w
	}
	first, a := open()
	second, b := open()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	now := time.Now()
	send := func(w *os.File, code uint16, value int32) {
		t.Helper()
		if _, err := w.Write(nativeTestEvent(1, code, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, now); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	legacy, ok := playhid.Event("shift", false, true)
	if !ok {
		t.Fatal("legacy shift mapping absent")
	}
	releases := func() int {
		n := 0
		for _, e := range feed.snapshot() {
			if e.Device == legacy.Device && e.Code == legacy.Code && e.Action == remoteinput.ActionRelease {
				n++
			}
		}
		return n
	}
	// Left and right Shift are distinct physical usages but the legacy
	// keyboard maps both to one core key.
	send(a, 42, 1)
	send(b, 54, 1)
	send(a, 42, 0)
	if releases() != 0 {
		t.Fatalf("legacy Shift released with peer held: %+v", feed.snapshot())
	}
	send(b, 54, 0)
	if releases() != 1 {
		t.Fatalf("legacy Shift final release missing: %+v", feed.snapshot())
	}
	send(a, 42, 1)
	send(b, 54, 1)
	send(b, 54, 0)
	if releases() != 1 {
		t.Fatalf("legacy Shift released in reverse order: %+v", feed.snapshot())
	}
	send(a, 42, 0)
	if releases() != 2 {
		t.Fatalf("legacy Shift reverse final release missing: %+v", feed.snapshot())
	}
	// The same keyboard can hold both physical Shift keys, which map to one
	// legacy core key. Releasing either one must preserve the other hold.
	send(a, 42, 1)
	send(a, 54, 1)
	send(a, 42, 0)
	if releases() != 2 {
		t.Fatalf("legacy Shift released while the same keyboard still held right Shift: %+v", feed.snapshot())
	}
	send(a, 54, 0)
	if releases() != 3 {
		t.Fatalf("same-keyboard legacy Shift final release missing: %+v", feed.snapshot())
	}
	send(a, 42, 1)
	send(a, 54, 1)
	send(a, 54, 0)
	if releases() != 3 {
		t.Fatalf("legacy Shift released while the same keyboard still held left Shift: %+v", feed.snapshot())
	}
	send(a, 42, 0)
	if releases() != 4 {
		t.Fatalf("same-keyboard legacy Shift reverse final release missing: %+v", feed.snapshot())
	}

	// If the keyboard capability observation changes before the physical
	// release, neutralize the exact key admitted on the earlier route.
	send(a, 42, 1)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	send(a, 42, 0)
	if releases() != 5 {
		t.Fatalf("release did not use the admitted legacy key: %+v", feed.snapshot())
	}
	hidShift, _ := hidkeys.Code(0xe1)
	for _, e := range feed.snapshot() {
		if e.Device == remoteinput.DeviceKeyboard && e.Code == hidShift && e.Action == remoteinput.ActionRelease {
			t.Fatalf("legacy hold released a different HID key: %+v", feed.snapshot())
		}
	}

	// An evdev repeat that arrives after the mode change must not re-admit
	// the held key under the new mapping: the eventual key-up still has to
	// release the legacy key first admitted, and no HID press is forwarded.
	app.mu.Lock()
	app.session.CoreKeyboardHID = false
	app.mu.Unlock()
	send(a, 42, 1)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	send(a, 42, 2)
	send(a, 42, 0)
	if releases() != 6 {
		t.Fatalf("repeat after a mode change replaced the admitted legacy key: %+v", feed.snapshot())
	}
	for _, e := range feed.snapshot() {
		if e.Device == remoteinput.DeviceKeyboard && e.Code == hidShift {
			t.Fatalf("repeat forwarded the held key under a different mapping: %+v", feed.snapshot())
		}
	}

	// A failed press admission never contributes a core hold, even though
	// evdev correctly records that keyboard as physically held.
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	send(a, 59, 1)
	app.mu.Lock()
	app.playHIDPending = maxPendingPlayHID
	app.mu.Unlock()
	send(b, 59, 1)
	app.mu.Lock()
	app.playHIDPending = 0
	app.mu.Unlock()
	send(a, 59, 0)
	f1, _ := hidkeys.Code(0x3a)
	for _, e := range feed.snapshot() {
		if e.Device == remoteinput.DeviceKeyboard && e.Code == f1 && e.Action == remoteinput.ActionRelease {
			return
		}
	}
	t.Fatalf("rejected peer press stranded F1: %+v", feed.snapshot())
}

func TestPendingNeutralReleaseWaitsForOriginalRouteAndDoesNotCrossSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	usage := uint8(0x3a)
	code, _ := hidkeys.Code(usage)
	countReleases := func() int {
		n := 0
		for _, e := range feed.snapshot() {
			if e.Device == remoteinput.DeviceKeyboard && e.Code == code && e.Action == remoteinput.ActionRelease {
				n++
			}
		}
		return n
	}
	app.handlePlayHIDScancode(123, "", usage, true, now)
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.sessionDisplayBinding = app.session
	app.sessionDisplayBusy = true
	app.sessionDisplayNonHandoff = true
	app.kitLeaseHave = true
	app.kitLease.ErrorCode = "KIT_LEASE_BUSY"
	app.mu.Unlock()
	app.handlePlayHIDScancode(123, "", usage, false, now)
	drainLocalKeyQueue(t, app)
	if countReleases() != 0 {
		t.Fatalf("foreign lease received a release: %+v", feed.snapshot())
	}
	app.mu.Lock()
	if len(app.playKeyPendingRelease) != 1 {
		t.Fatal("refused neutral release was not retained")
	}
	app.sessionDisplayBusy = false
	app.sessionDisplayNonHandoff = false
	app.kitLeaseHave = false
	app.mu.Unlock()
	app.Tick(now)
	drainLocalKeyQueue(t, app)
	if countReleases() != 1 {
		t.Fatalf("original route did not receive its pending release: %+v", feed.snapshot())
	}

	// A pending release belongs to the old session and must never leak into
	// a replacement when the route becomes available again.
	app.handlePlayHIDScancode(123, "", usage, true, now)
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.sessionDisplayBinding = app.session
	app.sessionDisplayBusy = true
	app.sessionDisplayNonHandoff = true
	app.kitLeaseHave = true
	app.kitLease.ErrorCode = "KIT_LEASE_BUSY"
	app.mu.Unlock()
	app.handlePlayHIDScancode(123, "", usage, false, now)
	app.mu.Lock()
	app.cancelPlayHIDLocked()
	app.sessionDisplayBusy = false
	app.sessionDisplayNonHandoff = false
	app.kitLeaseHave = false
	app.session.CorePackage.Generation++
	app.mu.Unlock()
	app.Tick(now)
	drainLocalKeyQueue(t, app)
	if countReleases() != 1 {
		t.Fatalf("old session release reached replacement: %+v", feed.snapshot())
	}
}

func TestKeyboardDisconnectDuringForeignLeaseDefersNeutralRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()
	now := time.Now()
	const source = 123
	app.handlePlayHIDScancode(source, "", 0x3a, true, now)
	drainLocalKeyQueue(t, app)
	lost := &nativeInput{fd: source, kind: InputKeyboard, keysHeld: map[uint16]string{59: nativeKeyName(59)}}
	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease.ErrorCode = "KIT_LEASE_BUSY"
	app.mu.Unlock()
	(&nativeInputs{}).releaseLostPlayInput(app, lost)
	drainLocalKeyQueue(t, app)
	code, _ := hidkeys.Code(0x3a)
	count := func() int {
		n := 0
		for _, e := range feed.snapshot() {
			if e.Device == remoteinput.DeviceKeyboard && e.Code == code && e.Action == remoteinput.ActionRelease {
				n++
			}
		}
		return n
	}
	if count() != 0 {
		t.Fatalf("disconnected release crossed foreign lease: %+v", feed.snapshot())
	}
	app.mu.Lock()
	if len(app.playKeyPendingRelease) != 1 {
		t.Fatal("disconnected release was not retained")
	}
	app.kitLeaseHave = false
	app.mu.Unlock()
	app.Tick(now)
	drainLocalKeyQueue(t, app)
	if count() != 1 {
		t.Fatalf("disconnected release did not recover: %+v", feed.snapshot())
	}
}
