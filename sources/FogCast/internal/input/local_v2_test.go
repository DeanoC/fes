package input

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type v2BackendFixture struct {
	c       *TargetController
	manager *kitlease.Manager
	token   string
	req     localhid.Request
	mu      sync.Mutex
	rows    []hidkeys.Rows
}

func newV2BackendFixture(t *testing.T) *v2BackendFixture {
	t.Helper()
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	f := &v2BackendFixture{manager: manager, token: grant.Token, req: localhid.Request{Publisher: strings.Repeat("1", 32), LeaseGeneration: grant.Status.Generation, RuntimeInstance: strings.Repeat("d", 32), PackageID: strings.Repeat("b", 64), CoreGeneration: 7, QueueEpoch: 1}}
	hid := &keyboardHIDSink{}
	hid.setPoster(func(_ context.Context, _ string, _ string, _ uint64, rows hidkeys.Rows) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.rows = append(f.rows, rows)
		return nil
	})
	ports := &controllerPortsSink{hid: hid}
	f.c = newTargetControllerWithSink("127.0.0.1:0", ports)
	f.c.ports = ports
	f.req.AgentInstance, _ = f.c.LocalAgentInstance()
	f.c.SetLocalLeaseManager(manager)
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration, KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}}, nil
	})
	ports.setObservation(CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration, KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}})
	f.c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: f.req.LeaseGeneration, binding: KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}}
	return f
}
func TestDelegatedV2BackendNeutralPreservesRemoteHold(t *testing.T) {
	f := newV2BackendFixture(t)
	shift, _ := hidkeys.Code(0xe1)
	if err := f.c.ports.hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	status, err := backend.Neutral(context.Background(), w)
	if err != nil || status != "neutral-confirmed" {
		t.Fatalf("neutral %q %v", status, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.rows[len(f.rows)-1]
	if last[8]&2 == 0 || last[0]&(1<<4) != 0 {
		t.Fatalf("remote hold lost or local key stuck: %+v", last)
	}
}

func TestDelegatedV2RejectsRestartWithReusedPackageGeneration(t *testing.T) {
	f := newV2BackendFixture(t)
	b := delegatedV2Backend{f.c}
	w, err := b.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	newInstance := strings.Repeat("e", 32)
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, RuntimeInstance: newInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration,
			KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: newInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}}, nil
	})
	if err := b.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("old runtime token wrote to successor")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 0 {
		t.Fatal("posted a stale edge")
	}
}

func TestDelegatedV2AmbiguousWriteFencesOtherSourcesUntilNeutral(t *testing.T) {
	f := newV2BackendFixture(t)
	hid := f.c.ports.hid
	shift, _ := hidkeys.Code(0xe1)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(shift, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	backend := delegatedV2Backend{f.c}
	witness, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	var posted []hidkeys.Rows
	fail := true
	hid.setPoster(func(_ context.Context, _ string, _ string, _ uint64, rows hidkeys.Rows) error {
		posted = append(posted, rows)
		if fail {
			return errors.New("ambiguous write")
		}
		return nil
	})
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("ambiguous edge succeeded")
	}
	if len(posted) != 1 {
		t.Fatalf("got %d posts", len(posted))
	}
	// The observation refresh used by unrelated pad/mouse input cannot replay it.
	binding := &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	_ = hid.replayUnconfirmed(context.Background(), binding)
	key, _ := hidkeys.Code(5)
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(key, remoteinput.ActionPress)); err == nil {
		t.Fatal("remote press accepted during uncertainty")
	}
	if len(posted) != 1 {
		t.Fatalf("ambiguous press was reposted: %d posts", len(posted))
	}
	fail = false
	if status, err := backend.Neutral(context.Background(), witness); err != nil || status != "neutral-confirmed" {
		t.Fatalf("neutral %q: %v", status, err)
	}
	last := posted[len(posted)-1]
	if last[8]&2 == 0 || last[0]&(1<<4) != 0 {
		t.Fatalf("neutral lost remote hold or retained local key: %v", last)
	}
	if err := hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(key, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
}

func TestDelegatedV2CancellationDuringSuccessfulPostFencesUnion(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { cancel(); return nil })
	if err := backend.Edge(ctx, w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("canceled post accepted")
	}
	hid := f.c.ports.hid
	hid.mu.Lock()
	defer hid.mu.Unlock()
	if hid.localV2Uncertain == nil {
		t.Fatal("canceled post not fenced")
	}
}

