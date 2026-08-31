package agent_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

const cachedDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type callLog struct {
	mu     sync.Mutex
	events []string
}

func (l *callLog) add(event string) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type contentRuntime struct {
	mu                   sync.Mutex
	health               protocol.Health
	reconciled           protocol.Status
	prepareErr           *protocol.APIError
	launchObserved       string
	launchErr            *protocol.APIError
	launchBeforeDispatch bool
	stopObserved         string
	stopErr              *protocol.APIError
	launchGate           chan struct{}
	waitForContext       bool
	prepareCalls         int
	launchCalls          int
	stopCalls            int
	prepareSpec          core.Spec
	preparePath          string
	launched             mister.PreparedLaunch
	log                  *callLog
}

func (f *contentRuntime) Health(string) protocol.Health {
	return f.health
}

func (f *contentRuntime) Reconcile(context.Context) protocol.Status {
	return cloneTestStatus(f.reconciled)
}

func (f *contentRuntime) Prepare(spec core.Spec, path string) (mister.PreparedLaunch, *protocol.APIError) {
	if f.log != nil {
		f.log.add("prepare")
	}
	f.mu.Lock()
	f.prepareCalls++
	f.prepareSpec = spec
	f.preparePath = path
	apiErr := f.prepareErr
	f.mu.Unlock()
	if apiErr != nil {
		return mister.PreparedLaunch{}, apiErr
	}
	return mister.PreparedLaunch{Spec: spec, AbsoluteROM: path, RelativeROM: "cached.sfc", MGL: []byte("cached")}, nil
}

func (f *contentRuntime) Launch(ctx context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	if f.log != nil {
		f.log.add("launch")
	}
	f.mu.Lock()
	f.launchCalls++
	f.launched = prepared
	gate := f.launchGate
	waitForContext := f.waitForContext
	observed, apiErr, beforeDispatch := f.launchObserved, f.launchErr, f.launchBeforeDispatch
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if waitForContext {
		<-ctx.Done()
		return observed, true, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "expected core did not appear before the deadline"}
	}
	return observed, !beforeDispatch, apiErr
}

func (f *contentRuntime) Stop(context.Context) (string, *protocol.APIError) {
	if f.log != nil {
		f.log.add("stop")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopObserved, f.stopErr
}

func (f *contentRuntime) snapshot() (prepareCalls, launchCalls, stopCalls int, spec core.Spec, path string, launched mister.PreparedLaunch) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepareCalls, f.launchCalls, f.stopCalls, f.prepareSpec, f.preparePath, f.launched
}

func (f *contentRuntime) setLaunchObserved(observed string) {
	f.mu.Lock()
	f.launchObserved = observed
	f.mu.Unlock()
}

func (f *contentRuntime) setLaunchError(apiErr *protocol.APIError) {
	f.mu.Lock()
	f.launchErr = apiErr
	f.mu.Unlock()
}

type recordingContentStore struct {
	mu                sync.Mutex
	probeResponse     protocol.CacheProbeResponse
	probeErr          *protocol.APIError
	putResponse       protocol.CacheUploadResponse
	putErr            *protocol.APIError
	resolved          targetcache.Resolved
	resolveErr        *protocol.APIError
	pinErr            *protocol.APIError
	intentErr         *protocol.APIError
	directIntentErr   *protocol.APIError
	abortErr          *protocol.APIError
	directAbortErr    *protocol.APIError
	directCommitErr   *protocol.APIError
	commitErr         *protocol.APIError
	clearErr          *protocol.APIError
	probeCalls        int
	putCalls          int
	resolveCalls      int
	pinCalls          int
	intentCalls       int
	directIntentCalls int
	abortCalls        int
	directAbortCalls  int
	directCommitCalls int
	commitCalls       int
	clearCalls        int
	lastProbeSystem   protocol.System
	lastProbeKey      protocol.ContentKey
	lastPutSystem     protocol.System
	lastPutContent    protocol.ContentIdentity
	lastPutBody       []byte
	lastResolveSystem protocol.System
	lastResolve       protocol.ContentIdentity
	lastPinSystem     protocol.System
	lastPin           protocol.ContentIdentity
	lastIntentSystem  protocol.System
	lastDirectIntent  protocol.System
	lastIntent        protocol.ContentIdentity
	lastAbortSystem   protocol.System
	lastDirectAbort   protocol.System
	lastDirectCommit  protocol.System
	lastAbort         protocol.ContentIdentity
	lastCommitSystem  protocol.System
	lastCommit        protocol.ContentIdentity
	reconciled        []protocol.Status
	reconcileSelected []*targetcache.ActiveRecordEntry
	activeSystem      *protocol.System
	activeSystems     *targetcache.ActiveRecords
	activeSystemErr   *protocol.APIError
	reconcileErr      *protocol.APIError
	pinned            bool
	log               *callLog
	onCommit          func()
	onDirectCommit    func()
	onClear           func()
	onReconcile       func(context.Context, protocol.Status)
}

