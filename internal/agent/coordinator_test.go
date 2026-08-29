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

	"github.com/DeanoC/FogCast-POC/internal/agent"
	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"github.com/DeanoC/FogCast-POC/internal/mister"
	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/protocol"
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
	stopGate       chan struct{}
	prepareCalls   int
	launchCalls    int
	stopCalls      int
	prepareSpec    core.Spec
	preparePath    string
	launched       mister.PreparedLaunch
}

type serialNormalGate struct {
	mu      sync.Mutex
	active  bool
	entered chan struct{}
}

type rejectingNormalGate struct{ err error }

type unobservableNormalGate struct{}

func (unobservableNormalGate) Enter(context.Context) (hardwareowner.Unlock, error) {
	return func() error { return nil }, nil
}

type failingUnlockGate struct {
	mu          sync.Mutex
	unlockError error
	unlockCalls int
}

func (g *failingUnlockGate) Enter(context.Context) (hardwareowner.Unlock, error) {
	return func() error {
		g.mu.Lock()
		g.unlockCalls++
		err := g.unlockError
		g.mu.Unlock()
		return err
	}, nil
}

func (g *failingUnlockGate) Observe() error { return nil }

func (g *failingUnlockGate) calls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.unlockCalls
}

type enterOnlyGate struct {
	gate   hardwareowner.NormalGate
	mu     sync.Mutex
	enters int
}

func (g *enterOnlyGate) Enter(ctx context.Context) (hardwareowner.Unlock, error) {
	g.mu.Lock()
	g.enters++
	g.mu.Unlock()
	return g.gate.Enter(ctx)
}

func (g *enterOnlyGate) Observe() error { return nil }

func (g *enterOnlyGate) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.enters
}

type mutableMaintenanceFence struct {
	mu  sync.Mutex
	err error
}

type countingObservedGate struct {
	gate        *hardwareowner.Gate
	mu          sync.Mutex
	enters      int
	unlockCalls int
}

func (g *countingObservedGate) Enter(ctx context.Context) (hardwareowner.Unlock, error) {
	g.mu.Lock()
	g.enters++
	g.mu.Unlock()
	unlock, err := g.gate.Enter(ctx)
	if err != nil || unlock == nil {
		return unlock, err
	}
	return func() error {
		err := unlock()
		g.mu.Lock()
		g.unlockCalls++
		g.mu.Unlock()
		return err
	}, nil
}

func (g *countingObservedGate) Observe() error { return g.gate.Observe() }

func (g *countingObservedGate) counts() (enters, unlocks int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.enters, g.unlockCalls
}

type delayedObserverGate struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (g *delayedObserverGate) Enter(context.Context) (hardwareowner.Unlock, error) {
	return func() error { return nil }, nil
}

func (g *delayedObserverGate) Observe() error {
	g.mu.Lock()
	g.calls++
	call := g.calls
	g.mu.Unlock()
	if call == 1 {
		g.started <- struct{}{}
		<-g.release
		return errors.New("temporary live observation failure")
	}
	return nil
}

