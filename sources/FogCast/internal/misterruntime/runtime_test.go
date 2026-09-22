package misterruntime_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"

	"errors"
	"io"

	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type recordingControl struct {
	mu               sync.Mutex
	statuses         []misterruntime.Protocol2Response
	statusErr        error
	development      misterruntime.Protocol2Response
	developmentErr   error
	stop             misterruntime.Protocol2Response
	stopErr          error
	statusN          int
	developmentN     int
	stopN            int
	developmentPaths []string
}

type blockingOwnedStopControl struct {
	started chan struct{}
	once    sync.Once
	mu      sync.Mutex
	stops   int
}

func (*blockingOwnedStopControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected LoadDevelopmentRBF")
}

func (*blockingOwnedStopControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected Status")
}

func (c *blockingOwnedStopControl) Protocol2Stop(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.stops++
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return misterruntime.Protocol2Response{}, ctx.Err()
}

func (c *blockingOwnedStopControl) stopCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stops
}

func (c *recordingControl) Protocol2LoadDevelopmentRBF(ctx context.Context, path string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.developmentN++
	c.developmentPaths = append(c.developmentPaths, path)
	if c.developmentErr != nil {
		return misterruntime.Protocol2Response{}, c.developmentErr
	}
	return c.development, ctx.Err()
}

func (c *recordingControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusN++
	if c.statusErr != nil {
		return misterruntime.Protocol2Response{}, c.statusErr
	}
	if len(c.statuses) == 0 {
		return misterruntime.Protocol2Response{}, errors.New("no recorded status")
	}
	index := c.statusN - 1
	if index >= len(c.statuses) {
		index = len(c.statuses) - 1
	}
	return c.statuses[index], ctx.Err()
}

func (c *recordingControl) Protocol2Stop(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopN++
	if c.stopErr != nil {
		return misterruntime.Protocol2Response{}, c.stopErr
	}
	return c.stop, ctx.Err()
}

func (c *recordingControl) calls() (status, stop int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusN, c.stopN
}

func (c *recordingControl) developmentCalls() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.developmentN, append([]string(nil), c.developmentPaths...)
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

func runtimeResponse(state, execution string) misterruntime.Protocol2Response {
	response := misterruntime.Protocol2Response{Capabilities: misterruntime.Protocol2Capabilities{ProgrammingProfiles: []string{}, ABIs: []misterruntime.Protocol2ABI{}, ActiveInterfaces: []misterruntime.Protocol2Interface{}}, Protocol: 2, OK: true, State: state, Execution: execution, Version: "test-runtime"}
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
		generation := uint64(1)
		response.Generation = &generation
	case "reboot_required":
		response.OK = false
		response.Error = &misterruntime.Protocol2Error{Code: "idle_failed", Message: "runtime could not load idle", Phase: "recovery"}
	}
	return response
}

func retainedErrorIdleResponse() misterruntime.Protocol2Response {
	response := runtimeResponse("idle", "none")
	response.Error = &misterruntime.Protocol2Error{Code: "io_failed", Message: "private prior failure", Phase: "recovery"}
	return response
}

