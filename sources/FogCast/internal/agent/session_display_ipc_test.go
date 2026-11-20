//go:build linux

package agent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
	"github.com/DeanoC/FogCast/protocol"
)

func displayIPCTestCoordinator(t *testing.T, launcherPath string) (*agent.Coordinator, *displayRuntime, protocol.DevelopmentMediaBinding) {
	t.Helper()
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, TargetID: "target-a"}
	contract := protocol.C64DiskInterface()
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: contract.ID, Major: contract.Major, Minor: contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: contract, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: "empty"}}}}
	runtime := &displayRuntime{fakeRuntime: fakeRuntime{reconciled: status}}
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithSessionDisplayIPC(launcherPath, b.TargetID))
	coordinator.Initialize(context.Background())
	return coordinator, runtime, b
}
func startDisplayIPCListener(t *testing.T, path string, callback func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error)) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.Cleanup(func() { cancel(); <-done })
	ready := make(chan error, 1)
	go func() {
		defer close(done)
		_ = sessiondisplayipc.ListenLauncher(ctx, path, uint32(os.Geteuid()), callback, ready)
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
}

func TestDisplayHeadlessAndExternalPrepare(t *testing.T) {
	t.Run("headless retains direct operation", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		launcher := filepath.Join(dir, "absent.sock")
		c, runtime, b := displayIPCTestCoordinator(t, launcher)
		if _, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err != nil || runtime.calls != 1 {
			t.Fatalf("headless call: %v", err)
		}
	})
	t.Run("live launcher prepares before mutation and acknowledges after", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		launcher := filepath.Join(dir, "launcher.sock")
		c, runtime, b := displayIPCTestCoordinator(t, launcher)
		phases := make(chan string, 2)
		startDisplayIPCListener(t, launcher, func(_ context.Context, _ sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
			if runtime.calls != 0 {
				return nil, errors.New("runtime mutated before prepare")
			}
			phases <- "prepared"
			return func(_ context.Context, o sessiondisplayipc.Outcome) error {
				if o != sessiondisplayipc.Applied || runtime.calls != 1 {
					return errors.New("wrong outcome")
				}
				phases <- "finished"
				return nil
			}, nil
		})
		if _, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err != nil {
			t.Fatal(err)
		}
		if <-phases != "prepared" || <-phases != "finished" {
			t.Fatal("out of order")
		}
	})
}

type blockedDisplayRuntime struct {
	displayRuntime
	entered chan struct{}
	release chan struct{}
}

func (r *blockedDisplayRuntime) SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.calls++
	close(r.entered)
	<-r.release
	return nil
}

func TestCompetingDisplayRequestsCannotMutateBeforeAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher.sock")
	b := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, TargetID: "target-a"}
	contract := protocol.C64DiskInterface()
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: contract.ID, Major: contract.Major, Minor: contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: contract, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: "empty"}}}}
	runtime := &blockedDisplayRuntime{displayRuntime: displayRuntime{fakeRuntime: fakeRuntime{reconciled: status}}, entered: make(chan struct{}), release: make(chan struct{})}
	c := agent.New(runtime, time.Second, time.Second, agent.WithSessionDisplayIPC(path, b.TargetID))
	c.Initialize(context.Background())
	startDisplayIPCListener(t, path, func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
		return func(context.Context, sessiondisplayipc.Outcome) error { return nil }, nil
	})
	result := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b)
		result <- err
	}()
	select {
	case <-runtime.entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not dispatch")
	}
	for _, target := range []protocol.SessionDisplay{protocol.SessionDisplayFocused, protocol.SessionDisplayClosed} {
		if _, err := c.SetSessionDisplay(context.Background(), target, b); err == nil || err.Code != protocol.CodeBusy {
			t.Fatalf("competing request admitted: %v", err)
		}
	}
	close(runtime.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if runtime.calls != 1 {
		t.Fatalf("physical dispatches=%d", runtime.calls)
	}
}

func TestLiveLauncherRequiresAuthoritativeTargetIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher.sock")
	c, runtime, b := displayIPCTestCoordinator(t, path)
	prepared := make(chan struct{}, 2)
	startDisplayIPCListener(t, path, func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
		prepared <- struct{}{}
		return func(_ context.Context, _ sessiondisplayipc.Outcome) error { return nil }, nil
	})
	for _, id := range []string{"", "another-kit"} {
		foreign := b
		foreign.TargetID = id
		if _, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, foreign); err == nil {
			t.Fatalf("target %q admitted", id)
		}
		select {
		case <-prepared:
			t.Fatal("foreign kit reached launcher prepare")
		default:
		}
	}
	if runtime.calls != 0 {
		t.Fatal("ambiguous or foreign kit reached runtime")
	}
}

func TestReplacementWaitsForPreparedDisplayMutation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cancelRequest bool
	}{{"launcher shutdown", false}, {"caller cancellation", true}} {
		t.Run(tc.name, func(t *testing.T) { testReplacementWaitsForDisplayMutation(t, tc.cancelRequest) })
	}
}

