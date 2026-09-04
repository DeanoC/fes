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
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

func TestNativeStopCompletingAfterTwoSecondTargetDeadlineReconcilesWithoutReplayAndRelaunches(t *testing.T) {
	const (
		requestTimeout   = 2 * time.Second
		operationTimeout = 5 * time.Second
		finishDelay      = 350 * time.Millisecond
	)
	rom := bytes.Repeat([]byte("deadline-owned-stop-rom"), 64)
	digest := sha256.Sum256(rom)
	identity := protocol.ContentIdentity{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(rom)), Extension: "md"}
	content := catalog.Content{SHA256: identity.SHA256, Size: identity.Size, Extension: identity.Extension}
	game := catalog.Game{
		ID: "megadrive-deadline-owned-stop", Title: "Deadline Owned Stop", LibraryID: "megadrive-main", RelativePath: "deadline-owned-stop.md",
		System: protocol.SystemMegaDrive, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: identity.Size, ModifiedNS: 123}, Content: &content,
	}
	root := catalog.Root{ID: game.LibraryID, System: game.System, Path: "/private/library"}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	control := newDeadlineOwnedStopControl()
	runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "missing-boot-id"), 25*time.Millisecond, 250*time.Millisecond)
	cache, err := targetcache.Open(targetcache.Config{
		Root: filepath.Join(t.TempDir(), "cache"), ActiveRecord: filepath.Join(t.TempDir(), "run", "active.json"), MaxBytes: 64 << 20,
	}, core.DefaultRegistry(), targetcache.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if _, apiErr := cache.Put(context.Background(), protocol.SystemMegaDrive, identity, bytes.NewReader(rom)); apiErr != nil {
		t.Fatalf("seed target cache: %v", apiErr)
	}
	process, stopProcess := context.WithCancel(context.Background())
	defer stopProcess()
	coordinator := agent.New(runtime, core.DefaultRegistry(), operationTimeout, operationTimeout, agent.WithOperationContext(process))
	controller := agent.NewContentController(coordinator, cache)
	coordinator.Initialize(context.Background())
	targetHandler := httpapi.New(coordinator, "test-token", version.Version, logger, httpapi.WithContent(controller))
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/v1/stop" {
			go func() {
				<-request.Context().Done()
				timer := time.NewTimer(finishDelay)
				defer timer.Stop()
				<-timer.C
				control.completeStop()
			}()
		}
		targetHandler.ServeHTTP(response, request)
	}))
	defer targetServer.Close()
	baseURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	targetClient := host.NewClient(baseURL, "test-token", targetServer.Client())
	service := newService(
		Config{Libraries: []catalog.Root{root}, RequestTimeout: requestTimeout, UploadTimeout: 2 * requestTimeout},
		Paths{Staging: t.TempDir()}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, targetClient,
	)
	defer service.Close()

	launched, err := service.Launch(context.Background(), game.ID, nil)
	if err != nil || !validServiceLaunch(launched, protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: identity}) {
		t.Fatalf("initial launch = %#v, %v", launched, err)
	}
	started := time.Now()
	stopped, stopErr := service.Stop(context.Background())
	elapsed := time.Since(started)
	select {
	case <-control.stopCompleted:
	case <-time.After(time.Second):
		t.Fatal("simulated daemon did not publish terminal idle")
	}
	if stopErr != nil || !exactDeadlineOwnedIdle(stopped) {
		t.Errorf("Stop = %#v, %v; want exact reconciled idle", stopped, stopErr)
	}
	if control.stopCount() != 1 {
		t.Errorf("runtime Stop calls = %d, want exactly one", control.stopCount())
	}
	if elapsed < requestTimeout || elapsed >= 2*requestTimeout {
		t.Errorf("external Stop elapsed = %s, want bounded Status reconciliation after the 2s target deadline", elapsed)
	}
	relaunched, relaunchErr := service.Launch(context.Background(), game.ID, nil)
	if relaunchErr != nil || !validServiceLaunch(relaunched, protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: identity}) {
		t.Errorf("immediate relaunch = %#v, %v", relaunched, relaunchErr)
	}
	launches, stops := control.counts()
	if launches != 2 || stops != 1 {
		t.Errorf("runtime mutations = launch:%d stop:%d, want 2/1", launches, stops)
	}
}

