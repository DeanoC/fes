package input

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// Delays only the complete tap request on its way to the target. It models
// host-to-target jitter; the target controls the time between its own edges.
type delayedTapConn struct{ net.Conn }

func (c delayedTapConn) Write(p []byte) (int, error) {
	if len(p) > 9 && (p[9] == protocol.InputTypeTap || p[9] == protocol.InputTypeTapChord) {
		time.Sleep(150 * time.Millisecond)
	}
	return c.Conn.Write(p)
}

func TestTargetTimedChordAcrossHostBridgeAndHIDSink(t *testing.T) {
	var mu sync.Mutex
	var rows []hidkeys.Rows
	var times []time.Time
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _, _ string, generation uint64, r hidkeys.Rows) error {
		if generation != 7 {
			t.Errorf("wrong generation %d", generation)
		}
		mu.Lock()
		defer mu.Unlock()
		rows = append(rows, r)
		times = append(times, time.Now())
		return nil
	})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "test-package", Generation: 7}}, nil
	})
	t.Cleanup(func() { _ = controller.Close() })
	starter := host.BridgeStarterFunc(func(ctx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		if err := controller.Attach(ctx, Spec{Session: spec.Session, Token: spec.Token, Core: spec.Core}); err != nil {
			return nil, err
		}
		ready := make(chan struct{})
		close(ready)
		return &localTapBridge{controller: controller, session: spec.Session, ready: ready}, nil
	})
	remote, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour, DialTimeout: time.Second, ReconnectGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	if err := remote.AttachWithCapabilities(context.Background(), "fes.atari-st", true); err != nil {
		t.Fatal(err)
	}
	event := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 0x1004}
	start := time.Now()
	if _, err := remote.TapKeyChord(context.Background(), remote.Status().SessionID, 7, event, 2, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	if len(rows) != 2 || rows[0][0] != 1<<4 || rows[0][8] != 2 || rows[1] != (hidkeys.Rows{}) {
		t.Fatalf("target chord edges %+v", rows)
	}
	hold := times[1].Sub(times[0])
	if hold < 55*time.Millisecond || hold > 500*time.Millisecond || elapsed < 150*time.Millisecond || elapsed-hold < 100*time.Millisecond {
		t.Fatalf("transport delay/target hold: total=%v hold=%v", elapsed, hold)
	}
}

type localTapBridge struct {
	controller *TargetController
	session    uint64
	ready      chan struct{}
}

func (b *localTapBridge) Ready() <-chan struct{} { return b.ready }
func (*localTapBridge) Endpoint() string         { return "" }
func (b *localTapBridge) Dial(ctx context.Context) (net.Conn, error) {
	c, err := b.controller.OpenStream(ctx, b.session)
	if err != nil {
		return nil, err
	}
	return delayedTapConn{c}, nil
}
func (b *localTapBridge) Stop(ctx context.Context) error { return b.controller.Detach(ctx, b.session) }

func TestTargetTimedTapAcrossRealHostBridgeAndHIDSink(t *testing.T) {
	var mu sync.Mutex
	var rows []hidkeys.Rows
	var times []time.Time
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ string, generation uint64, r hidkeys.Rows) error {
		if generation != 7 {
			t.Errorf("wrong generation %d", generation)
		}
		mu.Lock()
		defer mu.Unlock()
		rows = append(rows, r)
		times = append(times, time.Now())
		return nil
	})
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "test-package", Generation: 7}}, nil
	})
	t.Cleanup(func() { _ = controller.Close() })
	starter := host.BridgeStarterFunc(func(ctx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		if err := controller.Attach(ctx, Spec{Session: spec.Session, Token: spec.Token, Core: spec.Core}); err != nil {
			return nil, err
		}
		ready := make(chan struct{})
		close(ready)
		return &localTapBridge{controller: controller, session: spec.Session, ready: ready}, nil
	})
	remote, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour, DialTimeout: time.Second, ReconnectGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	if err := remote.AttachWithCapabilities(context.Background(), "fes.atari-st", true); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	event := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 0x1004}
	if _, err := remote.TapKey(context.Background(), remote.Status().SessionID, 7, event, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	if len(rows) != 2 || rows[0][0] != 1<<4 || rows[1] != (hidkeys.Rows{}) {
		t.Fatalf("target edges %+v", rows)
	}
	hold := times[1].Sub(times[0])
	if hold < 55*time.Millisecond || hold > 500*time.Millisecond {
		t.Fatalf("target hold %v", hold)
	}
	if elapsed < 150*time.Millisecond || elapsed-hold < 100*time.Millisecond {
		t.Fatalf("injected transport delay not observed: total=%v hold=%v", elapsed, hold)
	}
}