func (f *mutableMaintenanceFence) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *mutableMaintenanceFence) Observe() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *mutableMaintenanceFence) set(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (g rejectingNormalGate) Enter(context.Context) (hardwareowner.Unlock, error) {
	return nil, g.err
}

func (g rejectingNormalGate) Observe() error { return nil }

func (g *serialNormalGate) Enter(ctx context.Context) (hardwareowner.Unlock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	g.mu.Lock()
	if !g.active {
		g.active = true
		g.mu.Unlock()
		if g.entered != nil {
			select {
			case g.entered <- struct{}{}:
			case <-ctx.Done():
				g.mu.Lock()
				g.active = false
				g.mu.Unlock()
				return nil, ctx.Err()
			}
		}
		return func() error {
			g.mu.Lock()
			g.active = false
			g.mu.Unlock()
			return nil
		}, nil
	}
	g.mu.Unlock()
	for {
		g.mu.Lock()
		if !g.active {
			g.active = true
			g.mu.Unlock()
			return func() error {
				g.mu.Lock()
				g.active = false
				g.mu.Unlock()
				return nil
			}, nil
		}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (g *serialNormalGate) Observe() error { return nil }

func TestLaunchHoldsNormalAdmissionAcrossDispatchAndTerminalState(t *testing.T) {
	launchGate := make(chan struct{})
	firstEntered := make(chan struct{}, 1)
	gate := &serialNormalGate{entered: firstEntered}
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive", launchGate: launchGate}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)
	launchDone := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
		launchDone <- apiErr
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("launch did not acquire normal admission")
	}
	secondDone := make(chan hardwareowner.Unlock, 1)
	go func() {
		unlock, err := gate.Enter(context.Background())
		if err != nil {
			t.Errorf("development admission: %v", err)
			return
		}
		secondDone <- unlock
	}()
	select {
	case <-secondDone:
		t.Fatal("development transition acquired admission before normal dispatch completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(launchGate)
	if err := <-launchDone; err != nil {
		t.Fatal(err)
	}
	select {
	case unlock := <-secondDone:
		_ = unlock()
	case <-time.After(time.Second):
		t.Fatal("development admission did not proceed after normal terminal launch state")
	}
}

func TestStopHoldsNormalAdmissionThroughContentTeardown(t *testing.T) {
	firstEntered := make(chan struct{}, 1)
	gate := &serialNormalGate{entered: firstEntered}
	stopGate := make(chan struct{})
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, reconciled: protocol.Status{State: protocol.StateActive}, stopObserved: "MENU", stopGate: stopGate}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)
	coordinator.Initialize(context.Background())
	stopDone := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := coordinator.Stop(context.Background())
		stopDone <- apiErr
	}()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("stop did not acquire normal admission")
	}
	secondDone := make(chan hardwareowner.Unlock, 1)
	go func() {
		unlock, err := gate.Enter(context.Background())
		if err != nil {
			t.Errorf("development admission: %v", err)
			return
		}
		secondDone <- unlock
	}()
	select {
	case <-secondDone:
		t.Fatal("development transition acquired admission before normal stop teardown completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(stopGate)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	select {
	case unlock := <-secondDone:
		_ = unlock()
	case <-time.After(time.Second):
		t.Fatal("development admission did not proceed after normal stop teardown")
	}
}

func TestNormalAdmissionFailureProjectsUnavailableFailedStatus(t *testing.T) {
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, rejectingNormalGate{err: errors.New("owner is fenced")})
	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc",
	})
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("admission error = %#v; want unavailable", apiErr)
	}
	if status.State != protocol.StateFailed || status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("projected status = %#v; want failed unavailable", status)
	}
	if current := coordinator.Status(); current.State != protocol.StateFailed || current.LastError == nil || current.LastError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("current status = %#v; want failed unavailable", current)
	}
}

func TestStatusFailsClosedWhenNormalGateCannotBeObserved(t *testing.T) {
	coordinator := agent.New(&fakeRuntime{health: protocol.Health{Ready: true}}, core.DefaultRegistry(), time.Second, time.Second, unobservableNormalGate{})

	status := coordinator.Status()
	assertLiveUnavailableStatus(t, status)
	if health := coordinator.Health("test"); health.Ready {
		t.Fatalf("health without live observer = %#v; want not ready", health)
	}
}

func TestDirectLaunchReleaseFailureProjectsUnavailableWithoutReplacingPrimary(t *testing.T) {
	unlockErr := errors.New("private owner release detail")
	tests := []struct {
		name      string
		launchErr *protocol.APIError
		wantCode  protocol.ErrorCode
	}{
		{name: "after success", wantCode: ""},
		{name: "after primary failure", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "core did not appear"}, wantCode: protocol.CodeCoreTimeout},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			gate := &failingUnlockGate{unlockError: unlockErr}
			runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive", launchErr: test.launchErr}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)

			status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "megadrive-release-test", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"})
			if test.wantCode == "" {
				if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "MiSTer is unavailable" {
					t.Fatalf("release error = %#v; want generic unavailable", apiErr)
				}
			} else if apiErr == nil || apiErr.Code != test.wantCode || apiErr.Message != test.launchErr.Message {
				t.Fatalf("primary error = %#v; want %#v", apiErr, test.launchErr)
			}
			assertLiveUnavailableStatus(t, status)
			assertLiveUnavailableStatus(t, coordinator.Status())
			if gate.calls() != 1 {
				t.Fatalf("unlock calls = %d; want exactly one", gate.calls())
			}
			if strings.Contains(apiErr.Message, "private owner release detail") {
				t.Fatalf("private release error leaked through API: %#v", apiErr)
			}
		})
	}
}

