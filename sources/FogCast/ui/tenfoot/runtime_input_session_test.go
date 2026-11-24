package tenfoot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

type runtimeSessionTransport struct {
	calls        atomic.Int32
	firstStarted chan struct{}
	releaseFirst chan struct{}
	firstError   bool
}

func (r *runtimeSessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := r.calls.Add(1)
	instance := strings.Repeat("b", 32)
	if n == 1 {
		close(r.firstStarted)
		<-r.releaseFirst
		if r.firstError {
			return nil, fmt.Errorf("old observation failed")
		}
		instance = strings.Repeat("a", 32)
	}
	body := fmt.Sprintf(`{"id":"same","target_id":"kit","state":"active","core_package":{"package_id":"%s","generation":7},"runtime_input":{"kind":"described","runtime_instance":"%s","package_id":"%s","generation":7}}`, strings.Repeat("c", 64), instance, strings.Repeat("c", 64))
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRuntimeSessionRejectsOutOfOrderSuccessAndFailure(t *testing.T) {
	for _, oldError := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-success", true: "old-error"}[oldError], func(t *testing.T) {
			transport := &runtimeSessionTransport{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{}), firstError: oldError}
			app := NewApp(NewClient("http://unused.invalid", &http.Client{Transport: transport}), 1280, 720, 20)
			defer app.Stop()
			pkg := strings.Repeat("c", 64)
			app.mu.Lock()
			app.delegatedV2Active = true
			app.session = hostclient.SessionResult{ID: "same", TargetID: "kit", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: pkg, Generation: 7}, RuntimeInput: &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("a", 32), PackageID: pkg, Generation: 7}}
			app.mu.Unlock()
			done := make(chan struct{})
			go func() { app.fetchSession(context.Background()); close(done) }()
			<-transport.firstStarted
			app.fetchSession(context.Background())
			app.mu.Lock()
			if app.session.RuntimeInput == nil || app.session.RuntimeInput.RuntimeInstance != strings.Repeat("b", 32) || !app.nativeKeyboardQuarantined {
				app.mu.Unlock()
				t.Fatal("new identity did not retire old authority")
			}
			generation := app.nativeKeyboardGeneration
			app.mu.Unlock()
			close(transport.releaseFirst)
			<-done
			app.mu.Lock()
			defer app.mu.Unlock()
			if app.session.RuntimeInput == nil || app.session.RuntimeInput.RuntimeInstance != strings.Repeat("b", 32) || app.nativeKeyboardGeneration != generation {
				t.Fatal("older observation replaced or requarantined newer session")
			}
		})
	}
}

func TestRuntimeSessionMissingIdentityQuarantinesV2Only(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	defer app.Stop()
	app.mu.Lock()
	app.delegatedV2Active = true
	app.applySessionLocked(hostclient.SessionResult{State: "active"})
	if !app.nativeKeyboardQuarantined {
		app.mu.Unlock()
		t.Fatal("missing identity left v2 ready")
	}
	app.mu.Unlock()
}

func TestRuntimeSessionChangedOrMissingAgentQuarantinesGuestAndShowsStatus(t *testing.T) {
	for _, next := range []string{"", strings.Repeat("f", 32)} {
		a := NewApp(nil, 1280, 720, 20)
		id := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 1}
		a.mu.Lock()
		a.kitHost = true
		a.delegatedV2Active = true
		a.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true, RuntimeInput: id, InputAgentInstance: strings.Repeat("e", 32)}
		result := a.session
		result.InputAgentInstance = next
		a.applySessionLocked(result)
		quarantined, visible := a.nativeKeyboardQuarantined, a.kitLocalKeyboardUnavailableLocked()
		a.mu.Unlock()
		if !quarantined || !visible {
			t.Fatalf("agent %q did not quarantine and show unavailable", next)
		}
		a.Stop()
	}
}

func TestRuntimeSessionKeyboardCapabilityRemovalQuarantinesSource(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	a.nativeKeyboardQuarantined = false
	result := a.session
	result.CoreKeyboardHID = false
	a.applySessionLocked(result)
	quarantined := a.nativeKeyboardQuarantined
	a.mu.Unlock()
	if !quarantined {
		t.Fatal("keyboard capability removal did not quarantine")
	}
}

// runtimeSessionSequence answers each session read with the next body.
type runtimeSessionSequence struct {
	mu     sync.Mutex
	bodies []string
}

func (r *runtimeSessionSequence) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return nil, fmt.Errorf("no session body queued")
	}
	body := r.bodies[0]
	r.bodies = r.bodies[1:]
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// runtimeSessionBody is one active play; an empty instance omits
// runtime_input as the agent does during a coordinator transition.
func runtimeSessionBody(instance string) string {
	pkg := strings.Repeat("c", 64)
	identity := ""
	if instance != "" {
		identity = fmt.Sprintf(`,"runtime_input":{"kind":"described","runtime_instance":"%s","package_id":"%s","generation":7}`, instance, pkg)
	}
	return fmt.Sprintf(`{"id":"same","target_id":"kit","state":"active","input_agent_instance":"%s","core_package":{"package_id":"%s","generation":7}%s}`, strings.Repeat("e", 32), pkg, identity)
}

