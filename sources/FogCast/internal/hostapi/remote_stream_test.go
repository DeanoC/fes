package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type streamSink struct {
	mu     sync.Mutex
	events []protocol.InputFrame
}

func (s *streamSink) Apply(frame protocol.InputFrame) error {
	s.mu.Lock()
	s.events = append(s.events, frame)
	s.mu.Unlock()
	return nil
}
func (s *streamSink) ReleaseAll() error { return nil }
func (s *streamSink) Close() error      { return nil }
func (s *streamSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

type streamHandle struct{ server *bridge.Server }

func (h *streamHandle) Ready() <-chan struct{}     { return h.server.Ready() }
func (h *streamHandle) Endpoint() string           { return h.server.Addr().String() }
func (h *streamHandle) Stop(context.Context) error { return h.server.Close() }

type streamRig struct {
	input  *host.RemoteInput
	sink   *streamSink
	server *bridge.Server
}

func newStreamRig(t *testing.T) *streamRig {
	t.Helper()
	rig := &streamRig{sink: &streamSink{}}
	starter := host.BridgeStarterFunc(func(ctx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		server, err := bridge.New(bridge.Config{
			Addr: "127.0.0.1:0", Token: spec.Token, Session: spec.Session, Core: spec.Core,
		}, rig.sink)
		if err != nil {
			return nil, err
		}
		rig.server = server
		go func() { _ = server.ListenAndServe(ctx) }()
		return &streamHandle{server: server}, nil
	})
	input, err := host.NewRemoteInput(host.RemoteInputConfig{
		Starter:           starter,
		ReconnectGrace:    40 * time.Millisecond,
		HeartbeatInterval: time.Hour,
		DialTimeout:       40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.input = input
	t.Cleanup(func() {
		_ = input.Close()
		if rig.server != nil {
			_ = rig.server.Close()
		}
	})
	return rig
}

func (r *streamRig) attach(t *testing.T) string {
	t.Helper()
	if err := r.input.Attach(context.Background(), "SNES"); err != nil {
		t.Fatal(err)
	}
	return r.input.Status().SessionID
}

const remotePadEvent = `{"event":{"Device":1,"Kind":1,"Action":1,"Code":104,"Value":0}}`

func TestLauncherInputDeliversToLiveStream(t *testing.T) {
	rig := newStreamRig(t)
	sessionID := rig.attach(t)
	handler := launcherHandler(t, rig.input)
	server := httptest.NewServer(handler)
	defer server.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id="+sessionID, reader))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("live stream: %d %s", response.StatusCode, body)
	}
	var ready struct {
		Ready bool `json:"ready"`
	}
	if err = json.NewDecoder(response.Body).Decode(&ready); err != nil || !ready.Ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if _, err = io.WriteString(writer, remotePadEvent+"\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for rig.sink.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	rig.sink.mu.Lock()
	defer rig.sink.mu.Unlock()
	if len(rig.sink.events) != 1 || rig.sink.events[0].Code != uint16(remoteinput.ButtonA) {
		t.Fatalf("delivered %+v", rig.sink.events)
	}
}

func TestLauncherInputNoLiveStreamReturnsError(t *testing.T) {
	rig := newStreamRig(t)
	handler := launcherHandler(t, rig.input)
	req := launcherRequest("POST", "http://192.0.2.1/api/v1/launcher/input?session_id=abc", strings.NewReader(remotePadEvent+"\n"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
		t.Fatalf("missing stream: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"ready":true`) {
		t.Fatalf("missing stream accepted the pad: %s", rec.Body.String())
	}

	sessionID := rig.attach(t)
	if err := rig.server.Close(); err != nil {
		t.Fatal(err)
	}
	before := rig.sink.count()
	req = launcherRequest("POST", "http://192.0.2.1/api/v1/launcher/input?session_id="+sessionID, strings.NewReader(remotePadEvent+"\n"))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
		t.Fatalf("closed listener: %d %s", rec.Code, rec.Body.String())
	}
	if rig.sink.count() != before {
		t.Fatalf("closed listener delivered events: %d -> %d", before, rig.sink.count())
	}
}

func TestLauncherInputStaleSessionStaysConflictWhileStreamIsLive(t *testing.T) {
	rig := newStreamRig(t)
	rig.attach(t)
	handler := launcherHandler(t, rig.input)
	req := launcherRequest("POST", "http://192.0.2.1/api/v1/launcher/input?session_id=old", strings.NewReader("{}\n"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "no live input stream") {
		t.Fatalf("stale session: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSessionInputEventNoLiveStreamReturnsError(t *testing.T) {
	rig := newStreamRig(t)
	handler := hostapi.New(&launcherService{}, hostapi.WithRemoteInput(rig.input))
	rec := postSessionInput(handler, remotePadEvent)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
		t.Fatalf("missing stream: %d %s", rec.Code, rec.Body.String())
	}

	rig.attach(t)
	rec = postSessionInput(handler, remotePadEvent)
	if rec.Code != http.StatusOK {
		t.Fatalf("live stream: %d %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for rig.sink.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rig.sink.count() != 1 {
		t.Fatalf("live stream delivered %d", rig.sink.count())
	}

	if err := rig.server.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	before := rig.sink.count()
	rec = postSessionInput(handler, remotePadEvent)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
		t.Fatalf("closed listener: %d %s", rec.Code, rec.Body.String())
	}
	if rig.sink.count() != before {
		t.Fatalf("closed listener delivered events: %d -> %d", before, rig.sink.count())
	}
}

func postSessionInput(handler http.Handler, event string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", strings.NewReader(`{"event":`+eventJSON(event)+`}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func eventJSON(line string) string {
	var packet struct {
		Event json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal([]byte(line), &packet); err != nil || len(packet.Event) == 0 {
		return "null"
	}
	return string(packet.Event)
}

func TestSessionInputTapDeliversOrderedEdges(t *testing.T) {
	rig := newStreamRig(t)
	service := &fakeService{status: tapHIDStatus(), connection: fogcast.TargetConnection{State: "active"}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))
	session := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
	body := `{"session_id":"` + session + `","event":{"Device":0,"Kind":0,"Action":1,"Code":4116,"Value":0}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/tap", strings.NewReader(body))
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tap response: %d %s", rec.Code, rec.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for rig.sink.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	rig.sink.mu.Lock()
	defer rig.sink.mu.Unlock()
	if len(rig.sink.events) != 2 || rig.sink.events[0].Code != 4116 || rig.sink.events[0].Action != 1 || rig.sink.events[1].Code != 4116 || rig.sink.events[1].Action != 0 {
		t.Fatalf("unexpected tap frames: %+v", rig.sink.events)
	}
}

func TestSessionInputTapRejectsForeignLeaseAndInvalidKey(t *testing.T) {
	rig := newStreamRig(t)
	service := &fakeService{status: tapHIDStatus(), connection: fogcast.TargetConnection{State: "active"}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))
	session := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
	body := `{"session_id":"` + session + `","event":{"Device":0,"Kind":0,"Action":1,"Code":4100,"Value":0}}`
	foreign := &busyTargetService{fakeService: &fakeService{}, conn: fogcast.TargetConnection{State: "busy", Owner: "someone-else"}}
	for _, tc := range []struct {
		handler http.Handler
		body    string
		want    int
	}{
		{hostapi.New(foreign, hostapi.WithRemoteInput(rig.input)), body, http.StatusForbidden},
		{handler, strings.Replace(body, "4100", "4224", 1), http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/tap", strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		rec := httptest.NewRecorder()
		tc.handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("status %d want %d: %s", rec.Code, tc.want, rec.Body.String())
		}
	}
	if got := rig.sink.count(); got != 0 {
		t.Fatalf("rejected tap emitted %d events", got)
	}
}

func tapHIDStatus() protocol.Status {
	core := "fes.pcxt"
	return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7,
			ABI:              protocol.RuntimeContract{ID: "fes.computer", Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}}}}
}

func postTap(handler http.Handler, session string, usage int) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"session_id":%q,"event":{"Device":0,"Kind":0,"Action":1,"Code":%d,"Value":0}}`, session, usage)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/tap", strings.NewReader(body))
	req.Host = "127.0.0.1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestTapRequiresCurrentNegotiatedHIDKeyboard(t *testing.T) {
	for _, name := range []string{"native", "matrix", "future_hid", "stale_generation", "stale_interface"} {
		t.Run(name, func(t *testing.T) {
			rig := newStreamRig(t)
			status := tapHIDStatus()
			if name == "native" {
				status.Development = false
				status.CorePackage = nil
			} else if name == "matrix" {
				status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1}}
			} else if name == "future_hid" {
				status.CorePackage.Gamepad = true
				status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 2}}
			}
			service := &fakeService{status: status, connection: fogcast.TargetConnection{State: "active"}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))
			session := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
			if name == "stale_generation" {
				service.status.CorePackage.Generation++
			} else if name == "stale_interface" {
				service.status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1}}
			}
			// Q is dropped on a native SNES; A would be remapped to a gamepad.
			// The HID-only API must reject both without emitting either edge.
			for _, usage := range []int{0x1014, 0x1004} {
				if rec := postTap(handler, session, usage); rec.Code != http.StatusServiceUnavailable {
					t.Fatalf("%s usage %x accepted: %d %s", name, usage, rec.Code, rec.Body.String())
				}
			}
			if got := rig.sink.count(); got != 0 {
				t.Fatalf("rejected input emitted %d frames", got)
			}
		})
	}
}