func TestStopReleaseFailureProjectsUnavailableWithoutReplacingPrimary(t *testing.T) {
	unlockErr := errors.New("private owner release detail")
	tests := []struct {
		name     string
		stopErr  *protocol.APIError
		wantCode protocol.ErrorCode
	}{
		{name: "after success", wantCode: ""},
		{name: "after primary failure", stopErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "menu did not appear"}, wantCode: protocol.CodeCoreTimeout},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			gate := &failingUnlockGate{unlockError: unlockErr}
			runtime := &fakeRuntime{health: protocol.Health{Ready: true}, reconciled: protocol.Status{State: protocol.StateActive}, stopObserved: "MENU", stopErr: test.stopErr}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)
			coordinator.Initialize(context.Background())

			status, apiErr := coordinator.Stop(context.Background())
			if test.wantCode == "" {
				if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "MiSTer is unavailable" {
					t.Fatalf("release error = %#v; want generic unavailable", apiErr)
				}
			} else if apiErr == nil || apiErr.Code != test.wantCode || apiErr.Message != test.stopErr.Message {
				t.Fatalf("primary error = %#v; want %#v", apiErr, test.stopErr)
			}
			assertLiveUnavailableStatus(t, status)
			assertLiveUnavailableStatus(t, coordinator.Status())
			if gate.calls() != 1 {
				t.Fatalf("unlock calls = %d; want exactly one", gate.calls())
			}
			if strings.Contains(apiErr.Message, "private owner release detail") {
				t.Fatalf("private release error leaked through API: %#v", apiErr)
			}
		})
	}
}