func TestDelegatedV2LifecycleCleanupDoesNotForgetUnconfirmedNeutral(t *testing.T) {
	for _, transition := range []string{"lease-release", "core-replacement"} {
		t.Run(transition, func(t *testing.T) {
			f := newV2BackendFixture(t)
			backend := delegatedV2Backend{f.c}
			w, err := backend.Open(context.Background(), f.req)
			if err != nil {
				t.Fatal(err)
			}
			hid := f.c.ports.hid
			hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return errors.New("unconfirmed") })
			if err := backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err == nil {
				t.Fatal("edge succeeded")
			}
			if transition == "lease-release" {
				err = f.c.ReleaseAll(context.Background())
			} else {
				_, err = f.c.BeginCoreReplacement(context.Background())
			}
			if err == nil {
				t.Fatal("cleanup falsely reported neutral")
			}
			hid.mu.Lock()
			if hid.localV2Uncertain == nil || hid.binding == nil {
				hid.mu.Unlock()
				t.Fatal("uncertain binding was forgotten")
			}
			hid.mu.Unlock()
			hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return nil })
			if err := f.c.ReleaseAll(context.Background()); err != nil {
				t.Fatalf("neutral retry: %v", err)
			}
			hid.mu.Lock()
			defer hid.mu.Unlock()
			if hid.localV2Uncertain != nil {
				t.Fatal("confirmed neutral did not clear fence")
			}
		})
	}
}
func TestDelegatedV2BackendQueuedTransitionRejectsInput(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	f.c.lifecycle.Lock()
	done := make(chan error, 1)
	go func() { done <- backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}) }()
	time.Sleep(25 * time.Millisecond)
	transition := f.c.beginLifecycleTransition()
	f.c.lifecycle.Unlock()
	err = <-done
	transition()
	if err == nil {
		t.Fatal("edge crossed queued lifecycle transition")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 0 {
		t.Fatal("edge posted across transition")
	}
}
func TestDelegatedV2BackendLeaseRevocationCancelsWait(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	f.c.lifecycle.Lock()
	done := make(chan error, 1)
	go func() { done <- backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}) }()
	time.Sleep(25 * time.Millisecond)
	if _, err := f.manager.Release(f.token); err != nil {
		f.c.lifecycle.Unlock()
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			f.c.lifecycle.Unlock()
			t.Fatal("revoked edge succeeded")
		}
	case <-time.After(time.Second):
		f.c.lifecycle.Unlock()
		t.Fatal("revoked admission did not drain")
	}
	f.c.lifecycle.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 0 {
		t.Fatal("revoked edge posted")
	}
}
func TestDelegatedV2HIDMutexCancellationDoesNotPost(t *testing.T) {
	f := newV2BackendFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	key, _ := hidkeys.Code(0x04)
	hid := f.c.ports.hid
	hid.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- hid.applyFrom(ctx, sourceLocal, keyboardFrameFor(key, remoteinput.ActionPress)) }()
	time.Sleep(25 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			hid.mu.Unlock()
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		hid.mu.Unlock()
		t.Fatal("canceled HID waiter stalled")
	}
	hid.mu.Unlock()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 0 {
		t.Fatal("canceled waiter posted")
	}
}
func TestDelegatedV2NeutralReportsUnconfirmedAndRetired(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error { return errors.New("ambiguous") })
	if status, err := backend.Neutral(context.Background(), w); err == nil || status != "unconfirmed" {
		t.Fatalf("failed neutral %q %v", status, err)
	}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: 8, KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: 8}}, nil
	})
	if status, err := backend.Neutral(context.Background(), w); err != nil || status != "old-generation-retired" {
		t.Fatalf("retired %q %v", status, err)
	}
}

