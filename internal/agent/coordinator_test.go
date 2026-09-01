package agent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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

type nativeIdleControl struct {
	statusCalls int
	stopCalls   int
}

func (c *nativeIdleControl) Status(context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (c *nativeIdleControl) Stop(context.Context) (misterruntime.Response, error) {
	c.stopCalls++
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
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
