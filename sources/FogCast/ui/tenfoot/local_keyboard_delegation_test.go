//go:build linux

package tenfoot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

type boundKeyboardRecorder struct {
	mu       sync.Mutex
	bindings []localfeed.Binding
	events   []remoteinput.Event
	raw      int
	closed   chan localfeed.Binding
}

func (r *boundKeyboardRecorder) Send(e remoteinput.Event, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw++
	return nil
}
func (r *boundKeyboardRecorder) SendBound(_ context.Context, e remoteinput.Event, _ time.Time, b localfeed.Binding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	r.bindings = append(r.bindings, b)
	return nil
}
func (r *boundKeyboardRecorder) Close() {}
func (r *boundKeyboardRecorder) CloseBound(binding localfeed.Binding) {
	if r.closed != nil {
		r.closed <- binding
	}
}

func TestPhysicalKeyboardUsesBoundLocalRouteForForeignSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	feed := &boundKeyboardRecorder{}
	t.Cleanup(app.Stop)
	gen, pkg := strings.Repeat("a", 64), strings.Repeat("b", 64)
	app.mu.Lock()
	app.kitHost = true
	app.localFeed = feed
	app.session.Input = nil
	app.session.CoreKeyboardHID = true
	app.session.CorePackage.PackageID = pkg
	app.session.CorePackage.Generation = 7
	app.healthHave = true
	app.health.Ready, app.health.TargetReady, app.health.TargetReachable = true, true, true
	app.health.Connection.State, app.health.Connection.Owner = "busy", "remote"
	app.health.Connection.TargetID = app.session.TargetID
	app.health.Connection.LeaseGeneration = gen
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "remote", Generation: gen}
	app.mu.Unlock()
	device := &nativeInput{kind: InputKeyboard}
	device.key(app, 36, 1, time.Now())
	device.key(app, 36, 0, time.Now())
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	if feed.raw != 0 || len(feed.events) != 2 || feed.bindings[0].LeaseGeneration != gen || feed.bindings[0].PackageID != pkg || feed.bindings[0].CoreGeneration != 7 || feed.bindings[1] != feed.bindings[0] {
		t.Fatalf("physical events were not bound: raw=%d events=%+v bindings=%+v", feed.raw, feed.events, feed.bindings)
	}
	feed.mu.Unlock()
	if app.SendPlayHID(remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 4}) {
		t.Fatal("generic remote input bypassed the foreign lease fence")
	}
	app.mu.Lock()
	app.kitLease.Generation = strings.Repeat("c", 64)
	app.mu.Unlock()
	device.key(app, 36, 1, time.Now())
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if len(feed.events) != 2 {
		t.Fatalf("mismatched observation delivered: %+v", feed.events)
	}
}

func TestActiveLocalKeyboardBindingRequiresLiveMatchingObservations(t *testing.T) {
	tests := []struct {
		name       string
		invalidate func(*App)
	}{
		{"host not ready", func(a *App) { a.health.Ready = false }},
		{"target unreachable", func(a *App) { a.health.TargetReachable = false }},
		{"host unreachable", func(a *App) { a.hostUnreachable = true }},
		{"uncertain binding", func(a *App) { a.localKeyboardUncertain = true }},
		{"foreign lease changed", func(a *App) { a.kitLease.Generation = strings.Repeat("c", 64) }},
		{"foreign owner changed", func(a *App) { a.kitLease.Owner = "another" }},
		{"target changed", func(a *App) { a.health.Connection.TargetID = "another" }},
		{"session no longer active", func(a *App) { a.session.State = "idle" }},
		{"package unavailable", func(a *App) { a.session.CorePackage = nil }},
		{"connection failed", func(a *App) { a.health.Connection.State = "disconnected" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(http.NotFound))
			defer server.Close()
			app, _ := kitDisplayApp(t, server)
			gen := strings.Repeat("a", 64)
			app.mu.Lock()
			defer app.mu.Unlock()
			app.kitHost, app.localFeed, app.session.Input, app.session.CoreKeyboardHID = true, &boundKeyboardRecorder{}, nil, true
			app.session.CorePackage.PackageID, app.session.CorePackage.Generation = strings.Repeat("b", 64), 7
			app.healthHave = true
			app.health.Ready, app.health.TargetReady, app.health.TargetReachable = true, false, true
			app.health.Connection.State, app.health.Connection.Owner = "busy", "remote"
			app.health.Connection.TargetID, app.health.Connection.LeaseGeneration = app.session.TargetID, gen
			app.kitLeaseHave, app.kitLease = true, KitLeaseStatus{State: "held", Owner: "remote", Generation: gen}
			if _, ok := app.localKeyboardBindingLocked(); !ok {
				t.Fatal("active, non-idle target was rejected")
			}
			tt.invalidate(app)
			if _, ok := app.localKeyboardBindingLocked(); ok {
				t.Fatal("invalid observation admitted a physical binding")
			}
		})
	}
}

