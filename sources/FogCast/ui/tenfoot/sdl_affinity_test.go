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

func TestSDLKeyboardRemovalRetiresPhysicalContributions(t *testing.T) {
	for _, scenario := range []string{"single", "shared", "same-source-legacy-shift", "suppressed", "foreign-lease", "blocked-route", "transition-foreign-lease", "new-session", "transition-binding"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(http.NotFound))
			t.Cleanup(server.Close)
			app, feed := kitDisplayApp(t, server)
			app.mu.Lock()
			app.session.CoreKeyboardHID = scenario != "same-source-legacy-shift"
			app.session.CoreKeyboard = true
			app.mu.Unlock()
			now := time.Now()
			send := func(id, keycode, scancode int, down bool) {
				t.Helper()
				dispatchSyntheticSDLKey(app, id, keycode, scancode, down, now)
				drainSDLKeyQueue(t, app)
			}
			remove := func(id int) {
				t.Helper()
				dispatchSyntheticSDL(app, int(evKeyRemoved), id)
				drainSDLKeyQueue(t, app)
			}
			assertActions := func(want ...remoteinput.Action) {
				t.Helper()
				var got []remoteinput.Action
				for _, e := range feed.snapshot() {
					if e.Device == remoteinput.DeviceKeyboard {
						got = append(got, e.Action)
					}
				}
				if !slices.Equal(got, want) {
					t.Fatalf("keyboard actions = %v, want %v", got, want)
				}
			}
			if scenario == "same-source-legacy-shift" {
				// SDL keycodes use bit 30 to identify a scancode-based key.
				send(41, 1<<30|0xe1, 0xe1, true)
				send(41, 1<<30|0xe5, 0xe5, true)
			} else {
				send(41, int('a'), 4, true)
			}
			if scenario == "shared" {
				send(42, int('a'), 4, true)
			}
			if scenario == "suppressed" {
				app.mu.Lock()
				app.sessionDisplayBinding = app.session
				app.sessionDisplayBusy = true
				app.sessionDisplayNonHandoff = true
				app.mu.Unlock()
				send(42, int('a'), 4, true)
				remove(42)
				assertActions(remoteinput.ActionPress)
				app.mu.Lock()
				if len(app.playKeySuppressed) != 0 {
					t.Errorf("removed keyboard retained suppressed keys: %v", app.playKeySuppressed)
				}
				app.mu.Unlock()
			}
			if scenario == "foreign-lease" || scenario == "new-session" || scenario == "transition-foreign-lease" {
				app.mu.Lock()
				app.kitLeaseHave = true
				app.kitLease.ErrorCode = "KIT_LEASE_BUSY"
				if scenario == "transition-foreign-lease" {
					app.sessionDisplayBinding = app.session
					app.sessionDisplayBusy = true
					app.sessionDisplayNonHandoff = true
				}
				app.mu.Unlock()
			}
			if scenario == "blocked-route" {
				app.mu.Lock()
				app.sessionDisplayUncertain = true
				app.mu.Unlock()
			}
			if scenario == "transition-binding" {
				app.mu.Lock()
				app.sessionDisplayBinding = app.session
				app.sessionDisplayBinding.ID = "old-session"
				app.sessionDisplayBusy = true
				app.sessionDisplayNonHandoff = true
				app.mu.Unlock()
			}
			remove(41)
			if scenario == "shared" {
				assertActions(remoteinput.ActionPress)
				send(42, int('a'), 4, false)
			}
			if scenario == "foreign-lease" || scenario == "new-session" || scenario == "blocked-route" || scenario == "transition-foreign-lease" {
				assertActions(remoteinput.ActionPress)
				app.mu.Lock()
				if len(app.playKeyPendingRelease) != 1 {
					t.Errorf("pending neutrals = %v, want one", app.playKeyPendingRelease)
				}
				if scenario == "new-session" {
					app.cancelPlayHIDLocked()
					app.session.CorePackage.Generation++
				}
				app.kitLeaseHave = false
				app.sessionDisplayBusy = false
				app.sessionDisplayNonHandoff = false
				app.sessionDisplayUncertain = false
				app.mu.Unlock()
				app.Tick(now)
				drainSDLKeyQueue(t, app)
			}
			if scenario == "new-session" || scenario == "transition-binding" {
				assertActions(remoteinput.ActionPress)
			} else {
				assertActions(remoteinput.ActionPress, remoteinput.ActionRelease)
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			for key := range app.playKeyHeld {
				if key.source == 41 || scenario == "suppressed" && key.source == 42 {
					t.Errorf("removed keyboard still holds %v", key)
				}
			}
		})
	}
}