func TestNativeHealthIsReadyOnlyForControllableProductionStatesAndKeepsLegacyBooleansFalse(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response misterruntime.Protocol2Response
		ready    bool
	}{
		{name: "idle", response: runtimeResponse("idle", "none"), ready: true},
		{name: "idle with retained prior error", response: retainedErrorIdleResponse(), ready: true},
		{name: "idle with malformed retained error", response: func() misterruntime.Protocol2Response {
			response := retainedErrorIdleResponse()
			response.Error.Code = "unknown"
			return response
		}(), ready: false},
		{name: "starting", response: runtimeResponse("starting", "none"), ready: false},
		{name: "running Mega Drive", response: runtimeResponse("running_game", "game"), ready: false},
		{name: "running wrong core", response: func() misterruntime.Protocol2Response {
			response := runtimeResponse("running_game", "game")
			wrong := "MENU"
			response.Core = &wrong
			return response
		}(), ready: false},
		{name: "running development", response: runtimeResponse("running_development", "development"), ready: false},
		{name: "reboot required", response: runtimeResponse("reboot_required", "none"), ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{test.response}}
			runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "missing-boot-id"), time.Millisecond, time.Second)
			health := runtime.Health("agent-test")
			if health.APIVersion != "v1" || health.AgentVersion != "agent-test" || health.Ready != test.ready {
				t.Fatalf("health = %#v", health)
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

func TestNativeStopReadinessAcceptsIdleExactMegaDriveOrDevelopment(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response misterruntime.Protocol2Response
		ready    bool
	}{
		{name: "idle", response: runtimeResponse("idle", "none"), ready: true},
		{name: "idle with retained prior error", response: retainedErrorIdleResponse(), ready: true},
		{name: "idle with malformed retained error", response: func() misterruntime.Protocol2Response {
			response := retainedErrorIdleResponse()
			response.Error.Code = "unknown"
			return response
		}(), ready: false},
		{name: "running Mega Drive", response: runtimeResponse("running_game", "game"), ready: false},
		{name: "operation failed idle", response: func() misterruntime.Protocol2Response {
			response := retainedErrorIdleResponse()
			response.OK = false
			return response
		}(), ready: false},
		{name: "starting", response: runtimeResponse("starting", "game"), ready: false},
		{name: "running wrong core", response: func() misterruntime.Protocol2Response {
			response := runtimeResponse("running_game", "game")
			wrong := "MENU"
			response.Core = &wrong
			return response
		}(), ready: false},
		{name: "running development", response: runtimeResponse("running_development", "development"), ready: true},
		{name: "reboot required", response: runtimeResponse("reboot_required", "none"), ready: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{test.response}}
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
	response.Error = &misterruntime.Protocol2Error{Code: "io_failed", Message: "private runtime detail"}
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	health := runtime.Health("agent-test")
	if health.Ready || health.APIVersion != "v1" || health.AgentVersion != "agent-test" {
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

func (c *blockingHealthControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.statusCalls++
	_, c.hadDeadline = ctx.Deadline()
	<-ctx.Done()
	return misterruntime.Protocol2Response{}, ctx.Err()
}

func (*blockingHealthControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
}

func (*blockingHealthControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
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
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{runtimeResponse("idle", "none")}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	if status.State != protocol.StateIdle || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.Development || status.Recovery != "" || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
}

func TestNativeReconcilePreservesRetainedErrorFromOperationalIdle(t *testing.T) {
	t.Parallel()
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{retainedErrorIdleResponse()}}
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
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{response}}
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
			response.Error = &misterruntime.Protocol2Error{Code: "io_failed", Message: "private runtime detail"}
			if state == "starting" {
				system, coreName := "megadrive", "MegaDrive"
				response.System, response.Core = &system, &coreName
			}
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{response}}
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
	response.Error = &misterruntime.Protocol2Error{Code: "idle_failed", Message: "private hardware detail"}
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	status := runtime.Reconcile(context.Background())
	assertUnavailableStatus(t, status)
	if strings.Contains(status.LastError.Message, "private") || strings.Contains(status.LastError.Message, "hardware") {
		t.Fatalf("runtime detail leaked: %#v", status.LastError)
	}
}

