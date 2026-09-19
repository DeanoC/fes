package agent_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

type fakeRuntime struct {
	mu                   sync.Mutex
	health               protocol.Health
	reconciled           protocol.Status
	prepared             mister.PreparedLaunch
	prepareErr           *protocol.APIError
	launchObserved       string
	launchErr            *protocol.APIError
	stopObserved         string
	stopErr              *protocol.APIError
	developmentObserved  string
	developmentErr       *protocol.APIError
	developmentBody      []byte
	developmentSize      int64
	developmentCalls     int
	launchGate           chan struct{}
	prepareCalls         int
	launchCalls          int
	stopCalls            int
	developmentStopCalls int
	prepareSpec          core.Spec
	preparePath          string
	launched             mister.PreparedLaunch
}

type ownedContextRuntime struct {
	fakeRuntime
	started      chan struct{}
	operationErr chan error
}

type ownedStopContextRuntime struct {
	fakeRuntime
	legacyStarted chan struct{}
	ownedStarted  chan struct{}
	release       chan struct{}
	operationErr  chan error
}

type callerBoundStopRuntime struct {
	fakeRuntime
	contextErr chan error
}

type ownedDevelopmentContextRuntime struct {
	fakeRuntime
	started               chan struct{}
	release               chan struct{}
	contextValues         chan [3]string
	contextDeadlines      chan [3]bool
	observationRemaining  chan time.Duration
	operationErr          chan error
	ownedDevelopmentCalls int
	ownedStopCalls        int
	dispatchCalls         int
}

func (r *ownedDevelopmentContextRuntime) LoadDevelopmentRBFOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	r.mu.Lock()
	r.ownedDevelopmentCalls++
	r.mu.Unlock()
	if admission.Err() != nil {
		return "", false, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
	body, err := io.ReadAll(content)
	if err != nil {
		return "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "test reader failed"}
	}
	r.mu.Lock()
	r.developmentSize = size
	r.developmentBody = append([]byte(nil), body...)
	r.dispatchCalls++
	r.mu.Unlock()
	if r.contextValues != nil {
		r.contextValues <- [3]string{
			contextString(admission, "admission"),
			contextString(observation, "process"),
			contextString(operationOwner, "process"),
		}
	}
	if r.contextDeadlines != nil {
		_, admissionDeadline := admission.Deadline()
		_, observationDeadline := observation.Deadline()
		_, ownerDeadline := operationOwner.Deadline()
		r.contextDeadlines <- [3]bool{admissionDeadline, observationDeadline, ownerDeadline}
	}
	if r.observationRemaining != nil {
		deadline, ok := observation.Deadline()
		if !ok {
			r.observationRemaining <- -1
		} else {
			r.observationRemaining <- time.Until(deadline)
		}
	}
	if r.started != nil {
		close(r.started)
	}
	if r.release != nil {
		select {
		case <-r.release:
		case <-operationOwner.Done():
			if r.operationErr != nil {
				r.operationErr <- operationOwner.Err()
			}
			return "", true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
		}
	}
	return r.developmentObserved, true, r.developmentErr
}

func (r *ownedDevelopmentContextRuntime) StopOwned(context.Context, context.Context) (string, *protocol.APIError) {
	r.mu.Lock()
	r.ownedStopCalls++
	r.stopCalls++
	r.mu.Unlock()
	return r.stopObserved, r.stopErr
}

func (*ownedDevelopmentContextRuntime) StopReady() bool { return true }

func contextString(ctx context.Context, key string) string {
	value, _ := ctx.Value(key).(string)
	return value
}

func (r *ownedContextRuntime) LaunchOwned(_ context.Context, operation, _ context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	r.mu.Lock()
	r.launchCalls++
	r.launched = prepared
	r.mu.Unlock()
	close(r.started)
	<-operation.Done()
	r.operationErr <- operation.Err()
	return "", true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
}

func (r *ownedStopContextRuntime) Stop(ctx context.Context) (string, *protocol.APIError) {
	r.mu.Lock()
	r.stopCalls++
	r.mu.Unlock()
	close(r.legacyStarted)
	<-ctx.Done()
	return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
}

func (r *ownedStopContextRuntime) StopOwned(_ context.Context, operation context.Context) (string, *protocol.APIError) {
	r.mu.Lock()
	r.stopCalls++
	r.mu.Unlock()
	close(r.ownedStarted)
	select {
	case <-r.release:
		return "", nil
	case <-operation.Done():
		if r.operationErr != nil {
			r.operationErr <- operation.Err()
		}
		return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	}
}

func (*ownedStopContextRuntime) StopReady() bool { return true }

func (r *callerBoundStopRuntime) Stop(ctx context.Context) (string, *protocol.APIError) {
	r.mu.Lock()
	r.stopCalls++
	r.mu.Unlock()
	<-ctx.Done()
	r.contextErr <- ctx.Err()
	return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
}

func (f *fakeRuntime) Health(string) protocol.Health {
	return f.health
}

func (f *fakeRuntime) Reconcile(context.Context) protocol.Status {
	return f.reconciled
}

func (f *fakeRuntime) Prepare(spec core.Spec, path string) (mister.PreparedLaunch, *protocol.APIError) {
	f.mu.Lock()
	f.prepareCalls++
	f.prepareSpec = spec
	f.preparePath = path
	f.mu.Unlock()
	if f.prepareErr != nil {
		return mister.PreparedLaunch{}, f.prepareErr
	}
	result := f.prepared
	result.Spec = spec
	return result, nil
}

func (f *fakeRuntime) Launch(_ context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	f.mu.Lock()
	f.launchCalls++
	f.launched = prepared
	f.mu.Unlock()
	if f.launchGate != nil {
		<-f.launchGate
	}
	return f.launchObserved, true, f.launchErr
}

func (f *fakeRuntime) Stop(context.Context) (string, *protocol.APIError) {
	f.mu.Lock()
	f.stopCalls++
	f.mu.Unlock()
	return f.stopObserved, f.stopErr
}

func (f *fakeRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	f.mu.Lock()
	f.developmentStopCalls++
	f.mu.Unlock()
	return f.stopObserved, f.stopErr
}

func (f *fakeRuntime) LoadDevelopmentRBF(_ context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	body, err := io.ReadAll(content)
	if err != nil {
		return "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "test reader failed"}
	}
	f.mu.Lock()
	f.developmentCalls++
	f.developmentSize = size
	f.developmentBody = append([]byte(nil), body...)
	f.mu.Unlock()
	return f.developmentObserved, true, f.developmentErr
}

func (f *fakeRuntime) counts() (prepare, launch, stop int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCalls, f.launchCalls, f.stopCalls
}

func (f *fakeRuntime) launchInputs() (core.Spec, string, mister.PreparedLaunch) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareSpec, f.preparePath, f.launched
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
	spec, path, prepared := runtime.launchInputs()
	if path != "/media/fat/games/MegaDrive/test.md" || prepared.Spec.ROMRoot != "/media/fat/games/MegaDrive" || spec.ROMRoot != "/media/fat/games/MegaDrive" {
		t.Fatalf("v1 launch inputs = spec %#v path %q prepared %#v", spec, path, prepared)
	}
}

