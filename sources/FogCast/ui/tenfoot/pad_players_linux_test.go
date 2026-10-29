//go:build linux

package tenfoot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"golang.org/x/sys/unix"
)

// pipePad links an evdev-style node at dir/name to a pipe, which pipeKind
// classifies as a gamepad in place of EVIOCGBIT.
func pipePad(t *testing.T, dir, name string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	path := filepath.Join(dir, name)
	if err := os.Symlink(fdPath(int(r.Fd())), path); err != nil {
		t.Fatal(err)
	}
	return path
}

func pipeKind(fd int) InputKind {
	link, err := os.Readlink(fdPath(fd))
	if err == nil && strings.HasPrefix(link, "pipe:") {
		return InputGamepad
	}
	return InputNone
}

func runningLocalCoreApp(feed localPadSender) *App {
	a := NewApp(nil, 1280, 720, 10)
	a.mu.Lock()
	a.localPhase = localPhaseRunning
	a.localFeed = feed
	a.mu.Unlock()
	return a
}

// Ports follow connection order and stay with a pad. A third pad is menu-only
// and does not take a port that frees up later; the next pad opened does.
func TestAutoInputGivesPadsPortsInConnectionOrder(t *testing.T) {
	dir := t.TempDir()
	inputs, err := openNativeInputsDir("auto", "menu-display", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer inputs.close()
	inputs.kindOf = pipeKind
	app := NewApp(nil, 1280, 720, 10)
	for i := 0; i < 3; i++ {
		if err := inputs.openPath(app, pipePad(t, dir, fmt.Sprintf("event%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	players := func() []int8 {
		var out []int8
		for _, in := range inputs.devices {
			out = append(out, in.player)
		}
		return out
	}
	if got := players(); !reflect.DeepEqual(got, []int8{0, 1, noLocalPlayer}) {
		t.Fatalf("ports = %v", got)
	}
	inputs.drop(app, inputs.devices[0])
	if err := inputs.openPath(app, pipePad(t, dir, "event3")); err != nil {
		t.Fatal(err)
	}
	if got := players(); !reflect.DeepEqual(got, []int8{1, noLocalPlayer, 0}) {
		t.Fatalf("ports after unplugging the first pad = %v", got)
	}
}

// fixturePadInputs opens pipe pads in dir. A node whose name starts
// "fixture" reports the kit fixture's 081f:e401; any other reports a
// DualSense's 054c:0ce6.
func fixturePadInputs(t *testing.T, dir string) *nativeInputs {
	t.Helper()
	inputs, err := openNativeInputsDir("auto", "menu-display", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inputs.close)
	inputs.kindOf = pipeKind
	names := map[int]string{}
	inputs.openFile = func(path string) (int, error) {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			names[fd] = filepath.Base(path)
		}
		return fd, err
	}
	inputs.padID = func(fd int) (uint16, uint16) {
		if strings.HasPrefix(names[fd], "fixture") {
			return 0x081f, 0xe401
		}
		return 0x054c, 0x0ce6
	}
	return inputs
}

func padPorts(inputs *nativeInputs) map[string]int8 {
	out := map[string]int8{}
	for _, in := range inputs.devices {
		out[filepath.Base(in.path)] = in.player
	}
	return out
}

// The fixture pad opened first gives port 1 to the DualSense that follows,
// releasing what it held there, and moves to port 2. A third pad takes the
// fixture's port 2 and leaves it menu only.
func TestFixturePadYieldsItsPort(t *testing.T) {
	dir := t.TempDir()
	inputs := fixturePadInputs(t, dir)
	feed := &localPadRecorder{}
	app := runningLocalCoreApp(feed)
	if err := inputs.openPath(app, pipePad(t, dir, "fixture0")); err != nil {
		t.Fatal(err)
	}
	fixture := inputs.devices[0]
	if fixture.player != 0 {
		t.Fatalf("lone fixture port = %d", fixture.player)
	}
	fixture.button(app, ButtonSouth, 1, time.Now())
	feed.events = nil

	if err := inputs.openPath(app, pipePad(t, dir, "event0")); err != nil {
		t.Fatal(err)
	}
	if got := padPorts(inputs); !reflect.DeepEqual(got, map[string]int8{"fixture0": 1, "event0": 0}) {
		t.Fatalf("ports after DualSense = %v", got)
	}
	release := remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionRelease, Code: remoteinput.ButtonA}
	if len(feed.events) != 2 || feed.events[0] != release || !remoteinput.IsLocalPlayerDeparture(feed.events[1]) || feed.events[1].Player != 0 {
		t.Fatalf("yield events: %+v", feed.events)
	}
	if len(fixture.padHeld) != 0 {
		t.Fatalf("fixture kept port 1 holds: %v", fixture.padHeld)
	}

	if err := inputs.openPath(app, pipePad(t, dir, "event1")); err != nil {
		t.Fatal(err)
	}
	if got := padPorts(inputs); !reflect.DeepEqual(got, map[string]int8{"fixture0": noLocalPlayer, "event0": 0, "event1": 1}) {
		t.Fatalf("ports after second pad = %v", got)
	}
}

// A fixture that arrives after another pad takes the next free port, and
// fixtures do not take ports from each other.
func TestFixturePadArrivingLaterKeepsConnectionOrder(t *testing.T) {
	dir := t.TempDir()
	inputs := fixturePadInputs(t, dir)
	app := NewApp(nil, 1280, 720, 10)
	for _, name := range []string{"event0", "fixture0", "fixture1"} {
		if err := inputs.openPath(app, pipePad(t, dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if got := padPorts(inputs); !reflect.DeepEqual(got, map[string]int8{"event0": 0, "fixture0": 1, "fixture1": noLocalPlayer}) {
		t.Fatalf("ports = %v", got)
	}
}

func TestSecondPadForwardsAsPlayerTwo(t *testing.T) {
	feed := &localPadRecorder{}
	a := runningLocalCoreApp(feed)
	in := &nativeInput{fd: 9, kind: InputGamepad, player: 1}
	now := time.Now()
	in.button(a, ButtonSouth, 1, now)
	in.hat(a, 17, 1, now)
	if len(feed.events) != 2 || feed.events[0].Player != 1 || feed.events[0].Code != remoteinput.ButtonA ||
		feed.events[1].Player != 1 || feed.events[1].Code != remoteinput.ButtonDPadDown {
		t.Fatalf("second pad events: %+v", feed.events)
	}
	if _, held := in.padHeld[padKey{1, remoteinput.ButtonA}]; !held {
		t.Fatalf("held state not keyed by pad: %v", in.padHeld)
	}
}

// Unplugging pad 2 releases what pad 2 held and gives back its port. Pad 1's
// buttons and chord state are untouched.
func TestUnpluggedPadReleasesOnlyItsPort(t *testing.T) {
	feed := &localPadRecorder{}
	a := runningLocalCoreApp(feed)
	a.mu.Lock()
	a.localPair.set(0, remoteinput.ButtonSelect, true)
	a.localPair.set(1, remoteinput.ButtonSelect, true)
	a.localPair.set(1, remoteinput.ButtonStart, true)
	a.localChordSince = time.Now()
	a.localSent = map[padKey]bool{{0, remoteinput.ButtonSelect}: true, {1, remoteinput.ButtonSelect}: true}
	a.mu.Unlock()
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fds[0]) })
	held := func(player uint8) map[padKey]remoteinput.Event {
		e := padButtonFor(player, remoteinput.ButtonA, true)
		return map[padKey]remoteinput.Event{{player, e.Code}: e}
	}
	first := &nativeInput{fd: fds[0], kind: InputGamepad, player: 0, padHeld: held(0)}
	second := &nativeInput{fd: fds[1], kind: InputGamepad, player: 1, padHeld: held(1)}
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	inputs.drop(a, second)
	release := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionRelease, Code: remoteinput.ButtonA}
	if len(feed.events) != 2 || feed.events[0] != release || !remoteinput.IsLocalPlayerDeparture(feed.events[1]) || feed.events[1].Player != 1 {
		t.Fatalf("unplug events: %+v", feed.events)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.localPair.at(1) != (chordPair{}) || !a.localPair.at(0).selectDown || !a.localChordSince.IsZero() {
		t.Fatalf("unplug left pad 2 chord state or cleared pad 1: %+v since=%v", a.localPair, a.localChordSince)
	}
	if a.localSent[padKey{1, remoteinput.ButtonSelect}] || !a.localSent[padKey{0, remoteinput.ButtonSelect}] {
		t.Fatalf("sent state after unplug: %v", a.localSent)
	}
	if len(inputs.devices) != 1 || inputs.devices[0] != first || inputs.freePlayer() != 1 {
		t.Fatal("port 2 was not freed")
	}
}

// A pad that found both ports taken drives the menu but is ignored while a
// machine owns the pads, and never reaches the hidden menu underneath.
func TestPortlessPadIsMenuOnly(t *testing.T) {
	feed := &localPadRecorder{}
	a := runningLocalCoreApp(feed)
	in := &nativeInput{fd: 9, kind: InputGamepad, player: noLocalPlayer}
	now := time.Now()
	in.button(a, ButtonSouth, 1, now)
	in.hat(a, 17, 1, now)
	if len(feed.events) != 0 || len(in.controls) != 0 || len(in.padHeld) != 0 {
		t.Fatalf("portless pad reached the machine or the hidden menu: feed=%+v controls=%v", feed.events, in.controls)
	}
	a.mu.Lock()
	a.localPhase = ""
	a.mu.Unlock()
	in.button(a, ButtonSouth, 1, now)
	if len(feed.events) != 0 || len(in.controls) != 1 {
		t.Fatalf("portless pad did not drive the idle menu: feed=%+v controls=%v", feed.events, in.controls)
	}
}

// In the room shown over a running session, pads with ports drive the menu,
// so a pad without one does too, except Start and Select, which never act
// on the menu there.
func TestPortlessPadDrivesTheRoomDuringPlay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.roomDuringPlay = true
	app.mu.Unlock()
	now := time.Now()
	if app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonDPadDown, true), now) {
		t.Fatal("the room during play did not hand a ported pad to the menu")
	}
	in := &nativeInput{fd: 9, kind: InputGamepad, player: noLocalPlayer}
	in.button(app, ButtonDPadDown, 1, now)
	if len(in.controls) != 1 {
		t.Fatalf("portless pad did not reach the room: %v", in.controls)
	}
	in.button(app, ButtonStart, 1, now)
	in.button(app, ButtonBack, 1, now)
	if len(in.controls) != 1 || len(feed.snapshot()) != 0 {
		t.Fatalf("portless Start/Select acted: controls=%v feed=%+v", in.controls, feed.snapshot())
	}
	in.button(app, ButtonDPadDown, 0, now)
	if len(in.controls) != 0 {
		t.Fatalf("release left a menu control: %v", in.controls)
	}
}

