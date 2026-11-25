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

func TestDelegatedV2ManagerRevocationRetiresUncertainBindingAfterCoreStopsToIdle(t *testing.T) {
	f := newV2BackendFixture(t)
	// Include an unbound matrix sink alongside HID, matching non-matrix cores like fes.pcxt-dos.
	keys := NewKeyboardSink()
	keys.SetPoster(func(context.Context, localhid.MatrixPost, uint64) error {
		return errors.New("unbound matrix poster unavailable")
	})
	f.c.ports.keys = keys
	f.c.keyboard = keys

	currentObs := CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) { return currentObs, nil })
	setActive := func(instance, pkg string, gen uint64) {
		currentObs = CoreObservation{
			Active:          true,
			CoreID:          "fes.pcxt-dos",
			RuntimeInstance: instance,
			PackageID:       pkg,
			Generation:      gen,
			KeyboardHID:     &KeyboardHIDBinding{RuntimeInstance: instance, PackageID: pkg, Generation: gen},
		}
	}

	manager := kitlease.New(time.Minute, func(ctx context.Context) error { return f.c.ReleaseAll(ctx) })
	defer manager.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if status, err := manager.Settled(ctx); err != nil || status.State != "free" {
		t.Fatalf("startup lease reconciliation failed: status=%+v err=%v", status, err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("c", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = grant.Status.Generation
	f.c.SetLocalLeaseManager(manager)
	setActive(f.req.RuntimeInstance, f.req.PackageID, f.req.CoreGeneration)
	identity := KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	f.c.ports.hid.bind(&identity)
	f.c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: f.req.LeaseGeneration, binding: identity}

	b := delegatedV2Backend{f.c}
	w, err := b.Open(ctx, f.req)
	if err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error {
		return errors.New("runtime busy during stop flush")
	})
	if err := b.Edge(ctx, w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("edge succeeded during failed post")
	}

	// Simulate Stop() having completed to idle: poster for the retired generation still fails.
	currentObs = CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Settled(ctx)
	if err != nil || status.State != "free" {
		t.Fatalf("expected free lease after idle retirement, got status=%+v err=%v", status, err)
	}
	hid.mu.Lock()
	defer hid.mu.Unlock()
	if hid.localV2Uncertain != nil || hid.binding != nil {
		t.Fatalf("uncertain or stale HID binding remained after idle retirement: uncertain=%+v binding=%+v", hid.localV2Uncertain, hid.binding)
	}
}

func TestRetireLocalV2ObservedPolicies(t *testing.T) {
	instance := strings.Repeat("a", 32)
	pkg := strings.Repeat("b", 64)
	gen := uint64(7)
	otherInstance := strings.Repeat("c", 32)
	otherPkg := strings.Repeat("d", 64)

	newUncertainSink := func() *keyboardHIDSink {
		binding := KeyboardHIDBinding{RuntimeInstance: instance, PackageID: pkg, Generation: gen}
		s := &keyboardHIDSink{}
		s.bind(&binding)
		s.localV2Uncertain = &binding
		return s
	}

	t.Run("fails closed when same generation still active or observation invalid", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			obs  CoreObservation
			err  error
		}{
			{"same-active", CoreObservation{Active: true, RuntimeInstance: instance, PackageID: pkg, Generation: gen}, nil},
			{"unconfirmed", CoreObservation{RuntimeInstance: instance}, nil},
			{"missing-instance", CoreObservation{IdleConfirmed: true}, nil},
			{"idle-with-generation", CoreObservation{IdleConfirmed: true, RuntimeInstance: instance, Generation: gen}, nil},
			{"observe-error", CoreObservation{}, errors.New("unavailable")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := newUncertainSink()
				retired, err := s.retireLocalV2Observed(
					context.Background(),
					func(context.Context) (CoreObservation, error) { return tc.obs, tc.err },
					func(context.Context) error { return nil },
				)
				if err == nil || retired {
					t.Fatalf("retireLocalV2Observed(%s) = (%v, %v), want error", tc.name, retired, err)
				}
				if s.localV2Uncertain == nil || s.binding == nil {
					t.Fatalf("uncertain binding cleared on %s", tc.name)
				}
			})
		}
	})

	t.Run("fails closed when validation flips during observe", func(t *testing.T) {
		s := newUncertainSink()
		calls := 0
		retired, err := s.retireLocalV2Observed(
			context.Background(),
			func(context.Context) (CoreObservation, error) {
				return CoreObservation{IdleConfirmed: true, RuntimeInstance: instance}, nil
			},
			func(context.Context) error {
				calls++
				if calls > 1 {
					return errNoCore
				}
				return nil
			},
		)
		if err == nil || retired || s.localV2Uncertain == nil {
			t.Fatalf("validation change did not reject retirement: retired=%v err=%v uncertain=%+v", retired, err, s.localV2Uncertain)
		}
	})

	t.Run("retires when replacement core observed active", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			obs  CoreObservation
		}{
			{"new-instance", CoreObservation{Active: true, RuntimeInstance: otherInstance, PackageID: pkg, Generation: gen}},
			{"new-package", CoreObservation{Active: true, RuntimeInstance: instance, PackageID: otherPkg, Generation: gen}},
			{"new-generation", CoreObservation{Active: true, RuntimeInstance: instance, PackageID: pkg, Generation: gen + 1}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := newUncertainSink()
				retired, err := s.retireLocalV2Observed(
					context.Background(),
					func(context.Context) (CoreObservation, error) { return tc.obs, nil },
					func(context.Context) error { return nil },
				)
				if err != nil || !retired {
					t.Fatalf("retireLocalV2Observed(%s) = (%v, %v), want retired", tc.name, retired, err)
				}
				if s.localV2Uncertain != nil || s.binding != nil {
					t.Fatalf("uncertain or binding remained after %s retirement", tc.name)
				}
			})
		}
	})
}
