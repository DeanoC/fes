//go:build linux

package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

type stopHIDRecorder struct {
	mu     sync.Mutex
	events []remoteinput.Event
}

func (r *stopHIDRecorder) handler(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/api/v1/session/input/event" {
		http.NotFound(w, req)
		return
	}
	var body struct {
		Event remoteinput.Event `json:"event"`
	}
	if json.NewDecoder(req.Body).Decode(&body) != nil {
		w.WriteHeader(400)
		return
	}
	r.mu.Lock()
	r.events = append(r.events, body.Event)
	r.mu.Unlock()
	w.WriteHeader(200)
}
func (r *stopHIDRecorder) snapshot() []remoteinput.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]remoteinput.Event(nil), r.events...)
}

func stopHIDApp(t *testing.T, local bool) (*App, func() []remoteinput.Event) {
	t.Helper()
	recorder := &stopHIDRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	t.Cleanup(server.Close)
	if local {
		app, feed := kitDisplayApp(t, server)
		app.mu.Lock()
		app.kitHost = true
		app.session.Input = nil
		app.session.CoreMouse = true
		app.mu.Unlock()
		return app, feed.snapshot
	}
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.client.paired = true
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "free"}
	app.session = kitDisplaySession()
	app.session.CoreMouse = true
	t.Cleanup(app.Stop)
	return app, recorder.snapshot
}
func setStopState(app *App, phase string) {
	app.mu.Lock()
	defer app.mu.Unlock()
	if phase == "stopping" {
		app.stopPhase = "stopping"
	} else {
		app.stopPhase = "error"
		app.retryStopLock = true
	}
}

func TestStopBlocksMouseAndAxis(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			name := "paired/" + phase
			if local {
				name = "local/" + phase
			}
			t.Run(name, func(t *testing.T) {
				app, snapshot := stopHIDApp(t, local)
				setStopState(app, phase)
				app.HandlePlayMouseMotion(1, .5, .5)
				app.HandlePlayMouseButton(2, 1, true)
				app.HandlePlayMouseReport(3, 3, 4, 1)
				app.SendPlayHID(remoteinput.MouseEvent(2, 1, 0))
				axis := remoteinput.EventForCode(remoteinput.AxisLeftX, remoteinput.ActionAbsolute)
				axis.Value = 100
				app.SendPlayHID(axis)
				drainLocalKeyQueue(t, app)
				if got := snapshot(); len(got) != 0 {
					t.Errorf("input reached transport: %+v", got)
				}
				app.mu.Lock()
				defer app.mu.Unlock()
				for id, m := range app.playMice {
					if m.x != 0 || m.y != 0 || m.buttons != 0 {
						t.Errorf("mouse %d retained input: %+v", id, *m)
					}
				}
			})
		}
	}
}

func TestStopAllowsHIDNeutralization(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			name := "paired/" + phase
			if local {
				name = "local/" + phase
			}
			t.Run(name, func(t *testing.T) {
				app, snapshot := stopHIDApp(t, local)
				setStopState(app, phase)
				key := remoteinput.EventForCode(remoteinput.KeyA, remoteinput.ActionRelease)
				button := remoteinput.EventForCode(remoteinput.ButtonA, remoteinput.ActionRelease)
				axis := remoteinput.EventForCode(remoteinput.AxisLeftX, remoteinput.ActionAbsolute)
				mouse := remoteinput.MouseEvent(0, 0, 0)
				neutral := []remoteinput.Event{key, button, axis, mouse}
				for _, event := range neutral {
					if !app.SendPlayHID(event) {
						t.Errorf("neutral input rejected: %+v", event)
					}
				}
				app.mu.Lock()
				pressKey := key
				pressKey.Action = remoteinput.ActionPress
				pressButton := button
				pressButton.Action = remoteinput.ActionPress
				heldAxis := axis
				heldAxis.Value = 100
				for _, event := range []remoteinput.Event{pressKey, pressButton, heldAxis, remoteinput.MouseEvent(0, 0, 1)} {
					app.rememberPlayHIDLocked(event)
				}
				app.releasePlayHIDLocked()
				app.mu.Unlock()
				drainLocalKeyQueue(t, app)
				got := snapshot()
				for _, event := range neutral {
					count := 0
					for _, observed := range got {
						if observed == event {
							count++
						}
					}
					if count != 2 {
						t.Errorf("neutral event %+v observed %d times, want explicit and synthetic: %+v", event, count, got)
					}
				}
			})
		}
	}
}

func TestRetryStopViaKeyboard(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		code      uint16
		native    bool
	}{
		{"native-escape", "escape", 1, true}, {"native-backspace", "backspace", 14, true},
		{"named-escape", "escape", 1, false}, {"named-backspace", "backspace", 14, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type identity struct {
				ID         string `json:"id"`
				GameID     string `json:"game_id"`
				Target     string `json:"target"`
				TargetID   string `json:"target_id"`
				PackageID  string `json:"package_id"`
				Generation uint64 `json:"generation"`
				FlightID   string `json:"flight_id"`
			}
			type request struct {
				Expected identity `json:"expected_session"`
				Retain   bool     `json:"retain_lease"`
			}
			requests := make(chan request, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/session/stop" {
					http.NotFound(w, r)
					return
				}
				var body request
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					w.WriteHeader(400)
					return
				}
				requests <- body
				_, _ = w.Write([]byte(`{"state":"idle"}`))
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
			prior := kitDisplaySession()
			app.session, app.stopExpected, app.stopPhase, app.retryStopLock = prior, prior, "error", true
			if app.ConsumePlayHID() || !app.ForwardsPlayHID() {
				t.Fatal("failed Stop should allow navigation while retaining the HID route")
			}
			now := time.Now()
			if tc.native {
				(&nativeInput{kind: InputKeyboard}).key(app, tc.code, 1, now)
			} else if !app.HandlePlayHIDKey(tc.key, true, now) {
				app.Press(CommandFromKey(tc.key), now)
			}
			select {
			case got := <-requests:
				want := identity{prior.ID, prior.GameID, prior.Target, prior.TargetID, prior.CorePackage.PackageID, prior.CorePackage.Generation, prior.FlightID}
				if !got.Retain || got.Expected != want {
					t.Errorf("retry identity = %+v, want %+v with retained lease", got, want)
				}
			case <-time.After(time.Second):
				t.Fatal("keyboard did not retry Stop")
			}
			app.mu.Lock()
			done := app.stopWait
			app.mu.Unlock()
			if done != nil {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("retry did not complete")
				}
			}
			select {
			case extra := <-requests:
				t.Fatalf("unexpected additional Stop: %+v", extra)
			default:
			}
		})
	}
}