func TestOwnedLaunchUsesCoordinatorProcessContextForShutdown(t *testing.T) {
	process, stopProcess := context.WithCancel(context.Background())
	runtime := &ownedContextRuntime{
		fakeRuntime:  fakeRuntime{health: protocol.Health{Ready: true}},
		started:      make(chan struct{}),
		operationErr: make(chan error, 1),
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithOperationContext(process))
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
			GameID: "megadrive-owned-shutdown", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md",
		})
		done <- apiErr
	}()
	<-runtime.started
	stopProcess()
	select {
	case apiErr := <-done:
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("shutdown launch error = %#v", apiErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned launch did not stop with coordinator process context")
	}
	if err := <-runtime.operationErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("owned operation context error = %v, want canceled", err)
	}
}

func TestOwnedStopSurvivesInboundDeadlineAfterAdmission(t *testing.T) {
	runtime := &ownedStopContextRuntime{
		fakeRuntime:   fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive"},
		legacyStarted: make(chan struct{}),
		ownedStarted:  make(chan struct{}),
		release:       make(chan struct{}),
	}
	released := false
	defer func() {
		if !released {
			close(runtime.release)
		}
	}()
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithOperationContext(context.Background()))
	request := protocol.LaunchRequest{GameID: "megadrive-owned-stop", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"}
	if status, apiErr := coordinator.Launch(context.Background(), request); apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("launch = %#v, %#v", status, apiErr)
	}
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan struct {
		status protocol.Status
		err    *protocol.APIError
	}, 1)
	go func() {
		status, apiErr := coordinator.Stop(parent)
		done <- struct {
			status protocol.Status
			err    *protocol.APIError
		}{status: status, err: apiErr}
	}()
	select {
	case <-runtime.ownedStarted:
	case <-runtime.legacyStarted:
		t.Fatal("admitted native Stop used the inbound request context")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("runtime Stop was not dispatched")
	}
	<-parent.Done()
	select {
	case result := <-done:
		t.Fatalf("owned Stop ended with inbound request: status=%#v error=%#v", result.status, result.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(runtime.release)
	released = true
	select {
	case result := <-done:
		if result.err != nil || result.status.State != protocol.StateIdle {
			t.Fatalf("owned Stop completion = %#v, %#v", result.status, result.err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned Stop did not publish idle")
	}
	_, _, stopCalls := runtime.counts()
	if stopCalls != 1 {
		t.Fatalf("runtime Stop calls = %d, want one", stopCalls)
	}
}

func TestOwnedStopUsesCoordinatorProcessContextForShutdown(t *testing.T) {
	process, stopProcess := context.WithCancel(context.Background())
	runtime := &ownedStopContextRuntime{
		fakeRuntime:   fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive"},
		legacyStarted: make(chan struct{}),
		ownedStarted:  make(chan struct{}),
		release:       make(chan struct{}),
		operationErr:  make(chan error, 1),
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithOperationContext(process))
	request := protocol.LaunchRequest{GameID: "megadrive-owned-stop-shutdown", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"}
	if status, apiErr := coordinator.Launch(context.Background(), request); apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("launch = %#v, %#v", status, apiErr)
	}
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := coordinator.Stop(context.Background())
		done <- apiErr
	}()
	<-runtime.ownedStarted
	stopProcess()
	select {
	case apiErr := <-done:
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("shutdown Stop error = %#v", apiErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned Stop did not stop with coordinator process context")
	}
	if err := <-runtime.operationErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("owned Stop operation context error = %v, want canceled", err)
	}
}

func TestLegacyStopRemainsCallerBound(t *testing.T) {
	runtime := &callerBoundStopRuntime{
		fakeRuntime: fakeRuntime{
			health:     protocol.Health{Ready: true},
			reconciled: protocol.Status{State: protocol.StateActive, System: systemPtr(protocol.SystemSNES), ObservedCore: stringPtr("SNES")},
		},
		contextErr: make(chan error, 1),
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithOperationContext(context.Background()))
	coordinator.Initialize(context.Background())
	parent, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, apiErr := coordinator.Stop(parent)
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("legacy Stop error = %#v", apiErr)
	}
	if err := <-runtime.contextErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("legacy Stop context error = %v, want caller deadline", err)
	}
}

func TestDevelopmentRBFTransitionsToActiveAndStopsAtMenu(t *testing.T) {
	t.Parallel()
	payload := []byte("development-rbf")
	runtime := &fakeRuntime{
		health:              protocol.Health{Ready: true},
		developmentObserved: "DEVCORE",
		stopObserved:        "MENU",
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)

	status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil || status.System != nil || status.ObservedCore == nil || *status.ObservedCore != "DEVCORE" {
		t.Fatalf("development status = %#v", status)
	}
	runtime.mu.Lock()
	developmentCalls := runtime.developmentCalls
	developmentSize := runtime.developmentSize
	developmentBody := append([]byte(nil), runtime.developmentBody...)
	runtime.mu.Unlock()
	if developmentCalls != 1 || developmentSize != int64(len(payload)) || !bytes.Equal(developmentBody, payload) {
		t.Fatalf("development runtime call = count %d size %d body %q", developmentCalls, developmentSize, developmentBody)
	}
	runtime.health = protocol.Health{Ready: false}

	stopped, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || stopped.State != protocol.StateStopping || !stopped.Development || stopped.Recovery != protocol.RecoveryRebootRequired {
		t.Fatalf("stop = %#v, %#v", stopped, apiErr)
	}
	if runtime.developmentStopCalls != 0 || runtime.stopCalls != 0 {
		t.Fatalf("development recovery calls = %d normal stop calls = %d", runtime.developmentStopCalls, runtime.stopCalls)
	}
	stopped, apiErr = coordinator.RebootDevelopment(context.Background())
	if apiErr != nil || stopped.State != protocol.StateStopping || !stopped.Development || stopped.Recovery != protocol.RecoveryRebootRequired {
		t.Fatalf("development reboot = %#v, %#v", stopped, apiErr)
	}
	if runtime.developmentStopCalls != 1 || runtime.stopCalls != 0 {
		t.Fatalf("development recovery calls = %d normal stop calls = %d", runtime.developmentStopCalls, runtime.stopCalls)
	}
}

func TestOwnedDevelopmentRejectsCanceledAdmissionWithoutReadingOrDispatching(t *testing.T) {
	runtime := &ownedDevelopmentContextRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	admission, cancel := context.WithCancel(context.Background())
	cancel()
	body := &unreadNativeDevelopmentBody{}

	status, apiErr := coordinator.LoadDevelopmentRBF(admission, 3, body)
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("development error = %#v", apiErr)
	}
	if body.reads != 0 || runtime.dispatchCalls != 0 || runtime.developmentCalls != 0 || runtime.ownedDevelopmentCalls != 1 {
		t.Fatalf("canceled admission reads=%d dispatches=%d legacy=%d owned=%d", body.reads, runtime.dispatchCalls, runtime.developmentCalls, runtime.ownedDevelopmentCalls)
	}
	if status.State != protocol.StateFailed || !status.Development {
		t.Fatalf("status = %#v", status)
	}
}