func TestPortlessKeypadSelectConfirmsFocusedPicker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session = colecoKitSession()
	app.sessionDisplay = protocol.SessionDisplayFocused
	app.tapePickerOpen = true
	app.tapePickerRows = []TapePickerRow{{Name: tapePickerOSKLabel, Kind: tapePickerKindOSK}}
	app.tapePickerIndex = 0
	app.mu.Unlock()

	in := &nativeInput{fd: 9, kind: InputGamepad, player: noLocalPlayer}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	now := time.Now()
	in.button(app, ButtonBack, 1, now)
	if got := in.controls[uint16(ButtonBack)]; got != CmdSelect {
		t.Fatalf("portless keypad Select command = %v, want CmdSelect", got)
	}
	if inputs.applyButtons(app, now) {
		t.Fatal("picker Select requested quitting")
	}
	app.mu.Lock()
	oskKind := app.settingsOSKKind
	app.mu.Unlock()
	if oskKind != settingsOSKTapePath {
		t.Fatalf("picker Select did not open its path keyboard: %v", oskKind)
	}
	if got := feed.snapshot(); len(got) != 0 {
		t.Fatalf("portless Select reached the machine: %+v", got)
	}
	in.button(app, ButtonBack, 0, now.Add(time.Millisecond))
	inputs.applyButtons(app, now.Add(time.Millisecond))
	if len(in.controls) != 0 {
		t.Fatalf("portless Select release left controls held: %v", in.controls)
	}

	app.mu.Lock()
	app.tapePickerOpen = false
	app.tapePickerRows = nil
	app.settingsOSKKind = settingsOSKNone
	app.sessionDisplay = protocol.SessionDisplayShown
	app.mu.Unlock()
	in.button(app, ButtonBack, 1, now.Add(2*time.Millisecond))
	inputs.applyButtons(app, now.Add(2*time.Millisecond))
	if len(in.controls) != 0 {
		t.Fatalf("portless keypad Select reached the menu during play: %v", in.controls)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.settingsOSKKind != settingsOSKNone {
		t.Fatalf("portless Select opened launcher controls during play: %v", app.settingsOSKKind)
	}
}

