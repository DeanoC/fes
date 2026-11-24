//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// projectionService uses the actual host session coordinator but supplies
// already-observed target state. The target lease manager is real; the bound
// feed records launcher output in process and does not emulate a device.
type projectionService struct {
	hostapi.Service
	manager     *kitlease.Manager
	pkg, target string
}

func (s *projectionService) KitHostMode() bool                   { return true }
func (s *projectionService) ForegroundSessionTargetName() string { return fogcast.KitHostTargetName }
func (s *projectionService) SessionTargetName() string           { return fogcast.KitHostTargetName }
func (s *projectionService) TargetIDForName(string) string       { return s.target }
func (s *projectionService) TargetConnectionForName(string) fogcast.TargetConnection {
	return s.TargetConnection()
}
func (s *projectionService) TargetConnection() fogcast.TargetConnection {
	st := s.manager.Status()
	return fogcast.TargetConnection{State: "busy", Owner: st.Owner, LeaseGeneration: st.Generation, TargetID: s.target}
}
func (s *projectionService) KitLocalForeignLease(context.Context, string) (bool, error) {
	st := s.manager.Status()
	return st.State == "held" && st.Owner == "external", nil
}
func (s *projectionService) Health(context.Context) (protocol.Health, error) {
	// Runtime health reports launch readiness only when the target is idle.
	return protocol.Health{Ready: false, TargetID: s.target, APIVersion: "v1"}, nil
}
func (s *projectionService) Status(context.Context) (protocol.Status, error) {
	core := "fes.c64"
	return protocol.Status{State: protocol.StateActive, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
		PackageID: s.pkg, Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1, Minor: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.video.session-display", Major: 1}},
	}}, nil
}

// The mock target grant admits only the exact current manager lease and HID
// binding. It models the granted target boundary without creating a device.
type grantedProjectionFeed struct {
	boundKeyboardRecorder
	manager *kitlease.Manager
	pkg     string
}

func (f *grantedProjectionFeed) SendBound(ctx context.Context, event remoteinput.Event, now time.Time, b localfeed.Binding) error {
	_, done, err := f.manager.BeginLocalInput(b.LeaseGeneration)
	if err != nil {
		return err
	}
	defer done()
	if b.PackageID != f.pkg || b.CoreGeneration != 7 {
		return errors.New("target grant rejected binding")
	}
	return f.boundKeyboardRecorder.SendBound(ctx, event, now, b)
}

type rejectedLocalInput struct{ attempts, detaches int }

func (i *rejectedLocalInput) Attach(context.Context, string) error {
	i.attempts++
	return &protocol.APIError{Code: protocol.CodeKitLeaseBusy}
}
func (i *rejectedLocalInput) Detach(context.Context, string) error {
	i.detaches++
	return errors.New("unexpected detach")
}
func (*rejectedLocalInput) Status() host.RemoteInputStatus {
	return host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true, SessionID: "stale-local"}
}

func TestKitHostForeignOwnerSessionProjectsToBoundKeyboard(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "external", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	service := &projectionService{manager: manager, pkg: strings.Repeat("b", 64), target: "kit-target"}
	input := &rejectedLocalInput{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithRemoteInputFactory(func(string) host.RemoteInputController { return input }))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	feed := &grantedProjectionFeed{manager: manager, pkg: service.pkg}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.kitHost = true
	app.kitID = service.target
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.mu.Unlock()
	app.fetchHealth(ctx)
	app.fetchSession(ctx)
	app.mu.Lock()
	session := app.session
	health := app.health
	app.mu.Unlock()
	if !health.Ready || !health.TargetReachable || health.TargetReady {
		t.Fatalf("expected reachable host and active, non-idle target: %+v", health)
	}
	if session.State != "active" || session.CorePackage == nil || session.CorePackage.PackageID != service.pkg || !session.CoreKeyboardHID {
		t.Fatalf("active session not projected: %+v", session)
	}
	if input.attempts != 0 || input.detaches != 0 {
		t.Fatalf("foreign session mutated input: attach=%d detach=%d", input.attempts, input.detaches)
	}
	if session.Input == nil || session.Input.Ready || session.Input.State != "detached" || session.Input.SessionID != "" {
		t.Fatalf("stale local bridge projected as ready: %+v", session.Input)
	}
	app.HandlePlayHIDScancode("8", 0x25, true, time.Now())
	drainLocalKeyQueue(t, app)
	// A normal health refresh during an active key must not retire the
	// binding merely because the target cannot admit another launch.
	app.fetchHealth(ctx)
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	heldEvents := append([]remoteinput.Event(nil), feed.events...)
	feed.mu.Unlock()
	if len(heldEvents) != 1 || heldEvents[0].Action != remoteinput.ActionPress {
		t.Fatalf("health refresh retired the held key: %+v", heldEvents)
	}
	app.HandlePlayHIDScancode("8", 0x25, false, time.Now())
	drainLocalKeyQueue(t, app)
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if len(feed.events) != 2 || feed.events[1].Action != remoteinput.ActionRelease || len(feed.bindings) != 2 || feed.bindings[0].LeaseGeneration != grant.Status.Generation || feed.bindings[0].PackageID != service.pkg || feed.bindings[0].CoreGeneration != 7 || feed.bindings[0] != feed.bindings[1] {
		t.Fatalf("physical keyboard binding/events = %+v %+v", feed.bindings, feed.events)
	}
}