func TestTapCapabilityChangeReplacesStream(t *testing.T) {
	rig := newStreamRig(t)
	status := tapHIDStatus()
	status.CorePackage.Gamepad = true
	service := &fakeService{status: status, connection: fogcast.TargetConnection{State: "active"}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))
	first := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
	service.status.CorePackage.ActiveInterfaces = nil
	second := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
	if second == first {
		t.Fatal("dropping HID kept the previous input attachment")
	}
	for _, session := range []string{first, second} {
		if rec := postTap(handler, session, 0x1014); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("tap accepted without HID: %d %s", rec.Code, rec.Body.String())
		}
	}
	if rig.sink.count() != 0 {
		t.Fatal("non-HID attachment received tap frames")
	}
	service.status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}}
	third := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
	if third == first || third == second {
		t.Fatal("restoring HID reused a stale attachment")
	}
	if rec := postTap(handler, third, 0x1014); rec.Code != http.StatusOK {
		t.Fatalf("tap rejected after HID was restored: %d %s", rec.Code, rec.Body.String())
	}
	awaitSinkFrames(t, rig, 2)
}

type tapTargetService struct {
	fakeService
	foreground  fogcast.TargetConnection
	connections map[string]fogcast.TargetConnection
}

// Embedding only the public service interface deliberately hides the
// optional connection observation methods.
type tapNoConnectionService struct{ hostapi.Service }