func TestOwnedDevelopmentSeparatesAdmissionObservationAndProcessOwnership(t *testing.T) {
	process := context.WithValue(context.Background(), "process", "agent")
	runtime := &ownedDevelopmentContextRuntime{
		fakeRuntime:          fakeRuntime{health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE"},
		started:              make(chan struct{}),
		release:              make(chan struct{}),
		contextValues:        make(chan [3]string, 1),
		contextDeadlines:     make(chan [3]bool, 1),
		observationRemaining: make(chan time.Duration, 1),
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), 200*time.Millisecond, 3*time.Second, agent.WithOperationContext(process))
	admission, cancel := context.WithCancel(context.WithValue(context.Background(), "admission", "request"))
	done := make(chan struct {
		status protocol.Status
		err    *protocol.APIError
	}, 1)
	go func() {
		status, apiErr := coordinator.LoadDevelopmentRBF(admission, 3, bytes.NewReader([]byte("rbf")))
		done <- struct {
			status protocol.Status
			err    *protocol.APIError
		}{status: status, err: apiErr}
	}()

	select {
	case <-runtime.started:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned development mutation was not admitted")
	}
	values := <-runtime.contextValues
	if values != [3]string{"request", "agent", "agent"} {
		t.Fatalf("context ownership = %#v", values)
	}
	if deadlines := <-runtime.contextDeadlines; deadlines != [3]bool{false, true, false} {
		t.Fatalf("context deadlines = %#v, want admission/owner unbounded and observation bounded", deadlines)
	}
	if remaining := <-runtime.observationRemaining; remaining < 100*time.Millisecond || remaining > 300*time.Millisecond {
		t.Fatalf("observation deadline remaining = %v, want configured 200ms launch timeout", remaining)
	}
	cancel()
	select {
	case result := <-done:
		t.Fatalf("admitted mutation ended with caller cancellation: %#v %#v", result.status, result.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(runtime.release)
	select {
	case result := <-done:
		status := result.status
		if result.err != nil || status.State != protocol.StateActive || !status.Development || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore == nil || *status.ObservedCore != "DEVCORE" || status.LastError != nil || status.Recovery != "" {
			t.Fatalf("development completion = %#v, %#v", status, result.err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned development did not complete")
	}
	if runtime.dispatchCalls != 1 || runtime.ownedDevelopmentCalls != 1 || runtime.developmentCalls != 0 {
		t.Fatalf("dispatch count=%d owned calls=%d legacy calls=%d", runtime.dispatchCalls, runtime.ownedDevelopmentCalls, runtime.developmentCalls)
	}
}

func TestOwnedDevelopmentAmbiguousResultIsNotReplayed(t *testing.T) {
	runtime := &ownedDevelopmentContextRuntime{fakeRuntime: fakeRuntime{
		health:         protocol.Health{Ready: true},
		developmentErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "dispatch result is uncertain"},
	}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)

	status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || status.State != protocol.StateFailed || !status.Development {
		t.Fatalf("ambiguous development = %#v, %#v", status, apiErr)
	}
	if runtime.dispatchCalls != 1 || runtime.ownedDevelopmentCalls != 1 || runtime.developmentCalls != 0 {
		t.Fatalf("dispatch count=%d owned calls=%d legacy calls=%d", runtime.dispatchCalls, runtime.ownedDevelopmentCalls, runtime.developmentCalls)
	}
}

func TestOwnedDevelopmentStopsWithCoordinatorProcessShutdown(t *testing.T) {
	process, stopProcess := context.WithCancel(context.Background())
	runtime := &ownedDevelopmentContextRuntime{
		fakeRuntime:  fakeRuntime{health: protocol.Health{Ready: true}},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
		operationErr: make(chan error, 1),
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithOperationContext(process))
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
		done <- apiErr
	}()
	select {
	case <-runtime.started:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned development mutation was not admitted")
	}
	stopProcess()
	select {
	case apiErr := <-done:
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("shutdown development error = %#v", apiErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned development did not stop with process context")
	}
	if err := <-runtime.operationErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("operation context error = %v, want canceled", err)
	}
}

func TestNativeDevelopmentStopUsesNormalOwnedStopAndFinishesIdle(t *testing.T) {
	runtime := &ownedDevelopmentContextRuntime{
		fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE", stopObserved: "MENU"},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	if status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("load = %#v, %#v", status, apiErr)
	}
	runtime.health.Ready = false

	stopped, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || stopped.State != protocol.StateIdle || stopped.Development || stopped.Recovery != "" {
		t.Fatalf("stop = %#v, %#v", stopped, apiErr)
	}
	if runtime.ownedStopCalls != 1 || runtime.stopCalls != 1 || runtime.developmentStopCalls != 0 {
		t.Fatalf("owned stops=%d all stops=%d reboot recoveries=%d", runtime.ownedStopCalls, runtime.stopCalls, runtime.developmentStopCalls)
	}
	if _, rebootErr := coordinator.RebootDevelopment(context.Background()); rebootErr == nil || rebootErr.Code != protocol.CodeBadRequest {
		t.Fatalf("reboot error = %#v", rebootErr)
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

func TestInitializePublishesReconstructedDevelopmentBeforeCatalogueReconciliation(t *testing.T) {
	development := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: stringPtr("DEVCORE")}
	runtime := &ownedDevelopmentContextRuntime{fakeRuntime: fakeRuntime{reconciled: development}}
	staleSystem := protocol.SystemSNES
	store := &recordingContentStore{
		activeSystem: &staleSystem,
		reconcileErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "stale catalogue record must be bypassed"},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	if status := coordinator.Status(); !reflect.DeepEqual(status, development) {
		t.Fatalf("status = %#v, want %#v", status, development)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("development entered catalogue reconciliation: %#v", snapshot.reconciled)
	}
}

func TestInitializeUsesValidatedPendingSystemForSharedObservedCore(t *testing.T) {
	t.Parallel()
	gameGear := protocol.SystemGameGear
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateFailed,
		ObservedCore: stringPtr("SMS"),
		LastError:    &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is ambiguous"},
	}}
	store := &recordingContentStore{activeSystem: &gameGear}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateActive || status.System == nil || *status.System != protocol.SystemGameGear || status.ExpectedCore == nil || *status.ExpectedCore != "SMS" || status.ObservedCore == nil || *status.ObservedCore != "SMS" || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
	snapshot := store.snapshot()
	if len(snapshot.reconciled) != 1 || snapshot.reconciled[0].System == nil || *snapshot.reconciled[0].System != protocol.SystemGameGear {
		t.Fatalf("reconciled = %#v", snapshot.reconciled)
	}
}

func TestInitializeUsesDurableAtari2600IntentForAtari7800Observation(t *testing.T) {
	t.Parallel()
	atari2600 := protocol.SystemAtari2600
	observed := "ATARI7800"
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateFailed,
		ObservedCore: &observed,
		LastError:    &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core requires launch intent"},
	}}
	store := &recordingContentStore{activeSystem: &atari2600}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateActive || status.System == nil || *status.System != atari2600 || status.ExpectedCore == nil || *status.ExpectedCore != observed || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
}