func (s *recordingContentStore) Probe(_ context.Context, system protocol.System, key protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError) {
	if s.log != nil {
		s.log.add("probe")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probeCalls++
	s.lastProbeSystem = system
	s.lastProbeKey = key
	return s.probeResponse, s.probeErr
}

func (s *recordingContentStore) Put(_ context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	if s.log != nil {
		s.log.add("put")
	}
	data, _ := io.ReadAll(body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCalls++
	s.lastPutSystem = system
	s.lastPutContent = content
	s.lastPutBody = append([]byte(nil), data...)
	return s.putResponse, s.putErr
}

func (s *recordingContentStore) Resolve(_ context.Context, system protocol.System, content protocol.ContentIdentity) (targetcache.Resolved, *protocol.APIError) {
	if s.log != nil {
		s.log.add("resolve")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveCalls++
	s.lastResolveSystem = system
	s.lastResolve = content
	return s.resolved, s.resolveErr
}

func (s *recordingContentStore) PinForLaunch(system protocol.System, content protocol.ContentIdentity) *protocol.APIError {
	if s.log != nil {
		s.log.add("pin")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pinCalls++
	s.lastPinSystem = system
	s.lastPin = content
	if s.pinErr == nil {
		s.pinned = true
	}
	return s.pinErr
}

func (s *recordingContentStore) RecordLaunchIntent(system protocol.System, content protocol.ContentIdentity) (targetcache.LaunchIntent, *protocol.APIError) {
	if s.log != nil {
		s.log.add("intent")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intentCalls++
	s.lastIntentSystem = system
	s.lastIntent = content
	return targetcache.LaunchIntent{}, s.intentErr
}

func (s *recordingContentStore) RecordDirectLaunchIntent(system protocol.System) (targetcache.LaunchIntent, *protocol.APIError) {
	if s.log != nil {
		s.log.add("direct-intent")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.directIntentCalls++
	s.lastDirectIntent = system
	return targetcache.LaunchIntent{}, s.directIntentErr
}

func (s *recordingContentStore) AbortLaunch(system protocol.System, content protocol.ContentIdentity, _ targetcache.LaunchIntent) *protocol.APIError {
	if s.log != nil {
		s.log.add("abort")
	}
	s.mu.Lock()
	s.abortCalls++
	s.lastAbortSystem = system
	s.lastAbort = content
	s.pinned = false
	s.mu.Unlock()
	return s.abortErr
}

func (s *recordingContentStore) AbortDirectLaunch(system protocol.System, _ targetcache.LaunchIntent) *protocol.APIError {
	if s.log != nil {
		s.log.add("direct-abort")
	}
	s.mu.Lock()
	s.directAbortCalls++
	s.lastDirectAbort = system
	s.mu.Unlock()
	return s.directAbortErr
}

func (s *recordingContentStore) CommitDirectLaunch(system protocol.System, _ targetcache.LaunchIntent) *protocol.APIError {
	if s.log != nil {
		s.log.add("direct-commit")
	}
	s.mu.Lock()
	s.directCommitCalls++
	s.lastDirectCommit = system
	onCommit := s.onDirectCommit
	apiErr := s.directCommitErr
	if apiErr == nil {
		s.pinned = false
	}
	s.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}
	return apiErr
}

func (s *recordingContentStore) CommitLaunch(system protocol.System, content protocol.ContentIdentity) *protocol.APIError {
	if s.log != nil {
		s.log.add("commit")
	}
	s.mu.Lock()
	s.commitCalls++
	s.lastCommitSystem = system
	s.lastCommit = content
	onCommit := s.onCommit
	apiErr := s.commitErr
	s.mu.Unlock()
	if onCommit != nil {
		onCommit()
	}
	return apiErr
}

func (s *recordingContentStore) ClearActive() *protocol.APIError {
	if s.log != nil {
		s.log.add("clear")
	}
	s.mu.Lock()
	s.clearCalls++
	onClear := s.onClear
	apiErr := s.clearErr
	if s.clearErr == nil {
		s.pinned = false
	}
	s.mu.Unlock()
	if onClear != nil {
		onClear()
	}
	return apiErr
}

func (s *recordingContentStore) ReconcileActive(ctx context.Context, status protocol.Status, selected *targetcache.ActiveRecordEntry) *protocol.APIError {
	if s.log != nil {
		s.log.add("reconcile")
	}
	s.mu.Lock()
	s.reconciled = append(s.reconciled, cloneTestStatus(status))
	var copied *targetcache.ActiveRecordEntry
	if selected != nil {
		entry := *selected
		copied = &entry
	}
	s.reconcileSelected = append(s.reconcileSelected, copied)
	onReconcile := s.onReconcile
	apiErr := s.reconcileErr
	s.mu.Unlock()
	if onReconcile != nil {
		onReconcile(ctx, status)
	}
	return apiErr
}

func (s *recordingContentStore) ActiveRecordSystems(ctx context.Context) (targetcache.ActiveRecords, bool, *protocol.APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return targetcache.ActiveRecords{}, false, &protocol.APIError{Code: protocol.CodeInternal, Message: "cache verification was canceled"}
	}
	if s.activeSystemErr != nil {
		return targetcache.ActiveRecords{}, false, s.activeSystemErr
	}
	if s.activeSystems != nil {
		return *s.activeSystems, true, nil
	}
	if s.activeSystem == nil {
		return targetcache.ActiveRecords{}, false, nil
	}
	return targetcache.ActiveRecords{Candidate: targetcache.ActiveRecordEntry{System: *s.activeSystem}}, true, nil
}

type storeSnapshot struct {
	probeCalls, putCalls, resolveCalls, pinCalls, intentCalls, abortCalls, commitCalls, clearCalls int
	directIntentCalls, directAbortCalls, directCommitCalls                                         int
	probeSystem, putSystem, resolveSystem                                                          protocol.System
	pinSystem, intentSystem, abortSystem, commitSystem                                             protocol.System
	directIntentSystem, directAbortSystem, directCommitSystem                                      protocol.System
	probeKey                                                                                       protocol.ContentKey
	putContent, resolvedContent                                                                    protocol.ContentIdentity
	pinContent, intentContent, abortContent, commitContent                                         protocol.ContentIdentity
	putBody                                                                                        []byte
	reconciled                                                                                     []protocol.Status
	reconcileSelected                                                                              []*targetcache.ActiveRecordEntry
	pinned                                                                                         bool
}

func (s *recordingContentStore) snapshot() storeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	reconciled := make([]protocol.Status, len(s.reconciled))
	for i := range s.reconciled {
		reconciled[i] = cloneTestStatus(s.reconciled[i])
	}
	reconcileSelected := make([]*targetcache.ActiveRecordEntry, len(s.reconcileSelected))
	for i, selected := range s.reconcileSelected {
		if selected != nil {
			entry := *selected
			reconcileSelected[i] = &entry
		}
	}
	return storeSnapshot{
		probeCalls: s.probeCalls, putCalls: s.putCalls, resolveCalls: s.resolveCalls,
		pinCalls: s.pinCalls, intentCalls: s.intentCalls, abortCalls: s.abortCalls, commitCalls: s.commitCalls, clearCalls: s.clearCalls,
		directIntentCalls: s.directIntentCalls, directAbortCalls: s.directAbortCalls, directCommitCalls: s.directCommitCalls,
		probeSystem: s.lastProbeSystem, putSystem: s.lastPutSystem, resolveSystem: s.lastResolveSystem,
		pinSystem: s.lastPinSystem, intentSystem: s.lastIntentSystem, abortSystem: s.lastAbortSystem, commitSystem: s.lastCommitSystem,
		directIntentSystem: s.lastDirectIntent, directAbortSystem: s.lastDirectAbort, directCommitSystem: s.lastDirectCommit,
		probeKey: s.lastProbeKey, putContent: s.lastPutContent, resolvedContent: s.lastResolve,
		pinContent: s.lastPin, intentContent: s.lastIntent, abortContent: s.lastAbort, commitContent: s.lastCommit,
		putBody: append([]byte(nil), s.lastPutBody...), reconciled: reconciled, reconcileSelected: reconcileSelected, pinned: s.pinned,
	}
}

func TestContentControllerProbeAndPutValidateThenDelegateExactValues(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	system := protocol.SystemSNES
	store := &recordingContentStore{
		probeResponse: protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity},
		putResponse:   protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: system, Content: identity},
	}
	coordinator := agent.New(&contentRuntime{}, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)

	probe, apiErr := controller.ProbeContent(context.Background(), system, identity.Key())
	if apiErr != nil || !reflect.DeepEqual(probe, store.probeResponse) {
		t.Fatalf("ProbeContent = %#v, %#v", probe, apiErr)
	}
	upload, apiErr := controller.PutContent(context.Background(), system, identity, strings.NewReader("data"))
	if apiErr != nil || !reflect.DeepEqual(upload, store.putResponse) {
		t.Fatalf("PutContent = %#v, %#v", upload, apiErr)
	}
	snapshot := store.snapshot()
	if snapshot.probeCalls != 1 || snapshot.probeSystem != system || snapshot.probeKey != identity.Key() {
		t.Fatalf("probe delegation = %#v", snapshot)
	}
	if snapshot.putCalls != 1 || snapshot.putSystem != system || snapshot.putContent != identity || string(snapshot.putBody) != "data" {
		t.Fatalf("put delegation = %#v", snapshot)
	}
}

func TestContentControllerRejectsInvalidOperationsBeforeCacheAccess(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	tests := []struct {
		name string
		call func(*agent.ContentController) *protocol.APIError
		code protocol.ErrorCode
	}{
		{name: "probe system", call: func(c *agent.ContentController) *protocol.APIError {
			_, apiErr := c.ProbeContent(context.Background(), "mystery", identity.Key())
			return apiErr
		}, code: protocol.CodeUnsupportedSystem},
		{name: "probe key", call: func(c *agent.ContentController) *protocol.APIError {
			_, apiErr := c.ProbeContent(context.Background(), protocol.SystemSNES, protocol.ContentKey{SHA256: "private/bad", Extension: "sfc"})
			return apiErr
		}, code: protocol.CodeBadRequest},
		{name: "put system", call: func(c *agent.ContentController) *protocol.APIError {
			_, apiErr := c.PutContent(context.Background(), "mystery", identity, strings.NewReader("data"))
			return apiErr
		}, code: protocol.CodeUnsupportedSystem},
		{name: "put identity", call: func(c *agent.ContentController) *protocol.APIError {
			_, apiErr := c.PutContent(context.Background(), protocol.SystemSNES, protocol.ContentIdentity{SHA256: cachedDigest, Size: 0, Extension: "sfc"}, strings.NewReader("data"))
			return apiErr
		}, code: protocol.CodeBadRequest},
		{name: "put body", call: func(c *agent.ContentController) *protocol.APIError {
			_, apiErr := c.PutContent(context.Background(), protocol.SystemSNES, identity, nil)
			return apiErr
		}, code: protocol.CodeBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingContentStore{}
			coordinator := agent.New(&contentRuntime{}, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)
			apiErr := test.call(controller)
			if apiErr == nil || apiErr.Code != test.code {
				t.Fatalf("error = %#v", apiErr)
			}
			if strings.Contains(apiErr.Message, "private/bad") {
				t.Fatalf("error exposed rejected input: %#v", apiErr)
			}
			if snapshot := store.snapshot(); snapshot.probeCalls != 0 || snapshot.putCalls != 0 || snapshot.resolveCalls != 0 {
				t.Fatalf("invalid operation reached cache: %#v", snapshot)
			}
		})
	}
}

