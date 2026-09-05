package fogcast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

func TestServiceRetriesNativeDevelopmentRecoveryAfterPendingStop(t *testing.T) {
	dir := t.TempDir()
	bootIDPath := filepath.Join(dir, "boot-id")
	if err := os.WriteFile(bootIDPath, []byte("boot-before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	countPath := filepath.Join(dir, "reboot-count")
	rebootPath := filepath.Join(dir, "reboot")
	rebootScript := fmt.Sprintf("#!/bin/sh\ncount_file=%q\nboot_file=%q\ncount=0\nif test -f \"$count_file\"; then count=$(cat \"$count_file\"); fi\ncount=$((count + 1))\nprintf '%%s\\n' \"$count\" > \"$count_file\"\nif test \"$count\" -ge 2; then printf 'boot-after\\n' > \"$boot_file\"; fi\n", countPath, bootIDPath)
	if err := os.WriteFile(rebootPath, []byte(rebootScript), 0o700); err != nil {
		t.Fatal(err)
	}

	control := &pendingDevelopmentRecoveryControl{bootIDPath: bootIDPath, state: "idle"}
	runtime := misterruntime.NewRuntime(control, bootIDPath, time.Millisecond, 20*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(dir, "core.rbf")),
		misterruntime.WithRebootCommand(rebootPath))
	coordinator := agent.New(runtime, core.DefaultRegistry(), 100*time.Millisecond, 100*time.Millisecond)
	coordinator.Initialize(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetHandler := httpapi.New(coordinator, "test-token", version.Version, logger, httpapi.WithDevelopment(coordinator))
	targetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/v1/health" && control.bootChanged() {
			coordinator.Initialize(context.Background())
		}
		if request.Method == http.MethodGet && request.URL.Path == "/v1/status" && control.bootChanged() {
			coordinator.Initialize(context.Background())
		}
		targetHandler.ServeHTTP(response, request)
	}))
	defer targetServer.Close()
	baseURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := host.NewClient(baseURL, "test-token", targetServer.Client())
	service := newService(
		Config{
			Targets:        []TargetConfig{{Name: "dev", Enabled: true, Address: targetServer.URL, Agent: "test-token"}},
			SelectedTarget: "dev", RequestTimeout: 100 * time.Millisecond, UploadTimeout: 500 * time.Millisecond,
		},
		Paths{Staging: dir}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	defer service.Close()

	_, loadErr := service.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf")))
	var loadAPIError *protocol.APIError
	if !errors.As(loadErr, &loadAPIError) || loadAPIError.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("development load error = %v, want public unavailable", loadErr)
	}
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGADevelopment
	service.executionMu.Unlock()

	if _, err := service.Stop(context.Background()); err == nil {
		t.Fatal("first pending recovery Stop unexpectedly completed")
	}
	if control.stopCount() != 0 {
		t.Fatalf("first pending recovery invoked runtime Stop %d times", control.stopCount())
	}

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || service.activeExecution != "" {
		t.Fatalf("retried pending recovery Stop = %#v, %v", status, err)
	}
	if control.stopCount() != 0 {
		t.Fatalf("retried pending recovery invoked runtime Stop %d times", control.stopCount())
	}
}

type pendingDevelopmentRecoveryControl struct {
	mu         sync.Mutex
	bootIDPath string
	state      string
	stopCalls  int
}

func (c *pendingDevelopmentRecoveryControl) Status(context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "pending" && c.bootChangedLocked() {
		c.state = "idle"
	}
	if c.state == "idle" {
		return nativeRecoveryIdleResponse(), nil
	}
	return nativeRecoveryRequiredResponse(), nil
}

func (*pendingDevelopmentRecoveryControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected native launch")
}

func (c *pendingDevelopmentRecoveryControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	c.mu.Lock()
	c.state = "pending"
	c.mu.Unlock()
	return nativeRecoveryRequiredResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) Stop(context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
	return nativeRecoveryRequiredResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) bootChanged() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bootChangedLocked()
}

func (c *pendingDevelopmentRecoveryControl) bootChangedLocked() bool {
	bootID, err := os.ReadFile(c.bootIDPath)
	return err == nil && strings.TrimSpace(string(bootID)) == "boot-after"
}

func (c *pendingDevelopmentRecoveryControl) stopCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopCalls
}

func nativeRecoveryRequiredResponse() misterruntime.Response {
	return misterruntime.Response{
		Protocol: 1, OK: false, State: "reboot_required", Execution: "none",
		Error: &misterruntime.RemoteError{Code: "idle_failed", Message: "private cleanup detail"}, Version: "test",
	}
}

func nativeRecoveryIdleResponse() misterruntime.Response {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}
}