func TestInitializeReconcilesInterruptedLaunchWhenObservedCoreSelectsOneSide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate protocol.System
		previous  protocol.System
		observed  string
		selected  protocol.System
	}{
		{name: "Game Boy candidate", candidate: protocol.SystemGameBoy, previous: protocol.SystemSNES, observed: "GAMEBOY", selected: protocol.SystemGameBoy},
		{name: "Game Boy Advance candidate", candidate: protocol.SystemGBA, previous: protocol.SystemSNES, observed: "GBA", selected: protocol.SystemGBA},
		{name: "PC Engine candidate", candidate: protocol.SystemPCE, previous: protocol.SystemSNES, observed: "TGFX16", selected: protocol.SystemPCE},
		{name: "legacy previous", candidate: protocol.SystemGBA, previous: protocol.SystemNES, observed: "NES", selected: protocol.SystemNES},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtimeSystem := test.selected
			runtime := &fakeRuntime{reconciled: protocol.Status{
				State: protocol.StateActive, System: &runtimeSystem, ObservedCore: &test.observed,
			}}
			alternatives := targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: test.candidate}, Previous: &targetcache.ActiveRecordEntry{System: test.previous}, Interrupted: true}
			store := &recordingContentStore{activeSystems: &alternatives}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			agent.NewContentController(coordinator, store)

			coordinator.Initialize(context.Background())

			status := coordinator.Status()
			if status.State != protocol.StateActive || status.System == nil || *status.System != test.selected || status.ExpectedCore == nil || *status.ExpectedCore != test.observed || status.LastError != nil {
				t.Fatalf("status = %#v", status)
			}
			snapshot := store.snapshot()
			if len(snapshot.reconciled) != 1 || snapshot.reconciled[0].System == nil || *snapshot.reconciled[0].System != test.selected {
				t.Fatalf("reconciled = %#v", snapshot.reconciled)
			}
		})
	}
}

func TestInitializePreservesInterruptedSharedCoreTransitionAsAmbiguous(t *testing.T) {
	t.Parallel()

	sms := protocol.SystemSMS
	gameGear := protocol.SystemGameGear
	observed := "SMS"
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State: protocol.StateActive, System: &sms, ExpectedCore: &observed, ObservedCore: &observed,
	}}
	alternatives := targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: gameGear}, Previous: &targetcache.ActiveRecordEntry{System: sms}, Interrupted: true}
	store := &recordingContentStore{activeSystems: &alternatives}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.System != nil || status.ExpectedCore != nil || status.ObservedCore == nil || *status.ObservedCore != observed || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("ambiguous launch was destructively reconciled: %#v", snapshot.reconciled)
	}
}

func TestInitializePreservesInterruptedDirectSharedCoreTransitionBothDirections(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		candidate protocol.System
		previous  protocol.System
	}{
		{name: "SMS to Game Gear", candidate: protocol.SystemGameGear, previous: protocol.SystemSMS},
		{name: "Game Gear to SMS", candidate: protocol.SystemSMS, previous: protocol.SystemGameGear},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observed := "SMS"
			runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &test.candidate, ObservedCore: &observed}}
			alternatives := targetcache.ActiveRecords{
				Candidate: targetcache.ActiveRecordEntry{System: test.candidate, Direct: true},
				Previous:  &targetcache.ActiveRecordEntry{System: test.previous}, Interrupted: true,
			}
			store := &recordingContentStore{activeSystems: &alternatives}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			agent.NewContentController(coordinator, store)

			coordinator.Initialize(context.Background())

			status := coordinator.Status()
			if status.State != protocol.StateFailed || status.System != nil || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
				t.Fatalf("status = %#v", status)
			}
			if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
				t.Fatalf("ambiguous direct launch was destructively reconciled: %#v", snapshot.reconciled)
			}
		})
	}
}

func TestInitializeSelectsUniqueInterruptedDirectCandidateOrCachedPrevious(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		observed string
		selected targetcache.ActiveRecordEntry
	}{
		{name: "direct candidate", observed: "GBA", selected: targetcache.ActiveRecordEntry{System: protocol.SystemGBA, Direct: true}},
		{name: "cached previous", observed: "SNES", selected: targetcache.ActiveRecordEntry{System: protocol.SystemSNES}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &test.selected.System, ObservedCore: &test.observed}}
			previous := targetcache.ActiveRecordEntry{System: protocol.SystemSNES}
			alternatives := targetcache.ActiveRecords{
				Candidate: targetcache.ActiveRecordEntry{System: protocol.SystemGBA, Direct: true},
				Previous:  &previous, Interrupted: true,
			}
			store := &recordingContentStore{activeSystems: &alternatives}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			agent.NewContentController(coordinator, store)

			coordinator.Initialize(context.Background())

			status := coordinator.Status()
			if status.State != protocol.StateActive || status.System == nil || *status.System != test.selected.System || status.LastError != nil {
				t.Fatalf("status = %#v", status)
			}
			snapshot := store.snapshot()
			if len(snapshot.reconcileSelected) != 1 || snapshot.reconcileSelected[0] == nil || *snapshot.reconcileSelected[0] != test.selected {
				t.Fatalf("selected record = %#v, want %#v", snapshot.reconcileSelected, test.selected)
			}
		})
	}
}

func TestInitializeFinalizesIdenticalInterruptedRelaunch(t *testing.T) {
	t.Parallel()

	gba := protocol.SystemGBA
	observed := "GBA"
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "gba"}
	entry := targetcache.ActiveRecordEntry{System: gba, Content: identity}
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &gba, ObservedCore: &observed}}
	alternatives := targetcache.ActiveRecords{Candidate: entry, Previous: &entry, Interrupted: true}
	store := &recordingContentStore{activeSystems: &alternatives}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateActive || status.System == nil || *status.System != gba || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
	snapshot := store.snapshot()
	if len(snapshot.reconcileSelected) != 1 || snapshot.reconcileSelected[0] == nil || *snapshot.reconcileSelected[0] != entry {
		t.Fatalf("selected durable entry = %#v; want %#v", snapshot.reconcileSelected, entry)
	}
}

func TestInitializePreservesSameSystemDifferentContentInterruptedRelaunch(t *testing.T) {
	t.Parallel()

	gba := protocol.SystemGBA
	observed := "GBA"
	candidate := targetcache.ActiveRecordEntry{System: gba, Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "gba"}}
	previous := targetcache.ActiveRecordEntry{System: gba, Content: protocol.ContentIdentity{SHA256: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Size: 4, Extension: "gba"}}
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &gba, ObservedCore: &observed}}
	alternatives := targetcache.ActiveRecords{Candidate: candidate, Previous: &previous, Interrupted: true}
	store := &recordingContentStore{activeSystems: &alternatives}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("ambiguous same-system relaunch was reconciled: %#v", snapshot.reconciled)
	}
}

func TestInitializePreservesInterruptedLaunchWhenObservedCoreMatchesNeitherEntry(t *testing.T) {
	t.Parallel()

	gba := protocol.SystemGBA
	snes := protocol.SystemSNES
	nes := protocol.SystemNES
	observed := "NES"
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &nes, ObservedCore: &observed}}
	alternatives := targetcache.ActiveRecords{
		Candidate:   targetcache.ActiveRecordEntry{System: gba},
		Previous:    &targetcache.ActiveRecordEntry{System: snes},
		Interrupted: true,
	}
	store := &recordingContentStore{activeSystems: &alternatives}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("unmatched interrupted launch was reconciled: %#v", snapshot.reconciled)
	}
}