func TestNativeStopDoesNotReconcileConclusiveFailureOrCallerCancellation(t *testing.T) {
	for _, test := range []struct {
		name      string
		parent    func() (context.Context, context.CancelFunc)
		stop      func(context.Context) (protocol.Status, error)
		wantCode  protocol.ErrorCode
		wantCause error
	}{
		{
			name:   "conclusive target failure at deadline",
			parent: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			stop: func(ctx context.Context) (protocol.Status, error) {
				<-ctx.Done()
				return protocol.Status{}, &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "conclusive target response"}
			},
			wantCode: protocol.CodeCoreTimeout,
		},
		{
			name: "public caller cancellation",
			parent: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 15*time.Millisecond)
			},
			stop: func(ctx context.Context) (protocol.Status, error) {
				<-ctx.Done()
				return protocol.Status{}, ctx.Err()
			},
			wantCode: protocol.CodeMiSTerUnavailable, wantCause: context.DeadlineExceeded,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeServiceClient{stopFn: test.stop}
			client.statusFn = func(context.Context) (protocol.Status, error) {
				t.Fatal("non-ambiguous Stop entered Status reconciliation")
				return protocol.Status{}, nil
			}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
			service.requestTimeout = 20 * time.Millisecond
			service.executionMu.Lock()
			service.activeExecution = ExecutionFPGANative
			service.executionMu.Unlock()
			parent, cancel := test.parent()
			defer cancel()
			_, err := service.Stop(parent)
			assertServiceErrorCode(t, err, test.wantCode)
			if test.wantCause != nil && !errors.Is(err, test.wantCause) {
				t.Fatalf("Stop error = %v, want cause %v", err, test.wantCause)
			}
			if client.stopCalls != 1 || client.statusCalls != 0 {
				t.Fatalf("target calls = stop:%d status:%d, want 1/0", client.stopCalls, client.statusCalls)
			}
		})
	}
}

type deadlineOwnedStopControl struct {
	mu            sync.Mutex
	state         misterruntime.Response
	launchCalls   int
	stopCalls     int
	completeOne   sync.Once
	stopCompleted chan struct{}
}

func newDeadlineOwnedStopControl() *deadlineOwnedStopControl {
	return &deadlineOwnedStopControl{state: deadlineOwnedRuntimeResponse("idle", "none"), stopCompleted: make(chan struct{})}
}

func (c *deadlineOwnedStopControl) Status(context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, nil
}

func (c *deadlineOwnedStopControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.launchCalls++
	c.state = deadlineOwnedRuntimeResponse("running_game", "game")
	return c.state, nil
}

func (c *deadlineOwnedStopControl) Stop(ctx context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
	select {
	case <-c.stopCompleted:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.state, nil
	case <-ctx.Done():
		return misterruntime.Response{}, ctx.Err()
	}
}

func (c *deadlineOwnedStopControl) completeStop() {
	c.completeOne.Do(func() {
		c.mu.Lock()
		c.state = deadlineOwnedRuntimeResponse("idle", "none")
		c.mu.Unlock()
		close(c.stopCompleted)
	})
}

func (c *deadlineOwnedStopControl) stopCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopCalls
}

func (c *deadlineOwnedStopControl) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launchCalls, c.stopCalls
}

func exactDeadlineOwnedIdle(status protocol.Status) bool {
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil && status.ExpectedCore == nil &&
		status.ObservedCore == nil && status.LastError == nil && !status.Development && status.Recovery == ""
}