func TestNativeReconcileWaitsThroughStartingAndHonorsContext(t *testing.T) {
	t.Run("starting", func(t *testing.T) {
		control := &recordingControl{statuses: []misterruntime.Protocol2Response{runtimeResponse("starting", "none")}}
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
	for _, state := range []string{"running_game"} {
		t.Run(state, func(t *testing.T) {
			execution := "game"
			if state == "running_development" {
				execution = "development"
			}
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{runtimeResponse(state, execution)}}
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

type failOnRead struct {
	reads int
}

type cancelingDevelopmentBody struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelingDevelopmentBody) Read(buffer []byte) (int, error) {
	r.reads++
	if r.reads == 1 {
		buffer[0] = 'r'
		r.cancel()
		return 1, nil
	}
	copy(buffer, "bf")
	return 2, io.EOF
}

func (r *failOnRead) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("body must not be read")
}

func TestNativeDevelopmentStagesBeforeOneIdleAdmissionAndOneDispatch(t *testing.T) {
	stagedPath := filepath.Join(t.TempDir(), "development", "core.rbf")
	response := runtimeResponse("running_development", "development")
	control := &recordingControl{
		statuses:    []misterruntime.Protocol2Response{runtimeResponse("idle", "none")},
		development: response,
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 25*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(stagedPath))
	payload := []byte("exact development rbf")

	observed, attempted, apiErr := runtime.LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if observed != "" || !attempted || apiErr != nil {
		t.Fatalf("development load = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	staged, err := os.ReadFile(stagedPath)
	if err != nil || !bytes.Equal(staged, payload) {
		t.Fatalf("staged development RBF = %q, error %v", staged, err)
	}
	statusCalls, stopCalls := control.calls()
	developmentCalls, paths := control.developmentCalls()
	if statusCalls != 1 || developmentCalls != 1 || len(paths) != 1 || paths[0] != stagedPath || stopCalls != 0 {
		t.Fatalf("calls = status:%d development:%d paths:%v stop:%d", statusCalls, developmentCalls, paths, stopCalls)
	}
}

func TestNativeDevelopmentSuccessfulProvisionalResponsesEnterStatusObservation(t *testing.T) {
	for _, test := range []struct {
		name     string
		response misterruntime.Protocol2Response
	}{
		{name: "starting development", response: runtimeResponse("starting", "development")},
		{name: "clean idle", response: runtimeResponse("idle", "none")},
	} {
		t.Run(test.name, func(t *testing.T) {
			terminal := runtimeResponse("running_development", "development")
			control := &recordingControl{
				statuses:    []misterruntime.Protocol2Response{runtimeResponse("idle", "none"), terminal},
				development: test.response,
			}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
				misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

			observed, attempted, apiErr := runtime.LoadDevelopmentRBF(
				context.Background(), 3, bytes.NewReader([]byte("rbf")))
			if observed != "" || !attempted || apiErr != nil {
				t.Fatalf("development load = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
			}
			statusCalls, _ := control.calls()
			developmentCalls, _ := control.developmentCalls()
			if statusCalls != 2 || developmentCalls != 1 {
				t.Fatalf("calls = status:%d development:%d, want one dispatch and Status-only observation", statusCalls, developmentCalls)
			}
		})
	}
}

func TestNativeDevelopmentRejectsBeforeReadingOrDialing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		path   string
		size   int64
		cancel bool
	}{
		{name: "empty path", path: "", size: 3},
		{name: "relative path", path: "core.rbf", size: 3},
		{name: "unclean path", path: "/tmp/dir/../core.rbf", size: 3},
		{name: "NUL path", path: "/tmp/core.rbf\x00ignored", size: 3},
		{name: "zero size", path: "/tmp/core.rbf", size: 0},
		{name: "too large", path: "/tmp/core.rbf", size: protocol.MaxDevelopmentRBFBytes + 1},
		{name: "canceled admission", path: "/tmp/core.rbf", size: 3, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
				misterruntime.WithDevelopmentRBFPath(test.path))
			body := &failOnRead{}
			ctx, cancel := context.WithCancel(context.Background())
			if test.cancel {
				cancel()
			} else {
				defer cancel()
			}
			_, attempted, apiErr := runtime.LoadDevelopmentRBF(ctx, test.size, body)
			if attempted || apiErr == nil || body.reads != 0 {
				t.Fatalf("development load = attempted:%t error:%#v reads:%d", attempted, apiErr, body.reads)
			}
			statusCalls, stopCalls := control.calls()
			developmentCalls, _ := control.developmentCalls()
			if statusCalls != 0 || developmentCalls != 0 || stopCalls != 0 {
				t.Fatalf("control calls = status:%d development:%d stop:%d", statusCalls, developmentCalls, stopCalls)
			}
		})
	}
}

func TestNativeDevelopmentCancellationDuringStagingStopsBeforeAdmission(t *testing.T) {
	control := &recordingControl{}
	stagedPath := filepath.Join(t.TempDir(), "core.rbf")
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithDevelopmentRBFPath(stagedPath))
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelingDevelopmentBody{cancel: cancel}

	_, attempted, apiErr := runtime.LoadDevelopmentRBF(ctx, 3, body)
	if attempted || apiErr == nil || body.reads != 1 {
		t.Fatalf("canceled staging = attempted:%t error:%#v reads:%d", attempted, apiErr, body.reads)
	}
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Fatalf("canceled staging installed a final file: %v", err)
	}
	statusCalls, _ := control.calls()
	developmentCalls, _ := control.developmentCalls()
	if statusCalls != 0 || developmentCalls != 0 {
		t.Fatalf("canceled staging called control: status:%d development:%d", statusCalls, developmentCalls)
	}
}

func TestNativeDevelopmentReconcilesLostResponseThroughStatusOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newLostResponseSocketServer(t, cancel,
		`{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`,
		`{"protocol":2,"ok":true,"state":"starting","execution":"development","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`,
		`{"protocol":2,"ok":true,"state":"running_development","execution":"development","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":1,"inspected_package":null}`,
	)
	stagedPath := filepath.Join(t.TempDir(), "core.rbf")
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(server.listener.Addr().String()), "", time.Millisecond, 100*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(stagedPath))

	observed, attempted, apiErr := runtime.LoadDevelopmentRBF(ctx, 3, bytes.NewReader([]byte("rbf")))
	operations := server.stop(t)
	if observed != "" || !attempted || apiErr != nil {
		t.Fatalf("development load = observed:%q attempted:%t error:%#v operations:%v", observed, attempted, apiErr, operations)
	}
	if got := strings.Join(operations, ","); got != "status,load_development_rbf,status,status" {
		t.Fatalf("operations = %q, want one dispatch followed by Status only", got)
	}
}

