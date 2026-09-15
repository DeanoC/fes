package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestCachedNativeLaunchCompletingAfterTransportObservationDeadlinesReconcilesWithoutReplay(t *testing.T) {
	const (
		requestTimeout = 50 * time.Millisecond
		launchTimeout  = requestTimeout + 25*time.Millisecond
		healthTimeout  = 25 * time.Millisecond
		finishDelay    = 80 * time.Millisecond
		publicTimeout  = 500 * time.Millisecond
	)
	rom := bytes.Repeat([]byte("deadline-owned-megadrive-rom"), 64)
	digest := sha256.Sum256(rom)
	identity := protocol.ContentIdentity{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(rom)), Extension: "md"}
	content := catalog.Content{SHA256: identity.SHA256, Size: identity.Size, Extension: identity.Extension}
	game := catalog.Game{
		ID: "megadrive-deadline-owned", Title: "Deadline Owned", LibraryID: "megadrive-main", RelativePath: "deadline-owned.md",
		System: protocol.SystemMegaDrive, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: identity.Size, ModifiedNS: 123}, Content: &content,
	}
	root := catalog.Root{ID: game.LibraryID, System: game.System, Path: "/private/library"}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	control := newDeadlineOwnedLaunchControl()
	runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "missing-boot-id"), 25*time.Millisecond, healthTimeout)
	cache, err := targetcache.Open(targetcache.Config{
		Root: filepath.Join(t.TempDir(), "cache"), ActiveRecord: filepath.Join(t.TempDir(), "run", "active.json"), MaxBytes: 64 << 20,
	}, core.DefaultRegistry(), targetcache.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if _, apiErr := cache.Put(context.Background(), protocol.SystemMegaDrive, identity, bytes.NewReader(rom)); apiErr != nil {
		t.Fatalf("seed target cache: %v", apiErr)
	}
	coordinator := agent.New(runtime, core.DefaultRegistry(), launchTimeout, time.Second)
	controller := agent.NewContentController(coordinator, cache)
	coordinator.Initialize(context.Background())
	targetHandler := httpapi.New(coordinator, "test-token", version.Version, logger, httpapi.WithContent(controller))
	var targetLaunchCalls atomic.Int32
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/v2/launch" {
			targetLaunchCalls.Add(1)
			go func() {
				<-request.Context().Done()
				timer := time.NewTimer(finishDelay)
				defer timer.Stop()
				<-timer.C
				control.complete()
			}()
		}
		targetHandler.ServeHTTP(response, request)
	}))
	defer targetServer.Close()
	baseURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	targetClient := targetclient.NewClient(baseURL, "test-token", targetServer.Client())
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: requestTimeout, UploadTimeout: 2 * requestTimeout},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetClient,
	)
	defer service.Close()

	public, cancelPublic := context.WithTimeout(context.Background(), publicTimeout)
	defer cancelPublic()
	started := time.Now()
	response, launchErr := service.Launch(public, game.ID, nil)
	elapsed := time.Since(started)
	select {
	case <-control.completed:
	case <-time.After(time.Second):
		t.Fatal("simulated daemon did not publish its terminal launch state")
	}
	targetStatus := coordinator.Status()
	launchCalls := control.launchCount()

	if launchErr != nil {
		t.Errorf("Launch returned an error after Status-reconcilable completion: %v", launchErr)
	}
	if !validServiceLaunch(response, protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: identity}) {
		t.Errorf("Launch response = %#v, want exact active cached launch", response)
	}
	if !deadlineOwnedActiveStatus(targetStatus, game.ID) {
		t.Errorf("target status = %#v, want exact active Mega Drive ownership", targetStatus)
	}
	if launchCalls != 1 {
		t.Errorf("runtime launch calls = %d, want exactly one", launchCalls)
	}
	if got := targetLaunchCalls.Load(); got != 1 {
		t.Errorf("target Launch calls = %d, want exactly one", got)
	}
	if elapsed < requestTimeout+finishDelay || elapsed >= publicTimeout {
		t.Errorf("external launch elapsed = %s, want caller-bounded terminal observation after the transport and agent observation deadlines", elapsed)
	}
}

