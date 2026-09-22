package fogcast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestServiceDevelopmentStopRecoversIdleWithoutReboot(t *testing.T) {
	dir := t.TempDir()
	bootIDPath := filepath.Join(dir, "boot-id")
	if err := os.WriteFile(bootIDPath, []byte("boot-before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rebootPath := filepath.Join(dir, "reboot")
	marker := filepath.Join(dir, "rebooted")
	rebootScript := fmt.Sprintf("#!/bin/sh\n: > %q\n", marker)
	if err := os.WriteFile(rebootPath, []byte(rebootScript), 0o700); err != nil {
		t.Fatal(err)
	}

	control := &pendingDevelopmentRecoveryControl{state: "idle"}
	runtime := misterruntime.NewRuntime(control, bootIDPath, time.Millisecond, 20*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(dir, "core.rbf")),
		misterruntime.WithRebootCommand(rebootPath))
	coordinator := agent.New(runtime, 100*time.Millisecond, 100*time.Millisecond)
	coordinator.Initialize(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	targetHandler := httpapi.New(coordinator, "test-token", version.Version, logger, httpapi.WithDevelopment(coordinator))
	targetServer := httptest.NewServer(targetHandler)
	defer targetServer.Close()
	baseURL, err := url.Parse(targetServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := targetclient.NewClient(baseURL, "test-token", targetServer.Client())
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

	status, err := service.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle || service.activeExecution != "" {
		t.Fatalf("idle recovery Stop = %#v, %v", status, err)
	}
	if control.stopCount() != 0 {
		t.Fatalf("idle recovery invoked runtime Stop %d times", control.stopCount())
	}
	if control.recoverCount() != 1 {
		t.Fatalf("recover_idle calls = %d", control.recoverCount())
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("idle recovery executed reboot command: %v", statErr)
	}
}

type pendingDevelopmentRecoveryControl struct {
	mu           sync.Mutex
	state        string
	stopCalls    int
	recoverCalls int
}

func (c *pendingDevelopmentRecoveryControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "idle" {
		return nativeRecoveryIdleResponse(), nil
	}
	return nativeRecoveryRequiredResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) Protocol2RecoverIdle(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recoverCalls++
	c.state = "idle"
	return nativeRecoveryIdleResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.state = "pending"
	c.mu.Unlock()
	return nativeRecoveryRequiredResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
	return nativeRecoveryRequiredResponse(), nil
}

func (c *pendingDevelopmentRecoveryControl) stopCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopCalls
}

func (c *pendingDevelopmentRecoveryControl) recoverCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recoverCalls
}

func nativeRecoveryRequiredResponse() misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: false, State: "reboot_required", Execution: "none",
		Error: &misterruntime.Protocol2Error{Code: "idle_failed", Message: "private cleanup detail"}, Version: "test",
	}
}

func nativeRecoveryIdleResponse() misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}
}