func TestSamePlayHIDSessionTreatsMissingRuntimeIdentityAsUnobserved(t *testing.T) {
	pkg := strings.Repeat("c", 64)
	identity := func(instance string) *protocol.RuntimeInputIdentity {
		return &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: instance, PackageID: pkg, Generation: 7}
	}
	base := hostclient.SessionResult{ID: "same", TargetID: "kit", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: pkg, Generation: 7}, RuntimeInput: identity(strings.Repeat("a", 32))}
	omitted := base
	omitted.RuntimeInput = nil
	changed := base
	changed.RuntimeInput = identity(strings.Repeat("b", 32))
	invalid := base
	invalid.RuntimeInput = identity(strings.Repeat("0", 32))
	if !samePlayHIDSession(base, omitted) || !samePlayHIDSession(omitted, base) {
		t.Fatal("omitted runtime identity was a new play")
	}
	if samePlayHIDSession(base, changed) || samePlayHIDSession(invalid, omitted) || samePlayHIDSession(omitted, invalid) {
		t.Fatal("changed or invalid runtime identity was the same play")
	}
	kept, retained := retainSessionRuntimeInput(base, omitted)
	if !retained || kept.RuntimeInput == nil || kept.RuntimeInput == base.RuntimeInput || *kept.RuntimeInput != *base.RuntimeInput {
		t.Fatal("same play did not keep a copy of its runtime identity")
	}
	idle := omitted
	idle.State = "idle"
	reloaded := omitted
	reloaded.CorePackage = &hostclient.SessionCorePackage{PackageID: pkg, Generation: 8}
	for _, next := range []hostclient.SessionResult{idle, reloaded, changed} {
		if got, retained := retainSessionRuntimeInput(base, next); retained || got.RuntimeInput != next.RuntimeInput {
			t.Fatalf("runtime identity relabelled a different observation: %+v", next)
		}
	}
}

// The agent omits runtime_input during any coordinator transition (for
// example an owner-host media insert) and after a failed or slow runtime read.
// That gap must not end the play; a different identity after it still must.
func TestRuntimeSessionIdentityGapKeepsPlayUntilIdentityChanges(t *testing.T) {
	first, next := strings.Repeat("a", 32), strings.Repeat("b", 32)
	transport := &runtimeSessionSequence{bodies: []string{runtimeSessionBody(first), runtimeSessionBody(""), runtimeSessionBody(first), runtimeSessionBody(""), runtimeSessionBody(next)}}
	app := NewApp(NewClient("http://unused.invalid", &http.Client{Transport: transport}), 1280, 720, 20)
	defer app.Stop()
	app.mu.Lock()
	app.delegatedV2Active = true
	app.mu.Unlock()
	app.fetchSession(context.Background())
	app.mu.Lock()
	reported := app.sessionRuntimeInputReportedAt
	if app.session.RuntimeInput == nil || app.session.RuntimeInput.RuntimeInstance != first || app.sessionRuntimeInputRetained || reported.IsZero() {
		app.mu.Unlock()
		t.Fatal("reported runtime identity was not applied")
	}
	fence := &localfeed.PadWriteFence{}
	app.nativePadConnectionFence = fence
	app.nativeKeyboardQuarantined = false
	generation, display, hid, title := app.nativeKeyboardGeneration, app.sessionDisplayEpoch, app.playHIDEpoch, app.sessionTitleRequest
	app.mu.Unlock()
	for _, gap := range []bool{true, false, true} {
		app.fetchSession(context.Background())
		app.mu.Lock()
		if app.session.RuntimeInput == nil || app.session.RuntimeInput.RuntimeInstance != first || app.sessionRuntimeInputRetained != gap {
			app.mu.Unlock()
			t.Fatalf("gap=%v: runtime identity not kept", gap)
		}
		if fence.Revoked() || app.nativeKeyboardQuarantined || app.nativeKeyboardGeneration != generation ||
			app.sessionDisplayEpoch != display || app.playHIDEpoch != hid || app.sessionTitleRequest != title {
			app.mu.Unlock()
			t.Fatalf("gap=%v: same play revoked pads, quarantined keyboard, canceled play HID or closed its display", gap)
		}
		if gap && !app.sessionRuntimeInputReportedAt.Equal(reported) || !gap && app.sessionRuntimeInputReportedAt.Before(reported) {
			app.mu.Unlock()
			t.Fatalf("gap=%v: report time %v after %v", gap, app.sessionRuntimeInputReportedAt, reported)
		}
		reported = app.sessionRuntimeInputReportedAt
		if !app.sessionRuntimeInputFreshLocked(time.Now()) {
			app.mu.Unlock()
			t.Fatalf("gap=%v: fresh kept identity refused input", gap)
		}
		app.mu.Unlock()
	}
	app.fetchSession(context.Background())
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.session.RuntimeInput == nil || app.session.RuntimeInput.RuntimeInstance != next || app.sessionRuntimeInputRetained {
		t.Fatal("new runtime identity was not applied")
	}
	if !fence.Revoked() || !app.nativeKeyboardQuarantined || app.nativeKeyboardGeneration == generation ||
		app.sessionDisplayEpoch == display || app.playHIDEpoch == hid || app.sessionTitleRequest == title {
		t.Fatal("changed runtime identity after a gap kept the old play")
	}
}

