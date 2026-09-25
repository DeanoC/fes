package kitlauncher

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func listenLocalInput(t *testing.T, frames chan<- protocol.InputFrame) (net.Listener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "local-input.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			frame, err := protocol.DecodeInputFrame(reader, 4096)
			if err != nil {
				return
			}
			select {
			case frames <- frame:
			default:
			}
		}
	}()
	return ln, path
}

func TestLocalFeedKeepsPlayerAndRawKeyboard(t *testing.T) {
	frames := make(chan protocol.InputFrame, 4)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	feed := newLocalFeed(path)
	defer feed.Close()
	now := time.Unix(1, 0)
	pad := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := feed.send(pad, now); err != nil {
		t.Fatal(err)
	}
	key := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: zx81keys.Letter('J')}
	if err := feed.send(key, now); err != nil {
		t.Fatal(err)
	}
	gotPad := awaitFrame(t, frames)
	gotKey := awaitFrame(t, frames)
	if gotPad.Player != 1 || gotPad.Code != uint16(remoteinput.ButtonA) || gotPad.Header.Session != localInputSession {
		t.Fatalf("pad frame %+v", gotPad)
	}
	if gotKey.Device != uint8(remoteinput.DeviceKeyboard) || gotKey.Kind != uint8(remoteinput.KindKey) || gotKey.Code != uint16(zx81keys.Letter('J')) {
		t.Fatalf("keyboard frame %+v", gotKey)
	}
}

func TestRunFeedsLocalSocketWithoutHostInputReady(t *testing.T) {
	frames := make(chan protocol.InputFrame, 4)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	var hostInput atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/launcher/input" {
			hostInput.Add(1)
		}
		http.Error(w, "host down", http.StatusBadGateway)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = path
	pad := &oncePad{event: remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonB}}
	_ = Run(ctx, client, func(m Model) {
		if m.Message == localInputUnavailableMessage {
			t.Errorf("socket was up, message %q", m.Message)
		}
	}, func() (Pad, error) { return pad, nil })
	frame := awaitFrame(t, frames)
	if frame.Player != 1 || frame.Code != uint16(remoteinput.ButtonB) {
		t.Fatalf("frame %+v", frame)
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
}

func TestRunForeignLeaseStillFeedsLocalSocket(t *testing.T) {
	frames := make(chan protocol.InputFrame, 4)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	var hostInput atomic.Int64
	var foreign atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle","input":{"state":"detached","ready":false}}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true,"connection":{"state":"busy","owner":"other"}}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[]}`))
		case "/api/v1/launcher/input":
			hostInput.Add(1)
			http.Error(w, "kit pads must not post here", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = path
	pad := &oncePad{event: remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonDPadLeft}}
	_ = Run(ctx, client, func(m Model) {
		if m.ForeignLease {
			foreign.Store(true)
		}
	}, func() (Pad, error) { return pad, nil })
	frame := awaitFrame(t, frames)
	if frame.Code != uint16(remoteinput.ButtonDPadLeft) {
		t.Fatalf("frame %+v", frame)
	}
	if !foreign.Load() {
		t.Fatal("foreign lease was not recorded")
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
}

func TestRunMissingLocalSocketDoesNotFallBackToHost(t *testing.T) {
	var hostInput atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/launcher/input" {
			hostInput.Add(1)
		}
		http.Error(w, "host down", http.StatusBadGateway)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = filepath.Join(t.TempDir(), "missing-local-input.sock")
	var saw atomic.Bool
	_ = Run(ctx, client, func(m Model) {
		if m.Message == localInputUnavailableMessage {
			saw.Store(true)
			cancel()
		}
	}, func() (Pad, error) {
		return &oncePad{event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}}, nil
	})
	if !saw.Load() {
		t.Fatal("missing socket did not report local input unavailable")
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
}

func TestRunIdleCoreDoesNotOpenLocalSocket(t *testing.T) {
	accepted := make(chan struct{}, 1)
	path := filepath.Join(t.TempDir(), "local-input.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		accepted <- struct{}{}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "host down", http.StatusBadGateway)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return false, nil }
	client.localInputPath = path
	_ = Run(ctx, client, func(Model) {}, func() (Pad, error) {
		return &oncePad{event: remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}}, nil
	})
	select {
	case <-accepted:
		t.Fatal("browse input opened the local socket")
	default:
	}
}

func TestRuntimeCoreBoundMatchesAgentObservation(t *testing.T) {
	gen := uint64(4)
	bound := misterruntime.Protocol2Response{
		OK: true, State: "running_development",
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: "pkg"},
		Generation:    &gen,
	}
	if !runtimeCoreBound(bound) {
		t.Fatal("active package was not bound")
	}
	idle := bound
	idle.State = "idle"
	if runtimeCoreBound(idle) {
		t.Fatal("idle status was bound")
	}
	zero := uint64(0)
	bound.Generation = &zero
	if runtimeCoreBound(bound) {
		t.Fatal("generation 0 was bound")
	}
	bound.Generation = nil
	if runtimeCoreBound(bound) {
		t.Fatal("missing generation was bound")
	}
	bound.Generation = &gen
	bound.ActivePackage = nil
	if runtimeCoreBound(bound) {
		t.Fatal("missing package was bound")
	}
}

func TestProbeRuntimeCoreReadsIdleStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const idle = `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 64)
		_, _ = conn.Read(buf)
		_, _ = conn.Write([]byte(idle + "\n"))
	}()
	bound, err := probeRuntimeCore(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if bound {
		t.Fatal("idle runtime reported a bound core")
	}
	_, err = probeRuntimeCore(context.Background(), filepath.Join(t.TempDir(), "missing.sock"))
	if err == nil {
		t.Fatal("missing runtime socket succeeded")
	}
}

type oncePad struct {
	event remoteinput.Event
	n     int
}

func (p *oncePad) Poll() ([]remoteinput.Event, error) {
	if p.n > 0 {
		return nil, nil
	}
	p.n++
	return []remoteinput.Event{p.event}, nil
}

func (*oncePad) Close() error { return nil }

func awaitFrame(t *testing.T, frames <-chan protocol.InputFrame) protocol.InputFrame {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a local input frame")
		return protocol.InputFrame{}
	}
}
