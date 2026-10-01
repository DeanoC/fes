//go:build linux

package tenfoot

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/kitlauncher/controller"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeEvdevBothABIs(t *testing.T) {
	for _, size := range []int{16, 24} {
		b := make([]byte, size)
		binary.LittleEndian.PutUint16(b[size-8:], 1)
		binary.LittleEndian.PutUint16(b[size-6:], 103)
		binary.LittleEndian.PutUint32(b[size-4:], 1)
		typ, code, value := decodeNativeEvent(b)
		if typ != 1 || code != 103 || value != 1 {
			t.Fatalf("size %d: %d %d %d", size, typ, code, value)
		}
	}
}

func TestTenfootFixturePadNormalizationAndVirtualFilter(t *testing.T) {
	if controller.Eligible(6, "FogCast Virtual Gamepad", true) || controller.Eligible(3, "USB pad", false) {
		t.Fatal("automatic selection admitted virtual/non-gamepad device")
	}
	if !controller.Eligible(3, "USB SNES Pad", true) {
		t.Fatal("physical gamepad rejected")
	}
	mapper := controller.NewMapper(0x081f, 0xe401, map[uint16]controller.Range{
		0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255},
		16: {Min: -1, Max: 1}, 17: {Min: -1, Max: 1},
	})
	for _, tc := range []struct {
		code  uint16
		value int32
		want  remoteinput.Code
	}{
		{288, 1, remoteinput.ButtonY}, {289, 1, remoteinput.ButtonB},
		{290, 1, remoteinput.ButtonA}, {296, 1, remoteinput.ButtonSelect},
		{297, 1, remoteinput.ButtonStart},
	} {
		e, ok := mapper.Map(1, tc.code, tc.value)
		if !ok || e.Code != tc.want {
			t.Fatalf("button %d mapped to %#v, %v", tc.code, e, ok)
		}
	}
	for _, tc := range []struct {
		code  uint16
		value int32
		want  remoteinput.Code
	}{
		{0, 0, remoteinput.AxisLeftX}, {0, 255, remoteinput.AxisLeftX},
		{1, 0, remoteinput.AxisLeftY}, {16, -1, remoteinput.AxisLeftX},
	} {
		e, ok := mapper.Map(3, tc.code, tc.value)
		if !ok || e.Code != tc.want {
			t.Fatalf("axis %d=%d mapped to %#v, %v", tc.code, tc.value, e, ok)
		}
	}
}
func TestFramebufferKeyboardUsesTextInsteadOfShortcuts(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	a.HandleCommand(CmdSearch, time.Now())
	in := nativeInput{}
	for _, code := range []uint16{16, 30, 2} {
		if in.key(a, code, 1, time.Now()) {
			t.Fatal("typing q quit")
		}
	}
	if got := a.Snapshot().OSK.Buffer; got != "qa1" {
		t.Fatalf("text = %q", got)
	}
	in.key(a, 42, 1, time.Now())
	in.key(a, 30, 1, time.Now())
	in.key(a, 42, 0, time.Now())
	if got := a.Snapshot().OSK.Buffer; got != "qa1A" {
		t.Fatalf("shift text = %q", got)
	}
	in.key(a, 1, 1, time.Now())
	in.key(a, 1, 0, time.Now())
	in.key(a, 1, 1, time.Now())
	if a.OSKOpen() {
		t.Fatal("escape did not close search")
	}
	if !in.key(a, 16, 1, time.Now()) {
		t.Fatal("q outside text did not quit")
	}
}
func TestFramebufferControllerBackAndSettings(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	in := nativeInput{}
	inputs := &nativeInputs{devices: []*nativeInput{&in}}
	in.button(a, ButtonEast, 1, time.Now())
	if inputs.applyButtons(a, time.Now()) {
		t.Fatal("back quits launcher")
	}
	in.key(a, 24, 1, time.Now())
	if !a.Snapshot().Settings.Open {
		t.Fatal("settings did not open")
	}
	in.button(a, ButtonEast, 0, time.Now())
	inputs.applyButtons(a, time.Now())
	in.button(a, ButtonEast, 1, time.Now())
	inputs.applyButtons(a, time.Now())
	in.button(a, ButtonEast, 0, time.Now())
	inputs.applyButtons(a, time.Now())
	if a.Snapshot().Settings.Open {
		t.Fatal("controller back did not close settings")
	}
}

