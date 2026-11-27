package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

// Every local exit route must require a fresh observation of this host's
// lease. In particular a foreign session is represented as busy, not active.
func TestKitHostStopShortcutsRequireFreshOwnership(t *testing.T) {
	for _, shortcut := range []string{"chord", "guide", "f12", "ctrl-home"} {
		for _, ownership := range []string{"owned", "foreign", "missing", "stale", "future", "session-missing", "session-stale", "session-future"} {
			t.Run(shortcut+"/"+ownership, func(t *testing.T) {
				posts := make(chan struct{}, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/v1/session/stop" {
						posts <- struct{}{}
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"state":"idle"}`))
						return
					}
					http.NotFound(w, r)
				}))
				t.Cleanup(server.Close)
				app, feed := kitDisplayApp(t, server)
				now := time.Now()
				action := now.Add(2 * time.Second)
				app.mu.Lock()
				app.kitHost = true
				app.session = fixedVideoKitSession()
				ownedLocalKeyboardForTest(app)
				app.healthObservedAt = now
				app.sessionObservedAt = now
				switch ownership {
				case "foreign":
					app.health.Connection.State = "busy"
					app.health.Connection.Owner = "fogcast@another-host"
					app.health.Connection.LeaseOwned = false
					app.kitLease = KitLeaseStatus{State: "held", Owner: "fogcast@another-host"}
				case "missing":
					app.healthObservedAt = time.Time{}
				case "stale":
					app.healthObservedAt = action.Add(-6 * time.Second)
				case "future":
					app.healthObservedAt = action.Add(time.Second)
				case "session-missing":
					app.sessionObservedAt = time.Time{}
				case "session-stale":
					app.sessionObservedAt = action.Add(-6 * time.Second)
				case "session-future":
					app.sessionObservedAt = action.Add(time.Second)
				}
				app.syncGPUParkLocked()
				app.mu.Unlock()
				switch shortcut {
				case "chord":
					app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now)
					app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond))
					app.Tick(action)
				case "guide":
					if !app.RequestRoomlessStop(action) {
						t.Fatal("Guide not consumed")
					}
				case "f12":
					if !app.HandlePlayHIDScancode("F12", hidKeyboardF12, true, action) {
						t.Fatal("F12 not consumed")
					}
					app.HandlePlayHIDScancode("F12", hidKeyboardF12, false, action)
				case "ctrl-home":
					app.HandlePlayHIDScancode("ctrl", 0xe0, true, action)
					if !app.HandlePlayHIDScancode("home", 0x4a, true, action) {
						t.Fatal("Ctrl+Home not consumed")
					}
					app.HandlePlayHIDScancode("home", 0x4a, false, action)
					app.HandlePlayHIDScancode("ctrl", 0xe0, false, action)
				}
				if ownership == "owned" {
					select {
					case <-posts:
					case <-time.After(3 * time.Second):
						t.Fatal("fresh owned shortcut did not post Stop")
					}
					return
				}
				app.mu.Lock()
				phase := app.stopPhase
				app.mu.Unlock()
				if phase != "idle" {
					t.Fatalf("%s ownership entered stop phase %q", ownership, phase)
				}
				select {
				case <-posts:
					t.Fatal("unowned or stale shortcut posted Stop")
				default:
				}
				if ownership == "stale" || ownership == "session-stale" {
					if !app.HandleLocalPad(padButton(remoteinput.ButtonA, true), action.Add(time.Millisecond)) {
						t.Fatal("play input not consumed")
					}
					saw := false
					for _, event := range feed.snapshot() {
						if event.Code == remoteinput.ButtonA && event.Action == remoteinput.ActionPress {
							saw = true
						}
					}
					if !saw {
						t.Fatal("play input was blocked after rejected shortcut")
					}
				}
			})
		}
	}
}