func TestCachedLaunchValidatesCompleteRequestBeforeCacheAccess(t *testing.T) {
	valid := protocol.CachedLaunchRequest{
		GameID: "snes-cached-test", System: protocol.SystemSNES,
		Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"},
	}
	tests := []struct {
		name   string
		mutate func(*protocol.CachedLaunchRequest)
		code   protocol.ErrorCode
	}{
		{name: "game", mutate: func(request *protocol.CachedLaunchRequest) { request.GameID = "Private Game" }, code: protocol.CodeBadRequest},
		{name: "system", mutate: func(request *protocol.CachedLaunchRequest) { request.System = "mystery" }, code: protocol.CodeUnsupportedSystem},
		{name: "content", mutate: func(request *protocol.CachedLaunchRequest) { request.Content.Size = 0 }, code: protocol.CodeBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			store := &recordingContentStore{}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)
			before := coordinator.Status()
			_, apiErr := controller.LaunchContent(context.Background(), request)
			if apiErr == nil || apiErr.Code != test.code {
				t.Fatalf("error = %#v", apiErr)
			}
			if !reflect.DeepEqual(coordinator.Status(), before) {
				t.Fatalf("invalid request changed status: before=%#v after=%#v", before, coordinator.Status())
			}
			if snapshot := store.snapshot(); snapshot.resolveCalls != 0 || snapshot.pinCalls != 0 {
				t.Fatalf("invalid request reached cache: %#v", snapshot)
			}
		})
	}
}