func TestDelegatedV2RejectsTransitionQueuedDuringHIDWait(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	hid.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}) }()
	deadline := time.Now().Add(time.Second)
	for f.c.lifecycle.TryLock() {
		f.c.lifecycle.Unlock()
		if time.Now().After(deadline) {
			hid.mu.Unlock()
			t.Fatal("edge did not take lifecycle")
		}
		time.Sleep(time.Millisecond)
	}
	transition := f.c.beginLifecycleTransition()
	hid.mu.Unlock()
	err = <-done
	transition()
	if err == nil {
		t.Fatal("edge posted after transition queued")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rows) != 0 {
		t.Fatal("queued transition allowed HID post")
	}
}
func TestDelegatedV2NeutralDoesNotInferRetirementFromMissingHID(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	for _, obs := range []CoreObservation{{}, {Active: true}, {Active: false, PackageID: f.req.PackageID, Generation: 7}} {
		f.c.ObserveCore(func(context.Context) (CoreObservation, error) { return obs, nil })
		if status, err := backend.Neutral(context.Background(), w); err == nil || status != "unconfirmed" {
			t.Fatalf("unknown observation claimed retirement: %+v %q %v", obs, status, err)
		}
	}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}, nil
	})
	if status, err := backend.Neutral(context.Background(), w); err != nil || status != "old-generation-retired" {
		t.Fatalf("confirmed idle: %q %v", status, err)
	}
}
func TestDelegatedV2RevocationCancelsContendedHIDWait(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	hid.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}) }()
	time.Sleep(25 * time.Millisecond)
	if _, err := f.manager.Release(f.token); err != nil {
		hid.mu.Unlock()
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			hid.mu.Unlock()
			t.Fatal("revoked HID waiter succeeded")
		}
	case <-time.After(time.Second):
		hid.mu.Unlock()
		t.Fatal("revoked HID waiter stalled")
	}
	hid.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestDelegatedV2OpenHasIntrinsicHIDWaitBound(t *testing.T) {
	f := newV2BackendFixture(t)
	hid := f.c.ports.hid
	hid.mu.Lock()
	done := make(chan error, 1)
	go func() { _, err := (delegatedV2Backend{f.c}).Open(context.Background(), f.req); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			hid.mu.Unlock()
			t.Fatal("blocked open succeeded")
		}
	case <-time.After(3 * time.Second):
		hid.mu.Unlock()
		t.Fatal("open exceeded intrinsic bound")
	}
	hid.mu.Unlock()
}

func TestDelegatedV2OpenRefusesOldSuppressionAndMissingPoster(t *testing.T) {
	for _, mode := range []string{"blocked", "ambiguous", "nil-poster"} {
		t.Run(mode, func(t *testing.T) {
			f := newV2BackendFixture(t)
			hid := f.c.ports.hid
			hid.mu.Lock()
			switch mode {
			case "blocked":
				hid.blocked[sourceLocal] = map[uint8]bool{4: true}
			case "ambiguous":
				hid.shiftAmbiguous[sourceLocal] = true
			case "nil-poster":
				hid.poster = nil
			}
			hid.mu.Unlock()
			if _, err := (delegatedV2Backend{f.c}).Open(context.Background(), f.req); err == nil {
				t.Fatal("unclean source admitted")
			}
		})
	}
}
func TestDelegatedV2NilPosterCannotConfirmNeutralOrEdge(t *testing.T) {
	f := newV2BackendFixture(t)
	backend := delegatedV2Backend{f.c}
	w, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	hid := f.c.ports.hid
	hid.mu.Lock()
	hid.poster = nil
	hid.mu.Unlock()
	if err := backend.Edge(context.Background(), w, &localhid.Delta{Usage: 4, Down: false}); err == nil {
		t.Fatal("nil poster edge accepted")
	}
	if status, err := backend.Neutral(context.Background(), w); err == nil || status != "unconfirmed" {
		t.Fatalf("nil poster neutral %q %v", status, err)
	}
}

func TestDelegatedV2RawTrafficAllowlistRejectsKeyboardDisguises(t *testing.T) {
	frame := func(device remoteinput.Device, kind remoteinput.Kind, action remoteinput.Action) protocol.InputFrame {
		return protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Device: uint8(device), Kind: uint8(kind), Action: uint8(action)}
	}
	for _, tc := range []struct {
		name string
		f    protocol.InputFrame
		want bool
	}{
		{"gamepad-button", frame(remoteinput.DeviceGamepad, remoteinput.KindButton, remoteinput.ActionPress), true},
		{"gamepad-axis", frame(remoteinput.DeviceGamepad, remoteinput.KindAxis, remoteinput.ActionAbsolute), true},
		{"mouse-relative", frame(remoteinput.DeviceMouse, remoteinput.KindRelative, remoteinput.ActionRelative), true},
		{"keyboard", frame(remoteinput.DeviceKeyboard, remoteinput.KindKey, remoteinput.ActionPress), false},
		{"disguised-key", frame(remoteinput.DeviceGamepad, remoteinput.KindKey, remoteinput.ActionPress), false},
		{"wrong-axis-action", frame(remoteinput.DeviceGamepad, remoteinput.KindAxis, remoteinput.ActionPress), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegatedV2RawAllowed(tc.f); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}