func TestRetireForeignKeyboardNeutralizesAndSuppressesHeldKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	feed := &boundKeyboardRecorder{closed: make(chan localfeed.Binding, 1)}
	t.Cleanup(app.Stop)
	gen, pkg := strings.Repeat("a", 64), strings.Repeat("b", 64)
	app.mu.Lock()
	app.kitHost = true
	app.localFeed = feed
	app.session.Input = nil
	app.session.CoreKeyboardHID = true
	app.session.CorePackage.PackageID, app.session.CorePackage.Generation = pkg, 7
	app.healthHave = true
	app.health.Ready, app.health.TargetReady, app.health.TargetReachable = true, true, true
	app.health.Connection.State, app.health.Connection.Owner = "busy", "remote"
	app.health.Connection.TargetID, app.health.Connection.LeaseGeneration = app.session.TargetID, gen
	app.kitLeaseHave, app.kitLease = true, KitLeaseStatus{State: "held", Owner: "remote", Generation: gen}
	app.mu.Unlock()
	device := &nativeInput{kind: InputKeyboard, fd: 17}
	device.key(app, 36, 1, time.Now())
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.retireLocalKeyboardLocked()
	app.mu.Unlock()
	drainLocalKeyQueue(t, app)
	select {
	case closed := <-feed.closed:
		if closed.LeaseGeneration != gen || closed.CoreGeneration != 7 {
			t.Fatalf("closed wrong lifetime: %+v", closed)
		}
	case <-time.After(time.Second):
		t.Fatal("old bound connection was not retired")
	}
	feed.mu.Lock()
	if len(feed.events) != 2 || feed.events[0].Action != remoteinput.ActionPress || feed.events[1].Action != remoteinput.ActionRelease || feed.bindings[0] != feed.bindings[1] {
		t.Fatalf("hold not neutralized on its original binding: %+v %+v", feed.events, feed.bindings)
	}
	feed.mu.Unlock()
	app.mu.Lock()
	app.localKeyboardUncertain = false // fresh health on the same execution
	app.mu.Unlock()
	device.key(app, 36, 1, time.Now()) // a repeat is not a fresh physical press
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	if len(feed.events) != 2 {
		t.Fatalf("held key replayed after re-observation: %+v", feed.events)
	}
	feed.mu.Unlock()
}

type blockingKeyboardRecorder struct {
	started chan struct{}
	release chan struct{}
	second  chan struct{}
	mu      sync.Mutex
	calls   int
}

func (r *blockingKeyboardRecorder) Send(remoteinput.Event, time.Time) error { return nil }
func (r *blockingKeyboardRecorder) Close()                                  {}
func (r *blockingKeyboardRecorder) CloseBound(localfeed.Binding)            {}
func (r *blockingKeyboardRecorder) SendBound(_ context.Context, _ remoteinput.Event, _ time.Time, _ localfeed.Binding) error {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	if call == 1 {
		close(r.started)
		<-r.release
	} else {
		close(r.second)
	}
	return nil
}