func TestInitializeDoesNotPublishSelectedSystemWhenDurableFinalizationFails(t *testing.T) {
	t.Parallel()

	snes := protocol.SystemSNES
	gba := protocol.SystemGBA
	observed := "GBA"
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, System: &gba, ObservedCore: &observed}}
	alternatives := targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: gba}, Previous: &targetcache.ActiveRecordEntry{System: snes}, Interrupted: true}
	store := &recordingContentStore{
		activeSystems: &alternatives,
		reconcileErr:  &protocol.APIError{Code: protocol.CodeInternal, Message: "interrupted active cache record cannot be committed"},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.System != nil || status.ExpectedCore != nil || status.ObservedCore == nil || *status.ObservedCore != observed || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 1 {
		t.Fatalf("reconciled = %#v", snapshot.reconciled)
	}
}

func TestInitializeVerifiesDurableActiveWithIndependentContext(t *testing.T) {
	t.Parallel()
	gameGear := protocol.SystemGameGear
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateFailed,
		ObservedCore: stringPtr("SMS"),
		LastError:    &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is ambiguous"},
	}}
	var reconcileErr error
	var reconcileDeadline time.Time
	var reconcileHasDeadline bool
	store := &recordingContentStore{
		activeSystem: &gameGear,
		onReconcile: func(ctx context.Context, _ protocol.Status) {
			reconcileErr = ctx.Err()
			reconcileDeadline, reconcileHasDeadline = ctx.Deadline()
		},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)
	expired, cancel := context.WithCancel(context.Background())
	cancel()

	coordinator.Initialize(expired)

	status := coordinator.Status()
	if status.State != protocol.StateActive || status.System == nil || *status.System != protocol.SystemGameGear {
		t.Fatalf("status = %#v", status)
	}
	if reconcileErr != nil || !reconcileHasDeadline || !reconcileDeadline.After(time.Now()) {
		t.Fatalf("reconcile context = err %v, deadline %v, has deadline %v; want live bounded context", reconcileErr, reconcileDeadline, reconcileHasDeadline)
	}
}

func TestInitializePreservesDurableRecordWhenVerificationIsIndeterminate(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateActive,
		System:       systemPtr(protocol.SystemSMS),
		ExpectedCore: stringPtr("SMS"),
		ObservedCore: stringPtr("SMS"),
	}}
	store := &recordingContentStore{activeSystemErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "cache verification was canceled"}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.System != nil || status.ExpectedCore != nil || status.ObservedCore == nil || *status.ObservedCore != "SMS" || status.LastError == nil || status.LastError.Code != protocol.CodeInternal {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("indeterminate durable record was destructively reconciled: %#v", snapshot.reconciled)
	}
}

func TestInitializePreservesDurableRecordWhenRuntimeHasNoObservation(t *testing.T) {
	t.Parallel()
	gameGear := protocol.SystemGameGear
	sms := protocol.SystemSMS
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:     protocol.StateFailed,
		LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "startup reconciliation timed out"},
	}}
	alternatives := targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: gameGear}, Previous: &targetcache.ActiveRecordEntry{System: sms}, Interrupted: true}
	store := &recordingContentStore{activeSystems: &alternatives}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.ObservedCore != nil || status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("status = %#v", status)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("unobserved runtime state destructively reconciled durable content: %#v", snapshot.reconciled)
	}
}

func TestInitializeClearsInterruptedLaunchAfterConclusiveMenuObservation(t *testing.T) {
	t.Parallel()

	gameGear := protocol.SystemGameGear
	sms := protocol.SystemSMS
	interrupted := targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: gameGear}, Previous: &targetcache.ActiveRecordEntry{System: sms}, Interrupted: true}
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateIdle}}
	store := &recordingContentStore{activeSystems: &interrupted}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())

	status := coordinator.Status()
	if status.State != protocol.StateIdle || status.System != nil || status.ExpectedCore != nil || status.LastError != nil {
		t.Fatalf("status = %#v; want idle", status)
	}
	snapshot := store.snapshot()
	if len(snapshot.reconciled) != 1 || snapshot.reconciled[0].State != protocol.StateIdle || snapshot.reconcileSelected[0] != nil {
		t.Fatalf("reconciled = %#v selected = %#v", snapshot.reconciled, snapshot.reconcileSelected)
	}
}

func TestInitializeOverridesCanonicalSharedCoreWithPendingGameGearSystem(t *testing.T) {
	t.Parallel()
	gameGear := protocol.SystemGameGear
	sms := protocol.SystemSMS
	expected := "SMS"
	observed := "SMS"
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateActive,
		System:       &sms,
		ExpectedCore: &expected,
		ObservedCore: &observed,
	}}
	store := &recordingContentStore{activeSystem: &gameGear}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateActive || status.System == nil || *status.System != protocol.SystemGameGear || status.ExpectedCore == nil || *status.ExpectedCore != "SMS" || status.ObservedCore == nil || *status.ObservedCore != "SMS" || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
}

func TestInitializeDoesNotUseIncompatiblePendingSystem(t *testing.T) {
	t.Parallel()
	gameGear := protocol.SystemGameGear
	runtime := &fakeRuntime{reconciled: protocol.Status{
		State:        protocol.StateFailed,
		ObservedCore: stringPtr("SNES"),
		LastError:    &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is ambiguous"},
	}}
	store := &recordingContentStore{activeSystem: &gameGear}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	status := coordinator.Status()
	if status.State != protocol.StateFailed || status.System != nil || status.ObservedCore == nil || *status.ObservedCore != "SNES" || status.LastError == nil || status.LastError.Code != protocol.CodeUnrecognizedCore {
		t.Fatalf("status = %#v", status)
	}
}