func TestTapRequiresUsableForegroundObservation(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  int
	}{
		{"ready", http.StatusOK}, {"active", http.StatusOK},
		{"", http.StatusForbidden}, {"disconnected", http.StatusForbidden},
		{"connecting", http.StatusForbidden}, {"version_mismatch", http.StatusForbidden},
		{"unexpected_state", http.StatusForbidden}, {"busy", http.StatusForbidden},
		{"recovery-required", http.StatusForbidden},
	} {
		t.Run(tc.state, func(t *testing.T) {
			rig := newStreamRig(t)
			service := &fakeService{status: tapHIDStatus(), connection: fogcast.TargetConnection{State: "active"}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))
			session := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
			service.connection.State = tc.state
			rec := postTap(handler, session, 0x1014)
			if rec.Code != tc.want {
				t.Fatalf("state %q: got %d, want %d: %s", tc.state, rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusOK {
				awaitSinkFrames(t, rig, 2)
			} else if rig.sink.count() != 0 {
				t.Fatal("unusable target received input")
			}
		})
	}
	t.Run("missing provider", func(t *testing.T) {
		rig := newStreamRig(t)
		service := &fakeService{status: tapHIDStatus()}
		handler := hostapi.New(tapNoConnectionService{service}, hostapi.WithRemoteInput(rig.input))
		session := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
		if rec := postTap(handler, session, 0x1014); rec.Code != http.StatusForbidden || rig.sink.count() != 0 {
			t.Fatalf("missing target observation accepted: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func (s *tapTargetService) TargetConnection() fogcast.TargetConnection { return s.foreground }
func (s *tapTargetService) TargetConnectionForName(name string) fogcast.TargetConnection {
	return s.connections[name]
}
func (s *tapTargetService) TargetNameForID(id string) string {
	if id == launcherIDB {
		return "kit-b"
	}
	return "kit-a"
}

func TestTapUsesSelectedTargetLeaseNotForeground(t *testing.T) {
	for _, tc := range []struct {
		name               string
		foreground, paired fogcast.TargetConnection
		want               int
	}{
		{"foreign foreground does not block owned paired target", fogcast.TargetConnection{State: "busy", Owner: "other"}, fogcast.TargetConnection{State: "ready"}, http.StatusOK},
		{"foreign paired target is rejected even when foreground is ready", fogcast.TargetConnection{State: "ready"}, fogcast.TargetConnection{State: "busy", Owner: "other"}, http.StatusForbidden},
		{"active paired target remains usable", fogcast.TargetConnection{State: "busy"}, fogcast.TargetConnection{State: "active"}, http.StatusOK},
		{"unobserved paired target fails closed", fogcast.TargetConnection{State: "ready"}, fogcast.TargetConnection{State: "disconnected"}, http.StatusForbidden},
		{"incompatible paired target fails closed", fogcast.TargetConnection{State: "ready"}, fogcast.TargetConnection{State: "version_mismatch"}, http.StatusForbidden},
		{"unknown paired target fails closed", fogcast.TargetConnection{State: "ready"}, fogcast.TargetConnection{State: "unexpected_state"}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newStreamRig(t)
			service := &tapTargetService{fakeService: fakeService{status: tapHIDStatus()}, foreground: tc.foreground,
				connections: map[string]fogcast.TargetConnection{"kit-b": tc.paired}}
			handler := hostapi.New(service, hostapi.WithRemoteInputFactory(func(target string) host.RemoteInputController {
				if target != "kit-b" {
					t.Fatalf("wrong input target %q", target)
				}
				return rig.input
			}))
			request := func(method, path, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Host = "127.0.0.1"
				req = req.WithContext(fogcast.WithPairedTarget(req.Context(), launcherIDB))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				return rec
			}
			session := sessionStreamID(t, request(http.MethodGet, "/api/v1/session", ""))
			body := fmt.Sprintf(`{"session_id":%q,"event":{"Device":0,"Kind":0,"Action":1,"Code":4116,"Value":0}}`, session)
			rec := request(http.MethodPost, "/api/v1/session/input/tap", body)
			if rec.Code != tc.want {
				t.Fatalf("tap status=%d want=%d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusOK {
				awaitSinkFrames(t, rig, 2)
			} else if rig.sink.count() != 0 {
				t.Fatal("foreign kit received an input frame")
			}
		})
	}
}

type gatedTapInput struct {
	fakeRemoteInput
	entered, proceed chan struct{}
	delivered        bool
}

func (r *gatedTapInput) TapKey(_ context.Context, _ string, _ remoteinput.Event, check func() error) (time.Duration, error) {
	close(r.entered)
	<-r.proceed
	if err := check(); err != nil {
		return 0, err
	}
	r.delivered = true
	return time.Millisecond, nil
}

func TestTapRejectsBindingChangedWhileWaiting(t *testing.T) {
	service := &fakeService{status: tapHIDStatus(), connection: fogcast.TargetConnection{State: "active"}}
	input := &gatedTapInput{entered: make(chan struct{}), proceed: make(chan struct{})}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	if rec := serve(t, handler, http.MethodGet, "/api/v1/session"); rec.Code != http.StatusOK {
		t.Fatalf("initial attach failed: %d %s", rec.Code, rec.Body.String())
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- postTap(handler, "original-session", 0x1014) }()
	<-input.entered
	// Reconcile a replaced runtime generation while the tap waits to enter the
	// ordered input stream. It must not write into that newer attachment.
	service.status.CorePackage.Generation++
	if rec := serve(t, handler, http.MethodGet, "/api/v1/session"); rec.Code != http.StatusOK {
		t.Fatalf("generation reconcile failed: %d %s", rec.Code, rec.Body.String())
	}
	close(input.proceed)
	select {
	case rec := <-done:
		if rec.Code != http.StatusServiceUnavailable || input.delivered {
			t.Fatalf("stale tap accepted: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("binding change deadlocked with queued tap")
	}
}