func TestCachedLaunchRejectsProtocolUnsupportedSystemBeforeCustomRegistryCacheAccess(t *testing.T) {
	store := &recordingContentStore{}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "NES"}
	registry := core.NewRegistry(core.Spec{
		System: "mystery", ExpectedCore: "NES", RBFSelector: "_Console/NES", ROMRoot: "/media/fat/games/NES",
		Extensions: map[string]struct{}{".nes": {}}, FileDelay: 1, FileType: "f", FileIndex: 0,
	})
	coordinator := agent.New(runtime, registry, time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)

	_, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{
		GameID: "nes-cached-test", System: "mystery",
		Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "nes"},
	})
	if apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
		t.Fatalf("error = %#v", apiErr)
	}
	if snapshot := store.snapshot(); snapshot.resolveCalls != 0 || snapshot.pinCalls != 0 || snapshot.commitCalls != 0 {
		t.Fatalf("unsupported protocol system reached cache: %#v", snapshot)
	}
	prepareCalls, launchCalls, _, _, _, _ := runtime.snapshot()
	if prepareCalls != 0 || launchCalls != 0 || coordinator.Status().State != protocol.StateIdle {
		t.Fatalf("unsupported protocol system reached runtime: prepare=%d launch=%d status=%#v", prepareCalls, launchCalls, coordinator.Status())
	}
}

func TestCachedLaunchUsesResolvedRootAndPathThenCommitsObservedCore(t *testing.T) {
	log := &callLog{}
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	request := protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: identity}
	store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/" + cachedDigest + ".sfc"}, log: log}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES", log: log}
	registry := core.DefaultRegistry()
	coordinator := agent.New(runtime, registry, time.Second, time.Second)
	store.onCommit = func() {
		status := coordinator.Status()
		if status.State != protocol.StateActive || status.ObservedCore == nil || *status.ObservedCore != "SNES" {
			t.Errorf("status at commit = %#v", status)
		}
	}
	controller := agent.NewContentController(coordinator, store)

	response, apiErr := controller.LaunchContent(context.Background(), request)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	wantJSON := `{"status":{"state":"active","game_id":"snes-cached-test","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null},"content":{"sha256":"` + cachedDigest + `","size":4,"extension":"sfc"}}`
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != wantJSON {
		t.Fatalf("response JSON = %s, want %s", encoded, wantJSON)
	}
	prepareCalls, launchCalls, _, spec, path, launched := runtime.snapshot()
	if prepareCalls != 1 || launchCalls != 1 || path != store.resolved.Path || launched.AbsoluteROM != store.resolved.Path {
		t.Fatalf("runtime launch = prepare %d launch %d spec %#v path %q prepared %#v", prepareCalls, launchCalls, spec, path, launched)
	}
	if spec.ROMRoot != store.resolved.Root || spec.System != protocol.SystemSNES || spec.ExpectedCore != "SNES" || spec.RBFSelector != "_Console/SNES" || spec.FileDelay != 2 || spec.FileType != "f" || spec.FileIndex != 0 {
		t.Fatalf("prepared spec = %#v", spec)
	}
	if _, ok := spec.Extensions[".sfc"]; !ok || len(spec.Extensions) != 3 {
		t.Fatalf("prepared extensions = %#v", spec.Extensions)
	}
	original, _ := registry.Lookup(protocol.SystemSNES)
	if original.ROMRoot != "/media/fat/games/SNES" {
		t.Fatalf("cached launch mutated registry root: %q", original.ROMRoot)
	}
	if events := log.snapshot(); !reflect.DeepEqual(events, []string{"resolve", "pin", "prepare", "intent", "launch", "commit"}) {
		t.Fatalf("event order = %#v", events)
	}
	if snapshot := store.snapshot(); snapshot.resolveSystem != request.System || snapshot.resolvedContent != identity || snapshot.pinSystem != request.System || snapshot.pinContent != identity || snapshot.commitSystem != request.System || snapshot.commitContent != identity || !snapshot.pinned {
		t.Fatalf("cache calls = %#v", snapshot)
	}
}

func TestCachedLaunchMissPreservesCurrentGameWithoutRuntimeCalls(t *testing.T) {
	reconciled := protocol.Status{State: protocol.StateActive, System: testSystemPtr(protocol.SystemMegaDrive), ExpectedCore: testStringPtr("MegaDrive"), ObservedCore: testStringPtr("MegaDrive")}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, reconciled: reconciled}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	store := &recordingContentStore{resolveErr: &protocol.APIError{Code: protocol.CodeContentNotCached, Message: "requested content is not present in the verified cache"}}
	controller := agent.NewContentController(coordinator, store)
	request := protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}}

	response, apiErr := controller.LaunchContent(context.Background(), request)
	if apiErr == nil || apiErr.Code != protocol.CodeContentNotCached {
		t.Fatalf("error = %#v", apiErr)
	}
	if !reflect.DeepEqual(response.Status, reconciled) || !reflect.DeepEqual(coordinator.Status(), reconciled) {
		t.Fatalf("cache miss changed state: response=%#v current=%#v", response.Status, coordinator.Status())
	}
	prepareCalls, launchCalls, _, _, _, _ := runtime.snapshot()
	if prepareCalls != 0 || launchCalls != 0 {
		t.Fatalf("cache miss called runtime: prepare=%d launch=%d", prepareCalls, launchCalls)
	}
	if snapshot := store.snapshot(); snapshot.resolveCalls != 1 || snapshot.pinCalls != 0 || snapshot.abortCalls != 0 || snapshot.commitCalls != 0 {
		t.Fatalf("cache miss calls = %#v", snapshot)
	}
}

