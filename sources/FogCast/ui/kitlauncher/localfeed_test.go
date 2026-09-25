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

func TestRunBoundCoreIdleSessionDoesNotBrowseOrLaunch(t *testing.T) {
	frames := make(chan protocol.InputFrame, 16)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	var launches, hostInput atomic.Int64
	server := launcherCatalogServer(t, &launches, &hostInput, nil, `{"state":"idle","input":{"state":"detached","ready":false}}`,
		`{"ready":true,"target":{"reachable":true,"ready":true,"connection":{"state":"busy","owner":"other"}}}`)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = path
	pad := &scriptPad{events: boundPlayEvents()}
	var shelf atomic.Value
	var pack atomic.Value
	var wheelOpen atomic.Bool
	var searchOpen atomic.Bool
	var fromWheel atomic.Bool
	var focus atomic.Int32
	var sawNav atomic.Bool
	var foreign atomic.Bool
	_ = Run(ctx, client, func(m Model) {
		if m.ForeignLease {
			foreign.Store(true)
		}
		if len(m.Shelves) >= 2 && m.Connected && m.WheelOpen {
			pad.arm.Store(true)
		}
		if pad.sent.Load() {
			shelf.Store(m.Shelf)
			pack.Store(m.Pack)
			wheelOpen.Store(m.WheelOpen)
			searchOpen.Store(m.SearchOpen)
			fromWheel.Store(m.fromWheel)
			focus.Store(int32(m.Focus))
			sawNav.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return pad, nil })
	if !sawNav.Load() {
		t.Fatal("bound-core navigation was not observed")
	}
	if !foreign.Load() {
		t.Fatal("foreign lease was not recorded")
	}
	if got, _ := shelf.Load().(string); got != "all" {
		t.Fatalf("shelf %q, want all", got)
	}
	if got, _ := pack.Load().(string); got != "" {
		t.Fatalf("pack %q, want unchanged", got)
	}
	if !wheelOpen.Load() || fromWheel.Load() || searchOpen.Load() || focus.Load() != 0 {
		t.Fatalf("wheel=%v fromWheel=%v search=%v focus=%d, want the idle wheel", wheelOpen.Load(), fromWheel.Load(), searchOpen.Load(), focus.Load())
	}
	if launches.Load() != 0 {
		t.Fatalf("launches = %d, want none", launches.Load())
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
	got := collectFrames(t, frames, 7)
	if !frameHas(got, uint16(remoteinput.ButtonDPadRight)) || !frameHas(got, uint16(remoteinput.ButtonA)) || !frameHas(got, uint16(zx81keys.Letter('J'))) {
		t.Fatalf("frames missing play input: %+v", got)
	}
}

func TestRunBoundCoreStopChordStillPosts(t *testing.T) {
	frames := make(chan protocol.InputFrame, 8)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	stopped := make(chan struct{}, 1)
	var launches, hostInput atomic.Int64
	server := launcherCatalogServer(t, &launches, &hostInput, stopped,
		`{"state":"active","execution":"fpga_development","input":{"state":"detached","ready":false},"core_package":{"generation":9,"gamepad":true}}`,
		`{"ready":true,"target":{"reachable":true,"ready":true}}`)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = path
	pad := &scriptPad{repeat: true, events: []remoteinput.Event{
		buttonPress(remoteinput.ButtonA),
		buttonPress(remoteinput.ButtonSelect),
		buttonPress(remoteinput.ButtonStart),
	}}
	pad.arm.Store(true)
	var wheelOpen atomic.Bool
	var sawWheel atomic.Bool
	_ = Run(ctx, client, func(m Model) {
		if pad.sent.Load() && m.WheelOpen && !m.Busy {
			wheelOpen.Store(m.WheelOpen)
			sawWheel.Store(true)
		}
	}, func() (Pad, error) { return pad, nil })
	select {
	case <-stopped:
	default:
		t.Fatal("bound-core Stop chord was not dispatched")
	}
	if launches.Load() != 0 {
		t.Fatalf("launches = %d, want none", launches.Load())
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
	if !sawWheel.Load() || !wheelOpen.Load() {
		t.Fatal("Stop chord moved the platform wheel")
	}
	if !frameHas(collectFrames(t, frames, 1), uint16(remoteinput.ButtonA)) {
		t.Fatal("Stop chord did not also feed play input")
	}
}

func TestRunUnboundCoreStillBrowses(t *testing.T) {
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
	var launches, hostInput atomic.Int64
	server := launcherCatalogServer(t, &launches, &hostInput, nil,
		`{"state":"idle","input":{"state":"detached","ready":false}}`,
		`{"ready":true,"target":{"reachable":true,"ready":true}}`)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL, HPSFramebuffer: true})
	client.localCore = func(context.Context) (bool, error) { return false, nil }
	client.localInputPath = path
	pad := &scriptPad{events: []remoteinput.Event{
		buttonPress(remoteinput.ButtonDPadRight),
		buttonPress(remoteinput.ButtonA),
	}}
	var shelf atomic.Value
	var wheelOpen atomic.Bool
	var sawNav atomic.Bool
	_ = Run(ctx, client, func(m Model) {
		if len(m.Shelves) >= 2 && m.Connected && m.WheelOpen && !pad.sent.Load() {
			pad.arm.Store(true)
		}
		if pad.sent.Load() {
			shelf.Store(m.Shelf)
			wheelOpen.Store(m.WheelOpen)
			sawNav.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return pad, nil })
	if !sawNav.Load() {
		t.Fatal("browse navigation was not observed")
	}
	if got, _ := shelf.Load().(string); got != "pong" {
		t.Fatalf("shelf %q, want pong", got)
	}
	if wheelOpen.Load() {
		t.Fatal("A did not leave the platform wheel")
	}
	if launches.Load() != 0 {
		t.Fatalf("launches = %d, want none from a single A", launches.Load())
	}
	select {
	case <-accepted:
		t.Fatal("browse input opened the local socket")
	default:
	}
}

func launcherCatalogServer(t *testing.T, launches, hostInput *atomic.Int64, stopped chan struct{}, sessionBody, healthBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(sessionBody))
		case "/api/v1/health":
			_, _ = w.Write([]byte(healthBody))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"pong","game_count":1},{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			switch r.URL.Query().Get("platform") {
			case "pong":
				_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","system":"pong","state":"available","root_online":true,"launchable":true}]}`))
			case "megadrive":
				_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","state":"available","root_online":true,"launchable":true}]}`))
			default:
				_, _ = w.Write([]byte(`{"games":[]}`))
			}
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/api/v1/launcher/input":
			if hostInput != nil {
				hostInput.Add(1)
			}
			http.Error(w, "kit pads must not post here", http.StatusConflict)
		case "/api/v1/session/launch":
			if launches != nil {
				launches.Add(1)
			}
			http.Error(w, "launcher navigation must not launch", http.StatusConflict)
		case "/api/v1/session/stop":
			if stopped != nil {
				select {
				case stopped <- struct{}{}:
				default:
				}
			}
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func buttonPress(code remoteinput.Code) remoteinput.Event {
	return remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: code}
}

func boundPlayEvents() []remoteinput.Event {
	return []remoteinput.Event{
		buttonPress(remoteinput.ButtonDPadRight),
		buttonPress(remoteinput.ButtonA),
		buttonPress(remoteinput.ButtonA),
		buttonPress(remoteinput.ButtonSelect),
		buttonPress(remoteinput.ButtonStart),
		buttonPress(remoteinput.ButtonX),
		{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: zx81keys.Letter('J')},
	}
}

func frameHas(frames []protocol.InputFrame, code uint16) bool {
	for _, frame := range frames {
		if frame.Code == code {
			return true
		}
	}
	return false
}

func collectFrames(t *testing.T, frames <-chan protocol.InputFrame, want int) []protocol.InputFrame {
	t.Helper()
	got := make([]protocol.InputFrame, 0, want)
	deadline := time.After(2 * time.Second)
	for len(got) < want {
		select {
		case frame := <-frames:
			got = append(got, frame)
		case <-deadline:
			t.Fatalf("got %d local frames, want %d", len(got), want)
		}
	}
	return got
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

type scriptPad struct {
	arm    atomic.Bool
	sent   atomic.Bool
	repeat bool
	events []remoteinput.Event
}

func (p *scriptPad) Poll() ([]remoteinput.Event, error) {
	if !p.arm.Load() {
		return nil, nil
	}
	if p.sent.Load() && !p.repeat {
		return nil, nil
	}
	p.sent.Store(true)
	out := make([]remoteinput.Event, len(p.events))
	copy(out, p.events)
	return out, nil
}

func (*scriptPad) Close() error { return nil }

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
