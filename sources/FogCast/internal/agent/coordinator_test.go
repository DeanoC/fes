package agent_test

import (
	"bytes"
	"context"
	"errors"

	"io"

	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/misterruntime"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type fakeRuntime struct {
	mu                   sync.Mutex
	health               protocol.Health
	reconciled           protocol.Status
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
	preparePath          string
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

func (r *ownedStopContextRuntime) LoadDevelopmentRBFOwned(admission, _ context.Context, _ context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	return r.fakeRuntime.LoadDevelopmentRBF(admission, size, content)
}

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
	if f.launchGate != nil {
		<-f.launchGate
	}
	return f.developmentObserved, true, f.developmentErr
}

func (f *fakeRuntime) counts() (prepare, launch, stop int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCalls, f.launchCalls, f.stopCalls
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
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithOperationContext(context.Background()))
	if status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); apiErr != nil || status.State != protocol.StateActive {
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
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithOperationContext(process))
	if status, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); apiErr != nil || status.State != protocol.StateActive {
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
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithOperationContext(context.Background()))
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
	coordinator := agent.New(runtime, time.Second, time.Second)

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

type idleFirstRuntime struct {
	fakeRuntime
	idle      bool
	idleErr   *protocol.APIError
	idleCalls int
}

func (r *idleFirstRuntime) RecoverIdle(context.Context) (bool, *protocol.APIError) {
	r.idleCalls++
	return r.idle, r.idleErr
}

func TestDevelopmentRecoveryProgramsIdleWithoutBoardReboot(t *testing.T) {
	t.Parallel()
	runtime := &idleFirstRuntime{fakeRuntime: fakeRuntime{
		health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE", stopObserved: "MENU",
	}, idle: true}
	coordinator := agent.New(runtime, time.Second, time.Second)
	if _, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); apiErr != nil {
		t.Fatal(apiErr)
	}
	runtime.health = protocol.Health{Ready: false}
	stopped, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || stopped.Recovery != protocol.RecoveryRebootRequired {
		t.Fatalf("stop = %#v, %#v", stopped, apiErr)
	}
	stopped, apiErr = coordinator.RebootDevelopment(context.Background())
	if apiErr != nil || stopped.State != protocol.StateIdle || stopped.Development || stopped.Recovery != "" {
		t.Fatalf("idle recovery = %#v, %#v", stopped, apiErr)
	}
	if runtime.idleCalls != 1 || runtime.developmentStopCalls != 0 {
		t.Fatalf("idle calls=%d reboot calls=%d", runtime.idleCalls, runtime.developmentStopCalls)
	}
}

func TestDevelopmentRecoveryRebootsOnlyAfterIdleProgramFailure(t *testing.T) {
	t.Parallel()
	runtime := &idleFirstRuntime{fakeRuntime: fakeRuntime{
		health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE",
	}, idleErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "recovery", Message: "target runtime recovery is required"}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	if _, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); apiErr != nil {
		t.Fatal(apiErr)
	}
	runtime.health = protocol.Health{Ready: false}
	if _, apiErr := coordinator.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	stopped, apiErr := coordinator.RebootDevelopment(context.Background())
	if apiErr != nil || stopped.State != protocol.StateStopping || stopped.Recovery != protocol.RecoveryRebootRequired {
		t.Fatalf("reboot after idle failure = %#v, %#v", stopped, apiErr)
	}
	if runtime.idleCalls != 1 || runtime.developmentStopCalls != 1 {
		t.Fatalf("idle calls=%d reboot calls=%d", runtime.idleCalls, runtime.developmentStopCalls)
	}
}

func TestDevelopmentRecoveryRebootsWhenRecoverIdleIsUnknown(t *testing.T) {
	t.Parallel()
	runtime := &idleFirstRuntime{fakeRuntime: fakeRuntime{
		health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE",
	}, idleErr: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	if _, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); apiErr != nil {
		t.Fatal(apiErr)
	}
	runtime.health = protocol.Health{Ready: false}
	if _, apiErr := coordinator.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	stopped, apiErr := coordinator.RebootDevelopment(context.Background())
	if apiErr != nil || stopped.State != protocol.StateStopping || stopped.Recovery != protocol.RecoveryRebootRequired {
		t.Fatalf("unknown recover_idle = %#v, %#v", stopped, apiErr)
	}
	if runtime.idleCalls != 1 || runtime.developmentStopCalls != 1 {
		t.Fatalf("idle calls=%d reboot calls=%d", runtime.idleCalls, runtime.developmentStopCalls)
	}
}

func TestDevelopmentRecoveryDoesNotRebootWhenRuntimeSocketIsDead(t *testing.T) {
	t.Parallel()
	runtime := &idleFirstRuntime{fakeRuntime: fakeRuntime{
		health: protocol.Health{Ready: true}, developmentObserved: "DEVCORE",
	}, idleErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	if _, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); apiErr != nil {
		t.Fatal(apiErr)
	}
	runtime.health = protocol.Health{Ready: false}
	if _, apiErr := coordinator.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	stopped, apiErr := coordinator.RebootDevelopment(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || stopped.State != protocol.StateFailed {
		t.Fatalf("socket-dead recovery = %#v, %#v", stopped, apiErr)
	}
	if runtime.developmentStopCalls != 0 {
		t.Fatalf("board reboot calls = %d", runtime.developmentStopCalls)
	}
}

func TestOwnedDevelopmentRejectsCanceledAdmissionWithoutReadingOrDispatching(t *testing.T) {
	runtime := &ownedDevelopmentContextRuntime{fakeRuntime: fakeRuntime{health: protocol.Health{Ready: true}}}
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, 200*time.Millisecond, 3*time.Second, agent.WithOperationContext(process))
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
	coordinator := agent.New(runtime, time.Second, time.Second)

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
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithOperationContext(process))
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = coordinator.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf"))
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
	agent.NewContentController(coordinator, store)

	coordinator.Initialize(context.Background())
	if status := coordinator.Status(); !reflect.DeepEqual(status, development) {
		t.Fatalf("status = %#v, want %#v", status, development)
	}
	if snapshot := store.snapshot(); len(snapshot.reconciled) != 0 {
		t.Fatalf("development entered catalogue reconciliation: %#v", snapshot.reconciled)
	}
}

