package input

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestDelegatedV2CanceledNeutralKeepsFence(t *testing.T) {
	f := newV2BackendFixture(t)
	b := delegatedV2Backend{f.c}
	w, err := b.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { cancel(); return nil })
	if status, err := b.Neutral(ctx, w); err == nil || status == "neutral-confirmed" {
		t.Fatalf("neutral %q %v", status, err)
	}
	h := f.c.ports.hid
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.localV2Uncertain == nil {
		t.Fatal("neutral cancellation cleared fence")
	}
}

func TestDelegatedV2RemoteDetachReportsUnconfirmedHID(t *testing.T) {
	f := newV2BackendFixture(t)
	hid := f.c.ports.hid
	shift, _ := hidkeys.Code(0xe1)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	b := delegatedV2Backend{f.c}
	w, err := b.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return errors.New("uncertain") })
	if err := b.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("edge succeeded")
	}
	server, err := bridge.New(bridge.Config{Token: []byte("0123456789abcdef"), Session: 1}, &remoteSourceSink{ports: f.c.ports})
	if err != nil {
		t.Fatal(err)
	}
	f.c.active = &lease{session: 1, server: server}
	if err := f.c.Detach(context.Background(), 1); err == nil {
		t.Fatal("detach falsely reported confirmed HID release")
	}
	var last hidkeys.Rows
	hid.setPoster(func(_ context.Context, _ string, _ string, _ uint64, rows hidkeys.Rows) error {
		last = rows
		return nil
	})
	if status, err := b.Neutral(context.Background(), w); err != nil || status != "neutral-confirmed" {
		t.Fatalf("neutral %q: %v", status, err)
	}
	if last[8]&2 != 0 || last[0]&(1<<4) != 0 {
		t.Fatalf("detached sources still held: %v", last)
	}
}

func TestDelegatedV2ManagerRevocationDoesNotReportFailedNeutralAsFree(t *testing.T) {
	f := newV2BackendFixture(t)
	manager := kitlease.New(time.Minute, func(ctx context.Context) error { return f.c.ReleaseAll(ctx) })
	defer manager.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("c", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = grant.Status.Generation
	f.c.SetLocalLeaseManager(manager)
	identity := KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	f.c.ports.hid.bind(&identity)
	f.c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: f.req.LeaseGeneration, binding: identity}
	b := delegatedV2Backend{f.c}
	w, err := b.Open(ctx, f.req)
	if err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return errors.New("unconfirmed") })
	if err := b.Edge(ctx, w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("edge succeeded")
	}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Settled(ctx)
	if err != nil || status.State != "blocked" {
		t.Fatalf("manager cleanup status %+v %v", status, err)
	}
	hid.mu.Lock()
	if hid.binding == nil || hid.localV2Uncertain == nil {
		hid.mu.Unlock()
		t.Fatal("manager forgot unconfirmed binding")
	}
	hid.mu.Unlock()
	hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return nil })
}

func TestDelegatedV2FailedCleanupStillNeutralizesGamepad(t *testing.T) {
	f := newV2BackendFixture(t)
	b := delegatedV2Backend{f.c}
	w, err := b.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	var buttons []uint8
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, _ uint8, value uint8, _ uint16) error {
		buttons = append(buttons, value)
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := f.c.ports.Apply(protocol.InputFrame{Device: 1, Kind: 1, Action: 1, Code: 104}); err != nil {
		t.Fatal(err)
	}
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return errors.New("uncertain") })
	if err := b.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("edge succeeded")
	}
	if err := f.c.ports.ReleaseAll(); err == nil {
		t.Fatal("lost HID cleanup error")
	}
	if len(buttons) < 2 || buttons[len(buttons)-1] != 0 {
		t.Fatalf("gamepad not neutralized: %v", buttons)
	}
}
