//go:build linux

package tenfoot

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"golang.org/x/sys/unix"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	if err := framebufferLoop(ctx, opts, app, dev, func(*App, time.Time) (bool, error) { return false, nil }); err != nil {
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
