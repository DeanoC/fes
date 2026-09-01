package misterruntime_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type recordingControl struct {
	mu        sync.Mutex
	statuses  []misterruntime.Response
	statusErr error
	stop      misterruntime.Response
	stopErr   error
	statusN   int
	stopN     int
}

func (c *recordingControl) Status(ctx context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusN++
	if c.statusErr != nil {
		return misterruntime.Response{}, c.statusErr
	}
	if len(c.statuses) == 0 {
		return misterruntime.Response{}, errors.New("no recorded status")
	}
	index := c.statusN - 1
	if index >= len(c.statuses) {
		index = len(c.statuses) - 1
	}
	return c.statuses[index], ctx.Err()
}

func (c *recordingControl) Stop(ctx context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopN++
	if c.stopErr != nil {
		return misterruntime.Response{}, c.stopErr
	}
	return c.stop, ctx.Err()
}

func (c *recordingControl) calls() (status, stop int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusN, c.stopN
}

func runtimeResponse(state, execution string) misterruntime.Response {
	response := misterruntime.Response{Protocol: 1, OK: true, State: state, Execution: execution, Version: "test-runtime"}
	switch state {
	case "running_game":
		system, coreName := "megadrive", "MegaDrive"
		response.Execution, response.System, response.Core = "game", &system, &coreName
	case "running_development":
		response.Execution = "development"
	case "reboot_required":
		response.OK = false
		response.Error = &misterruntime.RemoteError{Code: "idle_failed", Message: "runtime could not load idle"}
	}
	return response
}

func TestNativeHealthIsReadyOnlyForIdleAndKeepsLegacyBooleansFalse(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		state string
		ready bool
	}{
		{name: "idle", state: "idle", ready: true},
		{name: "starting", state: "starting", ready: false},
		{name: "running game", state: "running_game", ready: false},
		{name: "running development", state: "running_development", ready: false},
		{name: "reboot required", state: "reboot_required", ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse(test.state, "none")}}
			runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "missing-boot-id"), time.Millisecond, time.Second)
			health := runtime.Health("agent-test")
			if health.APIVersion != "v1" || health.AgentVersion != "agent-test" || health.Ready != test.ready {
				t.Fatalf("health = %#v", health)
			}
			if health.MiSTerProcess || health.CommandPipe {
				t.Fatalf("legacy readiness leaked into native health: %#v", health)
			}
			statusCalls, stopCalls := control.calls()
			if statusCalls != 1 || stopCalls != 0 {
				t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
			}
		})
	}
}

type blockingHealthControl struct {
	statusCalls int
	hadDeadline bool
}

func (c *blockingHealthControl) Status(ctx context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	_, c.hadDeadline = ctx.Deadline()
	<-ctx.Done()
	return misterruntime.Response{}, ctx.Err()
}

func (*blockingHealthControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func TestNativeHealthUsesSingleBoundedStatusCall(t *testing.T) {
	t.Parallel()
	control := &blockingHealthControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 10*time.Millisecond)
	started := time.Now()
	health := runtime.Health("test")
	if health.Ready || control.statusCalls != 1 || !control.hadDeadline {
		t.Fatalf("health = %#v calls=%d deadline=%t", health, control.statusCalls, control.hadDeadline)
	}
	if elapsed := time.Since(started); elapsed < 5*time.Millisecond || elapsed > time.Second {
		t.Fatalf("health timeout elapsed = %s", elapsed)
	}
}

func TestNativeReconcileMapsIdleWithoutIdentity(t *testing.T) {
	t.Parallel()
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	if status.State != protocol.StateIdle || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.Development || status.Recovery != "" || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
}

func TestNativeReconcileMapsRebootRequiredToFailedUnavailable(t *testing.T) {
	t.Parallel()
	response := runtimeResponse("reboot_required", "none")
	response.OK = false
	response.Error = &misterruntime.RemoteError{Code: "idle_failed", Message: "private hardware detail"}
	control := &recordingControl{statuses: []misterruntime.Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	assertUnavailableStatus(t, status)
	if strings.Contains(status.LastError.Message, "private") || strings.Contains(status.LastError.Message, "hardware") {
		t.Fatalf("runtime detail leaked: %#v", status.LastError)
	}
}

func TestNativeReconcileWaitsThroughStartingAndHonorsContext(t *testing.T) {
	t.Run("starting", func(t *testing.T) {
		control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("starting", "none")}}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		status := runtime.Reconcile(ctx)
		assertUnavailableStatus(t, status)
		statusCalls, stopCalls := control.calls()
		if statusCalls < 2 || stopCalls != 0 {
			t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
		}
	})

	t.Run("socket unavailable", func(t *testing.T) {
		client := misterruntime.NewClient(filepath.Join(t.TempDir(), "missing-runtime.sock"))
		runtime := misterruntime.NewRuntime(client, "", time.Millisecond, time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		status := runtime.Reconcile(ctx)
		assertUnavailableStatus(t, status)
	})
}