func TestLiveOwnerObservationProjectsStatusAndHealthWithoutTransition(t *testing.T) {
	store := newLiveOwnerStore(t)
	if err := store.Replace(liveNormalRecord()); err != nil {
		t.Fatal(err)
	}
	fence := &mutableMaintenanceFence{}
	gate := hardwareowner.NewGate(store, hardwareowner.NewLocker(store.Path+".lock", store.ExpectedUID), fence, liveBootID)
	runtime := &fakeRuntime{health: protocol.Health{Ready: true, MiSTerProcess: true, CommandPipe: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)

	if status := coordinator.Status(); status.State != protocol.StateIdle {
		t.Fatalf("initial status = %#v; want cached idle", status)
	}
	if err := store.Replace(liveRecoveringRecord()); err != nil {
		t.Fatal(err)
	}

	status := coordinator.Status()
	assertLiveUnavailableStatus(t, status)
	health := coordinator.Health("test")
	if health.Ready {
		t.Fatalf("health after live owner change = %#v; want not ready", health)
	}
}

func TestLiveMaintenanceObservationProjectsStatusAndHealthWithoutTransition(t *testing.T) {
	store := newLiveOwnerStore(t)
	if err := store.Replace(liveNormalRecord()); err != nil {
		t.Fatal(err)
	}
	fence := &mutableMaintenanceFence{}
	gate := hardwareowner.NewGate(store, hardwareowner.NewLocker(store.Path+".lock", store.ExpectedUID), fence, liveBootID)
	runtime := &fakeRuntime{health: protocol.Health{Ready: true, MiSTerProcess: true, CommandPipe: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)

	if status := coordinator.Status(); status.State != protocol.StateIdle {
		t.Fatalf("initial status = %#v; want cached idle", status)
	}
	fence.set(errors.New("maintenance fence changed after construction"))

	status := coordinator.Status()
	assertLiveUnavailableStatus(t, status)
	health := coordinator.Health("test")
	if health.Ready {
		t.Fatalf("health after live maintenance change = %#v; want not ready", health)
	}
}

func TestLiveStatusObservationDoesNotAcquireOwnerLock(t *testing.T) {
	store := newLiveOwnerStore(t)
	if err := store.Replace(liveNormalRecord()); err != nil {
		t.Fatal(err)
	}
	fence := &mutableMaintenanceFence{}
	locker := hardwareowner.NewLocker(store.Path+".lock", store.ExpectedUID)
	gate := hardwareowner.NewGate(store, locker, fence, liveBootID)
	unlock, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()
	coordinator := agent.New(&fakeRuntime{health: protocol.Health{Ready: true}}, core.DefaultRegistry(), time.Second, time.Second, gate)

	observed := make(chan protocol.Status, 1)
	go func() { observed <- coordinator.Status() }()
	select {
	case status := <-observed:
		if status.State != protocol.StateIdle {
			t.Fatalf("status while transition lock held = %#v; want idle", status)
		}
	case <-time.After(time.Second):
		t.Fatal("live status observation blocked on the transition owner lock")
	}
}

func TestIdleStopConsultsAdmissionForLiveUnsafeStates(t *testing.T) {
	tests := []struct {
		name       string
		record     hardwareowner.Record
		bootID     string
		fenceError error
	}{
		{name: "fenced owner state", record: liveRecoveringRecord(), bootID: liveBootID},
		{name: "wrong boot", record: liveNormalRecord(), bootID: "fedcba98-7654-3210-fedc-ba9876543210"},
		{name: "nonterminal maintenance", record: liveNormalRecord(), bootID: liveBootID, fenceError: errors.New("maintenance journal pending")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			store := newLiveOwnerStore(t)
			if err := store.Replace(test.record); err != nil {
				t.Fatal(err)
			}
			fence := &mutableMaintenanceFence{err: test.fenceError}
			locker := hardwareowner.NewLocker(store.Path+".lock", store.ExpectedUID)
			realGate := hardwareowner.NewGate(store, locker, fence, test.bootID)
			gate := &enterOnlyGate{gate: realGate}
			coordinator := agent.New(&fakeRuntime{health: protocol.Health{Ready: true}}, core.DefaultRegistry(), time.Second, time.Second, gate)

			status, apiErr := coordinator.Stop(context.Background())
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
				t.Fatalf("idle stop error = %#v; want unavailable", apiErr)
			}
			assertLiveUnavailableStatus(t, status)
			if gate.count() != 1 {
				t.Fatalf("admission enters = %d; want one idle-stop admission check", gate.count())
			}
			unlock, err := locker.Lock(context.Background())
			if err != nil {
				t.Fatalf("owner lock remained unavailable after rejected idle stop: %v", err)
			}
			if unlockErr := unlock(); unlockErr != nil {
				t.Fatal(unlockErr)
			}
		})
	}
}

func TestLiveUnavailableOverlayDoesNotPersistAcrossFenceRecovery(t *testing.T) {
	store := newLiveOwnerStore(t)
	if err := store.Replace(liveNormalRecord()); err != nil {
		t.Fatal(err)
	}
	fence := &mutableMaintenanceFence{}
	locker := hardwareowner.NewLocker(store.Path+".lock", store.ExpectedUID)
	realGate := hardwareowner.NewGate(store, locker, fence, liveBootID)
	gate := &countingObservedGate{gate: realGate}
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)

	if status := coordinator.Status(); status.State != protocol.StateIdle {
		t.Fatalf("initial status = %#v; want idle", status)
	}
	fence.set(errors.New("temporary maintenance fence"))
	assertLiveUnavailableStatus(t, coordinator.Status())
	if health := coordinator.Health("test"); health.Ready {
		t.Fatalf("health while fence is set = %#v; want not ready", health)
	}

	fence.set(nil)
	if status := coordinator.Status(); status.State != protocol.StateIdle {
		t.Fatalf("status after fence recovery = %#v; want authoritative idle", status)
	}
	if health := coordinator.Health("test"); !health.Ready {
		t.Fatalf("health after fence recovery = %#v; want ready", health)
	}
	status, apiErr := coordinator.Stop(context.Background())
	if apiErr != nil || status.State != protocol.StateIdle {
		t.Fatalf("idle stop after fence recovery = %#v, %#v; want idle success", status, apiErr)
	}
	if _, _, stopCalls := runtime.counts(); stopCalls != 0 {
		t.Fatalf("runtime stop calls = %d; want zero for authoritative idle", stopCalls)
	}
	if enters, unlocks := gate.counts(); enters != 1 || unlocks != 1 {
		t.Fatalf("admission enters/unlocks = %d/%d; want exactly one each", enters, unlocks)
	}
}