func TestFramebufferFullAppLoop(t *testing.T) {
	server := framebufferFixture(t)
	opts := Options{APIBase: server.URL, Width: 640, Height: 480, Smoke: true, PrefsPath: t.TempDir() + "/prefs.json"}.normalized()
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	app.Start(t.Context())
	defer app.Stop()
	dst := make([]byte, 640*480*4)
	dev, err := gfx.NewLinuxFB(640, 480, dst, gfx.FBConfig{Width: 640, Height: 480, BPP: 32, Stride: 640 * 4})
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := framebufferLoop(ctx, opts, app, dev, func(*App, time.Time) (bool, error) { return false, nil }, "linuxfb"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(dst, make([]byte, len(dst))) {
		t.Fatal("full application did not render")
	}
	if len(app.Snapshot().Games) != 2 {
		t.Fatal("fixture library did not load")
	}
	// Exercise the same Systems UI through native keyboard/controller events.
	in := nativeInput{}
	inputs := &nativeInputs{devices: []*nativeInput{&in}}
	in.key(app, 24, 1, time.Now())
	for index, row := range app.Snapshot().Settings.Rows {
		if row.ID == "systems" {
			for step := 0; step < index; step++ {
				in.hat(app, 17, 1, time.Now())
				inputs.applyButtons(app, time.Now())
				in.hat(app, 17, 0, time.Now())
				inputs.applyButtons(app, time.Now())
			}
			in.button(app, ButtonSouth, 1, time.Now())
			inputs.applyButtons(app, time.Now())
			in.button(app, ButtonSouth, 0, time.Now())
			inputs.applyButtons(app, time.Now())
			break
		}
	}
	waitCoreLibrary(t, app)
	if rows := app.Snapshot().CoreLibrary.Rows; len(rows) != 2 || !strings.HasPrefix(rows[1].Name, "SMS") {
		t.Fatalf("systems = %#v", rows)
	}
	textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
	presentFrame(dev, app.Snapshot(), textures, labels, false)
	defer destroyTextures(dev, textures)
	defer destroyTextures(dev, labels)
}

func framebufferFixture(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("display smoke attempted mutation: %s %s", r.Method, r.URL)
			http.Error(w, "mutation", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/games":
			json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("sms-one", "SMS test one", "sms"), availableGame("sg-two", "SG-1000 test two", "sg1000")}})
		case "/api/v1/core-catalog":
			io.WriteString(w, `{"cores":[{"source_id":"fixture","core_id":"fes.sms","label":"SMS","artifact_state":"installed"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// Opt-in local display check: private fixture, no target/real host requests.
func TestFramebufferLocalDisplay(t *testing.T) {
	path := os.Getenv("FOGCAST_TEST_FRAMEBUFFER")
	if path == "" {
		t.Skip("local framebuffer check is opt-in")
	}
	dev, err := gfx.OpenLinuxFB(path)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), dev.Destination()...)
	dev.Close()
	server := framebufferFixture(t)
	if err := Run(t.Context(), Options{GFX: "linuxfb", Framebuffer: path, Input: "none", APIBase: server.URL, Smoke: true, SmokeTimeout: 5 * time.Second, PrefsPath: t.TempDir() + "/prefs.json"}); err != nil {
		t.Fatal(err)
	}
	dev, err = gfx.OpenLinuxFB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if !bytes.Equal(before, dev.Destination()) {
		t.Fatal("framebuffer was not restored")
	}
	t.Logf("full tenfoot library rendered on %s (%s); original framebuffer restored; fixture API only", path, dev.Config())

}

func TestFramebufferControllerHoldUsesSharedGate(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	in := &nativeInput{}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	now := time.Now()
	in.button(a, ButtonNorth, 1, now)
	inputs.applyButtons(a, now)
	if a.OSKOpen() {
		t.Fatal("North acted before hold/release")
	}
	in.button(a, ButtonNorth, 0, now.Add(100*time.Millisecond))
	inputs.applyButtons(a, now.Add(100*time.Millisecond))
	if !a.OSKOpen() {
		t.Fatal("short North did not open search")
	}
}

func nativeTestEvent(typ, code uint16, value int32) []byte {
	b := make([]byte, nativeEventSize)
	tail := b[nativeEventSize-8:]
	binary.NativeEndian.PutUint16(tail, typ)
	binary.NativeEndian.PutUint16(tail[2:], code)
	binary.NativeEndian.PutUint32(tail[4:], uint32(value))
	return b
}
func TestFramebufferPollPreservesPartialRecordsAndQuickTaps(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	fd := int(reader.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	in := &nativeInput{fd: fd}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	a := NewApp(nil, 1280, 720, 10)
	event := nativeTestEvent(1, 24, 1) // o opens Settings.
	writer.Write(event[:7])
	if _, err := inputs.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Settings.Open {
		t.Fatal("partial record was dispatched")
	}
	writer.Write(event[7:])
	if _, err := inputs.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !a.Snapshot().Settings.Open {
		t.Fatal("fragmented key record was lost")
	}
	// A complete East down/up pair in a single read must close Settings.
	writer.Write(append(nativeTestEvent(1, 305, 1), nativeTestEvent(1, 305, 0)...))
	if _, err := inputs.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().Settings.Open {
		t.Fatal("quick controller tap was lost")
	}
}

func TestFramebufferPollPreservesQuickAxisTap(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	fd := int(r.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	mapper := controller.NewMapper(0x081f, 0xe401, map[uint16]controller.Range{0: {Min: -1, Max: 1}})
	in := &nativeInput{fd: fd, mapper: mapper}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	a := NewApp(nil, 1280, 720, 10)
	a.mu.Lock()
	a.games = []hostclient.Game{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	a.grid.Count, a.grid.Columns = 2, 2
	a.mu.Unlock()
	batch := append(nativeTestEvent(3, 0, 1), nativeTestEvent(3, 0, 0)...)
	if _, err := w.Write(batch); err != nil {
		t.Fatal(err)
	}
	if _, err := inputs.poll(a, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := a.Snapshot().Grid.Focus; got != 1 {
		t.Fatalf("quick axis tap focus = %d, want 1 exactly once", got)
	}
}

type localPadRecorder struct{ events []remoteinput.Event }

func (f *localPadRecorder) Send(e remoteinput.Event, _ time.Time) error {
	f.events = append(f.events, e)
	return nil
}
func (f *localPadRecorder) Close() {}

func TestFramebufferDpadRoutesToLocalCoreAndMenu(t *testing.T) {
	for _, tc := range []struct {
		name string
		send func(*nativeInput, *App, time.Time)
	}{
		{"hat", func(in *nativeInput, a *App, n time.Time) { in.hat(a, 17, 1, n) }},
		{"axis", func(in *nativeInput, a *App, n time.Time) { in.axis(a, remoteinput.AxisLeftY, 32767, n) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewApp(nil, 1280, 720, 10)
			feed := &localPadRecorder{}
			a.mu.Lock()
			a.games = []hostclient.Game{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
			a.grid.Count, a.grid.Columns = 2, 1
			a.localPhase = localPhaseRunning
			a.localFeed = feed
			a.mu.Unlock()
			in := &nativeInput{fd: 9}
			now := time.Now()
			tc.send(in, a, now)
			if len(feed.events) != 1 || feed.events[0].Code != remoteinput.ButtonDPadDown || feed.events[0].Action != remoteinput.ActionPress {
				t.Fatalf("local events: %#v", feed.events)
			}
			if tc.name == "hat" {
				in.hat(a, 17, 0, now.Add(time.Millisecond))
			} else {
				in.axis(a, remoteinput.AxisLeftY, 0, now.Add(time.Millisecond))
			}
			if len(feed.events) != 2 || feed.events[1].Action != remoteinput.ActionRelease {
				t.Fatalf("local release: %#v", feed.events)
			}
			a.mu.Lock()
			a.localPhase = ""
			a.mu.Unlock()
			before := a.Snapshot().Grid.Focus
			tc.send(in, a, now.Add(2*time.Millisecond))
			inputs := &nativeInputs{devices: []*nativeInput{in}}
			inputs.applyButtons(a, now.Add(2*time.Millisecond))
			if a.Snapshot().Grid.Focus == before {
				t.Fatal("menu did not receive d-pad")
			}
		})
	}
}

func TestAutoInputRejectsUnknownIdentity(t *testing.T) {
	keys := make([]byte, 96)
	keys[304/8] |= 1 << uint(304%8)
	if got := nativeKindWithIdentity(keys, 0, "", 0, 0); got != InputNone {
		t.Fatalf("unknown identity classified as %v", got)
	}
	keys = make([]byte, 96)
	keys[288/8] |= 1 << uint(288%8)
	keys[289/8] |= 1 << uint(289%8)
	if got := nativeKindWithIdentity(keys, 0, "", 0, 0); got != InputNone {
		t.Fatalf("unknown BTN_GAMEPAD identity classified as %v", got)
	}
	keys = make([]byte, 96)
	keys[304/8] |= 1 << uint(304%8)
	if got := nativeKindWithIdentity(keys, 3, "physical pad", 1, 2); got != InputGamepad {
		t.Fatalf("known physical pad classified as %v", got)
	}
}

func TestAutoInputAcceptsGenericPhysicalTriggerPad(t *testing.T) {
	keys := make([]byte, 96)
	keys[288/8] |= 1 << uint(288%8)
	keys[289/8] |= 1 << uint(289%8)
	if got := nativeKindWithIdentity(keys, 3, "Generic USB Pad", 0x1234, 0x5678); got != InputGamepad {
		t.Fatalf("generic physical trigger pad classified as %v", got)
	}
}

func TestAutoInputRejectsVirtualGamepadIdentity(t *testing.T) {
	keys := make([]byte, 96)
	keys[288/8] |= 1 << uint(288%8)
	keys[289/8] |= 1 << uint(289%8)
	for _, tc := range []struct {
		bus  uint16
		name string
	}{{6, "USB Pad"}, {3, "FogCast Virtual Gamepad"}} {
		if got := nativeKindWithIdentity(keys, tc.bus, tc.name, 0x1234, 0x5678); got != InputNone {
			t.Errorf("virtual identity (%d, %q) classified as %v", tc.bus, tc.name, got)
		}
	}
}
func TestFramebufferControllerLongHold(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	in := &nativeInput{}
	inputs := &nativeInputs{devices: []*nativeInput{in}}
	now := time.Now()
	in.button(a, ButtonSouth, 1, now)
	inputs.applyButtons(a, now)
	a.Tick(now.Add(longPressMin + time.Millisecond))
	if !a.Snapshot().ViewPicker {
		t.Fatal("long South did not open view picker")
	}
}

func TestNativeDeviceClassification(t *testing.T) {
	keys := make([]byte, 96)
	set := func(code int) { keys[code/8] |= 1 << uint(code%8) }
	set(272)
	set(116)
	if nativeKindFromKeys(keys) != InputNone {
		t.Fatal("unrelated device accepted")
	}
	set(30)
	set(44)
	set(28)
	if nativeKindFromKeys(keys) != InputKeyboard {
		t.Fatal("keyboard not detected")
	}
	set(304)
	if nativeKindFromKeys(keys) != InputGamepad {
		t.Fatal("gamepad not detected")
	}
}
func TestAutoInputDisconnectCancelsHeldAction(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	fd, err := unix.Dup(int(reader.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	unix.SetNonblock(fd, true)
	writer.Close()
	in := &nativeInput{fd: fd, kind: InputGamepad}
	inputs := &nativeInputs{devices: []*nativeInput{in}, automatic: true}
	app := NewApp(nil, 1280, 720, 10)
	now := time.Now()
	in.button(app, ButtonNorth, 1, now)
	inputs.applyButtons(app, now)
	if _, err := inputs.poll(app, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if app.OSKOpen() {
		t.Fatal("disconnect emitted short North search")
	}
	if len(inputs.devices) != 0 || len(inputs.held) != 0 {
		t.Fatal("lost device kept held state")
	}
}

func TestNativeInputSeedPrefersGamepad(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	inputs := &nativeInputs{devices: []*nativeInput{{fd: 10, kind: InputKeyboard}, {fd: 11, kind: InputGamepad}}}
	inputs.seed(a)
	if got := a.Affinity(); got.Kind != InputGamepad || got.ID != 11 {
		t.Fatalf("startup affinity = %#v", got)
	}
}
func TestNativeInputLocalDiscovery(t *testing.T) {
	if os.Getenv("FOGCAST_TEST_EVDEV") != "1" {
		t.Skip("live evdev discovery is opt-in")
	}
	inputs, err := openNativeInputs("auto", "linuxfb")
	if err != nil {
		t.Fatal(err)
	}
	defer inputs.close()
	a := NewApp(nil, 1280, 720, 10)
	inputs.seed(a)
	t.Logf("classified %d supported nodes; startup owner %s; no events read", len(inputs.devices), a.Affinity().Kind)
}
func TestNativeInputLabel(t *testing.T) {
	_, err := openNativeInputs("/no/such/evdev", "menu-display")
	if err == nil || !strings.HasPrefix(err.Error(), "menu-display input /no/such/evdev:") {
		t.Fatalf("menu-display label: %v", err)
	}
	_, err = openNativeInputs("/no/such/evdev", "linuxfb")
	if err == nil || !strings.HasPrefix(err.Error(), "linuxfb input /no/such/evdev:") {
		t.Fatalf("linuxfb label: %v", err)
	}
}

func TestExplicitInputDisconnectStaysStrict(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer.Close()
	fd := int(reader.Fd())
	unix.SetNonblock(fd, true)
	inputs := &nativeInputs{devices: []*nativeInput{{fd: fd}}}
	if _, err := inputs.poll(NewApp(nil, 1280, 720, 10), time.Now()); err == nil {
		t.Fatal("explicit input disconnect did not fail")
	}
}

func TestAutoDroppedEventsKeepOtherDeviceHeld(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fd, err := unix.Dup(int(r.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	unix.SetNonblock(fd, true)
	otherR, otherW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer otherR.Close()
	defer otherW.Close()
	otherFD := int(otherR.Fd())
	unix.SetNonblock(otherFD, true)
	lost := &nativeInput{fd: fd, kind: InputGamepad}
	other := &nativeInput{fd: otherFD, kind: InputGamepad}
	inputs := &nativeInputs{devices: []*nativeInput{lost, other}, automatic: true}
	a := NewApp(nil, 1280, 720, 10)
	inputs.seed(a)
	now := time.Now()
	other.button(a, ButtonDPadRight, 1, now)
	lost.button(a, ButtonNorth, 1, now)
	inputs.applyButtons(a, now)
	w.Write(nativeTestEvent(0, 3, 0))
	if quit, err := inputs.poll(a, now.Add(time.Millisecond)); err != nil || quit {
		t.Fatalf("poll = %v, %v", quit, err)
	}
	if len(inputs.devices) != 1 || inputs.devices[0] != other || !inputs.held[CmdRight] {
		t.Fatal("healthy device hold lost")
	}
	if a.OSKOpen() || inputs.held[CmdSearch] {
		t.Fatal("dropped events emitted or retained search")
	}
}

func TestAutoInputNoNodesStaysLive(t *testing.T) {
	dir := t.TempDir()
	// A node the real classifier rejects must not be fatal and must not leak.
	rejected := filepath.Join(dir, "event0")
	if err := os.WriteFile(rejected, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inputs, err := openNativeInputsDir("auto", "menu-display", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer inputs.close()
	if inputs == nil || !inputs.automatic || len(inputs.devices) != 0 {
		t.Fatalf("inputs automatic=%v devices=%d", inputs != nil && inputs.automatic, len(inputs.devices))
	}
	app := NewApp(nil, 1280, 720, 10)
	if quit, err := inputs.poll(app, inputs.lastScan); err != nil || quit {
		t.Fatalf("poll = %v %v", quit, err)
	}
	if n := fdsFor(rejected); n != 0 {
		t.Fatalf("rejected node leaked %d fds", n)
	}
}

func TestAutoInputHotplugRescanIsBounded(t *testing.T) {
	dir := t.TempDir()
	inputs, err := openNativeInputsDir("auto", "menu-display", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer inputs.close()
	app := NewApp(nil, 1280, 720, 10)

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	gamepad := filepath.Join(dir, "event0")
	if err := os.Symlink(fdPath(int(reader.Fd())), gamepad); err != nil {
		t.Fatal(err)
	}
	rejected := filepath.Join(dir, "event1")
	if err := os.WriteFile(rejected, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var gamepadOpens, otherOpens int
	inputs.kindOf = func(fd int) InputKind {
		// Stand-in for EVIOCGBIT: a pipe is the hotplugged pad, anything else is not.
		link, err := os.Readlink(fdPath(fd))
		if err == nil && strings.HasPrefix(link, "pipe:") {
			return InputGamepad
		}
		return InputNone
	}
	inputs.openFile = func(path string) (int, error) {
		if filepath.Base(path) == "event0" {
			gamepadOpens++
		} else {
			otherOpens++
		}
		return unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	}

	base := inputs.lastScan
	if quit, err := inputs.poll(app, base.Add(time.Second-time.Millisecond)); err != nil || quit {
		t.Fatalf("early poll = %v %v", quit, err)
	}
	if gamepadOpens != 0 || otherOpens != 0 || len(inputs.devices) != 0 {
		t.Fatalf("rescanned before the interval: pad=%d other=%d devices=%d", gamepadOpens, otherOpens, len(inputs.devices))
	}

	if quit, err := inputs.poll(app, base.Add(time.Second)); err != nil || quit {
		t.Fatalf("hotplug poll = %v %v", quit, err)
	}
	if gamepadOpens != 1 || otherOpens != 1 || len(inputs.devices) != 1 {
		t.Fatalf("after hotplug pad=%d other=%d devices=%d", gamepadOpens, otherOpens, len(inputs.devices))
	}
	opened := inputs.devices[0]
	if opened.path != gamepad || opened.kind != InputGamepad {
		t.Fatalf("opened %#v", opened)
	}
	if got := app.Affinity(); got.Kind != InputGamepad || got.ID != opened.fd {
		t.Fatalf("hotplug affinity = %#v", got)
	}
	if n := fdsFor(rejected); n != 0 {
		t.Fatalf("rejected node leaked %d fds on first scan", n)
	}
	if err := unix.SetNonblock(opened.fd, true); err != nil {
		t.Fatal(err)
	}
	var buf [8]byte
	if _, err := unix.Read(opened.fd, buf[:]); err != unix.EAGAIN && err != unix.EWOULDBLOCK {
		t.Fatalf("open gamepad read: %v", err)
	}

	// Later scans must not open the pad again, and must close every rejected node.
	now := base.Add(time.Second)
	for step := 0; step < 5; step++ {
		now = now.Add(time.Second)
		if quit, err := inputs.poll(app, now); err != nil || quit {
			t.Fatalf("rescan %d = %v %v", step, quit, err)
		}
	}
	if gamepadOpens != 1 || otherOpens != 6 || len(inputs.devices) != 1 || inputs.devices[0] != opened {
		t.Fatalf("unbounded rescan pad=%d other=%d devices=%d same=%v", gamepadOpens, otherOpens, len(inputs.devices), len(inputs.devices) == 1 && inputs.devices[0] == opened)
	}
	if n := fdsFor(rejected); n != 0 {
		t.Fatalf("rejected node leaked %d fds", n)
	}

	// Same timestamp does not scan again.
	if quit, err := inputs.poll(app, now); err != nil || quit {
		t.Fatalf("repeat poll = %v %v", quit, err)
	}
	if gamepadOpens != 1 || otherOpens != 6 {
		t.Fatalf("repeat scan pad=%d other=%d", gamepadOpens, otherOpens)
	}

	// Hot-unplug drops the pad and leaves the loop alive.
	writer.Close()
	if quit, err := inputs.poll(app, now.Add(time.Millisecond)); err != nil || quit {
		t.Fatalf("unplug poll = %v %v", quit, err)
	}
	if len(inputs.devices) != 0 {
		t.Fatalf("unplugged devices = %d", len(inputs.devices))
	}
}

func fdPath(fd int) string {
	return "/proc/self/fd/" + strconv.Itoa(fd)
}

func fdsFor(path string) int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	n := 0
	for _, entry := range entries {
		target, err := os.Readlink("/proc/self/fd/" + entry.Name())
		if err == nil && target == path {
			n++
		}
	}
	return n
}