func TestCachedLaunchPrepareFailureAbortsButDispatchedFailureRetainsIntent(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	request := protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: identity}
	tests := []struct {
		name           string
		prepareErr     *protocol.APIError
		launchErr      *protocol.APIError
		beforeDispatch bool
		observed       string
		wantState      protocol.State
		wantObserved   *string
		wantEvents     []string
		wantAbort      int
		wantPinned     bool
	}{
		{
			name: "prepare", prepareErr: &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "cached ROM could not be prepared"},
			wantState: protocol.StateIdle, wantEvents: []string{"resolve", "pin", "prepare", "abort"}, wantAbort: 1,
		},
		{
			name: "pre-dispatch launch", launchErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "transient MGL could not be installed"}, beforeDispatch: true,
			wantState: protocol.StateFailed, wantEvents: []string{"resolve", "pin", "prepare", "intent", "launch", "abort"}, wantAbort: 1,
		},
		{
			name: "launch", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "expected core did not appear before the deadline"}, observed: "MENU",
			wantState: protocol.StateFailed, wantObserved: testStringPtr("MENU"), wantEvents: []string{"resolve", "pin", "prepare", "intent", "launch"}, wantPinned: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &callLog{}
			store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached.sfc"}, log: log}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}, prepareErr: test.prepareErr, launchErr: test.launchErr, launchObserved: test.observed, launchBeforeDispatch: test.beforeDispatch, log: log}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)

			response, apiErr := controller.LaunchContent(context.Background(), request)
			if apiErr == nil {
				t.Fatal("LaunchContent returned no error")
			}
			if response.Status.State != test.wantState || !equalOptionalString(response.Status.ObservedCore, test.wantObserved) {
				t.Fatalf("status = %#v", response.Status)
			}
			if events := log.snapshot(); !reflect.DeepEqual(events, test.wantEvents) {
				t.Fatalf("events = %#v", events)
			}
			if snapshot := store.snapshot(); snapshot.pinCalls != 1 || snapshot.pinSystem != request.System || snapshot.pinContent != identity || snapshot.abortCalls != test.wantAbort || snapshot.commitCalls != 0 || snapshot.pinned != test.wantPinned {
				t.Fatalf("pin lifecycle = %#v", snapshot)
			}
		})
	}
}

func TestCachedLaunchPreservesPrimaryErrorWhenAbortFails(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	request := protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: identity}
	cleanupErr := &protocol.APIError{Code: protocol.CodeInternal, Message: "cache cleanup failed"}
	tests := []struct {
		name       string
		prepareErr *protocol.APIError
		intentErr  *protocol.APIError
	}{
		{name: "prepare", prepareErr: &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "cached ROM could not be prepared"}},
		{name: "intent", intentErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "launch intent could not be recorded"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached.sfc"}, intentErr: test.intentErr, abortErr: cleanupErr}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}, prepareErr: test.prepareErr}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)

			_, apiErr := controller.LaunchContent(context.Background(), request)
			want := test.prepareErr
			if want == nil {
				want = test.intentErr
			}
			if apiErr == nil || apiErr.Code != want.Code || apiErr.Message != want.Message {
				t.Fatalf("LaunchContent error = %#v; want primary %#v", apiErr, want)
			}
			if snapshot := store.snapshot(); snapshot.abortCalls != 1 {
				t.Fatalf("cleanup calls = %#v; want one abort", snapshot)
			}
		})
	}
}

func TestCachedLaunchRetriesRetainedIntentAndCommits(t *testing.T) {
	content := []byte("cached retry content")
	digest := sha256.Sum256(content)
	identity := protocol.ContentIdentity{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content)), Extension: "sfc"}
	manager, err := targetcache.Open(targetcache.Config{
		Root: t.TempDir(), ActiveRecord: filepath.Join(t.TempDir(), "active.json"), MaxBytes: 64 << 20,
	}, core.DefaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if _, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, bytes.NewReader(content)); apiErr != nil {
		t.Fatalf("seed cache: %v", apiErr)
	}
	runtime := &contentRuntime{
		health: protocol.Health{Ready: true}, launchObserved: "MENU",
		launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "expected core did not appear before the deadline"},
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, manager)
	request := protocol.CachedLaunchRequest{GameID: "snes-retry-test", System: protocol.SystemSNES, Content: identity}
	if _, apiErr := controller.LaunchContent(context.Background(), request); apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout {
		t.Fatalf("first LaunchContent error = %#v; want retained launch failure", apiErr)
	}
	runtime.setLaunchObserved("SNES")
	runtime.setLaunchError(nil)
	response, apiErr := controller.LaunchContent(context.Background(), request)
	if apiErr != nil {
		t.Fatalf("retry LaunchContent: %v", apiErr)
	}
	if response.Status.State != protocol.StateActive || response.Status.ObservedCore == nil || *response.Status.ObservedCore != "SNES" {
		t.Fatalf("retry status = %#v", response.Status)
	}
	systems, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	if apiErr != nil || !ok || systems.Interrupted || systems.Candidate.System != protocol.SystemSNES {
		t.Fatalf("active systems after retry = %#v, %v, %v; want committed SNES", systems, ok, apiErr)
	}
	prepareCalls, launchCalls, _, _, _, _ := runtime.snapshot()
	if prepareCalls != 2 || launchCalls != 2 {
		t.Fatalf("runtime calls = prepare %d launch %d; want two attempts", prepareCalls, launchCalls)
	}
}

func TestCachedLaunchCancellationRetainsDispatchedIntent(t *testing.T) {
	log := &callLog{}
	store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached.sfc"}, log: log}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "MENU", waitForContext: true, log: log}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := controller.LaunchContent(ctx, protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}})
		done <- apiErr
	}()
	waitForState(t, coordinator, protocol.StateLaunching)
	cancel()
	apiErr := <-done
	if apiErr == nil || apiErr.Code != protocol.CodeCoreTimeout {
		t.Fatalf("canceled launch error = %#v", apiErr)
	}
	if events := log.snapshot(); !reflect.DeepEqual(events, []string{"resolve", "pin", "prepare", "intent", "launch"}) {
		t.Fatalf("events = %#v", events)
	}
	if snapshot := store.snapshot(); !snapshot.pinned || snapshot.abortCalls != 0 || snapshot.intentCalls != 1 {
		t.Fatalf("dispatched intent was not retained: %#v", snapshot)
	}
}

