package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
	if rec.Code/100 == 2 || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
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
	if rec.Code/100 == 2 || !strings.Contains(rec.Body.String(), "no live input stream") {
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
	if rec.Code/100 == 2 || !strings.Contains(rec.Body.String(), "INPUT_UNAVAILABLE") || !strings.Contains(rec.Body.String(), "no live input stream") {
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
	if rec.Code/100 == 2 || !strings.Contains(rec.Body.String(), "no live input stream") {
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
