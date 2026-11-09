//go:build linux

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestKitHostPhysicalKeyboardWithoutRemoteInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		hid  bool
	}{{"zx81 matrix", false}, {"ST HID keyboard without display plane", true}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(http.NotFound))
			t.Cleanup(server.Close)
			app, feed := kitDisplayApp(t, server)
			app.mu.Lock()
			app.kitHost = true
			app.session.Input = nil
			app.session.CoreKeyboardHID = tc.hid
			if tc.hid {
				app.session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.mouse.relative", Major: 1}}
			}
			app.mu.Unlock()
			device := &nativeInput{kind: InputKeyboard}
			now := time.Now()
			device.key(app, 36, 1, now) // Linux KEY_J.
			device.key(app, 36, 0, now)
			drainLocalKeyQueue(t, app)
			code := zx81keys.Letter('J')
			if tc.hid {
				code, _ = hidkeys.Code(nativeHIDUsage(36))
			}
			if !feed.saw(code, remoteinput.ActionPress) || !feed.saw(code, remoteinput.ActionRelease) {
				t.Fatalf("physical keyboard edges did not reach local feed: %+v", feed.snapshot())
			}
		})
	}
}

func TestKitHostLocalKeyboardHonorsForeignLease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.kitHost = true
	app.session.Input = nil
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "busy", Owner: "another-shell"}
	app.mu.Unlock()
	device := &nativeInput{kind: InputKeyboard}
	device.key(app, 36, 1, time.Now())
	device.key(app, 36, 0, time.Now())
	drainLocalKeyQueue(t, app)
	if len(feed.snapshot()) != 0 {
		t.Fatalf("foreign lease admitted input: %+v", feed.snapshot())
	}
}

func TestKitHostLocalKeyboardRequiresActiveVisibleLocalRoute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*App)
	}{
		{"not kit host", func(a *App) { a.kitHost = false }},
		{"no feed", func(a *App) { a.localFeed = nil }},
		{"no client", func(a *App) { a.client = nil }},
		{"inactive", func(a *App) { a.session.State = "idle" }},
		{"room open", func(a *App) { a.roomDuringPlay = true }},
		{"display busy", func(a *App) { a.sessionDisplayBusy = true }},
		{"display uncertain", func(a *App) { a.sessionDisplayUncertain = true }},
		{"tape picker", func(a *App) { a.tapePickerOpen = true }},
		{"firmware picker", func(a *App) { a.firmwarePickerOpen = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(http.NotFound))
			t.Cleanup(server.Close)
			app, _ := kitDisplayApp(t, server)
			app.mu.Lock()
			app.kitHost = true
			app.session.Input = nil
			tc.change(app)
			app.mu.Unlock()
			if app.ForwardsPlayHID() {
				t.Fatal("keyboard forwarded without active visible local route")
			}
		})
	}
}