func testReplacementWaitsForDisplayMutation(t *testing.T, cancelRequest bool) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher.sock")
	binding := protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, TargetID: "target-a"}
	contract := protocol.C64DiskInterface()
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: binding.PackageID, Generation: binding.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: contract.ID, Major: contract.Major, Minor: contract.Minor}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: contract, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: "empty"}}}}
	runtime := &blockedDisplayRuntime{displayRuntime: displayRuntime{fakeRuntime: fakeRuntime{reconciled: status}}, entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(runtime.release) }) })
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithSessionDisplayIPC(path, binding.TargetID))
	coordinator.Initialize(context.Background())
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ready1 := make(chan error, 1)
	done1 := make(chan error, 1)
	outcomes := make(chan sessiondisplayipc.Outcome, 1)
	handler := func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
		return func(_ context.Context, outcome sessiondisplayipc.Outcome) error { outcomes <- outcome; return nil }, nil
	}
	go func() { done1 <- sessiondisplayipc.ListenLauncher(ctx1, path, uint32(os.Geteuid()), handler, ready1) }()
	if err := <-ready1; err != nil {
		t.Fatal(err)
	}
	result := make(chan *protocol.APIError, 1)
	requestCtx, cancelRequestCtx := context.WithCancel(context.Background())
	defer cancelRequestCtx()
	go func() {
		_, err := coordinator.SetSessionDisplay(requestCtx, protocol.SessionDisplayFocused, binding)
		result <- err
	}()
	select {
	case <-runtime.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("physical transition did not start")
	}
	if cancelRequest {
		cancelRequestCtx()
	} else {
		cancel1()
	}
	select {
	case outcome := <-outcomes:
		if outcome != sessiondisplayipc.Unknown {
			t.Fatalf("outcome=%v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("launcher did not observe cancellation")
	}
	cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ready2 := make(chan error, 1)
	done2 := make(chan error, 1)
	go func() { done2 <- sessiondisplayipc.ListenLauncher(ctx2, path, uint32(os.Geteuid()), handler, ready2) }()
	select {
	case err := <-ready2:
		if err != nil {
			t.Fatalf("replacement could not start: %v", err)
		}
		select {
		case <-result:
			t.Fatal("mutation unexpectedly finished")
		default:
		}
		t.Fatal("replacement launcher published while old physical mutation was still in flight")
	case <-time.After(250 * time.Millisecond):
	}
	release.Do(func() { close(runtime.release) })
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("disconnected launcher was reported confirmed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("mutation did not finish")
	}
	select {
	case <-done1:
	case <-time.After(3 * time.Second):
		t.Fatal("old listener did not stop")
	}
	select {
	case err := <-ready2:
		if err != nil {
			t.Fatalf("replacement failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement stayed blocked")
	}
	cancel2()
	select {
	case <-done2:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement did not stop")
	}
	if runtime.calls != 1 {
		t.Fatalf("physical dispatches=%d", runtime.calls)
	}
}

func TestDisplayPublicationGuardHeldThroughFinish(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher.sock")
	c, runtime, binding := displayIPCTestCoordinator(t, path)
	finishStarted, finishRelease := make(chan struct{}), make(chan struct{})
	var release sync.Once
	startDisplayIPCListener(t, path, func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
		return func(_ context.Context, outcome sessiondisplayipc.Outcome) error {
			if outcome != sessiondisplayipc.Applied {
				return errors.New("unexpected finish outcome")
			}
			close(finishStarted)
			<-finishRelease
			return nil
		}, nil
	})
	t.Cleanup(func() { release.Do(func() { close(finishRelease) }) })
	result := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, binding)
		result <- err
	}()
	select {
	case <-finishStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("launcher finish did not start")
	}
	guardResult := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		g, err := sessiondisplayipc.AcquireDisplayGuard(ctx, path)
		if err == nil {
			err = g.Release()
		}
		guardResult <- err
	}()
	select {
	case err := <-guardResult:
		t.Fatalf("publication guard released before launcher finished: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
	release.Do(func() { close(finishRelease) })
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("display transition did not finish")
	}
	select {
	case err := <-guardResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publication guard did not release")
	}
	if runtime.calls != 1 {
		t.Fatalf("physical dispatches=%d", runtime.calls)
	}
}

type failingDisplayRuntime struct{ displayRuntime }

func (r *failingDisplayRuntime) SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) *protocol.APIError {
	r.calls++
	return &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "runtime failed"}
}

func TestRuntimeDisplayFailureReleasesPublicationGuard(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher.sock")
	_, base, b := displayIPCTestCoordinator(t, path)
	runtime := &failingDisplayRuntime{displayRuntime: *base}
	c := agent.New(runtime, time.Second, time.Second, agent.WithSessionDisplayIPC(path, b.TargetID))
	c.Initialize(context.Background())
	outcomes := make(chan sessiondisplayipc.Outcome, 1)
	startDisplayIPCListener(t, path, func(context.Context, sessiondisplayipc.Transition) (func(context.Context, sessiondisplayipc.Outcome) error, error) {
		return func(_ context.Context, outcome sessiondisplayipc.Outcome) error { outcomes <- outcome; return nil }, nil
	})
	if _, err := c.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, b); err == nil || err.Code != protocol.CodeTransferFailed {
		t.Fatalf("runtime failure not returned: %v", err)
	}
	select {
	case outcome := <-outcomes:
		if outcome != sessiondisplayipc.Unknown {
			t.Fatalf("outcome=%v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("launcher did not receive unknown outcome")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	guard, err := sessiondisplayipc.AcquireDisplayGuard(ctx, path)
	if err != nil {
		t.Fatalf("guard remained held after failure: %v", err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
	if runtime.calls != 1 {
		t.Fatalf("physical dispatches=%d", runtime.calls)
	}
}
