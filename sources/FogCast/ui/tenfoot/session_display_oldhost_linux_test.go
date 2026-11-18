//go:build linux

package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type oldHostDisplayRuntime struct {
	status protocol.Status
	calls  atomic.Int32
}

func (r *oldHostDisplayRuntime) Health(string) protocol.Health             { return protocol.Health{} }
func (r *oldHostDisplayRuntime) Reconcile(context.Context) protocol.Status { return r.status }
func (*oldHostDisplayRuntime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, nil
}
func (*oldHostDisplayRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", nil
}
func (*oldHostDisplayRuntime) Stop(context.Context) (string, *protocol.APIError) { return "", nil }
func (r *oldHostDisplayRuntime) SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.calls.Add(1)
	return nil
}

// An unchanged host forwards only the pre-existing identity and display body.
// The target still invokes the originating launcher's normal prepare callback.
func TestOldHostLocalDisplayUsesTargetCallback(t *testing.T) {
	prior := kitDisplaySession()
	runtime := &oldHostDisplayRuntime{status: protocol.Status{State: protocol.StateActive, Development: true, CorePackage: observedKitPackage(prior)}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "launcher.sock")
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithSessionDisplayIPC(socket, prior.TargetID))
	coordinator.Initialize(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		binding, ok := protocol.DevelopmentMediaHeaders(r.Header)
		if !ok {
			http.Error(w, "bad binding", 400)
			return
		}
		var req protocol.SessionDisplayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		display, ok := req.Display()
		if !ok {
			http.Error(w, "bad display", 400)
			return
		}
		if _, apiErr := coordinator.SetSessionDisplay(r.Context(), display, binding); apiErr != nil {
			http.Error(w, apiErr.Error(), 409)
			return
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	presenter := &countingSessionMenu{}
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	display.SetChangeDriven(true)
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan error, 1)
	go func() {
		defer close(done)
		_ = sessiondisplayipc.ListenLauncher(ctx, socket, uint32(os.Geteuid()), app.prepareExternalSessionDisplay, ready)
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-done })
	presenter.set(menudisplay.Status{Available: true, Session: true, PackageID: prior.CorePackage.PackageID, CoreGeneration: prior.CorePackage.Generation, Generation: 4})
	app.mu.Lock()
	app.displayIPC = true
	app.beginSessionDisplayLocked(protocol.SessionDisplayFocused, false)
	app.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for {
		snap := app.Snapshot()
		if snap.displayFrameRequest != 0 {
			display.ForceNextFrame()
			display.Present()
			app.submittedDisplayFrame(snap.displayFrameRequest, display.QueuedSeq())
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("local request did not reach launcher callback")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		app.mu.Lock()
		pending, focused := app.displayOutbound, app.sessionDisplay == protocol.SessionDisplayFocused
		app.mu.Unlock()
		if !pending {
			if !focused || runtime.calls.Load() != 1 {
				t.Fatalf("result focused=%v calls=%d", focused, runtime.calls.Load())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old-host request did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	// Avoid an exit return after the listener is shut down by test cleanup.
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayClosed
	app.mu.Unlock()
}

func TestOlderAgentSuccessWithoutCallbackIsUncertain(t *testing.T) {
	prior := kitDisplaySession()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeKitDisplaySession(w, prior) }))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.mu.Lock()
	app.displayIPC = true
	app.beginSessionDisplayLocked(protocol.SessionDisplayFocused, false)
	done := app.displayOutboundWait
	app.mu.Unlock()
	if done == nil {
		t.Fatal("request not started")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("request did not complete")
	}
	app.mu.Lock()
	uncertain := app.sessionDisplayUncertain
	app.sessionDisplayUncertain = false
	app.mu.Unlock()
	if !uncertain {
		t.Fatal("old agent success without callback was accepted")
	}
}

func TestFramebufferStopsInputOnLauncherListenerFailure(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	failure := make(chan error, 1)
	failure <- errors.New("listener exited")
	app.mu.Lock()
	app.displayIPCFailure = failure
	app.mu.Unlock()
	polled := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := framebufferLoop(ctx, Options{}, app, nil, func(*App, time.Time) (bool, error) { polled = true; return false, nil }, "test")
	if err == nil || polled {
		t.Fatalf("listener failure not fenced: err=%v polled=%v", err, polled)
	}
}