func TestHealthReportsAttachedArtifacts(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	artifacts := &protocol.Artifacts{
		RuntimeCommit: "1111111111111111111111111111111111111111",
		Cores:         map[string]string{"megadrive": strings.Repeat("a", 64)},
	}
	coordinator := agent.New(runtime, time.Second, time.Second, agent.WithArtifacts(artifacts))
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
		health:     protocol.Health{Ready: true},
		reconciled: protocol.Status{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "not ready"}},
	}
	coordinator := agent.New(runtime, time.Second, time.Second)
	coordinator.Initialize(context.Background())
	if health := coordinator.Health("0.1.0"); health.Ready {
		t.Fatalf("health = %#v", health)
	}
}

type nativeIdleControl struct {
	statusCalls int
	launchCalls int
	stopCalls   int
	launch      misterruntime.Protocol2Response
	statusError *misterruntime.Protocol2Error
	stopResult  *misterruntime.Protocol2Response
}

func (c *nativeIdleControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	c.statusCalls++
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Error: c.statusError, Version: "test"}, nil
}

func (c *nativeIdleControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	c.stopCalls++
	if c.stopResult != nil {
		return *c.stopResult, nil
	}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (*nativeIdleControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
}

type nativeDevelopmentRecoveryControl struct {
	stopCalls int
}

func (c *nativeDevelopmentRecoveryControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	generation := uint64(1)
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development",
		Generation: &generation, Version: "test",
	}, nil
}

func (*nativeDevelopmentRecoveryControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected LoadDevelopmentRBF")
}

func (c *nativeDevelopmentRecoveryControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	c.stopCalls++
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: false, State: "reboot_required", Execution: "none",
		Error:   &misterruntime.Protocol2Error{Phase: "lifecycle", Code: "idle_failed", Message: "private cleanup detail"},
		Version: "test",
	}, nil
}

func TestCoordinatorNativeDevelopmentStopSurfacesExplicitRecoveryState(t *testing.T) {
	t.Parallel()
	control := &nativeDevelopmentRecoveryControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)

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

func TestCoordinatorStopWhileNativeIdleDoesNotCallRuntimeStop(t *testing.T) {
	t.Parallel()
	control := &nativeIdleControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	coordinator := agent.New(runtime, time.Second, time.Second)
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
		stopResult    misterruntime.Protocol2Response
		wantError     bool
		wantState     protocol.State
		wantLastError bool
	}{
		{
			name:       "confirmed stop clears retained error",
			stopResult: misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"},
			wantState:  protocol.StateIdle,
		},
		{
			name: "failed stop retains recovery error",
			stopResult: misterruntime.Protocol2Response{
				Protocol: 2, OK: false, State: "idle", Execution: "none", Version: "test",
				Error: &misterruntime.Protocol2Error{Phase: "lifecycle", Code: "io_failed", Message: "missing native core"},
			},
			wantError:     true,
			wantState:     protocol.StateFailed,
			wantLastError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := &nativeIdleControl{
				statusError: &misterruntime.Protocol2Error{Phase: "lifecycle", Code: "io_failed", Message: "missing native core"},
				stopResult:  &test.stopResult,
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
		coordinator := agent.New(runtime, time.Second, time.Second)
		_, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf"))
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})

	t.Run("development", func(t *testing.T) {
		runtime := &fakeRuntime{health: protocol.Health{Ready: false}}
		coordinator := agent.New(runtime, time.Second, time.Second)
		_, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})

	t.Run("stop", func(t *testing.T) {
		runtime := &fakeRuntime{health: protocol.Health{Ready: true}, reconciled: protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{}}}
		coordinator := agent.New(runtime, time.Second, time.Second)
		coordinator.Initialize(context.Background())
		runtime.health.Ready = false
		_, apiErr := coordinator.Stop(context.Background())
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != wantMessage {
			t.Fatalf("error = %#v", apiErr)
		}
	})
}

func TestReturnedErrorCannotMutateFailedStatus(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MENU", developmentErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "timeout"}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	_, apiErr := coordinator.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf"))
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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
	coordinator := agent.New(runtime, time.Second, time.Second)
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

func TestCompositionStatusReturnsDefensiveCopy(t *testing.T) {
	runtime := &fakeRuntime{reconciled: protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{Composition: &expansion.Composition{ExpansionID: "original"}}}}
	coordinator := agent.New(runtime, time.Second, time.Second)
	coordinator.Initialize(context.Background())
	first := coordinator.Status()
	first.CorePackage.Composition.ExpansionID = "changed"
	if got := coordinator.Status().CorePackage.Composition.ExpansionID; got != "original" {
		t.Fatalf("caller changed retained composition: %q", got)
	}
}
