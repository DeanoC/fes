//go:build linux

package tenfoot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

type kitKeyboardStopTransport func(*http.Request) (*http.Response, error)

func (f kitKeyboardStopTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestKitHostLocalKeyboardBlockedDuringStopAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		begin func(*App)
	}{
		{
			name: "stop in progress",
			begin: func(app *App) {
				app.mu.Lock()
				app.startStopLocked()
				app.mu.Unlock()
			},
		},
		{
			name: "retry stop",
			begin: func(app *App) {
				app.mu.Lock()
				app.stopPhase = "error"
				app.lockRetryStopLocked("SAVE_FAILED", "save failed")
				app.mu.Unlock()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopStarted := make(chan struct{})
			stopFinished := make(chan struct{})
			finishStop := make(chan struct{})
			var finishOnce sync.Once
			var stopOnce sync.Once
			transport := kitKeyboardStopTransport(func(r *http.Request) (*http.Response, error) {
				stopOnce.Do(func() {
					close(stopStarted)
					<-finishStop
					close(stopFinished)
				})
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(`{"state":"idle"}`)),
					Request:    r,
				}, nil
			})
			client := NewClient("http://kit-host.test", &http.Client{Transport: transport})
			app := NewApp(client, 1280, 720, 20)
			feed := &transitionPadFeed{}
			app.localCores = &fakeLocalCores{}
			app.localFeed = feed
			app.session = kitDisplaySession()
			t.Cleanup(func() {
				finishOnce.Do(func() { close(finishStop) })
				app.Stop()
			})
			app.mu.Lock()
			app.kitHost = true
			app.session.Input = nil
			app.session.CoreKeyboardHID = true
			app.session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.keyboard.hid", Major: 1}}
			app.mu.Unlock()

			if tc.name == "stop in progress" {
				tc.begin(app)
				select {
				case <-stopStarted:
				case <-time.After(2 * time.Second):
					t.Fatal("Stop request did not reach the blocked handler")
				}
			} else {
				tc.begin(app)
			}

			device := &nativeInput{kind: InputKeyboard}
			device.key(app, 36, 1, time.Now()) // Linux KEY_J.
			drainLocalKeyQueue(t, app)
			if app.ConsumePlayHID() {
				t.Fatal("key was consumed as play HID during teardown")
			}
			if got := feed.snapshot(); len(got) != 0 {
				t.Fatalf("key reached local feed during teardown: %+v", got)
			}

			finishOnce.Do(func() { close(finishStop) })
			if tc.name == "stop in progress" {
				select {
				case <-stopFinished:
				case <-time.After(2 * time.Second):
					t.Fatal("Stop request did not finish")
				}
			}
		})
	}
}

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
