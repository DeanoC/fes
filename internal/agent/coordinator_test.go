package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/clawzai2-tech/mister-remote/internal/agent"
	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type fakeRuntime struct {
	mu             sync.Mutex
	health         protocol.Health
	reconciled     protocol.Status
	prepared       mister.PreparedLaunch
	prepareErr     *protocol.APIError
	launchObserved string
	launchErr      *protocol.APIError
	stopObserved   string
	stopErr        *protocol.APIError
	launchGate     chan struct{}
	prepareCalls   int
	launchCalls    int
	stopCalls      int
}

func (f *fakeRuntime) Health(string) protocol.Health {
	return f.health
}

func (f *fakeRuntime) Reconcile(context.Context) protocol.Status {
	return f.reconciled
}

func (f *fakeRuntime) Prepare(spec core.Spec, _ string) (mister.PreparedLaunch, *protocol.APIError) {
	f.mu.Lock()
	f.prepareCalls++
	f.mu.Unlock()
	if f.prepareErr != nil {
		return mister.PreparedLaunch{}, f.prepareErr
	}
	result := f.prepared
	result.Spec = spec
	return result, nil
}

func (f *fakeRuntime) Launch(context.Context, mister.PreparedLaunch) (string, *protocol.APIError) {
	f.mu.Lock()
	f.launchCalls++
	f.mu.Unlock()
	if f.launchGate != nil {
		<-f.launchGate
	}
	return f.launchObserved, f.launchErr
}

func (f *fakeRuntime) Stop(context.Context) (string, *protocol.APIError) {
	f.mu.Lock()
	f.stopCalls++
	f.mu.Unlock()
	return f.stopObserved, f.stopErr
}

func (f *fakeRuntime) counts() (prepare, launch, stop int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCalls, f.launchCalls, f.stopCalls
}

func TestLaunchTransitionsToActive(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != "megadrive-test" || status.ObservedCore == nil || *status.ObservedCore != "MegaDrive" {
		t.Fatalf("status = %#v", status)
	}
	prepareCalls, launchCalls, _ := runtime.counts()
	if prepareCalls != 1 || launchCalls != 1 {
		t.Fatalf("prepare calls = %d, launch calls = %d", prepareCalls, launchCalls)
	}
}

func TestConcurrentTransitionReturnsBusy(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive", launchGate: gate}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
	}()
	deadline := time.After(time.Second)
	for coordinator.Status().State != protocol.StateLaunching {
		select {
		case <-deadline:
			t.Fatal("launch never entered launching state")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	_, apiErr := coordinator.Stop(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeBusy {
		t.Fatalf("stop error = %#v", apiErr)
	}
	close(gate)
	<-done
}

func TestInitializeUsesReconciledStatus(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: systemPtr(protocol.SystemSNES), ObservedCore: stringPtr("SNES")}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateActive || status.GameID != nil || status.System == nil || *status.System != protocol.SystemSNES {
		t.Fatalf("status = %#v", status)
	}
}

func TestHealthRemainsNotReadyAfterUnavailableReconciliation(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{
		health:     protocol.Health{Ready: true, MiSTerProcess: true, CommandPipe: true},
		reconciled: protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "not ready"}},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	if health := coordinator.Health("0.1.0"); health.Ready {
		t.Fatalf("health = %#v", health)
	}
}

func TestInvalidLaunchPreservesPreviousState(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, prepareErr: &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "invalid"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	before := coordinator.Status()
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "relative.sfc"})
	if apiErr == nil || apiErr.Code != protocol.CodeInvalidROMPath {
		t.Fatalf("error = %#v", apiErr)
	}
	if after := coordinator.Status(); after.State != before.State || after.LastError != before.LastError {
		t.Fatalf("state changed: before=%#v after=%#v", before, after)
	}
	_, launchCalls, _ := runtime.counts()
	if launchCalls != 0 {
		t.Fatalf("runtime launch calls = %d", launchCalls)
	}
}

func TestUnsupportedSystemPreservesPreviousState(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	before := coordinator.Status()
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "nes-test", System: "nes", ROMPath: "/media/fat/games/NES/test.nes"})
	if apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
		t.Fatalf("error = %#v", apiErr)
	}
	if after := coordinator.Status(); after.State != before.State {
		t.Fatalf("state changed: before=%#v after=%#v", before, after)
	}
	prepareCalls, launchCalls, _ := runtime.counts()
	if prepareCalls != 0 || launchCalls != 0 {
		t.Fatalf("prepare calls = %d, launch calls = %d", prepareCalls, launchCalls)
	}
}

func TestLaunchTimeoutTransitionsToFailed(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MENU", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "timeout"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
	if apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout {
		t.Fatalf("error = %#v", apiErr)
	}
	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.ObservedCore == nil || *status.ObservedCore != "MENU" || status.LastError == nil || status.LastError.Code != protocol.CodeCoreTimeout {
		t.Fatalf("status = %#v", status)
	}
}

func TestReturnedErrorCannotMutateFailedStatus(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MENU", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "timeout"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
	if apiErr == nil {
		t.Fatal("launch returned no error")
	}
	apiErr.Code = protocol.CodeInternal
	apiErr.Message = "changed"
	status := coordinator.Status()
	if status.LastError == nil || status.LastError.Code != protocol.CodeCoreTimeout || status.LastError.Message != "timeout" {
		t.Fatalf("caller mutation changed failed status: %#v", status)
	}
}

func TestStopTransitionsActiveToIdle(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{
		health:       protocol.Health{Ready: true},
		reconciled:   protocol.Status{State: protocol.StateActive, System: systemPtr(protocol.SystemSNES), ObservedCore: stringPtr("SNES")},
		stopObserved: "MENU",
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateIdle || status.GameID != nil || status.ObservedCore != nil {
		t.Fatalf("stop = %#v, %#v", status, apiErr)
	}
	_, _, stopCalls := runtime.counts()
	if stopCalls != 1 {
		t.Fatalf("runtime stop calls = %d", stopCalls)
	}
}

func TestStopIsIdempotentWhenIdle(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateIdle {
		t.Fatalf("stop = %#v, %#v", status, apiErr)
	}
	_, _, stopCalls := runtime.counts()
	if stopCalls != 0 {
		t.Fatalf("runtime stop calls = %d", stopCalls)
	}
}

func TestStatusReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: systemPtr(protocol.SystemSNES), ObservedCore: stringPtr("SNES")}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	first := coordinator.Status()
	*first.System = protocol.SystemMegaDrive
	*first.ObservedCore = "changed"
	second := coordinator.Status()
	if second.System == nil || *second.System != protocol.SystemSNES || second.ObservedCore == nil || *second.ObservedCore != "SNES" {
		t.Fatalf("caller mutation changed coordinator status: %#v", second)
	}
}

func stringPtr(value string) *string {
	return &value
}

func systemPtr(value protocol.System) *protocol.System {
	return &value
}
