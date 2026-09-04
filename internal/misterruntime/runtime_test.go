package misterruntime_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
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
	launch    misterruntime.Response
	launchErr error
	stop      misterruntime.Response
	stopErr   error
	statusN   int
	launchN   int
	stopN     int
	requests  []misterruntime.LaunchRequest
}

type blockingOwnedStopControl struct {
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	stops   int
}

func (*blockingOwnedStopControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected Launch")
}

func (*blockingOwnedStopControl) Status(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected Status")
}

func (c *blockingOwnedStopControl) Stop(ctx context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	c.stops++
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return misterruntime.Response{}, ctx.Err()
}

func (c *blockingOwnedStopControl) stopCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stops
}

func (c *recordingControl) Launch(ctx context.Context, request misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.launchN++
	copied := request
	copied.Media = cloneStringMap(request.Media)
	copied.Settings = cloneStringMap(request.Settings)
	c.requests = append(c.requests, copied)
	if c.launchErr != nil {
		return misterruntime.Response{}, c.launchErr
	}
	return c.launch, ctx.Err()
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

func (c *recordingControl) launchCalls() (int, []misterruntime.LaunchRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	requests := make([]misterruntime.LaunchRequest, len(c.requests))
	for i, request := range c.requests {
		requests[i] = request
		requests[i].Media = cloneStringMap(request.Media)
		requests[i].Settings = cloneStringMap(request.Settings)
	}
	return c.launchN, requests
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}

func runtimeResponse(state, execution string) misterruntime.Response {
	response := misterruntime.Response{Protocol: 1, OK: true, State: state, Execution: execution, Version: "test-runtime"}
	switch state {
	case "starting":
		if execution == "game" {
			system, coreName := "megadrive", "MegaDrive"
			response.System, response.Core = &system, &coreName
		}
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

func retainedErrorIdleResponse() misterruntime.Response {
	response := runtimeResponse("idle", "none")
	response.Error = &misterruntime.RemoteError{Code: "io_failed", Message: "private prior failure"}
	return response
}

func TestNativeHealthIsReadyOnlyForControllableProductionStatesAndKeepsLegacyBooleansFalse(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response misterruntime.Response
		ready    bool
	}{
		{name: "idle", response: runtimeResponse("idle", "none"), ready: true},
		{name: "idle with retained prior error", response: retainedErrorIdleResponse(), ready: true},
		{name: "idle with malformed retained error", response: func() misterruntime.Response {
			response := retainedErrorIdleResponse()
			response.Error.Code = "unknown"
			return response
		}(), ready: false},
		{name: "starting", response: runtimeResponse("starting", "none"), ready: false},
		{name: "running Mega Drive", response: runtimeResponse("running_game", "game"), ready: false},
		{name: "running wrong core", response: func() misterruntime.Response {
			response := runtimeResponse("running_game", "game")
			wrong := "MENU"
			response.Core = &wrong
			return response
		}(), ready: false},
		{name: "running development", response: runtimeResponse("running_development", "development"), ready: false},
		{name: "reboot required", response: runtimeResponse("reboot_required", "none"), ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Response{test.response}}
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

type stopReadyRuntime interface {
	StopReady() bool
}

func TestNativeStopReadinessAcceptsOnlyIdleOrExactMegaDrive(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response misterruntime.Response
		ready    bool
	}{
		{name: "idle", response: runtimeResponse("idle", "none"), ready: true},
		{name: "idle with retained prior error", response: retainedErrorIdleResponse(), ready: true},
		{name: "idle with malformed retained error", response: func() misterruntime.Response {
			response := retainedErrorIdleResponse()
			response.Error.Code = "unknown"
			return response
		}(), ready: false},
		{name: "running Mega Drive", response: runtimeResponse("running_game", "game"), ready: true},
		{name: "operation failed idle", response: func() misterruntime.Response {
			response := retainedErrorIdleResponse()
			response.OK = false
			return response
		}(), ready: false},
		{name: "starting", response: runtimeResponse("starting", "game"), ready: false},
		{name: "running wrong core", response: func() misterruntime.Response {
			response := runtimeResponse("running_game", "game")
			wrong := "MENU"
			response.Core = &wrong
			return response
		}(), ready: false},
		{name: "running development", response: runtimeResponse("running_development", "development"), ready: false},
		{name: "reboot required", response: runtimeResponse("reboot_required", "none"), ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Response{test.response}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			admission, ok := any(runtime).(stopReadyRuntime)
			if !ok {
				t.Fatal("native runtime has no operation-specific Stop readiness")
			}
			if ready := admission.StopReady(); ready != test.ready {
				t.Fatalf("StopReady = %t, want %t", ready, test.ready)
			}
			statusCalls, stopCalls := control.calls()
			if statusCalls != 1 || stopCalls != 0 {
				t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
			}
		})
	}
}

func TestNativeHealthRejectsOperationFailedIdleResponse(t *testing.T) {
	t.Parallel()
	response := runtimeResponse("idle", "none")
	response.OK = false
	response.Error = &misterruntime.RemoteError{Code: "io_failed", Message: "private runtime detail"}
	control := &recordingControl{statuses: []misterruntime.Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	health := runtime.Health("agent-test")
	if health.Ready || health.APIVersion != "v1" || health.AgentVersion != "agent-test" || health.MiSTerProcess || health.CommandPipe {
		t.Fatalf("health = %#v", health)
	}
	statusCalls, stopCalls := control.calls()
	if statusCalls != 1 || stopCalls != 0 {
		t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
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

func (*blockingHealthControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
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

func TestNativeReconcilePreservesRetainedErrorFromOperationalIdle(t *testing.T) {
	t.Parallel()
	control := &recordingControl{statuses: []misterruntime.Response{retainedErrorIdleResponse()}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	if status.State != protocol.StateIdle || status.GameID != nil || status.System != nil ||
		status.ExpectedCore != nil || status.ObservedCore != nil || status.Development || status.Recovery != "" {
		t.Fatalf("status = %#v", status)
	}
	if status.LastError == nil || status.LastError.Code != protocol.CodeMiSTerUnavailable ||
		status.LastError.Message != "target runtime is unavailable" {
		t.Fatalf("retained error = %#v", status.LastError)
	}
}

func TestNativeReconcileRejectsMalformedRetainedError(t *testing.T) {
	t.Parallel()
	response := retainedErrorIdleResponse()
	response.Error.Code = "unknown"
	control := &recordingControl{statuses: []misterruntime.Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	assertUnavailableStatus(t, status)
}

func TestNativeReconcileTreatsErrorBearingIdleAndStartingAsImmediateUnavailable(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"idle", "starting"} {
		t.Run(state, func(t *testing.T) {
			response := runtimeResponse(state, "none")
			response.OK = false
			response.Error = &misterruntime.RemoteError{Code: "io_failed", Message: "private runtime detail"}
			if state == "starting" {
				system, coreName := "megadrive", "MegaDrive"
				response.System, response.Core = &system, &coreName
			}
			control := &recordingControl{statuses: []misterruntime.Response{response}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			status := runtime.Reconcile(ctx)
			assertUnavailableStatus(t, status)
			if status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.Development || status.Recovery != "" {
				t.Fatalf("runtime identity leaked: %#v", status)
			}
			statusCalls, stopCalls := control.calls()
			if statusCalls != 1 || stopCalls != 0 {
				t.Fatalf("control calls = status:%d stop:%d, want one conclusive status call", statusCalls, stopCalls)
			}
		})
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

func TestNativePrepareAcceptsOnlyTheRegisteredMegaDriveShapeAndAnAbsoluteROM(t *testing.T) {
	t.Parallel()
	control := &recordingControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	spec, ok := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	if !ok {
		t.Fatal("Mega Drive registry entry is missing")
	}
	rom := writeNativeROM(t, ".bin")

	prepared, apiErr := runtime.Prepare(spec, rom)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if prepared.Spec.System != protocol.SystemMegaDrive || prepared.Spec.ExpectedCore != "MegaDrive" ||
		prepared.AbsoluteROM != rom || prepared.RelativeROM != "" || len(prepared.MGL) != 0 {
		t.Fatalf("prepared = %#v", prepared)
	}

	unsupported, ok := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry is missing")
	}
	if _, apiErr := runtime.Prepare(unsupported, rom); apiErr == nil || apiErr.Code != protocol.CodeUnsupportedSystem {
		t.Fatalf("SNES prepare error = %#v", apiErr)
	}
	for _, test := range []struct {
		name string
		path string
		code protocol.ErrorCode
	}{
		{name: "relative", path: "sonic2.bin", code: protocol.CodeInvalidROMPath},
		{name: "NUL", path: rom + "\x00ignored", code: protocol.CodeInvalidROMPath},
		{name: "missing", path: filepath.Join(t.TempDir(), "missing.bin"), code: protocol.CodeROMNotFound},
		{name: "directory", path: t.TempDir(), code: protocol.CodeROMNotFound},
		{name: "wrong extension", path: writeNativeROM(t, ".sfc"), code: protocol.CodeInvalidROMPath},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, apiErr := runtime.Prepare(spec, test.path); apiErr == nil || apiErr.Code != test.code {
				t.Fatalf("Prepare(%q) error = %#v, want %s", test.path, apiErr, test.code)
			}
		})
	}
	if launches, _ := control.launchCalls(); launches != 0 {
		t.Fatalf("prepare called runtime launch %d times", launches)
	}
}

func TestNativeLaunchMapsPreparedMegaDriveToTheExactRuntimeRequest(t *testing.T) {
	t.Parallel()
	control := &recordingControl{
		statuses: []misterruntime.Response{runtimeResponse("idle", "none")},
		launch:   runtimeResponse("running_game", "game"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}

	observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
	if apiErr != nil || !attempted || observed != "MegaDrive" {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	launches, requests := control.launchCalls()
	if launches != 1 || len(requests) != 1 {
		t.Fatalf("launch calls = %d requests = %#v", launches, requests)
	}
	request := requests[0]
	if request.System != "megadrive" || request.RBF != "/usr/share/mister-runtime/cores/megadrive.rbf" ||
		len(request.Media) != 1 || request.Media["cartridge"] != prepared.AbsoluteROM ||
		request.Settings == nil || len(request.Settings) != 0 {
		t.Fatalf("runtime request = %#v", request)
	}
}

func TestNativeLaunchAdmitsRecoveredIdleWithRetainedError(t *testing.T) {
	t.Parallel()
	control := &recordingControl{
		statuses: []misterruntime.Response{retainedErrorIdleResponse()},
		launch:   runtimeResponse("running_game", "game"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}

	observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
	if apiErr != nil || !attempted || observed != "MegaDrive" {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	statusCalls, _ := control.calls()
	launchCalls, _ := control.launchCalls()
	if statusCalls != 1 || launchCalls != 1 {
		t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
	}
}

type blockingAdmissionControl struct {
	statusCalls int
	launchCalls int
}

func (c *blockingAdmissionControl) Status(ctx context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	<-ctx.Done()
	return misterruntime.Response{}, ctx.Err()
}

func (c *blockingAdmissionControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.launchCalls++
	return misterruntime.Response{}, errors.New("launch must not be dispatched")
}

func (*blockingAdmissionControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func TestNativeLaunchInitialAdmissionHonorsCallerCancellationWithoutDispatch(t *testing.T) {
	control := &blockingAdmissionControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 500*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()

	started := time.Now()
	observed, attempted, apiErr := runtime.Launch(ctx, prepared)
	elapsed := time.Since(started)
	if observed != "" || attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable ||
		apiErr.Message != "target runtime is unavailable" {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	if control.statusCalls != 1 || control.launchCalls != 0 {
		t.Fatalf("calls = launch:%d status:%d", control.launchCalls, control.statusCalls)
	}
	if elapsed < 5*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("caller-bound admission elapsed = %s", elapsed)
	}
}

type ownedLaunchContextControl struct {
	launchStarted chan struct{}
	release       chan struct{}
	startOnce     sync.Once
	launchCalls   int
}

func (c *ownedLaunchContextControl) Status(context.Context) (misterruntime.Response, error) {
	return runtimeResponse("idle", "none"), nil
}

func (c *ownedLaunchContextControl) Launch(ctx context.Context, _ misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.launchCalls++
	c.startOnce.Do(func() { close(c.launchStarted) })
	select {
	case <-c.release:
		return runtimeResponse("running_game", "game"), nil
	case <-ctx.Done():
		return misterruntime.Response{}, ctx.Err()
	}
}

func (*ownedLaunchContextControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func TestNativeOwnedLaunchKeepsAdmittedMutationOnProcessOwnerAfterObservationDeadline(t *testing.T) {
	control := &ownedLaunchContextControl{launchStarted: make(chan struct{}), release: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 25*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	process, stopProcess := context.WithCancel(context.Background())
	defer stopProcess()
	observation, cancelObservation := context.WithTimeout(process, 20*time.Millisecond)
	defer cancelObservation()
	type result struct {
		observed  string
		attempted bool
		err       *protocol.APIError
	}
	done := make(chan result, 1)
	go func() {
		observed, attempted, err := runtime.LaunchOwned(context.Background(), observation, process, prepared)
		done <- result{observed: observed, attempted: attempted, err: err}
	}()
	<-control.launchStarted
	<-observation.Done()
	select {
	case got := <-done:
		t.Fatalf("LaunchOwned returned before runtime input readiness: %#v", got)
	case <-time.After(10 * time.Millisecond):
	}
	close(control.release)
	select {
	case got := <-done:
		if got.observed != "MegaDrive" || !got.attempted || got.err != nil {
			t.Fatalf("LaunchOwned after observation deadline = observed:%q attempted:%t error:%#v", got.observed, got.attempted, got.err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("process-owned Launch did not return after runtime input readiness")
	}
	if control.launchCalls != 1 {
		t.Fatalf("runtime Launch calls = %d, want exactly one", control.launchCalls)
	}
}

type operationDeadlineLaunchControl struct {
	mu               sync.Mutex
	state            misterruntime.Response
	launchCalls      int
	statusCalls      int
	operationExpired chan struct{}
	reconcileStarted chan struct{}
	terminal         chan struct{}
	expiredOnce      sync.Once
	reconcileOnce    sync.Once
	terminalOnce     sync.Once
}

type delayedDeadlineContext struct {
	context.Context
	deadline time.Time
	done     <-chan struct{}
}

func (c delayedDeadlineContext) Deadline() (time.Time, bool) { return c.deadline, true }
func (c delayedDeadlineContext) Done() <-chan struct{}       { return c.done }
func (c delayedDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

type deadlinePublicationRaceControl struct {
	mu          sync.Mutex
	statusCalls int
	launchCalls int
	deadline    time.Time
}

func (c *deadlinePublicationRaceControl) Status(ctx context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	c.statusCalls++
	statusCalls := c.statusCalls
	c.mu.Unlock()
	if statusCalls == 1 {
		return runtimeResponse("idle", "none"), nil
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return misterruntime.Response{}, context.DeadlineExceeded
	}
	return runtimeResponse("running_game", "game"), nil
}

func (c *deadlinePublicationRaceControl) Launch(ctx context.Context, _ misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.mu.Lock()
	c.launchCalls++
	c.mu.Unlock()
	timer := time.NewTimer(time.Until(c.deadline))
	defer timer.Stop()
	select {
	case <-timer.C:
		return misterruntime.Response{}, context.DeadlineExceeded
	case <-ctx.Done():
		return misterruntime.Response{}, ctx.Err()
	}
}

func (*deadlinePublicationRaceControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func (c *deadlinePublicationRaceControl) counts() (launch, status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launchCalls, c.statusCalls
}

func newOperationDeadlineLaunchControl() *operationDeadlineLaunchControl {
	return &operationDeadlineLaunchControl{
		state:            runtimeResponse("idle", "none"),
		operationExpired: make(chan struct{}),
		reconcileStarted: make(chan struct{}),
		terminal:         make(chan struct{}),
	}
}

func (c *operationDeadlineLaunchControl) Status(ctx context.Context) (misterruntime.Response, error) {
	if err := ctx.Err(); err != nil {
		return misterruntime.Response{}, err
	}
	c.mu.Lock()
	c.statusCalls++
	statusCalls := c.statusCalls
	state := c.state
	c.mu.Unlock()
	if statusCalls > 1 {
		c.reconcileOnce.Do(func() { close(c.reconcileStarted) })
	}
	return state, nil
}

func (c *operationDeadlineLaunchControl) Launch(ctx context.Context, _ misterruntime.LaunchRequest) (misterruntime.Response, error) {
	c.mu.Lock()
	c.launchCalls++
	c.state = runtimeResponse("starting", "game")
	c.mu.Unlock()
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		c.expiredOnce.Do(func() { close(c.operationExpired) })
		return misterruntime.Response{}, context.DeadlineExceeded
	case <-ctx.Done():
		c.expiredOnce.Do(func() { close(c.operationExpired) })
		return misterruntime.Response{}, ctx.Err()
	}
}

func (*operationDeadlineLaunchControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func (c *operationDeadlineLaunchControl) complete() {
	c.terminalOnce.Do(func() {
		c.mu.Lock()
		c.state = runtimeResponse("running_game", "game")
		c.mu.Unlock()
		close(c.terminal)
	})
}

func (c *operationDeadlineLaunchControl) counts() (launch, status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.launchCalls, c.statusCalls
}

func TestNativeOwnedLaunchReconcilesTerminalAfterOperationDeadline(t *testing.T) {
	control := newOperationDeadlineLaunchControl()
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 100*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	go func() {
		<-control.operationExpired
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		control.complete()
	}()
	operation, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	started := time.Now()
	observed, attempted, apiErr := runtime.LaunchOwned(context.Background(), operation, context.Background(), prepared)
	elapsed := time.Since(started)
	<-control.terminal
	launchCalls, statusCalls := control.counts()
	if observed != "MegaDrive" || !attempted || apiErr != nil {
		t.Fatalf("LaunchOwned = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	if launchCalls != 1 || statusCalls < 2 {
		t.Fatalf("calls = launch:%d status:%d, want one mutation and fresh Status reconciliation", launchCalls, statusCalls)
	}
	if elapsed < 20*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Fatalf("owned launch elapsed = %s, want bounded terminal reconciliation", elapsed)
	}
}

func TestNativeOwnedLaunchReconcilesWhenDeadlineElapsedBeforeContextErrorPublication(t *testing.T) {
	deadline := time.Now().Add(10 * time.Millisecond)
	control := &deadlinePublicationRaceControl{deadline: deadline}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 100*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	done := make(chan struct{})
	publish := time.AfterFunc(100*time.Millisecond, func() { close(done) })
	defer func() {
		if publish.Stop() {
			close(done)
		}
		<-done
	}()
	operation := delayedDeadlineContext{Context: context.Background(), deadline: deadline, done: done}

	observed, attempted, apiErr := runtime.LaunchOwned(context.Background(), operation, context.Background(), prepared)
	launchCalls, statusCalls := control.counts()
	if observed != "MegaDrive" || !attempted || apiErr != nil {
		t.Fatalf("LaunchOwned = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	if launchCalls != 1 || statusCalls != 2 {
		t.Fatalf("calls = launch:%d status:%d, want one mutation and one fresh Status after admission", launchCalls, statusCalls)
	}
}

func TestNativeOwnedLaunchTerminalReconciliationHonorsProcessShutdown(t *testing.T) {
	control := newOperationDeadlineLaunchControl()
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	process, stopProcess := context.WithCancel(context.Background())
	operation, cancelOperation := context.WithTimeout(process, 20*time.Millisecond)
	defer cancelOperation()
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, _, err := runtime.LaunchOwned(context.Background(), operation, process, prepared)
		done <- err
	}()
	<-control.operationExpired
	<-control.reconcileStarted
	started := time.Now()
	stopProcess()
	select {
	case apiErr := <-done:
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("shutdown reconciliation error = %#v", apiErr)
		}
		if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
			t.Fatalf("process shutdown cancellation elapsed = %s", elapsed)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned launch reconciliation ignored process shutdown")
	}
	launchCalls, _ := control.counts()
	if launchCalls != 1 {
		t.Fatalf("launch calls = %d, want one", launchCalls)
	}
}

func TestNativeOwnedLaunchTransfersOnlyPostAdmissionWorkToOperationContext(t *testing.T) {
	control := &ownedLaunchContextControl{launchStarted: make(chan struct{}), release: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	admission, cancelAdmission := context.WithCancel(context.Background())
	operation, cancelOperation := context.WithTimeout(context.Background(), time.Second)
	defer cancelOperation()
	type result struct {
		observed  string
		attempted bool
		err       *protocol.APIError
	}
	done := make(chan result, 1)
	go func() {
		observed, attempted, err := runtime.LaunchOwned(admission, operation, context.Background(), prepared)
		done <- result{observed: observed, attempted: attempted, err: err}
	}()
	<-control.launchStarted
	cancelAdmission()
	close(control.release)
	got := <-done
	if got.observed != "MegaDrive" || !got.attempted || got.err != nil {
		t.Fatalf("LaunchOwned after caller cancellation = observed:%q attempted:%t error:%#v", got.observed, got.attempted, got.err)
	}
}

func TestNativeOwnedLaunchHonorsAdmissionAndOperationCancellationBoundaries(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	t.Run("canceled admission never dispatches", func(t *testing.T) {
		control := &blockingAdmissionControl{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond)
		prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
		if apiErr != nil {
			t.Fatal(apiErr)
		}
		admission, cancel := context.WithCancel(context.Background())
		cancel()
		_, attempted, apiErr := runtime.LaunchOwned(admission, context.Background(), context.Background(), prepared)
		if attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || control.launchCalls != 0 {
			t.Fatalf("canceled admission = attempted:%t error:%#v launch calls:%d", attempted, apiErr, control.launchCalls)
		}
	})

	t.Run("operation owner cancellation stops waiting", func(t *testing.T) {
		control := &ownedLaunchContextControl{launchStarted: make(chan struct{}), release: make(chan struct{})}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond)
		prepared, apiErr := runtime.Prepare(spec, writeNativeROM(t, ".bin"))
		if apiErr != nil {
			t.Fatal(apiErr)
		}
		process, cancel := context.WithCancel(context.Background())
		operation, cancelOperation := context.WithCancel(process)
		defer cancelOperation()
		done := make(chan *protocol.APIError, 1)
		go func() {
			_, _, err := runtime.LaunchOwned(context.Background(), operation, process, prepared)
			done <- err
		}()
		<-control.launchStarted
		cancel()
		select {
		case apiErr := <-done:
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
				t.Fatalf("operation cancellation error = %#v", apiErr)
			}
		case <-time.After(250 * time.Millisecond):
			t.Fatal("owned launch ignored operation-owner cancellation")
		}
	})
}

func TestNativeOwnedLaunchReconcilesLostResponseWithinOperationAndTerminalBudgets(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}
	t.Run("terminal status", func(t *testing.T) {
		control := &recordingControl{
			launchErr: errors.New("response lost after dispatch"),
			statuses: []misterruntime.Response{
				runtimeResponse("idle", "none"),
				runtimeResponse("starting", "game"),
				runtimeResponse("running_game", "game"),
			},
		}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 500*time.Millisecond)
		operation, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		observed, attempted, apiErr := runtime.LaunchOwned(context.Background(), operation, context.Background(), prepared)
		if observed != "MegaDrive" || !attempted || apiErr != nil {
			t.Fatalf("LaunchOwned = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
		}
		statusCalls, _ := control.calls()
		launchCalls, _ := control.launchCalls()
		if statusCalls != 3 || launchCalls != 1 {
			t.Fatalf("calls = launch:%d status:%d, want one dispatch and Status-only reconciliation", launchCalls, statusCalls)
		}
	})

	t.Run("operation deadline", func(t *testing.T) {
		control := &recordingControl{
			launchErr: errors.New("response lost after dispatch"),
			statuses: []misterruntime.Response{
				runtimeResponse("idle", "none"),
				runtimeResponse("starting", "game"),
			},
		}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 25*time.Millisecond)
		operation, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		started := time.Now()
		_, attempted, apiErr := runtime.LaunchOwned(context.Background(), operation, context.Background(), prepared)
		elapsed := time.Since(started)
		if !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("LaunchOwned = attempted:%t error:%#v", attempted, apiErr)
		}
		if elapsed < 20*time.Millisecond || elapsed > 150*time.Millisecond {
			t.Fatalf("owned reconciliation elapsed = %s, want operation budget plus bounded terminal reserve", elapsed)
		}
		launchCalls, _ := control.launchCalls()
		if launchCalls != 1 {
			t.Fatalf("launch calls = %d, want no replay", launchCalls)
		}
	})
}

func TestNativeLaunchRejectsPreparedValuesOutsideTheMegaDriveContractWithoutControlMutation(t *testing.T) {
	t.Parallel()
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	rom := writeNativeROM(t, ".bin")
	cases := []struct {
		name     string
		prepared mister.PreparedLaunch
		code     protocol.ErrorCode
	}{
		{name: "unsupported system", prepared: mister.PreparedLaunch{Spec: core.Spec{System: protocol.SystemSNES, ExpectedCore: "SNES"}, AbsoluteROM: rom}, code: protocol.CodeUnsupportedSystem},
		{name: "wrong registry identity", prepared: mister.PreparedLaunch{Spec: core.Spec{System: protocol.SystemMegaDrive, ExpectedCore: "Wrong"}, AbsoluteROM: rom}, code: protocol.CodeUnsupportedSystem},
		{name: "relative cartridge", prepared: mister.PreparedLaunch{Spec: spec, AbsoluteROM: "sonic2.bin"}, code: protocol.CodeInvalidROMPath},
		{name: "FAT core smuggling", prepared: mister.PreparedLaunch{Spec: spec, AbsoluteROM: rom, RelativeROM: "/media/fat/_Console/MegaDrive.rbf"}, code: protocol.CodeInvalidROMPath},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			_, attempted, apiErr := runtime.Launch(context.Background(), test.prepared)
			if attempted || apiErr == nil || apiErr.Code != test.code {
				t.Fatalf("Launch = attempted:%t error:%#v", attempted, apiErr)
			}
			if launches, _ := control.launchCalls(); launches != 0 {
				t.Fatalf("invalid launch reached control %d times", launches)
			}
		})
	}
}

func TestNativeLaunchMapsDaemonErrorsToStableFogCastErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		remote string
		code   protocol.ErrorCode
	}{
		{remote: "invalid_request", code: protocol.CodeBadRequest},
		{remote: "unsupported_protocol", code: protocol.CodeMiSTerUnavailable},
		{remote: "unknown_system", code: protocol.CodeUnsupportedSystem},
		{remote: "missing_media", code: protocol.CodeROMNotFound},
		{remote: "busy", code: protocol.CodeBusy},
		{remote: "program_failed", code: protocol.CodeMiSTerUnavailable},
		{remote: "core_mismatch", code: protocol.CodeUnrecognizedCore},
		{remote: "io_failed", code: protocol.CodeMiSTerUnavailable},
		{remote: "idle_failed", code: protocol.CodeMiSTerUnavailable},
	}
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}
	for _, test := range cases {
		t.Run(test.remote, func(t *testing.T) {
			response := runtimeResponse("idle", "none")
			response.OK = false
			response.Error = &misterruntime.RemoteError{Code: test.remote, Message: "/private/runtime/detail"}
			control := &recordingControl{
				statuses: []misterruntime.Response{runtimeResponse("idle", "none")},
				launch:   response,
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
			if observed != "" || !attempted || apiErr == nil || apiErr.Code != test.code ||
				strings.Contains(apiErr.Message, "private") {
				t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
			}
		})
	}
}

func TestNativeLaunchRejectsEverySuccessfulResponseExceptExactMegaDriveIdentity(t *testing.T) {
	t.Parallel()
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}
	cases := []struct {
		name     string
		response misterruntime.Response
	}{
		{name: "idle", response: runtimeResponse("idle", "none")},
		{name: "starting", response: runtimeResponse("starting", "game")},
		{name: "wrong system", response: func() misterruntime.Response {
			response := runtimeResponse("running_game", "game")
			wrong := "snes"
			response.System = &wrong
			return response
		}()},
		{name: "wrong core", response: func() misterruntime.Response {
			response := runtimeResponse("running_game", "game")
			wrong := "MENU"
			response.Core = &wrong
			return response
		}()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{
				statuses: []misterruntime.Response{runtimeResponse("idle", "none")},
				launch:   test.response,
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
			if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
				t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
			}
		})
	}
}

func TestNativeLaunchReconcilesALostResponseOnlyThroughStatus(t *testing.T) {
	t.Parallel()
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}

	t.Run("running Mega Drive", func(t *testing.T) {
		control := &recordingControl{
			launchErr: errors.New("response lost after request write"),
			statuses: []misterruntime.Response{
				runtimeResponse("idle", "none"),
				runtimeResponse("running_game", "game"),
			},
		}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
		if observed != "MegaDrive" || !attempted || apiErr != nil {
			t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
		}
		statusCalls, _ := control.calls()
		launchCalls, _ := control.launchCalls()
		if statusCalls != 2 || launchCalls != 1 {
			t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
		}
	})

	t.Run("invalid terminal status", func(t *testing.T) {
		control := &recordingControl{
			launchErr: errors.New("response lost after request write"),
			statuses: []misterruntime.Response{
				runtimeResponse("idle", "none"),
				runtimeResponse("running_development", "development"),
			},
		}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
		if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
		}
		statusCalls, _ := control.calls()
		launchCalls, _ := control.launchCalls()
		if statusCalls != 2 || launchCalls != 1 {
			t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
		}
	})
}

type lostResponseSocketServer struct {
	listener    net.Listener
	cancel      context.CancelFunc
	statuses    []string
	done        chan struct{}
	stopOnce    sync.Once
	mu          sync.Mutex
	operations  []string
	statusIndex int
	serveErr    error
}

func newLostResponseSocketServer(t *testing.T, cancel context.CancelFunc, statuses ...string) *lostResponseSocketServer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mister-runtime.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &lostResponseSocketServer{
		listener: listener,
		cancel:   cancel,
		statuses: append([]string(nil), statuses...),
		done:     make(chan struct{}),
	}
	go server.serve()
	t.Cleanup(func() { server.stop(t) })
	return server
}

func (s *lostResponseSocketServer) serve() {
	defer close(s.done)
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.setServeError(err)
			}
			return
		}
		line, err := bufio.NewReader(connection).ReadString('\n')
		if err != nil {
			_ = connection.Close()
			s.setServeError(err)
			return
		}
		var request struct {
			Operation string `json:"operation"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &request); err != nil {
			_ = connection.Close()
			s.setServeError(err)
			return
		}
		s.mu.Lock()
		s.operations = append(s.operations, request.Operation)
		s.mu.Unlock()
		switch request.Operation {
		case "launch":
			s.cancel()
		case "status":
			if s.statusIndex >= len(s.statuses) {
				_ = connection.Close()
				s.setServeError(errors.New("unexpected extra status request"))
				return
			}
			_, err = io.WriteString(connection, s.statuses[s.statusIndex]+"\n")
			s.statusIndex++
		default:
			err = errors.New("unexpected runtime operation")
		}
		_ = connection.Close()
		if err != nil {
			s.setServeError(err)
			return
		}
	}
}

func (s *lostResponseSocketServer) setServeError(err error) {
	s.mu.Lock()
	if s.serveErr == nil {
		s.serveErr = err
	}
	s.mu.Unlock()
}

func (s *lostResponseSocketServer) stop(t *testing.T) []string {
	t.Helper()
	s.stopOnce.Do(func() {
		_ = s.listener.Close()
		<-s.done
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serveErr != nil {
		t.Fatalf("runtime socket fixture failed: %v", s.serveErr)
	}
	return append([]string(nil), s.operations...)
}

func TestNativeLaunchReconcilesExpiredLostResponseThroughBoundedStatusOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newLostResponseSocketServer(t, cancel,
		`{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}`,
		`{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}`,
		`{"protocol":1,"ok":true,"state":"starting","execution":"game","system":"megadrive","core":"MegaDrive","error":null,"version":"git-test"}`,
		`{"protocol":1,"ok":true,"state":"running_game","execution":"game","system":"megadrive","core":"MegaDrive","error":null,"version":"git-test"}`,
	)
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(server.listener.Addr().String()), "", time.Millisecond, 100*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}

	observed, attempted, apiErr := runtime.Launch(ctx, prepared)
	operations := server.stop(t)
	if observed != "MegaDrive" || !attempted || apiErr != nil {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v operations:%v", observed, attempted, apiErr, operations)
	}
	if got := strings.Join(operations, ","); got != "status,launch,status,status,status" {
		t.Fatalf("operations = %q, want idle admission, launch, then Status-only reconciliation", got)
	}
}

func TestNativeLaunchLostResponseProvisionalIdleReconciliationIsBounded(t *testing.T) {
	control := &recordingControl{
		launchErr: errors.New("response lost after request write"),
		statuses: []misterruntime.Response{
			runtimeResponse("idle", "none"),
			runtimeResponse("idle", "none"),
		},
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 15*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}

	started := time.Now()
	observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
	elapsed := time.Since(started)
	if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable ||
		apiErr.Message != "target runtime is unavailable" {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	statusCalls, _ := control.calls()
	launchCalls, _ := control.launchCalls()
	if launchCalls != 1 || statusCalls < 3 {
		t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
	}
	if elapsed < 5*time.Millisecond || elapsed > time.Second {
		t.Fatalf("bounded reconciliation elapsed = %s", elapsed)
	}
}

func TestNativeLaunchLostResponseStartingReconciliationIsBounded(t *testing.T) {
	control := &recordingControl{
		launchErr: errors.New("response lost after request write"),
		statuses: []misterruntime.Response{
			runtimeResponse("idle", "none"),
			runtimeResponse("starting", "game"),
		},
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 15*time.Millisecond)
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}

	started := time.Now()
	observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
	elapsed := time.Since(started)
	if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	statusCalls, _ := control.calls()
	launchCalls, _ := control.launchCalls()
	if launchCalls != 1 || statusCalls < 3 {
		t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
	}
	if elapsed < 5*time.Millisecond || elapsed > time.Second {
		t.Fatalf("bounded reconciliation elapsed = %s", elapsed)
	}
}

func TestNativeLaunchLostResponseRejectsFailedOrInvalidTerminalStatus(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}
	wrongCore := runtimeResponse("running_game", "game")
	wrong := "MENU"
	wrongCore.Core = &wrong
	for _, test := range []struct {
		name     string
		response misterruntime.Response
	}{
		{name: "failed", response: runtimeResponse("reboot_required", "none")},
		{name: "invalid identity", response: wrongCore},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{
				launchErr: errors.New("response lost after request write"),
				statuses: []misterruntime.Response{
					runtimeResponse("idle", "none"),
					test.response,
				},
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
			if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable ||
				apiErr.Message != "target runtime is unavailable" {
				t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
			}
			statusCalls, _ := control.calls()
			launchCalls, _ := control.launchCalls()
			if launchCalls != 1 || statusCalls != 2 {
				t.Fatalf("calls = launch:%d status:%d", launchCalls, statusCalls)
			}
		})
	}
}

func TestNativeLaunchRejectsAPreExistingActiveSessionBeforeDispatch(t *testing.T) {
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	prepared := mister.PreparedLaunch{Spec: spec, AbsoluteROM: writeNativeROM(t, ".bin")}
	control := &recordingControl{
		statuses:  []misterruntime.Response{runtimeResponse("running_game", "game")},
		launchErr: errors.New("busy response was lost"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)

	observed, attempted, apiErr := runtime.Launch(context.Background(), prepared)
	if observed != "" || attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable ||
		apiErr.Message != "target runtime is unavailable" {
		t.Fatalf("Launch = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	statusCalls, _ := control.calls()
	launchCalls, _ := control.launchCalls()
	if statusCalls != 1 || launchCalls != 0 {
		t.Fatalf("calls = launch:%d status:%d, want pre-existing session rejected before dispatch", launchCalls, statusCalls)
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

	t.Run("idle error result", func(t *testing.T) {
		response := runtimeResponse("idle", "none")
		response.OK = false
		response.Error = &misterruntime.RemoteError{Code: "io_failed", Message: "private idle stop detail"}
		control := &recordingControl{stop: response}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		observed, apiErr := runtime.Stop(context.Background())
		if observed != "" || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Message != "target runtime is unavailable" {
			t.Fatalf("stop = observed:%q error:%#v", observed, apiErr)
		}
		if strings.Contains(apiErr.Message, "private") {
			t.Fatalf("stop detail leaked: %#v", apiErr)
		}
	})
}

func TestNativeOwnedStopKeepsAdmissionCallerBoundAndOperationOwnerBound(t *testing.T) {
	t.Run("canceled admission does not dispatch", func(t *testing.T) {
		control := &recordingControl{stop: runtimeResponse("idle", "none")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		admission, cancel := context.WithCancel(context.Background())
		cancel()
		_, apiErr := runtime.StopOwned(admission, context.Background())
		_, stopCalls := control.calls()
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || stopCalls != 0 {
			t.Fatalf("canceled admission = error:%#v stop calls:%d", apiErr, stopCalls)
		}
	})

	t.Run("operation owner cancellation stops dispatched wait", func(t *testing.T) {
		control := &blockingOwnedStopControl{started: make(chan struct{})}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		operation, cancel := context.WithCancel(context.Background())
		done := make(chan *protocol.APIError, 1)
		go func() {
			_, apiErr := runtime.StopOwned(context.Background(), operation)
			done <- apiErr
		}()
		<-control.started
		cancel()
		select {
		case apiErr := <-done:
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
				t.Fatalf("operation cancellation error = %#v", apiErr)
			}
		case <-time.After(250 * time.Millisecond):
			t.Fatal("owned Stop ignored operation-owner cancellation")
		}
		if control.stopCalls() != 1 {
			t.Fatalf("control Stop calls = %d, want one", control.stopCalls())
		}
	})

	t.Run("fast conclusive failure remains prompt", func(t *testing.T) {
		control := &recordingControl{stopErr: errors.New("fast target failure")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
		started := time.Now()
		_, apiErr := runtime.StopOwned(context.Background(), context.Background())
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("fast failure error = %#v", apiErr)
		}
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			t.Fatalf("fast Stop failure took %s", elapsed)
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

func writeNativeROM(t *testing.T, extension string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sonic2"+extension)
	if err := os.WriteFile(path, []byte("native Mega Drive ROM fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

var _ io.Reader = (*failOnRead)(nil)