func TestNativeDevelopmentLostResponseRetainedErrorIdleIsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := newLostResponseSocketServer(t, cancel,
		`{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`,
		`{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"io_failed","message":"private primary error","phase":"admission"},"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`,
	)
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(server.listener.Addr().String()), "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

	observed, attempted, apiErr := runtime.LoadDevelopmentRBF(ctx, 3, bytes.NewReader([]byte("rbf")))
	operations := server.stop(t)
	if observed != "" || !attempted || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("development load = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	if got := strings.Join(operations, ","); got != "status,load_development_rbf,status" {
		t.Fatalf("operations = %q, want retained-error Status terminal", got)
	}
}

func TestNativeOwnedDevelopmentLoadPreservesRebootRequiredRecoveryMarker(t *testing.T) {
	t.Parallel()
	response := runtimeResponse("reboot_required", "none")
	control := &recordingControl{
		statuses:    []misterruntime.Protocol2Response{runtimeResponse("idle", "none")},
		development: response,
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	type recoveryLoader interface {
		LoadDevelopmentRBFOwnedWithRecovery(context.Context, context.Context, context.Context, int64, io.Reader) (string, string, bool, *protocol.APIError)
	}
	loader, ok := any(runtime).(recoveryLoader)
	if !ok {
		t.Fatal("native runtime does not expose owned development recovery result")
	}

	observed, recovery, attempted, apiErr := loader.LoadDevelopmentRBFOwnedWithRecovery(
		context.Background(), context.Background(), context.Background(), 3, bytes.NewReader([]byte("rbf")))
	if observed != "" || recovery != protocol.RecoveryRebootRequired || !attempted ||
		apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("owned development load = observed:%q recovery:%q attempted:%t error:%#v",
			observed, recovery, attempted, apiErr)
	}
	statusCalls, stopCalls := control.calls()
	developmentCalls, _ := control.developmentCalls()
	if statusCalls != 1 || developmentCalls != 1 || stopCalls != 0 {
		t.Fatalf("control calls = status:%d development:%d stop:%d, want one admission and one dispatch",
			statusCalls, developmentCalls, stopCalls)
	}
}

type ownedDevelopmentContextControl struct {
	mu                 sync.Mutex
	statusCalls        int
	statusHadDeadlines []bool
	developmentCalls   int
	developmentStarted chan struct{}
	observation        context.Context
	terminal           misterruntime.Protocol2Response
}

type admittedDevelopmentControl struct {
	started chan struct{}
	release chan struct{}
}

type gatedDevelopmentBody struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	sent    bool
}

func (r *gatedDevelopmentBody) Read(buffer []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.once.Do(func() { close(r.started) })
	<-r.release
	copy(buffer, "rbf")
	r.sent = true
	return 3, io.EOF
}

type successfulProvisionalDevelopmentControl struct {
	mu                 sync.Mutex
	statusCalls        int
	developmentCalls   int
	statusHadDeadlines []bool
	observation        context.Context
	response           misterruntime.Protocol2Response
}

func (c *successfulProvisionalDevelopmentControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.statusCalls++
	call := c.statusCalls
	_, hadDeadline := ctx.Deadline()
	c.statusHadDeadlines = append(c.statusHadDeadlines, hadDeadline)
	c.mu.Unlock()
	if call == 1 {
		return runtimeResponse("idle", "none"), ctx.Err()
	}
	terminal := runtimeResponse("running_development", "development")
	return terminal, ctx.Err()
}

func (c *successfulProvisionalDevelopmentControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.developmentCalls++
	c.mu.Unlock()
	if c.observation != nil {
		<-c.observation.Done()
	}
	return c.response, nil
}

func (*successfulProvisionalDevelopmentControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected Stop")
}

func (*admittedDevelopmentControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	return runtimeResponse("idle", "none"), ctx.Err()
}

func (c *admittedDevelopmentControl) Protocol2LoadDevelopmentRBF(ctx context.Context, _ string) (misterruntime.Protocol2Response, error) {
	close(c.started)
	select {
	case <-ctx.Done():
		return misterruntime.Protocol2Response{}, ctx.Err()
	case <-c.release:
		response := runtimeResponse("running_development", "development")
		return response, nil
	}
}

func (*admittedDevelopmentControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected Stop")
}

func (c *ownedDevelopmentContextControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.statusCalls++
	call := c.statusCalls
	_, hadDeadline := ctx.Deadline()
	c.statusHadDeadlines = append(c.statusHadDeadlines, hadDeadline)
	c.mu.Unlock()
	if call == 1 {
		return runtimeResponse("idle", "none"), ctx.Err()
	}
	return c.terminal, ctx.Err()
}

func (c *ownedDevelopmentContextControl) Protocol2LoadDevelopmentRBF(ctx context.Context, _ string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.developmentCalls++
	c.mu.Unlock()
	close(c.developmentStarted)
	<-c.observation.Done()
	if ctx.Err() != nil {
		return misterruntime.Protocol2Response{}, ctx.Err()
	}
	return misterruntime.Protocol2Response{}, errors.New("response lost after mutation")
}

