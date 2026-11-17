//go:build sdl3

package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/remoteinput"
)

func drainSDLKeyQueue(t *testing.T, app *App) {
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
		t.Fatal("SDL keyboard queue did not drain")
	}
}

func TestSDLHotplugEventsClaimAndRestoreAffinity(t *testing.T) {
	app := pointerCatalog(4)

	if dispatchSyntheticSDL(app, int(evKeyAdded), 11) {
		t.Fatal("keyboard add quit")
	}
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 11 {
		t.Fatalf("keyboard add = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evMouseAdded), 12) {
		t.Fatal("mouse add quit")
	}
	if got := app.Affinity(); got.Kind != InputMouse || got.ID != 12 {
		t.Fatalf("mouse add = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evMouseRemoved), 12) {
		t.Fatal("mouse remove quit")
	}
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 11 {
		t.Fatalf("mouse remove = %#v", got)
	}

	if dispatchSyntheticSDL(app, int(evKeyRemoved), 11) {
		t.Fatal("keyboard remove quit")
	}
	if got := app.Affinity(); got.Kind != InputNone {
		t.Fatalf("keyboard remove = %#v", got)
	}
}

func TestSDLKeyboardsCoalesceSharedHIDUsageUntilLastRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.mu.Unlock()

	code, ok := hidkeys.Code(0x04) // USB HID A.
	if !ok {
		t.Fatal("A has no HID event code")
	}
	now := time.Now()
	send := func(keyboardID int, down bool) {
		t.Helper()
		if dispatchSyntheticSDLKey(app, keyboardID, int('a'), 4, down, now) {
			t.Fatalf("SDL key event from keyboard %d quit the launcher", keyboardID)
		}
		drainSDLKeyQueue(t, app)
	}
	actions := func() []remoteinput.Action {
		t.Helper()
		var got []remoteinput.Action
		for _, event := range feed.snapshot() {
			if event.Device == remoteinput.DeviceKeyboard && event.Code == code {
				got = append(got, event.Action)
			}
		}
		return got
	}
	assertActions := func(want ...remoteinput.Action) {
		t.Helper()
		if got := actions(); !slices.Equal(got, want) {
			t.Fatalf("SDL shared-key actions = %v, want %v", got, want)
		}
	}

	send(41, true)
	send(42, true)
	assertActions(remoteinput.ActionPress)
	send(41, false)
	assertActions(remoteinput.ActionPress)
	send(42, false)
	assertActions(remoteinput.ActionPress, remoteinput.ActionRelease)
}