func TestHealthReportsAttachedArtifacts(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	artifacts := &protocol.Artifacts{
		RuntimeCommit: "1111111111111111111111111111111111111111",
		Cores:         map[string]string{"megadrive": strings.Repeat("a", 64)},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, agent.WithArtifacts(artifacts))
	health := coordinator.Health("0.1.0")
	if health.Artifacts == nil || health.Artifacts.RuntimeCommit != artifacts.RuntimeCommit || health.Artifacts.Cores["megadrive"] != artifacts.Cores["megadrive"] {
		t.Fatalf("health artifacts = %#v", health.Artifacts)
	}
	artifacts.Cores["megadrive"] = strings.Repeat("b", 64)
	if health.Artifacts.Cores["megadrive"] == artifacts.Cores["megadrive"] {
		t.Fatal("health artifacts aliased caller map")
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

type nativeIdleControl struct {
	statusCalls int
	launchCalls int
	stopCalls   int
	launch      misterruntime.Response
	request     misterruntime.LaunchRequest
	statusError *misterruntime.RemoteError
	stopResult  *misterruntime.Response
}

func (c *nativeIdleControl) Status(context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Error: c.statusError, Version: "test"}, nil
}

func (c *nativeIdleControl) Stop(context.Context) (misterruntime.Response, error) {
	c.stopCalls++
	if c.stopResult != nil {
		return *c.stopResult, nil
	}
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (c *nativeIdleControl) Launch(_ context.Context, request misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.launchCalls++
	c.request = request
	return c.launch, nil
}

func (*nativeIdleControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func TestCoordinatorLaunchesMegaDriveThroughTheNativeRuntimeTranslation(t *testing.T) {
	t.Parallel()
	rom := filepath.Join(t.TempDir(), "sonic2.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	system, coreName := "megadrive", "MegaDrive"
	control := &nativeIdleControl{launch: misterruntime.Response{
		Protocol: 1, OK: true, State: "running_game", Execution: "game",
		System: &system, Core: &coreName, Version: "test",
	}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())

	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-sonic2", System: protocol.SystemMegaDrive, ROMPath: rom,
	})
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != "megadrive-sonic2" ||
		status.System == nil || *status.System != protocol.SystemMegaDrive ||
		status.ExpectedCore == nil || *status.ExpectedCore != "MegaDrive" ||
		status.ObservedCore == nil || *status.ObservedCore != "MegaDrive" {
		t.Fatalf("status = %#v", status)
	}
	if control.launchCalls != 1 || control.request.System != "megadrive" ||
		control.request.RBF != "/usr/share/mister-runtime/cores/megadrive.rbf" ||
		len(control.request.Media) != 1 || control.request.Media["cartridge"] != rom ||
		control.request.Settings == nil || len(control.request.Settings) != 0 {
		t.Fatalf("native request = %#v calls=%d", control.request, control.launchCalls)
	}
}

type nativeLifecycleControl struct {
	state               string
	idleError           *misterruntime.RemoteError
	statusCalls         int
	launchCalls         int
	stopCalls           int
	subsequentLaunchErr error
}

type nativeDevelopmentRecoveryControl struct {
	stopCalls int
}

func (c *nativeDevelopmentRecoveryControl) Status(context.Context) (misterruntime.Response, error) {
	coreName := "DEVCORE"
	return misterruntime.Response{
		Protocol: 1, OK: true, State: "running_development", Execution: "development",
		Core: &coreName, Version: "test",
	}, nil
}

func (*nativeDevelopmentRecoveryControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected Launch")
}

func (*nativeDevelopmentRecoveryControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected LoadDevelopmentRBF")
}

func (c *nativeDevelopmentRecoveryControl) Stop(context.Context) (misterruntime.Response, error) {
	c.stopCalls++
	return misterruntime.Response{
		Protocol: 1, OK: false, State: "reboot_required", Execution: "none",
		Error:   &misterruntime.RemoteError{Code: "idle_failed", Message: "private cleanup detail"},
		Version: "test",
	}, nil
}

func TestCoordinatorNativeDevelopmentStopSurfacesExplicitRecoveryState(t *testing.T) {
	t.Parallel()
	control := &nativeDevelopmentRecoveryControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())

	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateStopping || !status.Development ||
		status.Recovery != protocol.RecoveryRebootRequired || status.LastError != nil {
		t.Fatalf("native development stop = %#v, %#v", status, apiErr)
	}
	if control.stopCalls != 1 {
		t.Fatalf("native development stop calls = %d, want one", control.stopCalls)
	}
}

type nativeDevelopmentLoadRecoveryRuntime struct {
	fakeRuntime
	legacyLoadCalls   int
	recoveryLoadCalls int
	ownedStopCalls    int
	recoveryStopCalls int
}

func (r *nativeDevelopmentLoadRecoveryRuntime) LoadDevelopmentRBFOwned(context.Context, context.Context, context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	r.legacyLoadCalls++
	return "", true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "legacy owned load should not be selected"}
}

func (r *nativeDevelopmentLoadRecoveryRuntime) LoadDevelopmentRBFOwnedWithRecovery(context.Context, context.Context, context.Context, int64, io.Reader) (string, string, bool, *protocol.APIError) {
	r.recoveryLoadCalls++
	return "", protocol.RecoveryRebootRequired, true, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
}

func (r *nativeDevelopmentLoadRecoveryRuntime) StopReady() bool { return false }

func (r *nativeDevelopmentLoadRecoveryRuntime) StopOwned(context.Context, context.Context) (string, *protocol.APIError) {
	r.ownedStopCalls++
	return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "pending recovery must not call Stop"}
}

func (r *nativeDevelopmentLoadRecoveryRuntime) StopOwnedWithRecovery(context.Context, context.Context) (string, string, *protocol.APIError) {
	r.recoveryStopCalls++
	return "", "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "pending recovery must not call Stop"}
}

func TestCoordinatorPreservesLoadRecoveryAndRetriesStopWithoutHardwareStop(t *testing.T) {
	runtime := &nativeDevelopmentLoadRecoveryRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)

	status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("development load error = %#v, want public unavailable", apiErr)
	}
	if status.State != protocol.StateStopping || !status.Development || status.Recovery != protocol.RecoveryRebootRequired || status.LastError != nil {
		t.Fatalf("development load status = %#v, want pending recovery", status)
	}
	if runtime.recoveryLoadCalls != 1 || runtime.legacyLoadCalls != 0 {
		t.Fatalf("development load calls = recovery:%d legacy:%d", runtime.recoveryLoadCalls, runtime.legacyLoadCalls)
	}

	status, apiErr = coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateStopping || !status.Development || status.Recovery != protocol.RecoveryRebootRequired || status.LastError != nil {
		t.Fatalf("pending recovery Stop = %#v, %#v", status, apiErr)
	}
	if runtime.ownedStopCalls != 0 || runtime.recoveryStopCalls != 0 {
		t.Fatalf("pending recovery invoked hardware Stop = owned:%d recovery:%d", runtime.ownedStopCalls, runtime.recoveryStopCalls)
	}
}

type nativeGameRecoveryRuntime struct {
	fakeRuntime
	stopCalls int
}

func (*nativeGameRecoveryRuntime) StopReady() bool { return true }

func (r *nativeGameRecoveryRuntime) StopOwned(context.Context, context.Context) (string, *protocol.APIError) {
	r.stopCalls++
	return "", nil
}

func (r *nativeGameRecoveryRuntime) StopOwnedWithRecovery(context.Context, context.Context) (string, string, *protocol.APIError) {
	r.stopCalls++
	return "", protocol.RecoveryRebootRequired, nil
}

func TestCoordinatorNativeGameStopDoesNotTreatRecoveryAsIdle(t *testing.T) {
	t.Parallel()
	runtime := &nativeGameRecoveryRuntime{fakeRuntime: fakeRuntime{
		health:         protocol.Health{Ready: true},
		launchObserved: "MegaDrive",
	}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)

	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-recovery", System: protocol.SystemMegaDrive,
		ROMPath: "/media/fat/games/MegaDrive/recovery.bin",
	})
	if apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("launch = %#v, %#v", status, apiErr)
	}
	status, apiErr = coordinator.Stop(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable ||
		status.State != protocol.StateFailed || status.LastError == nil ||
		status.LastError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("native game stop = %#v, %#v", status, apiErr)
	}
	if runtime.stopCalls != 1 {
		t.Fatalf("native game stop calls = %d, want one", runtime.stopCalls)
	}
}

func (c *nativeLifecycleControl) Status(context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	if c.state == "running_game" {
		system, coreName := "megadrive", "MegaDrive"
		return misterruntime.Response{
			Protocol: 1, OK: true, State: "running_game", Execution: "game",
			System: &system, Core: &coreName, Version: "test",
		}, nil
	}
	response := misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}
	if c.idleError != nil {
		retained := *c.idleError
		response.Error = &retained
	}
	return response, nil
}

func (c *nativeLifecycleControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.launchCalls++
	if c.launchCalls > 1 && c.subsequentLaunchErr != nil {
		return misterruntime.Response{}, c.subsequentLaunchErr
	}
	c.state = "running_game"
	system, coreName := "megadrive", "MegaDrive"
	return misterruntime.Response{
		Protocol: 1, OK: true, State: "running_game", Execution: "game",
		System: &system, Core: &coreName, Version: "test",
	}, nil
}