func (*ownedDevelopmentContextControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected Stop")
}

func TestNativeOwnedDevelopmentUsesFreshProcessBoundWindowAfterObservationExpires(t *testing.T) {
	observation, cancelObservation := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelObservation()
	terminal := runtimeResponse("running_development", "development")
	control := &ownedDevelopmentContextControl{
		developmentStarted: make(chan struct{}), observation: observation, terminal: terminal,
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

	observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
		context.Background(), observation, context.Background(), 3, bytes.NewReader([]byte("rbf")))
	if observed != "" || !attempted || apiErr != nil {
		t.Fatalf("owned development load = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.developmentCalls != 1 || control.statusCalls != 2 ||
		len(control.statusHadDeadlines) != 2 || !control.statusHadDeadlines[0] || !control.statusHadDeadlines[1] {
		t.Fatalf("calls = development:%d status:%d deadlines:%v", control.developmentCalls, control.statusCalls, control.statusHadDeadlines)
	}
}

func TestNativeOwnedDevelopmentDispatchesAfterStagingConsumesObservationWindow(t *testing.T) {
	observation, cancelObservation := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelObservation()
	response := runtimeResponse("running_development", "development")
	control := &recordingControl{
		statuses:    []misterruntime.Protocol2Response{runtimeResponse("idle", "none")},
		development: response,
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	body := &gatedDevelopmentBody{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct {
		observed  string
		attempted bool
		apiErr    *protocol.APIError
	}, 1)
	go func() {
		observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
			context.Background(), observation, context.Background(), 3, body)
		done <- struct {
			observed  string
			attempted bool
			apiErr    *protocol.APIError
		}{observed: observed, attempted: attempted, apiErr: apiErr}
	}()
	<-body.started
	<-observation.Done()
	close(body.release)

	result := <-done
	if result.observed != "" || !result.attempted || result.apiErr != nil {
		t.Fatalf("owned development after staging deadline = observed:%q attempted:%t error:%#v",
			result.observed, result.attempted, result.apiErr)
	}
	statusCalls, _ := control.calls()
	developmentCalls, _ := control.developmentCalls()
	if statusCalls != 1 || developmentCalls != 1 {
		t.Fatalf("calls = status:%d development:%d, want one admission and one dispatch", statusCalls, developmentCalls)
	}
}

func TestNativeOwnedDevelopmentProvisionalAfterStagingObservationExpiryUsesFreshProcessWindow(t *testing.T) {
	observation, cancelObservation := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelObservation()
	control := &successfulProvisionalDevelopmentControl{
		observation: observation,
		response:    runtimeResponse("idle", "none"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	body := &gatedDevelopmentBody{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct {
		observed  string
		attempted bool
		apiErr    *protocol.APIError
	}, 1)
	go func() {
		observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
			context.Background(), observation, context.Background(), 3, body)
		done <- struct {
			observed  string
			attempted bool
			apiErr    *protocol.APIError
		}{observed: observed, attempted: attempted, apiErr: apiErr}
	}()
	<-body.started
	<-observation.Done()
	close(body.release)

	result := <-done
	if result.observed != "" || !result.attempted || result.apiErr != nil {
		t.Fatalf("owned provisional development after staging deadline = observed:%q attempted:%t error:%#v",
			result.observed, result.attempted, result.apiErr)
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.developmentCalls != 1 || control.statusCalls != 2 ||
		len(control.statusHadDeadlines) != 2 || !control.statusHadDeadlines[0] || !control.statusHadDeadlines[1] {
		t.Fatalf("calls = development:%d status:%d deadlines:%v, want one dispatch and bounded admission/reconciliation",
			control.developmentCalls, control.statusCalls, control.statusHadDeadlines)
	}
}

func TestNativeOwnedDevelopmentSuccessfulProvisionalResponsesUseStatusOnly(t *testing.T) {
	t.Run("starting uses live observation", func(t *testing.T) {
		control := &successfulProvisionalDevelopmentControl{response: runtimeResponse("starting", "development")}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
			misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

		observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
			context.Background(), context.Background(), context.Background(), 3, bytes.NewReader([]byte("rbf")))
		if observed != "" || !attempted || apiErr != nil {
			t.Fatalf("owned development = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
		}
		control.mu.Lock()
		defer control.mu.Unlock()
		if control.developmentCalls != 1 || control.statusCalls != 2 {
			t.Fatalf("calls = development:%d status:%d", control.developmentCalls, control.statusCalls)
		}
	})

	t.Run("clean idle after exhausted observation uses fresh process window", func(t *testing.T) {
		observation, cancelObservation := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancelObservation()
		control := &successfulProvisionalDevelopmentControl{
			observation: observation,
			response:    runtimeResponse("idle", "none"),
		}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
			misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

		observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
			context.Background(), observation, context.Background(), 3, bytes.NewReader([]byte("rbf")))
		if observed != "" || !attempted || apiErr != nil {
			t.Fatalf("owned development = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
		}
		control.mu.Lock()
		defer control.mu.Unlock()
		if control.developmentCalls != 1 || control.statusCalls != 2 ||
			len(control.statusHadDeadlines) != 2 || !control.statusHadDeadlines[0] || !control.statusHadDeadlines[1] {
			t.Fatalf("calls = development:%d status:%d deadlines:%v", control.developmentCalls, control.statusCalls, control.statusHadDeadlines)
		}
	})
}

func TestNativeOwnedDevelopmentSurvivesCallerCancellationAfterAdmission(t *testing.T) {
	control := &admittedDevelopmentControl{started: make(chan struct{}), release: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 50*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	admission, cancelAdmission := context.WithCancel(context.Background())
	done := make(chan struct {
		observed  string
		attempted bool
		apiErr    *protocol.APIError
	}, 1)
	go func() {
		observed, attempted, apiErr := runtime.LoadDevelopmentRBFOwned(
			admission, context.Background(), context.Background(), 3, bytes.NewReader([]byte("rbf")))
		done <- struct {
			observed  string
			attempted bool
			apiErr    *protocol.APIError
		}{observed: observed, attempted: attempted, apiErr: apiErr}
	}()
	<-control.started
	cancelAdmission()
	close(control.release)
	result := <-done
	if result.observed != "" || !result.attempted || result.apiErr != nil {
		t.Fatalf("owned development = observed:%q attempted:%t error:%#v", result.observed, result.attempted, result.apiErr)
	}
}

func TestNativeOwnedDevelopmentStopsWhenProcessOwnerIsCanceled(t *testing.T) {
	observation, cancelObservation := context.WithTimeout(context.Background(), time.Second)
	defer cancelObservation()
	operationOwner, cancelOwner := context.WithCancel(context.Background())
	control := &ownedDevelopmentContextControl{
		developmentStarted: make(chan struct{}), observation: operationOwner,
		terminal: runtimeResponse("running_development", "development"),
	}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 25*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	done := make(chan *protocol.APIError, 1)
	go func() {
		_, _, apiErr := runtime.LoadDevelopmentRBFOwned(context.Background(), observation, operationOwner, 3, bytes.NewReader([]byte("rbf")))
		done <- apiErr
	}()
	<-control.developmentStarted
	cancelOwner()
	select {
	case apiErr := <-done:
		if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
			t.Fatalf("process cancellation error = %#v", apiErr)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("owned development ignored process cancellation")
	}
}

func TestNativeReconcileReconstructsDevelopmentWithoutReplay(t *testing.T) {
	server := newLostResponseSocketServer(t, func() {},
		`{"protocol":2,"ok":true,"state":"running_development","execution":"development","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":1,"inspected_package":null}`,
	)
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(server.listener.Addr().String()), "", time.Millisecond, time.Second,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))

	status := runtime.Reconcile(context.Background())
	operations := server.stop(t)
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore != nil || status.LastError != nil {
		t.Fatalf("reconciled status = %#v", status)
	}
	if got := strings.Join(operations, ","); got != "status" {
		t.Fatalf("agent restart operations = %q, want one protocol-2 Status", got)
	}
}

func TestNativeReconcileTreatsRuntimeRestartIdleAsIdleWithoutReplay(t *testing.T) {
	server := newLostResponseSocketServer(t, func() {},
		`{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}`,
	)
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(server.listener.Addr().String()), "", time.Millisecond, time.Second,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	status := runtime.Reconcile(context.Background())
	operations := server.stop(t)
	if status.State != protocol.StateIdle || status.Development || strings.Join(operations, ",") != "status" {
		t.Fatalf("runtime-restart status = %#v, operations = %v", status, operations)
	}
}

func TestNativeDevelopmentRejectsWrongIdentityBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response misterruntime.Protocol2Response
	}{
		{name: "non-null system", response: func() misterruntime.Protocol2Response {
			response := runtimeResponse("running_development", "development")
			system := "megadrive"
			response.System = &system
			return response
		}()},
		{name: "empty core", response: func() misterruntime.Protocol2Response {
			response := runtimeResponse("running_development", "development")
			empty := ""
			response.Core = &empty
			return response
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{test.response}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
				misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
			status := runtime.Reconcile(context.Background())
			assertUnavailableStatus(t, status)
			if runtime.StopReady() {
				t.Fatal("wrong development identity admitted Stop")
			}
		})
	}
}

func TestNativeReconcileRejectsDevelopmentStartingWithCoreIdentity(t *testing.T) {
	response := runtimeResponse("starting", "development")
	coreName := "invalid"
	response.Core = &coreName
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{response}}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithDevelopmentRBFPath(filepath.Join(t.TempDir(), "core.rbf")))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	status := runtime.Reconcile(ctx)
	assertUnavailableStatus(t, status)
	statusCalls, _ := control.calls()
	if statusCalls != 1 {
		t.Fatalf("malformed development starting used %d Status calls, want one conclusive rejection", statusCalls)
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
		response.Error = &misterruntime.Protocol2Error{Code: "io_failed", Message: "private idle stop detail"}
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

func TestNativeOwnedStopPropagatesRebootRequiredRecovery(t *testing.T) {
	t.Parallel()
	control := &recordingControl{stop: runtimeResponse("reboot_required", "none")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)

	observed, recovery, apiErr := runtime.StopOwnedWithRecovery(context.Background(), context.Background())
	if observed != "" || recovery != protocol.RecoveryRebootRequired || apiErr != nil {
		t.Fatalf("owned stop = observed:%q recovery:%q error:%#v", observed, recovery, apiErr)
	}
	statusCalls, stopCalls := control.calls()
	if statusCalls != 0 || stopCalls != 1 {
		t.Fatalf("control calls = status:%d stop:%d", statusCalls, stopCalls)
	}
}

type recoverIdleControl struct {
	recordingControl
	recover    misterruntime.Protocol2Response
	recoverErr error
	recoverN   int
}

func (c *recoverIdleControl) Protocol2RecoverIdle(context.Context) (misterruntime.Protocol2Response, error) {
	c.recoverN++
	if c.recoverErr != nil {
		return misterruntime.Protocol2Response{}, c.recoverErr
	}
	return c.recover, nil
}

func TestRecoverIdleReportsProgrammedIdle(t *testing.T) {
	t.Parallel()
	control := &recoverIdleControl{recover: runtimeResponse("idle", "none")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	idle, apiErr := runtime.RecoverIdle(context.Background())
	if !idle || apiErr != nil || control.recoverN != 1 {
		t.Fatalf("idle=%t err=%#v calls=%d", idle, apiErr, control.recoverN)
	}
}

func TestRecoverIdleSurfacesIdleProgramFailure(t *testing.T) {
	t.Parallel()
	control := &recoverIdleControl{recover: runtimeResponse("reboot_required", "none")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	idle, apiErr := runtime.RecoverIdle(context.Background())
	if idle || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "recovery" {
		t.Fatalf("idle=%t err=%#v", idle, apiErr)
	}
}

func TestRecoverIdleMapsMissingOperationToUnsupported(t *testing.T) {
	t.Parallel()
	const caps = `"capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null`
	for _, test := range []struct {
		name string
		body string
		code protocol.ErrorCode
	}{
		{
			name: "reboot required unknown operation",
			body: `{"protocol":2,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":{"code":"invalid_request","message":"unknown operation","phase":"request"},"version":"old",` + caps + `}`,
			code: protocol.CodeUnsupportedOperation,
		},
		{
			name: "idle invalid request",
			body: `{"protocol":2,"ok":false,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"invalid_request","message":"request contains an unknown field","phase":"request"},"version":"old",` + caps + `}`,
			code: protocol.CodeUnsupportedOperation,
		},
		{
			name: "unknown operation code",
			body: `{"protocol":2,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":{"code":"unknown_operation","message":"unknown operation","phase":"request"},"version":"old",` + caps + `}`,
			code: protocol.CodeUnsupportedOperation,
		},
		{
			name: "busy stays fail closed",
			body: `{"protocol":2,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":{"code":"busy","message":"runtime mutation is busy","phase":"request"},"version":"old",` + caps + `}`,
			code: protocol.CodeMiSTerUnavailable,
		},
		{
			name: "unexpected failure stays fail closed",
			body: `{"protocol":2,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":{"code":"io_failed","message":"private detail","phase":"lifecycle"},"version":"old",` + caps + `}`,
			code: protocol.CodeMiSTerUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				_, _ = bufio.NewReader(conn).ReadBytes('\n')
				_, _ = conn.Write(append([]byte(test.body), '\n'))
			}()
			runtime := misterruntime.NewRuntime(misterruntime.NewClient(path), "", time.Millisecond, time.Second)
			idle, apiErr := runtime.RecoverIdle(context.Background())
			if idle || apiErr == nil || apiErr.Code != test.code {
				t.Fatalf("idle=%t err=%#v want %s", idle, apiErr, test.code)
			}
			if test.code == protocol.CodeUnsupportedOperation && apiErr.Message != "requested operation is unsupported" {
				t.Fatalf("unsupported message = %q", apiErr.Message)
			}
			if test.code == protocol.CodeMiSTerUnavailable && strings.Contains(apiErr.Message, "private") {
				t.Fatalf("failure detail leaked: %#v", apiErr)
			}
		})
	}
}

func TestRecoverIdleTransportFailureStaysUnavailable(t *testing.T) {
	t.Parallel()
	control := &recoverIdleControl{recoverErr: errors.New("dial unix: connection refused")}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
	idle, apiErr := runtime.RecoverIdle(context.Background())
	if idle || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("idle=%t err=%#v", idle, apiErr)
	}
	runtime = misterruntime.NewRuntime(misterruntime.NewClient(filepath.Join(t.TempDir(), "missing.sock")), "", time.Millisecond, time.Second)
	idle, apiErr = runtime.RecoverIdle(context.Background())
	if idle || apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("missing socket idle=%t err=%#v", idle, apiErr)
	}
}