// An identity is dropped only together with the play it identified.
func TestRuntimeSessionUncarriedIdentityEndsPlay(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	defer app.Stop()
	app.mu.Lock()
	defer app.mu.Unlock()
	app.session = hostclient.SessionResult{ID: "dev", TargetID: "kit", State: "active", Development: true,
		RuntimeInput: &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 1}}
	display := app.sessionDisplayEpoch
	next := app.session
	next.Development = false
	next.RuntimeInput = nil
	app.applySessionLocked(next)
	if app.session.RuntimeInput != nil || app.sessionRuntimeInputRetained || app.sessionDisplayEpoch == display {
		t.Fatal("an identity the observation cannot carry was dropped without ending the play")
	}
}

// A gap longer than the observation window fails input closed, as a failed
// session read does, without ending the play.
func TestRuntimeSessionKeptIdentityExpiresForInputOnly(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	defer app.Stop()
	pkg := strings.Repeat("c", 64)
	app.mu.Lock()
	defer app.mu.Unlock()
	app.delegatedV2Active = true
	app.session = hostclient.SessionResult{ID: "same", TargetID: "kit", State: "active", InputAgentInstance: strings.Repeat("e", 32),
		CorePackage:  &hostclient.SessionCorePackage{PackageID: pkg, Generation: 7},
		RuntimeInput: &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("a", 32), PackageID: pkg, Generation: 7}}
	app.sessionRuntimeInputReportedAt = time.Now().Add(-runtimeInputRetention - time.Second)
	fence := &localfeed.PadWriteFence{}
	app.nativePadConnectionFence = fence
	generation, display, hid, title := app.nativeKeyboardGeneration, app.sessionDisplayEpoch, app.playHIDEpoch, app.sessionTitleRequest
	gap := app.session
	gap.RuntimeInput = nil
	app.applySessionLocked(gap)
	if !fence.Revoked() || !app.nativeKeyboardQuarantined || app.nativeKeyboardGeneration == generation || app.sessionRuntimeInputFreshLocked(time.Now()) {
		t.Fatal("expired kept identity still admitted input")
	}
	if app.session.RuntimeInput == nil || !app.sessionRuntimeInputRetained || app.sessionDisplayEpoch != display || app.playHIDEpoch != hid || app.sessionTitleRequest != title {
		t.Fatal("expired runtime identity ended the play")
	}
}

type runtimeHealthTransport struct {
	calls                      atomic.Int32
	firstStarted, releaseFirst chan struct{}
}

func (r *runtimeHealthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	boot := "boot-new"
	if r.calls.Add(1) == 1 {
		close(r.firstStarted)
		<-r.releaseFirst
		boot = "boot-old"
	}
	body := fmt.Sprintf(`{"ready":true,"target":{"reachable":true,"ready":false,"connection":{"state":"active","target_id":"kit","boot_id":"%s","lease_generation":"lease","lease_owned":true}}}`, boot)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestRuntimeHealthRejectsOutOfOrderBootObservation(t *testing.T) {
	transport := &runtimeHealthTransport{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	app := NewApp(NewClient("http://unused.invalid", &http.Client{Transport: transport}), 1280, 720, 20)
	defer app.Stop()
	app.mu.Lock()
	app.delegatedV2Active = true
	app.healthHave = true
	app.health = hostclient.HealthResult{Ready: true, TargetReachable: true, Connection: hostclient.TargetConnection{State: "active", TargetID: "kit", BootID: "boot-old", LeaseGeneration: "lease", LeaseOwned: true}}
	app.mu.Unlock()
	done := make(chan struct{})
	go func() { app.fetchHealth(context.Background()); close(done) }()
	<-transport.firstStarted
	app.fetchHealth(context.Background())
	app.mu.Lock()
	if app.health.Connection.BootID != "boot-new" || !app.nativeKeyboardQuarantined {
		app.mu.Unlock()
		t.Fatal("new boot did not retire old authority")
	}
	generation := app.nativeKeyboardGeneration
	app.mu.Unlock()
	close(transport.releaseFirst)
	<-done
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.health.Connection.BootID != "boot-new" || app.nativeKeyboardGeneration != generation {
		t.Fatal("older health observation replaced or requarantined newer boot")
	}
}