func (*nativeLifecycleControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func (c *nativeLifecycleControl) Stop(context.Context) (misterruntime.Response, error) {
	c.stopCalls++
	c.state = "idle"
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func TestCoordinatorStopsAnActiveNativeGameAndImmediatelyRelaunches(t *testing.T) {
	rom := filepath.Join(t.TempDir(), "sonic2.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &nativeLifecycleControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	request := protocol.LaunchRequest{
		GameID: "megadrive-sonic2", System: protocol.SystemMegaDrive, ROMPath: rom,
	}

	launched, apiErr := coordinator.Launch(context.Background(), request)
	if apiErr != nil || launched.State != protocol.StateActive {
		t.Fatalf("launch = %#v error:%#v", launched, apiErr)
	}
	if health := coordinator.Health("test"); health.Ready {
		t.Fatalf("active native game was globally launch-ready: %#v", health)
	}
	stopped, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || stopped.State != protocol.StateIdle {
		t.Fatalf("stop = %#v error:%#v", stopped, apiErr)
	}
	relaunched, apiErr := coordinator.Launch(context.Background(), request)
	if apiErr != nil || relaunched.State != protocol.StateActive ||
		relaunched.ObservedCore == nil || *relaunched.ObservedCore != "MegaDrive" {
		t.Fatalf("relaunch = %#v error:%#v", relaunched, apiErr)
	}
	if control.launchCalls != 2 || control.stopCalls != 1 || control.statusCalls != 7 || control.state != "running_game" {
		t.Fatalf("control = state:%q status:%d launch:%d stop:%d", control.state, control.statusCalls, control.launchCalls, control.stopCalls)
	}
}

func TestCoordinatorRecoversOperationalIdleWithRetainedErrorAndLaunches(t *testing.T) {
	rom := filepath.Join(t.TempDir(), "sonic2.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &nativeLifecycleControl{idleError: &misterruntime.RemoteError{
		Code: "io_failed", Message: "private prior cleanup detail",
	}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())

	recovered := coordinator.Status()
	if recovered.State != protocol.StateIdle || recovered.LastError == nil ||
		recovered.LastError.Code != protocol.CodeMiSTerUnavailable ||
		recovered.LastError.Message != "target runtime is unavailable" {
		t.Errorf("recovered status = %#v", recovered)
	}
	if health := coordinator.Health("test"); !health.Ready {
		t.Errorf("recovered idle health = %#v", health)
	}
	launched, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-sonic2", System: protocol.SystemMegaDrive, ROMPath: rom,
	})
	if apiErr != nil || launched.State != protocol.StateActive || launched.GameID == nil ||
		*launched.GameID != "megadrive-sonic2" || launched.LastError != nil {
		t.Fatalf("launch = %#v error:%#v", launched, apiErr)
	}
	if control.statusCalls != 4 || control.launchCalls != 1 || control.state != "running_game" {
		t.Fatalf("control = state:%q status:%d launch:%d", control.state, control.statusCalls, control.launchCalls)
	}
}

func TestCoordinatorRejectsLostSecondLaunchAgainstTheOldNativeSessionWithoutChangingIntent(t *testing.T) {
	rom := filepath.Join(t.TempDir(), "sonic2.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &nativeLifecycleControl{subsequentLaunchErr: errors.New("busy response was lost")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	store := &recordingContentStore{}
	agent.NewContentController(coordinator, store)
	coordinator.Initialize(context.Background())

	first, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-sonic2", System: protocol.SystemMegaDrive, ROMPath: rom,
	})
	if apiErr != nil || first.State != protocol.StateActive || first.GameID == nil || *first.GameID != "megadrive-sonic2" {
		t.Fatalf("first launch = %#v error:%#v", first, apiErr)
	}
	beforeIntent := store.snapshot()

	second, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-sonic3", System: protocol.SystemMegaDrive, ROMPath: rom,
	})
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "target runtime is unavailable" {
		t.Fatalf("second launch error = %#v", apiErr)
	}
	if !reflect.DeepEqual(second, first) || !reflect.DeepEqual(coordinator.Status(), first) {
		t.Fatalf("old active status changed: first=%#v returned=%#v current=%#v", first, second, coordinator.Status())
	}
	afterIntent := store.snapshot()
	if afterIntent.directIntentCalls != beforeIntent.directIntentCalls ||
		afterIntent.directCommitCalls != beforeIntent.directCommitCalls ||
		afterIntent.directAbortCalls != beforeIntent.directAbortCalls {
		t.Fatalf("rejected launch changed intent: before=%#v after=%#v", beforeIntent, afterIntent)
	}
	if control.launchCalls != 1 || control.state != "running_game" {
		t.Fatalf("old runtime session was mutated: state=%q launch calls=%d", control.state, control.launchCalls)
	}
}

func TestCoordinatorStopWhileNativeIdleDoesNotCallRuntimeStop(t *testing.T) {
	t.Parallel()
	control := &nativeIdleControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	coordinator := agent.New(runtime, core.NewRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateIdle {
		t.Fatalf("stop = %#v, %#v", status, apiErr)
	}
	if control.statusCalls != 1 || control.stopCalls != 0 {
		t.Fatalf("control calls = status:%d stop:%d", control.statusCalls, control.stopCalls)
	}
}

func TestCoordinatorStopRetriesIdleRuntimeError(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		stopResult    misterruntime.Response
		wantError     bool
		wantState     protocol.State
		wantLastError bool
	}{
		{
			name:       "confirmed stop clears retained error",
			stopResult: misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"},
			wantState:  protocol.StateIdle,
		},
		{
			name: "failed stop retains recovery error",
			stopResult: misterruntime.Response{
				Protocol: 1, OK: false, State: "idle", Execution: "none", Version: "test",
				Error: &misterruntime.RemoteError{Code: "io_failed", Message: "missing native core"},
			},
			wantError:     true,
			wantState:     protocol.StateFailed,
			wantLastError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := &nativeIdleControl{
				statusError: &misterruntime.RemoteError{Code: "io_failed", Message: "missing native core"},
				stopResult:  &test.stopResult,
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			coordinator := agent.New(runtime, core.NewRegistry(), time.Second, time.Second)
			coordinator.Initialize(context.Background())

			status, apiErr := coordinator.Stop(context.Background())
			if (apiErr != nil) != test.wantError || status.State != test.wantState || (status.LastError != nil) != test.wantLastError {
				t.Fatalf("stop = %#v, %#v", status, apiErr)
			}
			if control.stopCalls != 1 {
				t.Fatalf("runtime Stop calls = %d, want one", control.stopCalls)
			}
		})
	}
}

type unreadNativeDevelopmentBody struct {
	reads int
}

func (r *unreadNativeDevelopmentBody) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("native unsupported body must not be read")
}