func TestCancelledForeignKeyboardQueueDrainsBeforeSuccessor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	feed := &blockingKeyboardRecorder{started: make(chan struct{}), release: make(chan struct{}), second: make(chan struct{})}
	t.Cleanup(app.Stop)
	gen, pkg := strings.Repeat("a", 64), strings.Repeat("b", 64)
	app.mu.Lock()
	app.kitHost, app.localFeed, app.session.Input, app.session.CoreKeyboardHID = true, feed, nil, true
	app.session.CorePackage.PackageID, app.session.CorePackage.Generation = pkg, 7
	app.healthHave = true
	app.health.Ready, app.health.TargetReady, app.health.TargetReachable = true, true, true
	app.health.Connection.State, app.health.Connection.Owner = "busy", "remote"
	app.health.Connection.TargetID, app.health.Connection.LeaseGeneration = app.session.TargetID, gen
	app.kitLeaseHave, app.kitLease = true, KitLeaseStatus{State: "held", Owner: "remote", Generation: gen}
	app.mu.Unlock()
	device := &nativeInput{kind: InputKeyboard, fd: 17}
	device.key(app, 36, 1, time.Now())
	select {
	case <-feed.started:
	case <-time.After(time.Second):
		t.Fatal("first write did not start")
	}
	app.mu.Lock()
	app.retireLocalKeyboardLocked()
	app.cancelPlayHIDLocked()
	app.localKeyboardUncertain = false // fresh observation for the next session
	app.mu.Unlock()
	device.key(app, 37, 1, time.Now())
	select {
	case <-feed.second:
		t.Fatal("new session overtook old in-flight input")
	case <-time.After(50 * time.Millisecond):
	}
	close(feed.release)
	select {
	case <-feed.second:
	case <-time.After(time.Second):
		t.Fatal("successor did not run after drain")
	}
	drainLocalKeyQueue(t, app)
}

func TestForeignKeyboardCannotFallBackToRawOnStaleActiveSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	feed := &boundKeyboardRecorder{}
	t.Cleanup(app.Stop)
	gen, pkg := strings.Repeat("a", 64), strings.Repeat("b", 64)
	app.mu.Lock()
	app.kitHost, app.localFeed, app.session.Input, app.session.CoreKeyboardHID = true, feed, nil, true
	app.session.CorePackage.PackageID, app.session.CorePackage.Generation = pkg, 7
	app.healthHave = true
	app.health.Ready, app.health.TargetReady, app.health.TargetReachable = true, true, true
	app.health.Connection.State, app.health.Connection.Owner = "busy", "remote"
	app.health.Connection.TargetID, app.health.Connection.LeaseGeneration = app.session.TargetID, gen
	app.kitLeaseHave, app.kitLease = true, KitLeaseStatus{State: "held", Owner: "remote", Generation: gen}
	app.mu.Unlock()
	device := &nativeInput{kind: InputKeyboard, fd: 17}
	device.key(app, 36, 1, time.Now())
	device.key(app, 36, 0, time.Now())
	drainLocalKeyQueue(t, app)
	app.mu.Lock()
	app.retireLocalKeyboardLocked()
	app.health.Connection.State, app.health.Connection.Owner = "ready", ""
	app.kitLease = KitLeaseStatus{State: "free"}
	app.localKeyboardUncertain = false
	app.mu.Unlock()
	device.key(app, 37, 1, time.Now())
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	raw := feed.raw
	feed.mu.Unlock()
	if raw != 0 {
		t.Fatal("stale active session fell back to raw feed")
	}
	// A directly replaced locally owned session does not need an observed
	// idle boundary. Its fresh keyboard frame still carries the new binding.
	newGen := strings.Repeat("c", 64)
	app.mu.Lock()
	app.health.Connection.State, app.health.Connection.LeaseOwned = "active", true
	app.health.Connection.LeaseGeneration = newGen
	app.kitLease = KitLeaseStatus{State: "active", Generation: newGen}
	app.session.CorePackage.Generation = 8
	app.mu.Unlock()
	device.key(app, 37, 0, time.Now())
	device.key(app, 37, 1, time.Now())
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if feed.raw != 0 || len(feed.bindings) < 3 || feed.bindings[len(feed.bindings)-1].LeaseGeneration != newGen || feed.bindings[len(feed.bindings)-1].CoreGeneration != 8 {
		t.Fatalf("replacement did not use its new bound route: raw=%d bindings=%+v", feed.raw, feed.bindings)
	}
}