func TestRuntimeSocketOpenIsFalseWhenDialFails(t *testing.T) {
	t.Parallel()
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(filepath.Join(t.TempDir(), "missing.sock")), "", time.Millisecond, time.Second)
	if runtime.RuntimeSocketOpen(context.Background()) {
		t.Fatal("missing runtime socket reported open")
	}
}

func TestNativeDevelopmentRecoveryRequiresConfiguredExecutable(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		path func(string) string
	}{
		{name: "missing", path: func(dir string) string { return filepath.Join(dir, "missing-reboot") }},
		{name: "directory", path: func(dir string) string { return dir }},
		{name: "not executable", path: func(dir string) string { return filepath.Join(dir, "reboot") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := test.path(dir)
			if test.name == "not executable" {
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runtime := misterruntime.NewRuntime(nil, "", time.Millisecond, time.Second,
				misterruntime.WithRebootCommand(path))

			_, apiErr := runtime.RecoverDevelopment(context.Background())
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
				t.Fatalf("recovery error = %#v", apiErr)
			}
		})
	}
}

func TestNativeDevelopmentRecoveryHonorsCanceledContextBeforeStartingCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	reboot := filepath.Join(dir, "reboot")
	script := "#!/bin/sh\n: > '" + marker + "'\n"
	if err := os.WriteFile(reboot, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runtime := misterruntime.NewRuntime(nil, "", time.Millisecond, time.Second,
		misterruntime.WithRebootCommand(reboot))

	_, apiErr := runtime.RecoverDevelopment(ctx)
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("canceled recovery error = %#v", apiErr)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("canceled recovery started command: %v", err)
	}
}

