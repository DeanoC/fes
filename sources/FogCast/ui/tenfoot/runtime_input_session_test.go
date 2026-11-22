package tenfoot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
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