func TestCachedLaunchCommitFailureReturnsActiveStatusAndRetainsPin(t *testing.T) {
	log := &callLog{}
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	store := &recordingContentStore{
		resolved:  targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached.sfc"},
		commitErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "active cache record cannot be committed"},
		log:       log,
	}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES", log: log}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)

	response, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: identity})
	if apiErr == nil || apiErr.Code != protocol.CodeInternal || apiErr.Message != "active cache record cannot be committed" {
		t.Fatalf("commit error = %#v", apiErr)
	}
	if response.Status.State != protocol.StateActive || coordinator.Status().State != protocol.StateActive {
		t.Fatalf("active status was lost: response=%#v current=%#v", response.Status, coordinator.Status())
	}
	if events := log.snapshot(); !reflect.DeepEqual(events, []string{"resolve", "pin", "prepare", "intent", "launch", "commit"}) {
		t.Fatalf("events = %#v", events)
	}
	if snapshot := store.snapshot(); !snapshot.pinned || snapshot.abortCalls != 0 || snapshot.commitCalls != 1 {
		t.Fatalf("commit failure dropped safety pin: %#v", snapshot)
	}
}

func TestCachedLaunchAndStopUseOneTransitionLock(t *testing.T) {
	gate := make(chan struct{})
	store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached.sfc"}}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES", launchGate: gate, stopObserved: "MENU"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{GameID: "snes-cached-test", System: protocol.SystemSNES, Content: protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}})
		done <- apiErr
	}()
	waitForState(t, coordinator, protocol.StateLaunching)

	status, apiErr := coordinator.Stop(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeBusy || status.State != protocol.StateLaunching {
		t.Fatalf("concurrent stop = %#v, %#v", status, apiErr)
	}
	if snapshot := store.snapshot(); snapshot.clearCalls != 0 {
		t.Fatalf("busy stop cleared active content: %#v", snapshot)
	}
	close(gate)
	if apiErr := <-done; apiErr != nil {
		t.Fatal(apiErr)
	}
}

func TestStopClearsActiveContentOnlyAfterSuccessfulMenuObservation(t *testing.T) {
	reconciled := protocol.Status{State: protocol.StateActive, System: testSystemPtr(protocol.SystemSNES), ExpectedCore: testStringPtr("SNES"), ObservedCore: testStringPtr("SNES")}
	tests := []struct {
		name       string
		stopErr    *protocol.APIError
		clearErr   *protocol.APIError
		wantState  protocol.State
		wantEvents []string
		wantPinned bool
	}{
		{name: "success", wantState: protocol.StateIdle, wantEvents: []string{"stop", "clear"}},
		{name: "stop failure", stopErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "Menu did not appear"}, wantState: protocol.StateFailed, wantEvents: []string{"stop"}, wantPinned: true},
		{name: "clear failure", clearErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "active cache record cannot be cleared"}, wantState: protocol.StateFailed, wantEvents: []string{"stop", "clear"}, wantPinned: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &callLog{}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}, reconciled: reconciled, stopObserved: "MENU", stopErr: test.stopErr, log: log}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			coordinator.Initialize(context.Background())
			store := &recordingContentStore{clearErr: test.clearErr, pinned: true, log: log}
			agent.NewContentController(coordinator, store)

			status, apiErr := coordinator.Stop(context.Background())
			if status.State != test.wantState {
				t.Fatalf("status = %#v", status)
			}
			if (test.stopErr != nil || test.clearErr != nil) != (apiErr != nil) {
				t.Fatalf("error = %#v", apiErr)
			}
			if events := log.snapshot(); !reflect.DeepEqual(events, test.wantEvents) {
				t.Fatalf("events = %#v", events)
			}
			if snapshot := store.snapshot(); snapshot.pinned != test.wantPinned {
				t.Fatalf("pin state = %#v", snapshot)
			}
		})
	}
}

func TestInitializeFinalizesContentBeforePublishingReconciledState(t *testing.T) {
	statuses := []protocol.Status{
		{State: protocol.StateActive, System: testSystemPtr(protocol.SystemSNES), ExpectedCore: testStringPtr("SNES"), ObservedCore: testStringPtr("SNES")},
		{State: protocol.StateIdle},
		{State: protocol.StateFailed, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "not ready"}},
		{State: protocol.StateFailed, ObservedCore: testStringPtr("Unknown"), LastError: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "observed core is not registered"}},
		{State: protocol.StateActive, System: testSystemPtr(protocol.SystemMegaDrive), ExpectedCore: testStringPtr("MegaDrive"), ObservedCore: testStringPtr("MegaDrive")},
	}
	for _, reconciled := range statuses {
		t.Run(string(reconciled.State)+optionalSystem(reconciled.System)+optionalObserved(reconciled.ObservedCore), func(t *testing.T) {
			runtime := &contentRuntime{reconciled: reconciled}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			store := &recordingContentStore{}
			store.onReconcile = func(_ context.Context, received protocol.Status) {
				if current := coordinator.Status(); !reflect.DeepEqual(current, protocol.Status{State: protocol.StateIdle}) {
					t.Errorf("status was published before durable reconciliation: current=%#v received=%#v", current, received)
				}
			}
			agent.NewContentController(coordinator, store)

			coordinator.Initialize(context.Background())
			if current := coordinator.Status(); !reflect.DeepEqual(current, reconciled) {
				t.Fatalf("status = %#v, want %#v", current, reconciled)
			}
			snapshot := store.snapshot()
			if len(snapshot.reconciled) != 1 || !reflect.DeepEqual(snapshot.reconciled[0], reconciled) {
				t.Fatalf("reconciled = %#v", snapshot.reconciled)
			}
		})
	}
}