func TestDelayedLiveObservationCannotOverwriteNewerTransition(t *testing.T) {
	gate := &delayedObserverGate{started: make(chan struct{}, 1), release: make(chan struct{})}
	runtime := &fakeRuntime{health: protocol.Health{Ready: true}, launchObserved: "MegaDrive"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second, gate)

	observed := make(chan protocol.Status, 1)
	go func() { observed <- coordinator.Status() }()
	select {
	case <-gate.started:
	case <-time.After(time.Second):
		t.Fatal("live observation did not enter delayed window")
	}

	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "megadrive-delayed-observation", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md",
	})
	if apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("new transition during delayed observation = %#v, %#v; want active success", status, apiErr)
	}
	close(gate.release)
	assertLiveUnavailableStatus(t, <-observed)
	if current := coordinator.Status(); current.State != protocol.StateActive {
		t.Fatalf("authoritative status after delayed overlay = %#v; want newer active transition", current)
	}
}

const liveBootID = "01234567-89ab-cdef-0123-456789abcdef"

func newLiveOwnerStore(t *testing.T) *hardwareowner.Store {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return hardwareowner.NewStore(filepath.Join(root, "hardware-owner-v1.json"), uint32(os.Getuid()))
}

func liveNormalRecord() hardwareowner.Record {
	return hardwareowner.Record{
		Schema:              hardwareowner.SchemaVersion,
		State:               hardwareowner.StateNormalMain,
		BootID:              liveBootID,
		GenerationHighWater: 42,
		ActiveSession:       "11111111111111111111111111111111",
		ActiveGeneration:    42,
		ActiveMode:          hardwareowner.ModeFPGANative,
		CandidateMode:       hardwareowner.ModeNone,
		QuiescingOwner:      hardwareowner.OwnerNone,
		CandidateOwner:      hardwareowner.OwnerNone,
		ActiveOwner:         hardwareowner.OwnerCompatMain,
		ActiveLeases:        hardwareowner.NormalLeases(),
		RequestedResources:  []string{},
	}
}

func liveRecoveringRecord() hardwareowner.Record {
	return hardwareowner.Record{
		Schema:              hardwareowner.SchemaVersion,
		State:               hardwareowner.StateRecoveringIntent,
		Phase:               hardwareowner.PhaseIntentCommitted,
		BootID:              liveBootID,
		RunID:               "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		GenerationHighWater: 43,
		ActiveSession:       "11111111111111111111111111111111",
		ActiveGeneration:    42,
		ActiveMode:          hardwareowner.ModeFPGANative,
		CandidateSession:    "22222222222222222222222222222222",
		CandidateGeneration: 43,
		CandidateMode:       hardwareowner.ModeUpdating,
		QuiescingOwner:      hardwareowner.OwnerCompatMain,
		CandidateOwner:      hardwareowner.OwnerFPGADev,
		ActiveOwner:         hardwareowner.OwnerCompatMain,
		ActiveLeases:        hardwareowner.NormalLeases(),
		RequestedResources:  hardwareowner.DevelopmentLeases(),
	}
}

func assertLiveUnavailableStatus(t *testing.T, status protocol.Status) {
	t.Helper()
	if status.State != protocol.StateFailed || status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable || status.LastError.Message != "MiSTer is unavailable" {
		t.Fatalf("live projected status = %#v; want failed/mister_unavailable", status)
	}
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
	if f.stopGate != nil {
		<-f.stopGate
	}
	return f.stopObserved, f.stopErr
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