func TestNativeDevelopmentRecoveryStartsConfiguredCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	reboot := filepath.Join(dir, "reboot")
	script := "#!/bin/sh\n: > '" + marker + "'\n"
	if err := os.WriteFile(reboot, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := misterruntime.NewRuntime(nil, "", time.Millisecond, time.Second,
		misterruntime.WithRebootCommand(reboot))

	observed, apiErr := runtime.RecoverDevelopment(context.Background())
	if observed != "" || apiErr != nil {
		t.Fatalf("recovery = observed:%q error:%#v", observed, apiErr)
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("configured recovery command was not started")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNativeDevelopmentRecoverySurvivesCallerCancellationAfterStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "finished")
	reboot := filepath.Join(dir, "reboot")
	script := "#!/bin/sh\nsleep 0.05\n: > '" + marker + "'\n"
	if err := os.WriteFile(reboot, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime := misterruntime.NewRuntime(nil, "", time.Millisecond, time.Second,
		misterruntime.WithRebootCommand(reboot))
	if _, apiErr := runtime.RecoverDevelopment(ctx); apiErr != nil {
		t.Fatalf("recovery error = %#v", apiErr)
	}
	cancel()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery command did not survive caller cancellation")
		}
		time.Sleep(5 * time.Millisecond)
	}
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
	control := &recordingControl{statuses: []misterruntime.Protocol2Response{runtimeResponse("idle", "none")}}
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
		case "launch", "load_development_rbf":
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

type delayedStartupControl struct {
	recordingControl
	readyAt time.Time
}

func (c *delayedStartupControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	if _, ok := ctx.Deadline(); !ok {
		return misterruntime.Protocol2Response{}, errors.New("status observation is unbounded")
	}
	if time.Now().Before(c.readyAt) {
		return runtimeResponse("starting", "none"), nil
	}
	return runtimeResponse("idle", "none"), nil
}
func TestStartupReconciliationUsesCallerBudgetBeyondHealthPoll(t *testing.T) {
	control := &delayedStartupControl{readyAt: time.Now().Add(30 * time.Millisecond)}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 5*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if status := runtime.Reconcile(ctx); status.State != protocol.StateIdle {
		t.Fatalf("startup cut short by health timeout: %+v", status)
	}
}