func TestV1LaunchRemainsPathBasedAndStagesDirectIntentWithoutContentPin(t *testing.T) {
	store := &recordingContentStore{}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES"}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	agent.NewContentController(coordinator, store)
	path := "/media/fat/games/SNES/original.sfc"

	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-original", System: protocol.SystemSNES, ROMPath: path})
	if apiErr != nil || status.State != protocol.StateActive {
		t.Fatalf("v1 launch = %#v, %#v", status, apiErr)
	}
	_, _, _, spec, preparedPath, launched := runtime.snapshot()
	if preparedPath != path || launched.AbsoluteROM != path || spec.ROMRoot != "/media/fat/games/SNES" {
		t.Fatalf("v1 runtime values = spec %#v path %q launched %#v", spec, preparedPath, launched)
	}
	if snapshot := store.snapshot(); snapshot.resolveCalls != 0 || snapshot.pinCalls != 0 || snapshot.abortCalls != 0 || snapshot.commitCalls != 0 || snapshot.directIntentCalls != 1 || snapshot.directAbortCalls != 0 || snapshot.directCommitCalls != 1 || snapshot.clearCalls != 0 || snapshot.directIntentSystem != protocol.SystemSNES || snapshot.directCommitSystem != protocol.SystemSNES {
		t.Fatalf("v1 launch touched content launch lifecycle: %#v", snapshot)
	}
}

func TestV1LaunchDirectIntentLifecycleTracksDispatchBoundary(t *testing.T) {
	tests := []struct {
		name             string
		intentErr        *protocol.APIError
		launchErr        *protocol.APIError
		beforeDispatch   bool
		wantEvents       []string
		wantLaunchCalls  int
		wantDirectAbort  int
		wantDirectCommit int
	}{
		{
			name:       "staging failure prevents dispatch",
			intentErr:  &protocol.APIError{Code: protocol.CodeInternal, Message: "direct intent cannot be recorded"},
			wantEvents: []string{"prepare", "direct-intent"},
		},
		{
			name:      "pre-dispatch failure restores prior record",
			launchErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "dispatch unavailable"}, beforeDispatch: true,
			wantEvents: []string{"prepare", "direct-intent", "launch", "direct-abort"}, wantLaunchCalls: 1, wantDirectAbort: 1,
		},
		{
			name:       "post-dispatch failure preserves marker",
			launchErr:  &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "core timeout"},
			wantEvents: []string{"prepare", "direct-intent", "launch"}, wantLaunchCalls: 1,
		},
		{
			name:       "success commits durable direct identity after dispatch",
			wantEvents: []string{"prepare", "direct-intent", "launch", "direct-commit"}, wantLaunchCalls: 1, wantDirectCommit: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &callLog{}
			store := &recordingContentStore{directIntentErr: test.intentErr, log: log}
			runtime := &contentRuntime{
				health: protocol.Health{Ready: true}, launchObserved: "SMS", launchErr: test.launchErr,
				launchBeforeDispatch: test.beforeDispatch, log: log,
			}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			agent.NewContentController(coordinator, store)

			_, _ = coordinator.Launch(context.Background(), protocol.LaunchRequest{
				GameID: "gg-direct", System: protocol.SystemGameGear, ROMPath: "/media/fat/games/GameGear/direct.gg",
			})

			_, launchCalls, _, _, _, _ := runtime.snapshot()
			if launchCalls != test.wantLaunchCalls {
				t.Fatalf("runtime launch calls = %d, want %d", launchCalls, test.wantLaunchCalls)
			}
			snapshot := store.snapshot()
			if snapshot.directIntentCalls != 1 || snapshot.directAbortCalls != test.wantDirectAbort || snapshot.directCommitCalls != test.wantDirectCommit || snapshot.clearCalls != 0 {
				t.Fatalf("direct lifecycle = %#v", snapshot)
			}
			if events := log.snapshot(); !reflect.DeepEqual(events, test.wantEvents) {
				t.Fatalf("events = %#v, want %#v", events, test.wantEvents)
			}
		})
	}
}

func TestSuccessfulV1LaunchClearsPriorCachedPinAfterActivation(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	tests := []struct {
		name        string
		request     protocol.LaunchRequest
		observed    string
		wantROMRoot string
	}{
		{
			name: "same system",
			request: protocol.LaunchRequest{
				GameID: "snes-path-b", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/path-b.sfc",
			},
			observed: "SNES", wantROMRoot: "/media/fat/games/SNES",
		},
		{
			name: "different system",
			request: protocol.LaunchRequest{
				GameID: "megadrive-path-b", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/path-b.md",
			},
			observed: "MegaDrive", wantROMRoot: "/media/fat/games/MegaDrive",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &callLog{}
			store := &recordingContentStore{
				resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached-a.sfc"}, log: log,
			}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES", log: log}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)
			store.onDirectCommit = func() {
				current := coordinator.Status()
				if current.State != protocol.StateActive || current.GameID == nil || *current.GameID != test.request.GameID || current.System == nil || *current.System != test.request.System {
					t.Errorf("status during direct commit = %#v", current)
				}
				if _, busyErr := coordinator.Stop(context.Background()); busyErr == nil || busyErr.Code != protocol.CodeBusy {
					t.Errorf("transition token was released before direct commit: %#v", busyErr)
				}
			}
			if _, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{
				GameID: "snes-cached-a", System: protocol.SystemSNES, Content: identity,
			}); apiErr != nil {
				t.Fatalf("cached launch A: %v", apiErr)
			}
			runtime.setLaunchObserved(test.observed)

			status, apiErr := coordinator.Launch(context.Background(), test.request)
			if apiErr != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != test.request.GameID || status.System == nil || *status.System != test.request.System {
				t.Fatalf("v1 launch B = %#v, %#v", status, apiErr)
			}
			_, _, _, spec, path, _ := runtime.snapshot()
			if spec.ROMRoot != test.wantROMRoot || path != test.request.ROMPath {
				t.Fatalf("v1 launch inputs = spec %#v path %q", spec, path)
			}
			snapshot := store.snapshot()
			if snapshot.directCommitCalls != 1 || snapshot.directCommitSystem != test.request.System || snapshot.clearCalls != 0 || snapshot.pinned {
				t.Fatalf("v1 launch did not clear cached A after activation: %#v", snapshot)
			}
			wantEvents := []string{"resolve", "pin", "prepare", "intent", "launch", "commit", "prepare", "direct-intent", "launch", "direct-commit"}
			if events := log.snapshot(); !reflect.DeepEqual(events, wantEvents) {
				t.Fatalf("events = %#v, want %#v", events, wantEvents)
			}
		})
	}
}