func TestCachedLaunchDoesNotReconcileFastFailureOrCallerCancellation(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	for _, test := range []struct {
		name      string
		parent    func() (context.Context, context.CancelFunc)
		launch    func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error)
		wantCode  protocol.ErrorCode
		wantCause error
	}{
		{
			name:   "fast target failure",
			parent: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			launch: func(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "private target detail"}
			},
			wantCode: protocol.CodeCoreTimeout,
		},
		{
			name: "public caller cancellation",
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 20*time.Millisecond)
			},
			launch: func(ctx context.Context, _ protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				<-ctx.Done()
				return protocol.CachedLaunchResponse{}, ctx.Err()
			},
			wantCode: protocol.CodeMiSTerUnavailable, wantCause: context.DeadlineExceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeServiceClient{}
			client.probe = func(_ context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
				return protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, nil
			}
			client.launch = test.launch
			client.statusFn = func(context.Context) (protocol.Status, error) {
				t.Fatal("non-ambiguous launch failure entered Status reconciliation")
				return protocol.Status{}, nil
			}
			service := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
			parent, cancel := test.parent()
			defer cancel()
			_, err := service.Launch(parent, game.ID, nil)
			assertServiceErrorCode(t, err, test.wantCode)
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("Launch error = %v, want cause %v", err, test.wantCause)
			}
			if client.launchCalls != 1 || client.statusCalls != 0 {
				t.Fatalf("target calls = launch:%d status:%d, want 1/0", client.launchCalls, client.statusCalls)
			}
		})
	}
}

func TestCachedLaunchDeadlineDoesNotReclassifyConclusiveTargetErrorAsAmbiguous(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	game := serviceGame(content)
	client := &fakeServiceClient{}
	client.launch = func(ctx context.Context, _ protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
		<-ctx.Done()
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "conclusive target response"}
	}
	client.statusFn = func(context.Context) (protocol.Status, error) {
		t.Fatal("conclusive target error entered lost-response reconciliation")
		return protocol.Status{}, nil
	}
	service := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
	service.requestTimeout = 20 * time.Millisecond
	_, err := service.launchContent(context.Background(), game, contentIdentity(content))
	assertServiceErrorCode(t, err, protocol.CodeCoreTimeout)
	if client.launchCalls != 1 || client.statusCalls != 0 {
		t.Fatalf("target calls = launch:%d status:%d, want 1/0", client.launchCalls, client.statusCalls)
	}
}

func TestHostLostContentLaunchReconciliationMatrix(t *testing.T) {
	content := catalog.Content{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	identity := contentIdentity(content)
	game := serviceGame(content)
	request := protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: identity}
	idle := protocol.Status{State: protocol.StateIdle}
	launching := exactHostLaunchStatus(protocol.StateLaunching, request)
	active := exactHostLaunchStatus(protocol.StateActive, request)
	failed := exactHostLaunchStatus(protocol.StateFailed, request)
	failed.LastError = &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "target transition failed"}
	wrongGame := active
	wrong := "wrong-game"
	wrongGame.GameID = &wrong
	malformedIdle := idle
	malformedIdle.GameID = &wrong

	tests := []struct {
		name            string
		statuses        []protocol.Status
		parent          func() (context.Context, context.CancelFunc)
		wantSuccess     bool
		wantCode        protocol.ErrorCode
		wantStatusCalls int
	}{
		{name: "idle launching active", statuses: []protocol.Status{idle, launching, active}, wantSuccess: true, wantStatusCalls: 3},
		{
			name: "idle forever times out", statuses: []protocol.Status{idle},
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 200*time.Millisecond)
			},
			wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: -1,
		},
		{
			name: "launching forever times out", statuses: []protocol.Status{launching},
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 200*time.Millisecond)
			},
			wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: -1,
		},
		{name: "failed terminal", statuses: []protocol.Status{failed}, wantCode: protocol.CodeCoreTimeout, wantStatusCalls: 1},
		{name: "malformed terminal", statuses: []protocol.Status{malformedIdle}, wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: 1},
		{name: "null identity", statuses: []protocol.Status{{State: protocol.StateActive}}, wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: 1},
		{name: "wrong identity", statuses: []protocol.Status{wrongGame}, wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: 1},
		{
			name: "caller cancellation",
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 15*time.Millisecond)
			},
			wantCode: protocol.CodeMiSTerUnavailable, wantStatusCalls: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeServiceClient{}
			client.launch = func(ctx context.Context, _ protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
				<-ctx.Done()
				return protocol.CachedLaunchResponse{}, ctx.Err()
			}
			statusIndex := 0
			client.statusFn = func(context.Context) (protocol.Status, error) {
				if len(test.statuses) == 0 {
					t.Fatal("caller cancellation entered Status reconciliation")
				}
				index := statusIndex
				if index >= len(test.statuses) {
					index = len(test.statuses) - 1
				}
				statusIndex++
				return test.statuses[index], nil
			}
			service := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
			service.requestTimeout = 75 * time.Millisecond
			parent := context.Background()
			cancel := func() {}
			if test.parent != nil {
				parent, cancel = test.parent()
			}
			defer cancel()
			response, err := service.launchContent(parent, game, identity)
			if test.wantSuccess {
				if err != nil || !validServiceLaunch(response, request) {
					t.Fatalf("reconciled launch = %#v, %v", response, err)
				}
			} else {
				assertServiceErrorCode(t, err, test.wantCode)
			}
			if client.launchCalls != 1 {
				t.Fatalf("target Launch calls = %d, want one", client.launchCalls)
			}
			if test.wantStatusCalls > 0 && client.statusCalls != test.wantStatusCalls {
				t.Fatalf("target Status calls = %d, want %d", client.statusCalls, test.wantStatusCalls)
			}
			if test.wantStatusCalls == 0 && client.statusCalls != 0 {
				t.Fatalf("target Status calls = %d, want zero", client.statusCalls)
			}
			if test.wantStatusCalls < 0 && client.statusCalls == 0 {
				t.Fatal("target Status was not polled before the reconciliation timeout")
			}
		})
	}
}