func TestPortlessPadBReturnsAfterLostHDMIOpen(t *testing.T) {
	prior := kitDisplaySession()
	var opens, closes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Visible bool `json:"visible"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.Visible {
			opens.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close() // The physical open may have succeeded before its reply was lost.
			return
		}
		closes.Add(1)
		b, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok || b.Generation != prior.CorePackage.Generation || b.Target != prior.Target || r.Header.Get(protocol.HostSessionIDHeader) != prior.ID {
			t.Error("portless return lost the original display binding")
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "lost HDMI open response", func(s Snapshot) bool {
		return strings.Contains(s.Status, "HDMI controls unavailable")
	})

	in := &nativeInput{fd: 9, kind: InputGamepad, player: noLocalPlayer}
	in.button(app, ButtonEast, 1, time.Now()) // B is the guarded return, not a menu command.
	if len(in.controls) != 0 {
		t.Fatalf("portless recovery B reached menu controls: %v", in.controls)
	}
	waitFor(t, app, "portless HDMI return", func(s Snapshot) bool {
		return s.GPUParked && app.ForwardsPlayHID() && !s.Room.DuringPlay
	})
	if opens.Load() != 1 || closes.Load() != 1 || app.Snapshot().Session.State != "active" {
		t.Fatalf("portless pad did not make one guarded return: opens=%d closes=%d session=%q", opens.Load(), closes.Load(), app.Snapshot().Session.State)
	}
}

// Guide is a system button: a pad without a port still stops a kit-local core.
func TestPortlessPadGuideStopsAKitLocalCore(t *testing.T) {
	a, fake := runningLocalApp(t)
	inputs, poll := pollNative(t, nativeTestEvent(1, 316, 1), nativeTestEvent(1, 316, 0))
	inputs.devices[0].kind = InputGamepad
	inputs.devices[0].player = noLocalPlayer
	poll(a)
	waitStops(t, fake, 1)
}

// A pad without a port holds nothing at the machine, so unplugging it sends
// nothing and leaves both ports alone.
func TestUnpluggedPortlessPadSendsNothing(t *testing.T) {
	feed := &localPadRecorder{}
	a := runningLocalCoreApp(feed)
	a.mu.Lock()
	a.localSent = map[padKey]bool{{0, remoteinput.ButtonA}: true}
	a.localPair.set(0, remoteinput.ButtonSelect, true)
	a.mu.Unlock()
	var fds [2]int
	if err := unix.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fds[1]) })
	first := &nativeInput{fd: fds[1], kind: InputGamepad, player: 0}
	portless := &nativeInput{fd: fds[0], kind: InputGamepad, player: noLocalPlayer, controls: map[uint16]Command{uint16(ButtonSouth): CmdSelect}}
	inputs := &nativeInputs{devices: []*nativeInput{first, portless}}
	inputs.drop(a, portless)
	if len(feed.events) != 0 {
		t.Fatalf("portless unplug sent %+v", feed.events)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.localSent[padKey{0, remoteinput.ButtonA}] || !a.localPair.at(0).selectDown {
		t.Fatal("portless unplug cleared pad 1's state")
	}
}

// A node named with -input that the classifier cannot place still takes a
// port, as its buttons reached the core before pads had ports.
func TestExplicitUnclassifiedInputTakesAPort(t *testing.T) {
	dir := t.TempDir()
	inputs, err := openNativeInputsDir(pipePad(t, dir, "event0"), "menu-display", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer inputs.close()
	if len(inputs.devices) != 1 || inputs.devices[0].kind != InputNone || inputs.devices[0].player != 0 {
		t.Fatalf("explicit input: %+v", inputs.devices)
	}
}