func TestNativeReconcileTreatsNonIdleStartupAsUnavailable(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"running_game", "running_development"} {
		t.Run(state, func(t *testing.T) {
			execution := "game"
			if state == "running_development" {
				execution = "development"
			}
			control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse(state, execution)}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			status := runtime.Reconcile(context.Background())
			assertUnavailableStatus(t, status)
			if status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.Development {
				t.Fatalf("native identity was admitted: %#v", status)
			}
		})
	}

	t.Run("control failure", func(t *testing.T) {
		control := &recordingControl{statusErr: errors.New("private socket path and protocol detail")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		status := runtime.Reconcile(ctx)
		assertUnavailableStatus(t, status)
		if strings.Contains(status.LastError.Message, "private") || strings.Contains(status.LastError.Message, "socket") {
			t.Fatalf("control detail leaked: %#v", status.LastError)
		}
	})
}

func TestNativePrepareAndLaunchRejectEveryGameWithoutControlMutation(t *testing.T) {
	t.Parallel()
	control := &recordingControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES, "future"} {
		spec := core.Spec{System: system, ExpectedCore: "SHOULD_NOT_RUN"}
		prepared, apiErr := runtime.Prepare(spec, "/games/test.rom")
		if apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
			t.Fatalf("prepare %q = %#v, %#v", system, prepared, apiErr)
		}
		_, attempted, apiErr := runtime.Launch(context.Background(), mister.PreparedLaunch{Spec: spec})
		if attempted || apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
			t.Fatalf("launch %q = attempted:%t error:%#v", system, attempted, apiErr)
		}
	}
	statusCalls, stopCalls := control.calls()
	if statusCalls != 0 || stopCalls != 0 {
		t.Fatalf("unsupported games mutated control: status:%d stop:%d", statusCalls, stopCalls)
	}
}

type failOnRead struct {
	reads int
}

func (r *failOnRead) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("body must not be read")
}

func TestNativeDevelopmentRejectsWithoutReadingBodyOrCallingControl(t *testing.T) {
	t.Parallel()
	control := &recordingControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	body := &failOnRead{}
	_, attempted, apiErr := runtime.LoadDevelopmentRBF(context.Background(), 123, body)
	if attempted || apiErr == nil || apiErr.Code != protocol.CodeUnsupportedOperation || apiErr.Message != "requested operation is unsupported" {
		t.Fatalf("development load = attempted:%t error:%#v", attempted, apiErr)
	}
	if body.reads != 0 {
		t.Fatalf("development body reads = %d", body.reads)
	}
	_, apiErr = runtime.RecoverDevelopment(context.Background())
	if apiErr == nil || apiErr.Code != protocol.CodeUnsupportedOperation || apiErr.Message != "requested operation is unsupported" {
		t.Fatalf("development recovery error = %#v", apiErr)
	}
	statusCalls, stopCalls := control.calls()
	if statusCalls != 0 || stopCalls != 0 {
		t.Fatalf("unsupported development mutated control: status:%d stop:%d", statusCalls, stopCalls)
	}
}

func TestNativeDirectStopTranslationMapsControlResultForLaterMilestone(t *testing.T) {
	t.Parallel()
	t.Run("idle", func(t *testing.T) {
		control := &recordingControl{stop: runtimeResponse("idle", "none")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, apiErr := runtime.Stop(context.Background())
		if observed != "" || apiErr != nil {
			t.Fatalf("stop = observed:%q error:%#v", observed, apiErr)
		}
		statusCalls, stopCalls := control.calls()
		if statusCalls != 0 || stopCalls != 1 {
			t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		control := &recordingControl{stopErr: errors.New("private stop detail")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, apiErr := runtime.Stop(context.Background())
		if observed != "" || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "target runtime is unavailable" {
			t.Fatalf("stop = observed:%q error:%#v", observed, apiErr)
		}
	})

	t.Run("non idle result", func(t *testing.T) {
		control := &recordingControl{stop: runtimeResponse("reboot_required", "none")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, apiErr := runtime.Stop(context.Background())
		if observed != "" || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "target runtime is unavailable" {
			t.Fatalf("stop = observed:%q error:%#v", observed, apiErr)
		}
	})
}

func TestNativeHealthReadsBootIDWithoutLeakingReadErrors(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	bootIDFile := filepath.Join(directory, "boot-id")
	if err := os.WriteFile(bootIDFile, []byte(" boot-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	control := &recordingControl{statuses: []misterruntime.Response{runtimeResponse("idle", "none")}}
	runtime := misterruntime.NewRuntime(control, bootIDFile, time.Millisecond, time.Second)
	if health := runtime.Health("test"); health.BootID != "boot-123" {
		t.Fatalf("health = %#v", health)
	}

	missing := misterruntime.NewRuntime(control, filepath.Join(directory, "private-missing-boot-id"), time.Millisecond, time.Second)
	health := missing.Health("test")
	if health.BootID != "" || !health.Ready {
		t.Fatalf("missing boot ID health = %#v", health)
	}
}

func assertUnavailableStatus(t *testing.T, status protocol.Status) {
	t.Helper()
	if status.State != protocol.StateFailed || status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable || status.LastError.Message != "target runtime is unavailable" {
		t.Fatalf("status = %#v", status)
	}
}

var _ io.Reader = (*failOnRead)(nil)
