//go:build linux

package tenfoot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"golang.org/x/sys/unix"
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

func TestStopMouseCaptureDisabled(t *testing.T) {
	for _, phase := range []string{"stopping", "retry"} {
		t.Run(phase, func(t *testing.T) {
			app, _ := stopHIDApp(t, true)
			if !app.ForwardsPlayMouse() {
				t.Fatal("active mouse session did not capture the pointer")
			}
			setStopState(app, phase)
			if app.ForwardsPlayMouse() {
				t.Fatal("SDL relative mouse capture remains enabled during Stop")
			}
		})
	}
}

// Use the framebuffer's real evdev mapper and SYN_REPORT path, including the
// full button vector. A report can release one button while another stays down.
func stopNativeMouse(t *testing.T, app *App) func(...[]byte) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	fd := int(reader.Fd())
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	inputs := &nativeInputs{devices: []*nativeInput{{fd: fd, kind: InputMouse, mapper: controller.NewMouseMapper()}}}
	return func(events ...[]byte) {
		t.Helper()
		for _, event := range events {
			if _, err := writer.Write(event); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := inputs.poll(app, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStopPhysicalMouseReleasesWithoutReplayingInput(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			for _, native := range []bool{true, false} {
				name := "paired/" + phase + "/sdl"
				if local {
					name = "local/" + phase + "/sdl"
				}
				if native {
					name = name[:len(name)-3] + "native"
				}
				t.Run(name, func(t *testing.T) {
					app, snapshot := stopHIDApp(t, local)
					var poll func(...[]byte)
					if native {
						poll = stopNativeMouse(t, app)
						poll(nativeTestEvent(1, 272, 1), nativeTestEvent(1, 273, 1), nativeTestEvent(0, 0, 0))
					} else {
						app.HandlePlayMouseButton(1, 1, true)
						app.HandlePlayMouseButton(1, 3, true)
					}
					drainLocalKeyQueue(t, app)
					before := len(snapshot())
					setStopState(app, phase)
					if native {
						// A button-up and motion in the same report must only release.
						poll(nativeTestEvent(2, 0, 12), nativeTestEvent(2, 1, -4), nativeTestEvent(1, 272, 0), nativeTestEvent(0, 0, 0))
						// A new down must not return that button to the core snapshot.
						poll(nativeTestEvent(1, 272, 1), nativeTestEvent(0, 0, 0))
						poll(nativeTestEvent(1, 273, 0), nativeTestEvent(0, 0, 0))
					} else {
						app.HandlePlayMouseButton(1, 1, false)
						app.HandlePlayMouseButton(1, 1, true)
						app.HandlePlayMouseMotion(1, .5, -4)
						app.HandlePlayMouseButton(1, 3, false)
					}
					drainLocalKeyQueue(t, app)
					events := snapshot()[before:]
					var buttons []uint8
					for _, event := range events {
						x, y, b, ok := remoteinput.MouseVector(event)
						if !ok || x != 0 || y != 0 || b&1 != 0 {
							t.Errorf("new input reached the core during Stop: %+v", event)
						}
						if len(buttons) == 0 || buttons[len(buttons)-1] != b {
							buttons = append(buttons, b)
						}
					}
					if len(buttons) != 2 || buttons[0] != 2 || buttons[1] != 0 {
						t.Errorf("physical release snapshots = %v, want [2 0]: %+v", buttons, events)
					}
					app.mu.Lock()
					defer app.mu.Unlock()
					for id, mouse := range app.playMice {
						if mouse.buttons != 0 || mouse.x != 0 || mouse.y != 0 {
							t.Errorf("mouse %d retained blocked input: %+v", id, *mouse)
						}
					}
				})
			}
		}
	}
}

func TestStopPhysicalKeyboardReleaseReachesCore(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			for _, path := range []string{"native-hid", "native-matrix", "sdl-hid", "sdl-matrix", "named-matrix"} {
				name := "paired/" + phase + "/" + path
				if local {
					name = "local/" + phase + "/" + path
				}
				t.Run(name, func(t *testing.T) {
					app, snapshot := stopHIDApp(t, local)
					app.mu.Lock()
					app.session.CoreKeyboardHID = path == "native-hid" || path == "sdl-hid"
					app.mu.Unlock()
					device := &nativeInput{kind: InputKeyboard}
					key := func(name string, code uint16, usage uint8, down bool) {
						t.Helper()
						if path == "native-hid" || path == "native-matrix" {
							value := int32(0)
							if down {
								value = 1
							}
							device.key(app, code, value, time.Now())
						} else if path == "named-matrix" {
							app.HandlePlayHIDKey(name, down, time.Now())
						} else {
							app.HandlePlayHIDScancode(name, usage, down, time.Now())
						}
					}
					key("j", 36, 0x0d, true)
					drainLocalKeyQueue(t, app)
					initial := snapshot()
					if len(initial) != 1 || initial[0].Action != remoteinput.ActionPress {
						t.Fatalf("physical key did not reach the core before Stop: %+v", initial)
					}
					setStopState(app, phase)
					key("j", 36, 0x0d, false)
					key("k", 37, 0x0e, true)
					drainLocalKeyQueue(t, app)
					want := initial[0]
					want.Action = remoteinput.ActionRelease
					events := snapshot()[len(initial):]
					if len(events) != 1 || events[0] != want {
						t.Errorf("physical release/new press during Stop = %+v, want only %+v", events, want)
					}
				})
			}
		}
	}
}