func TestTargetTapCancelThroughRealBridgeReleases(t *testing.T) {
	posts := make(chan hidkeys.Rows, 8)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ string, _ uint64, r hidkeys.Rows) error { posts <- r; return nil })
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	t.Cleanup(func() { _ = controller.Close() })
	starter := host.BridgeStarterFunc(func(ctx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		if err := controller.Attach(ctx, Spec{Session: spec.Session, Token: spec.Token, Core: spec.Core}); err != nil {
			return nil, err
		}
		ready := make(chan struct{})
		close(ready)
		return &localTapBridge{controller: controller, session: spec.Session, ready: ready}, nil
	})
	remote, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour, DialTimeout: time.Second, ReconnectGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	if err := remote.AttachWithCapabilities(context.Background(), "fes.atari-st", true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := remote.TapKey(ctx, remote.Status().SessionID, 7, remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 0x1004}, func() error { return nil })
		done <- err
	}()
	select {
	case r := <-posts:
		if r[0] != 1<<4 {
			t.Fatalf("press: %v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no press")
	}
	cancel()
	select {
	case r := <-posts:
		if r != (hidkeys.Rows{}) {
			t.Fatalf("release: %v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no release after cancel")
	}
	select {
	case err := <-done:
		if !errors.Is(err, host.ErrRemoteInputUnconfirmed) {
			t.Fatalf("tap error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tap did not finish")
	}
}

// notifiedTapConn reports that the host's tap frame crossed the existing
// bridge connection, before the target processes it. It is only a test wrapper
// around the same connection used by localTapBridge.
type notifiedTapConn struct {
	net.Conn
	sent chan struct{}
	once *sync.Once
}

func (c notifiedTapConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if len(p) > 9 && p[9] == protocol.InputTypeTap && err == nil && n == len(p) {
		c.once.Do(func() { close(c.sent) })
	}
	return n, err
}

type notifiedTapBridge struct {
	*localTapBridge
	sent chan struct{}
	once sync.Once
}

func (b *notifiedTapBridge) Dial(ctx context.Context) (net.Conn, error) {
	conn, err := b.controller.OpenStream(ctx, b.session)
	if err != nil {
		return nil, err
	}
	return notifiedTapConn{Conn: conn, sent: b.sent, once: &b.once}, nil
}

// Revocation cancels the bridge's target-side tap even while the authenticated
// CONNECT admission remains outstanding and cleanup has not begun. The socket
// and poster here are the existing in-memory host/bridge test path; no kit or
// local-input device is involved.
func TestTargetTapWaitingForLifecycleStopsOnLeaseRevocation(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	lifetime, endConnect, err := manager.Begin(grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer endConnect()
	posts := make(chan hidkeys.Rows, 8)
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _, _ string, _ uint64, rows hidkeys.Rows) error { posts <- rows; return nil })
	ports := &controllerPortsSink{hid: hid}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: "p", Generation: 7}}, nil
	})
	t.Cleanup(func() { _ = controller.Close() })
	sent := make(chan struct{})
	starter := host.BridgeStarterFunc(func(startCtx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		startCtx = kitlease.WithAdmission(kitlease.WithLeaseLifetime(startCtx, lifetime))
		if err := controller.Attach(startCtx, Spec{Session: spec.Session, Token: spec.Token, Core: spec.Core}); err != nil {
			return nil, err
		}
		ready := make(chan struct{})
		close(ready)
		return &notifiedTapBridge{localTapBridge: &localTapBridge{controller: controller, session: spec.Session, ready: ready}, sent: sent}, nil
	})
	remote, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour, DialTimeout: time.Second, ReconnectGrace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	if err := remote.AttachWithCapabilities(ctx, "fes.c64", true); err != nil {
		t.Fatal(err)
	}
	controller.lifecycle.Lock()
	tapDone := make(chan error, 1)
	go func() {
		_, err := remote.TapKey(ctx, remote.Status().SessionID, 7, remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 0x1025}, func() error { return nil })
		tapDone <- err
	}()
	select {
	case <-sent:
	case <-ctx.Done():
		controller.lifecycle.Unlock()
		t.Fatal("tap frame was not sent")
	}
	if _, err := manager.Release(grant.Token); err != nil {
		controller.lifecycle.Unlock()
		t.Fatal(err)
	}
	select {
	case <-lifetime.Done():
	case <-ctx.Done():
		controller.lifecycle.Unlock()
		t.Fatal("lease lifetime was not canceled")
	}
	controller.lifecycle.Unlock()
	select {
	case err := <-tapDone:
		if !errors.Is(err, host.ErrRemoteInputUnconfirmed) {
			t.Fatalf("revoked tap: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("revoked tap did not finish")
	}
	select {
	case rows := <-posts:
		t.Fatalf("revoked tap reached the HID poster: %+v", rows)
	default:
	}
	endConnect()
}
