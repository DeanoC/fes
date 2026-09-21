package agent_test

import (
	"context"

	"io"

	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
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
	preparePath          string
	log                  *callLog
}

func (f *contentRuntime) Health(string) protocol.Health {
	return f.health
}

func (f *contentRuntime) Reconcile(context.Context) protocol.Status {
	return cloneTestStatus(f.reconciled)
}

func (f *contentRuntime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}
func (f *contentRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
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
	coordinator := agent.New(&contentRuntime{}, time.Second, time.Second)
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
			coordinator := agent.New(&contentRuntime{}, time.Second, time.Second)
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

func cloneTestStatus(status protocol.Status) protocol.Status {
	copy := status
	if status.GameID != nil {
		copy.GameID = stringPtr(*status.GameID)
	}
	if status.System != nil {
		copy.System = systemPtr(*status.System)
	}
	if status.ExpectedCore != nil {
		copy.ExpectedCore = stringPtr(*status.ExpectedCore)
	}
	if status.ObservedCore != nil {
		copy.ObservedCore = stringPtr(*status.ObservedCore)
	}
	if status.LastError != nil {
		lastError := *status.LastError
		copy.LastError = &lastError
	}
	return copy
}