func TestStopEntryNeutralizesHeldHIDBeforeFailedStop(t *testing.T) {
	for _, local := range []bool{true, false} {
		name := "paired"
		if local {
			name = "local"
		}
		t.Run(name, func(t *testing.T) {
			recorder := &stopHIDRecorder{}
			stopStarted := make(chan []remoteinput.Event, 1)
			finishStop := make(chan struct{})
			var snapshot func() []remoteinput.Event
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/v1/session/stop" {
					stopStarted <- snapshot()
					<-finishStop
					_, _ = w.Write([]byte(`{"state":"active","error_code":"SAVE_FAILED","error_message":"test save failed"}`))
					return
				}
				recorder.handler(w, req)
			}))
			t.Cleanup(server.Close)
			var app *App
			if local {
				var feed *transitionPadFeed
				app, feed = kitDisplayApp(t, server)
				snapshot = feed.snapshot
				app.kitHost = true
				app.session.Input = nil
			} else {
				app = NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
				app.client.paired = true
				app.kitLeaseHave = true
				app.kitLease = KitLeaseStatus{State: "free"}
				app.session = kitDisplaySession()
				snapshot = recorder.snapshot
				t.Cleanup(app.Stop)
			}
			var finishOnce sync.Once
			t.Cleanup(func() { finishOnce.Do(func() { close(finishStop) }) })
			app.session.CoreMouse = true
			app.session.CoreKeyboardHID = true
			device := &nativeInput{kind: InputKeyboard}
			device.key(app, 36, 1, time.Now())
			app.HandlePlayMouseButton(1, 1, true)
			app.HandlePlayMouseMotion(1, .5, .5)
			button := remoteinput.EventForCode(remoteinput.ButtonA, remoteinput.ActionPress)
			axis := remoteinput.EventForCode(remoteinput.AxisLeftX, remoteinput.ActionAbsolute)
			axis.Value = 100
			app.SendPlayHID(button)
			app.SendPlayHID(axis)
			drainLocalKeyQueue(t, app)
			before := snapshot()
			if len(before) != 4 {
				t.Fatalf("pre-Stop holds = %+v, want keyboard, mouse, button and axis", before)
			}
			app.mu.Lock()
			app.startStopLocked()
			done := app.stopWait
			app.mu.Unlock()
			select {
			case atStop := <-stopStarted:
				got := atStop[len(before):]
				for _, held := range before {
					want := held
					if held.Device == remoteinput.DeviceMouse {
						want = remoteinput.MouseEvent(0, 0, 0)
					} else {
						want.Action = remoteinput.ActionRelease
						want.Value = 0
						if held.Kind == remoteinput.KindAxis {
							want.Action = remoteinput.ActionAbsolute
						}
					}
					found := false
					for _, event := range got {
						found = found || event == want
					}
					if !found {
						t.Errorf("Stop reached host before held input was neutralized: missing %+v in %+v", want, got)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("Stop did not reach the host")
			}
			finishOnce.Do(func() { close(finishStop) })
			if done != nil {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("failed Stop did not finish")
				}
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if !app.retryStopLock || app.session.State != "active" {
				t.Fatal("test did not retain the playing core after failed Stop")
			}
			if len(app.playHIDHeld) != 0 || len(app.playMice) != 0 {
				t.Fatalf("failed Stop retained HID holds or fractional motion: keys=%+v mice=%+v", app.playHIDHeld, app.playMice)
			}
		})
	}
}

