package kitlauncher

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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
	feed := newLocalFeed(path, nil)
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

func TestLocalInputDefaultsStayUnset(t *testing.T) {
	client := NewClient(Config{})
	if client.localInputSocket() != "" {
		t.Fatalf("socket %q, want an injected path", client.localInputSocket())
	}
	bound, err := client.readLocalCore(context.Background())
	if err != nil || bound {
		t.Fatalf("probe bound=%v err=%v, want unbound", bound, err)
	}
}

func TestSetLocalInputInstallsProbeAndSocket(t *testing.T) {
	client := NewClient(Config{})
	called := false
	client.SetLocalInput("/run/fogcast/local-input.sock", func(context.Context) (bool, error) {
		called = true
		return true, nil
	})
	if client.localInputSocket() != "/run/fogcast/local-input.sock" {
		t.Fatal(client.localInputSocket())
	}
	bound, err := client.readLocalCore(context.Background())
	if err != nil || !bound || !called {
		t.Fatalf("bound=%v err=%v called=%v", bound, err, called)
	}
}

func TestLocalFeedUnavailableDialsOnceUntilCooldown(t *testing.T) {
	var dials atomic.Int64
	feed := newLocalFeed(filepath.Join(t.TempDir(), "missing.sock"), func(network, address string, timeout time.Duration) (net.Conn, error) {
		if network != "unix" || timeout != localInputDial {
			t.Errorf("dial %s %s", network, timeout)
		}
		dials.Add(1)
		return nil, errors.New("missing")
	})
	now := time.Unix(10, 0)
	event := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	for i := 0; i < 8; i++ {
		if err := feed.send(event, now); !errors.Is(err, errLocalInputUnavailable) {
			t.Fatal(err)
		}
	}
	if dials.Load() != 1 {
		t.Fatalf("dials = %d, want 1", dials.Load())
	}
	if err := feed.send(event, now.Add(localInputRetry)); !errors.Is(err, errLocalInputUnavailable) {
		t.Fatal(err)
	}
	if dials.Load() != 2 {
		t.Fatalf("dials after cooldown = %d, want 2", dials.Load())
	}
}

func TestRunMissingSocketDialsOncePerCooldown(t *testing.T) {
	var dials atomic.Int64
	var hostInput atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/launcher/input" {
			hostInput.Add(1)
		}
		http.Error(w, "host down", http.StatusBadGateway)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = filepath.Join(t.TempDir(), "missing-local-input.sock")
	client.localDial = func(network, address string, timeout time.Duration) (net.Conn, error) {
		if network != "unix" || timeout != localInputDial {
			t.Errorf("dial %s %s", network, timeout)
		}
		dials.Add(1)
		return nil, errors.New("missing")
	}
	pad := &repeatPad{events: []remoteinput.Event{
		{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA},
		{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonB},
		{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: 20000},
		{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftY, Value: -20000},
		{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 36},
		{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionRelease, Code: remoteinput.ButtonA},
	}}
	var saw atomic.Bool
	started := time.Now()
	_ = Run(ctx, client, func(m Model) {
		if m.Message == localInputUnavailableMessage {
			saw.Store(true)
		}
	}, func() (Pad, error) { return pad, nil })
	elapsed := time.Since(started)
	got := dials.Load()
	polls := pad.polls.Load()
	if polls < 20 {
		t.Fatalf("polls = %d, want enough repeats to show a dial storm", polls)
	}
	// One dial per cooldown, not one per event. 2.2s covers two or three tries.
	if got < 2 || got > 4 {
		t.Fatalf("dials = %d over %d polls in %s, want one per %s", got, polls, elapsed, localInputRetry)
	}
	if !saw.Load() {
		t.Fatal("missing socket did not report local input unavailable")
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
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

type repeatPad struct {
	events []remoteinput.Event
	polls  atomic.Int64
}

func (p *repeatPad) Poll() ([]remoteinput.Event, error) {
	p.polls.Add(1)
	return p.events, nil
}

func (*repeatPad) Close() error { return nil }

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