func exactHostLaunchStatus(state protocol.State, request protocol.CachedLaunchRequest) protocol.Status {
	spec, _ := core.DefaultRegistry().Lookup(request.System)
	gameID, system, expected := request.GameID, request.System, spec.ExpectedCore
	status := protocol.Status{State: state, GameID: &gameID, System: &system, ExpectedCore: &expected}
	if state == protocol.StateActive {
		observed := expected
		status.ObservedCore = &observed
	}
	return status
}

type deadlineOwnedLaunchControl struct {
	mu          sync.Mutex
	state       misterruntime.Response
	launchCalls int
	completeOne sync.Once
	completed   chan struct{}
}

func newDeadlineOwnedLaunchControl() *deadlineOwnedLaunchControl {
	return &deadlineOwnedLaunchControl{state: deadlineOwnedRuntimeResponse("idle", "none"), completed: make(chan struct{})}
}

func (c *deadlineOwnedLaunchControl) Status(context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, nil
}

func (c *deadlineOwnedLaunchControl) Launch(ctx context.Context, request misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.mu.Lock()
	c.launchCalls++
	c.state = deadlineOwnedRuntimeResponse("starting", "game")
	c.mu.Unlock()
	select {
	case <-c.completed:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.state, nil
	case <-ctx.Done():
		return misterruntime.Response{}, ctx.Err()
	}
}

func (*deadlineOwnedLaunchControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func (c *deadlineOwnedLaunchControl) Stop(context.Context) (misterruntime.Response, error) {
	return deadlineOwnedRuntimeResponse("idle", "none"), nil
}

func (c *deadlineOwnedLaunchControl) complete() {
	c.completeOne.Do(func() {
		c.mu.Lock()
		c.state = deadlineOwnedRuntimeResponse("running_game", "game")
		c.mu.Unlock()
		close(c.completed)
	})
}

func (c *deadlineOwnedLaunchControl) launchCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launchCalls
}

func deadlineOwnedRuntimeResponse(state, execution string) misterruntime.Response {
	response := misterruntime.Response{Protocol: 1, OK: true, State: state, Execution: execution, Version: "git-test"}
	if execution == "game" {
		system, observed := "megadrive", "MegaDrive"
		response.System, response.Core = &system, &observed
	}
	return response
}

func deadlineOwnedActiveStatus(status protocol.Status, gameID string) bool {
	return status.State == protocol.StateActive && status.GameID != nil && *status.GameID == gameID &&
		status.System != nil && *status.System == protocol.SystemMegaDrive &&
		status.ExpectedCore != nil && *status.ExpectedCore == "MegaDrive" &&
		status.ObservedCore != nil && *status.ObservedCore == "MegaDrive" && status.LastError == nil
}