func TestNativeUnsupportedDevelopmentPreservesIdleAndPublicStopDoesNotMutateControl(t *testing.T) {
	t.Parallel()
	control := &nativeIdleControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	coordinator := agent.New(runtime, core.NewRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	body := &unreadNativeDevelopmentBody{}

	returned, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, body)
	if apiErr == nil || apiErr.Code != protocol.CodeUnsupportedOperation || apiErr.Message != "requested operation is unsupported" {
		t.Fatalf("development error = %#v", apiErr)
	}
	assertCleanIdleStatus(t, returned)
	assertCleanIdleStatus(t, coordinator.Status())
	if body.reads != 0 {
		t.Fatalf("development body reads = %d", body.reads)
	}

	stopped, stopErr := coordinator.Stop(context.Background())
	if stopErr != nil {
		t.Fatalf("idle stop error = %#v", stopErr)
	}
	assertCleanIdleStatus(t, stopped)
	if control.statusCalls != 2 || control.stopCalls != 0 {
		t.Fatalf("control calls = status:%d stop:%d", control.statusCalls, control.stopCalls)
	}
}

func TestAttemptedDevelopmentFailureStillReportsFailedOwnership(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{
		health:              protocol.Health{Ready: true},
		developmentObserved: "DEVCORE",
		developmentErr:      &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "dispatch result is uncertain"},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("development error = %#v", apiErr)
	}
	if status.State != protocol.StateFailed || !status.Development || status.ObservedCore == nil || *status.ObservedCore != "DEVCORE" || status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("status = %#v", status)
	}
}

func assertCleanIdleStatus(t *testing.T, status protocol.Status) {
	t.Helper()
	if status.State != protocol.StateIdle || status.Development || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.LastError != nil || status.Recovery != "" {
		t.Fatalf("status = %#v", status)
	}
}

func TestUnavailableCoordinatorErrorsUseBackendNeutralMessage(t *testing.T) {
	t.Parallel()
	const wantMessage = "target runtime is unavailable"

	t.Run("launch", func(t *testing.T) {
		runtime := &fakeRuntime{health: protocol.Health{Ready: false}}
		coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
		_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/games/test.sfc"})
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})

	t.Run("development", func(t *testing.T) {
		runtime := &fakeRuntime{health: protocol.Health{Ready: false}}
		coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
		_, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})

	t.Run("stop", func(t *testing.T) {
		runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES"}
		coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
		if _, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/games/test.sfc"}); apiErr != nil {
			t.Fatal(apiErr)
		}
		runtime.health.Ready = false
		_, apiErr := coordinator.Stop(context.Background())
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})
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
	_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "mystery-test", System: "mystery", ROMPath: "/media/fat/games/Mystery/test.bin"})
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

type lostStopLifecycleControl struct{ nativeLifecycleControl }

func (c *lostStopLifecycleControl) Stop(ctx context.Context) (misterruntime.Response, error) {
	_, _ = c.nativeLifecycleControl.Stop(ctx)
	return misterruntime.Response{}, io.EOF
}

func TestCoordinatorRelaunchesAfterLostSuccessfulNativeStop(t *testing.T) {
	rom := filepath.Join(t.TempDir(), "game.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &lostStopLifecycleControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	request := protocol.LaunchRequest{GameID: "recovery-game", System: protocol.SystemMegaDrive, ROMPath: rom}
	if _, err := coordinator.Launch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	stopped, err := coordinator.Stop(context.Background())
	if err != nil || stopped.State != protocol.StateIdle || stopped.LastError != nil {
		t.Fatalf("Stop = %#v, %#v", stopped, err)
	}
	if !coordinator.Health("test").Ready {
		t.Fatal("confirmed idle is not ready")
	}
	launched, err := coordinator.Launch(context.Background(), request)
	if err != nil || launched.State != protocol.StateActive {
		t.Fatalf("relaunch = %#v, %#v", launched, err)
	}
	if control.stopCalls != 1 || control.launchCalls != 2 {
		t.Fatalf("Stop=%d Launch=%d", control.stopCalls, control.launchCalls)
	}
}

type recoveredLaunchControl struct {
	nativeLifecycleControl
	fail        bool
	failureCode string
}

func (c *recoveredLaunchControl) Launch(ctx context.Context, request misterruntime.LaunchRequest) (misterruntime.Response, error) {
	if !c.fail {
		return c.nativeLifecycleControl.Launch(ctx, request)
	}
	c.launchCalls++
	c.state = "idle"
	code := c.failureCode
	if code == "" {
		code = "io_failed"
	}
	return misterruntime.Response{Protocol: 1, OK: false, State: "idle", Execution: "none", Version: "test", Error: &misterruntime.RemoteError{Code: code, Message: "private launch failure"}}, nil
}

func TestCoordinatorFailedNativeLaunchRecoveredToIdleAllowsNextLaunch(t *testing.T) {
	rom := filepath.Join(t.TempDir(), "game.bin")
	if err := os.WriteFile(rom, []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &recoveredLaunchControl{fail: true}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t))
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	request := protocol.LaunchRequest{GameID: "recovery", System: protocol.SystemMegaDrive, ROMPath: rom}
	status, err := coordinator.Launch(context.Background(), request)
	if err == nil || status.State != protocol.StateIdle || status.LastError == nil || status.GameID != nil {
		t.Fatalf("failed launch = %#v, %#v", status, err)
	}
	if !coordinator.Health("test").Ready {
		t.Fatal("safe idle remains unavailable")
	}
	control.fail = false
	status, err = coordinator.Launch(context.Background(), request)
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("next launch = %#v, %#v", status, err)
	}
	if control.launchCalls != 2 || control.stopCalls != 0 {
		t.Fatalf("launch=%d stop=%d", control.launchCalls, control.stopCalls)
	}
}

func TestFailedNativeLaunchClearsContentOnlyAfterConfirmedIdle(t *testing.T) {
	for _, cleanupFails := range []bool{false, true} {
		t.Run(fmt.Sprint("cleanupFails=", cleanupFails), func(t *testing.T) {
			control := &recoveredLaunchControl{fail: true, failureCode: "invalid_request"}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, nativeCoreFixture(t, "pong"))
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			store := &recordingContentStore{pinned: true}
			if cleanupFails {
				store.clearErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "cannot clear active record"}
			}
			agent.NewContentController(coordinator, store)
			status, err := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "pong", System: protocol.SystemPong})
			if err == nil || status.LastError == nil {
				t.Fatalf("launch = %#v, %#v", status, err)
			}
			if store.clearCalls != 1 || store.pinned != cleanupFails {
				t.Fatalf("clear=%d pinned=%v", store.clearCalls, store.pinned)
			}
			if cleanupFails {
				if status.State != protocol.StateFailed || coordinator.Health("test").Ready {
					t.Fatalf("cleanup failure reported ready: %#v", status)
				}
				control.fail = false
				if _, nextErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "pong", System: protocol.SystemPong}); nextErr == nil || control.launchCalls != 1 {
					t.Fatal("new launch bypassed failed content cleanup")
				}
				if _, devErr := coordinator.LoadDevelopmentRBF(context.Background(), 1, strings.NewReader("x")); devErr == nil || devErr.Code != protocol.CodeMiSTerUnavailable {
					t.Fatalf("development bypassed cleanup: %#v", devErr)
				}
				store.clearErr = nil
				if stopped, stopErr := coordinator.Stop(context.Background()); stopErr != nil || stopped.State != protocol.StateIdle || !coordinator.Health("test").Ready {
					t.Fatalf("cleanup retry = %#v, %#v", stopped, stopErr)
				}

			} else if status.State != protocol.StateIdle || !coordinator.Health("test").Ready {
				t.Fatalf("confirmed cleanup not ready: %#v", status)
			}
		})
	}
}