func TestStopPhysicalReleasesSurviveDisplayTransition(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			name := "paired/" + phase
			if local {
				name = "local/" + phase
			}
			t.Run(name, func(t *testing.T) {
				app, snapshot := stopHIDApp(t, local)
				app.mu.Lock()
				app.session.CoreKeyboardHID = true
				app.mu.Unlock()
				keyboard := &nativeInput{kind: InputKeyboard}
				keyboard.key(app, 36, 1, time.Now())
				mouse := stopNativeMouse(t, app)
				mouse(nativeTestEvent(1, 272, 1), nativeTestEvent(1, 273, 1), nativeTestEvent(0, 0, 0))
				drainLocalKeyQueue(t, app)
				initial := snapshot()
				if len(initial) != 2 {
					t.Fatalf("pre-Stop input = %+v, want key and both-button snapshot", initial)
				}
				setStopState(app, phase)
				app.mu.Lock()
				app.suppressHeldPlayKeysLocked()
				if phase == "stopping" {
					app.sessionDisplayBusy = true
				} else {
					app.sessionDisplayUncertain = true
				}
				app.mu.Unlock()
				t.Cleanup(func() {
					app.mu.Lock()
					app.sessionDisplayBusy = false
					app.sessionDisplayUncertain = false
					app.mu.Unlock()
				})
				keyboard.key(app, 36, 0, time.Now())
				mouse(nativeTestEvent(2, 0, 8), nativeTestEvent(1, 272, 0), nativeTestEvent(0, 0, 0))
				mouse(nativeTestEvent(1, 273, 0), nativeTestEvent(0, 0, 0))
				keyboard.key(app, 37, 1, time.Now())
				mouse(nativeTestEvent(1, 272, 1), nativeTestEvent(2, 0, 8), nativeTestEvent(0, 0, 0))
				drainLocalKeyQueue(t, app)
				keyRelease := initial[0]
				keyRelease.Action = remoteinput.ActionRelease
				want := []remoteinput.Event{keyRelease, remoteinput.MouseEvent(0, 0, 2), remoteinput.MouseEvent(0, 0, 0)}
				got := snapshot()[len(initial):]
				if len(got) != len(want) {
					t.Fatalf("display transition releases = %+v, want %+v", got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Errorf("release %d = %+v, want %+v", i, got[i], want[i])
					}
				}
			})
		}
	}
}

func TestStopPhysicalMousePartialReleaseAtQueueLimit(t *testing.T) {
	for _, local := range []bool{true, false} {
		for _, phase := range []string{"stopping", "retry"} {
			name := "paired/" + phase
			if local {
				name = "local/" + phase
			}
			t.Run(name, func(t *testing.T) {
				app, snapshot := stopHIDApp(t, local)
				app.HandlePlayMouseButton(1, 1, true)
				app.HandlePlayMouseButton(1, 3, true)
				drainLocalKeyQueue(t, app)
				initial := snapshot()
				if len(initial) != 2 || initial[1] != remoteinput.MouseEvent(0, 0, 3) {
					t.Fatalf("pre-Stop mouse buttons = %+v, want both held", initial)
				}
				setStopState(app, phase)
				// Model a full ordered queue without manufacturing 128 unrelated
				// input requests. Release snapshots must bypass press backpressure.
				app.mu.Lock()
				app.playHIDPending = maxPendingPlayHID
				app.mu.Unlock()
				t.Cleanup(func() {
					drainLocalKeyQueue(t, app)
					app.mu.Lock()
					app.playHIDPending = 0
					app.mu.Unlock()
				})
				app.HandlePlayMouseButton(1, 1, false)
				app.HandlePlayMouseButton(1, 1, true)
				app.HandlePlayMouseMotion(1, 4, -3)
				drainLocalKeyQueue(t, app)
				got := snapshot()[len(initial):]
				want := remoteinput.MouseEvent(0, 0, 2)
				if len(got) != 1 || got[0] != want {
					t.Errorf("mouse at queue limit = %+v, want only partial release %+v", got, want)
				}
			})
		}
	}
}