func TestFailedV1LaunchRetainsPriorCachedPinWithoutClearing(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	tests := []struct {
		name       string
		prepareErr *protocol.APIError
		launchErr  *protocol.APIError
		observed   string
		wantState  protocol.State
	}{
		{
			name: "prepare failure", prepareErr: &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "invalid v1 path"},
			wantState: protocol.StateActive,
		},
		{
			name: "launch failure", launchErr: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "v1 core timeout"},
			observed: "SNES", wantState: protocol.StateFailed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingContentStore{resolved: targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached-a.sfc"}}
			runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES"}
			coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
			controller := agent.NewContentController(coordinator, store)
			if _, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{
				GameID: "snes-cached-a", System: protocol.SystemSNES, Content: identity,
			}); apiErr != nil {
				t.Fatalf("cached launch A: %v", apiErr)
			}
			runtime.mu.Lock()
			runtime.prepareErr = test.prepareErr
			runtime.launchErr = test.launchErr
			runtime.launchObserved = test.observed
			runtime.mu.Unlock()

			status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
				GameID: "snes-path-b", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/path-b.sfc",
			})
			if apiErr == nil || status.State != test.wantState {
				t.Fatalf("failed v1 launch = %#v, %#v", status, apiErr)
			}
			if snapshot := store.snapshot(); snapshot.clearCalls != 0 || !snapshot.pinned {
				t.Fatalf("failed v1 launch changed cached A pin: %#v", snapshot)
			}
		})
	}
}

func TestV1LaunchDirectCommitFailureReturnsActiveStatusAndRetainsCachedPin(t *testing.T) {
	private := "/private/target/cache/active-record"
	identity := protocol.ContentIdentity{SHA256: cachedDigest, Size: 4, Extension: "sfc"}
	log := &callLog{}
	store := &recordingContentStore{
		resolved:        targetcache.Resolved{Root: "/target/cache", Path: "/target/cache/snes/cached-a.sfc"},
		directCommitErr: &protocol.APIError{Code: protocol.CodeInternal, Message: private},
		log:             log,
	}
	runtime := &contentRuntime{health: protocol.Health{Ready: true}, launchObserved: "SNES", log: log}
	coordinator := agent.New(runtime, core.DefaultRegistry(), time.Second, time.Second)
	controller := agent.NewContentController(coordinator, store)
	if _, apiErr := controller.LaunchContent(context.Background(), protocol.CachedLaunchRequest{
		GameID: "snes-cached-a", System: protocol.SystemSNES, Content: identity,
	}); apiErr != nil {
		t.Fatalf("cached launch A: %v", apiErr)
	}

	status, apiErr := coordinator.Launch(context.Background(), protocol.LaunchRequest{
		GameID: "snes-path-b", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/path-b.sfc",
	})
	if apiErr == nil || apiErr.Code != protocol.CodeInternal || apiErr.Message != "direct active record cannot be committed" || strings.Contains(apiErr.Error(), private) {
		t.Fatalf("v1 direct commit error = %#v", apiErr)
	}
	if status.State != protocol.StateActive || status.GameID == nil || *status.GameID != "snes-path-b" || status.LastError != nil || !reflect.DeepEqual(status, coordinator.Status()) {
		t.Fatalf("active v1 status was lost: returned=%#v current=%#v", status, coordinator.Status())
	}
	if snapshot := store.snapshot(); snapshot.directCommitCalls != 1 || snapshot.clearCalls != 0 || !snapshot.pinned {
		t.Fatalf("direct commit failure dropped cached A pin: %#v", snapshot)
	}
	if events := log.snapshot(); !reflect.DeepEqual(events, []string{"resolve", "pin", "prepare", "intent", "launch", "commit", "prepare", "direct-intent", "launch", "direct-commit"}) {
		t.Fatalf("events = %#v", events)
	}
}

func waitForState(t *testing.T, coordinator *agent.Coordinator, state protocol.State) {
	t.Helper()
	deadline := time.After(time.Second)
	for coordinator.Status().State != state {
		select {
		case <-deadline:
			t.Fatalf("coordinator never entered %s: %#v", state, coordinator.Status())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func cloneTestStatus(status protocol.Status) protocol.Status {
	copy := status
	if status.GameID != nil {
		copy.GameID = testStringPtr(*status.GameID)
	}
	if status.System != nil {
		copy.System = testSystemPtr(*status.System)
	}
	if status.ExpectedCore != nil {
		copy.ExpectedCore = testStringPtr(*status.ExpectedCore)
	}
	if status.ObservedCore != nil {
		copy.ObservedCore = testStringPtr(*status.ObservedCore)
	}
	if status.LastError != nil {
		lastError := *status.LastError
		copy.LastError = &lastError
	}
	return copy
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func testStringPtr(value string) *string {
	return &value
}

func testSystemPtr(value protocol.System) *protocol.System {
	return &value
}

func optionalSystem(system *protocol.System) string {
	if system == nil {
		return ""
	}
	return "-" + string(*system)
}

func optionalObserved(observed *string) string {
	if observed == nil {
		return ""
	}
	return "-" + *observed
}
