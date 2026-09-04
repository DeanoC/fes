package misterruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/protocol"
)

const unavailableMessage = "target runtime is unavailable"
const unsupportedOperationMessage = "requested operation is unsupported"
const unsupportedSystemMessage = "system is unsupported by target runtime"

type Runtime struct {
	control       Control
	bootIDFile    string
	pollInterval  time.Duration
	healthTimeout time.Duration
}

func NewRuntime(control Control, bootIDFile string, pollInterval, healthTimeout time.Duration) *Runtime {
	return &Runtime{control: control, bootIDFile: bootIDFile, pollInterval: pollInterval, healthTimeout: healthTimeout}
}

func (r *Runtime) Health(version string) protocol.Health {
	health := protocol.Health{APIVersion: "v1", AgentVersion: version}
	if bootID, err := os.ReadFile(r.bootIDFile); err == nil {
		health.BootID = strings.TrimSpace(string(bootID))
	}
	response, err := r.boundedStatus(context.Background())
	health.Ready = err == nil && validIdle(response)
	return health
}

func (r *Runtime) StopReady() bool {
	response, err := r.boundedStatus(context.Background())
	return err == nil && (validIdle(response) || validMegaDriveRunning(response))
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	for {
		response, err := r.control.Status(ctx)
		if err == nil {
			if validIdle(response) {
				status := protocol.Status{State: protocol.StateIdle}
				if response.Error != nil {
					status.LastError = mapRemoteError(response.Error)
				}
				return status
			}
			if !response.OK || response.State != "starting" {
				return unavailableStatus()
			}
		} else if !errors.Is(err, errRuntimeConnection) {
			return unavailableStatus()
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return unavailableStatus()
		}
	}
}

func (r *Runtime) Prepare(spec core.Spec, candidate string) (mister.PreparedLaunch, *protocol.APIError) {
	if !validMegaDriveSpec(spec) {
		return mister.PreparedLaunch{}, unsupportedSystemError()
	}
	rom, apiErr := validateNativeROM(candidate)
	if apiErr != nil {
		return mister.PreparedLaunch{}, apiErr
	}
	return mister.PreparedLaunch{Spec: spec, AbsoluteROM: rom}, nil
}

func (r *Runtime) Launch(ctx context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return r.launch(ctx, ctx, ctx, prepared, false)
}

// LaunchOwned keeps admission caller-bound, then transfers the sole dispatched
// mutation to the agent process owner. The daemon owns its phase deadlines;
// the separately bounded observation context limits only lost-response Status
// observation. Process shutdown still cancels both mutation and observation.
func (r *Runtime) LaunchOwned(admission, observation, operationOwner context.Context, prepared mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return r.launch(admission, observation, operationOwner, prepared, true)
}

func (r *Runtime) launch(admission, observation, operationOwner context.Context, prepared mister.PreparedLaunch, owned bool) (string, bool, *protocol.APIError) {
	if !validMegaDriveSpec(prepared.Spec) {
		return "", false, unsupportedSystemError()
	}
	if prepared.RelativeROM != "" || len(prepared.MGL) != 0 {
		return "", false, invalidROMPathError()
	}
	rom, apiErr := validateNativeROM(prepared.AbsoluteROM)
	if apiErr != nil || rom != prepared.AbsoluteROM {
		if apiErr != nil {
			return "", false, apiErr
		}
		return "", false, invalidROMPathError()
	}
	response, err := r.boundedStatus(admission)
	if err != nil || !validIdle(response) {
		return "", false, unavailableError()
	}
	if admission.Err() != nil || observation.Err() != nil || (owned && operationOwner.Err() != nil) {
		return "", false, unavailableError()
	}
	request := LaunchRequest{
		System: "megadrive",
		RBF:    megaDriveRBFPath,
		Media: map[string]string{
			"cartridge": rom,
		},
		Settings: map[string]string{},
	}
	launchContext := observation
	if owned {
		launchContext = operationOwner
	}
	response, err = r.control.Launch(launchContext, request)
	if err != nil {
		var reconciled bool
		if owned {
			reconciled = r.reconcileOwnedLostLaunch(observation, operationOwner)
		} else {
			reconciled = r.reconcileLostLaunch(admission)
		}
		if reconciled {
			return "MegaDrive", true, nil
		}
		return "", true, unavailableError()
	}
	if response.Error != nil || !response.OK {
		return "", true, mapRemoteError(response.Error)
	}
	if !validMegaDriveRunning(response) {
		return "", true, unavailableError()
	}
	return "MegaDrive", true, nil
}

func (r *Runtime) boundedStatus(parent context.Context) (Response, error) {
	ctx, cancel := context.WithTimeout(parent, r.healthTimeout)
	defer cancel()
	return r.control.Status(ctx)
}

func (r *Runtime) reconcileLostLaunch(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), r.healthTimeout)
	defer cancel()
	reconciled, _ := r.reconcileLostLaunchWithin(ctx)
	return reconciled
}

func (r *Runtime) reconcileOwnedLostLaunch(observation, operationOwner context.Context) bool {
	if operationOwner.Err() != nil {
		return false
	}
	if !contextExhausted(observation) {
		if reconciled, exhausted := r.reconcileLostLaunchWithin(observation); reconciled {
			return true
		} else if !exhausted {
			return false
		}
	}
	if operationOwner.Err() != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(operationOwner, r.healthTimeout)
	defer cancel()
	reconciled, _ := r.reconcileLostLaunchWithin(ctx)
	return reconciled
}

func (r *Runtime) reconcileLostLaunchWithin(ctx context.Context) (reconciled, exhausted bool) {
	for {
		response, err := r.control.Status(ctx)
		if err != nil {
			return false, contextExhausted(ctx)
		}
		if validMegaDriveRunning(response) {
			return true, false
		}
		if !validIdle(response) && !validMegaDriveStarting(response) {
			return false, false
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return false, true
		}
	}
}

func contextExhausted(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	return ok && !time.Now().Before(deadline)
}

func (r *Runtime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, unsupportedOperationError()
}

func (r *Runtime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", unsupportedOperationError()
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	return r.stop(ctx, ctx, false)
}

// StopOwned keeps cancellation caller-bound until dispatch, then gives the
// sole admitted mutation to the separately bounded agent operation context.
func (r *Runtime) StopOwned(admission, operation context.Context) (string, *protocol.APIError) {
	return r.stop(admission, operation, true)
}

func (r *Runtime) stop(admission, operation context.Context, owned bool) (string, *protocol.APIError) {
	ctx := admission
	if owned {
		if admission.Err() != nil || operation.Err() != nil {
			return "", unavailableError()
		}
		ctx = operation
	}
	response, err := r.control.Stop(ctx)
	if err != nil || !response.OK || response.State != "idle" {
		return "", unavailableError()
	}
	return "", nil
}

func waitForPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func unavailableStatus() protocol.Status {
	return protocol.Status{State: protocol.StateFailed, LastError: unavailableError()}
}

func unavailableError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: unavailableMessage}
}

func unsupportedOperationError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: unsupportedOperationMessage}
}

func unsupportedSystemError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: unsupportedSystemMessage}
}

func invalidROMPathError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInvalidROMPath, Message: "ROM path is invalid for native Mega Drive launch"}
}

func validMegaDriveSpec(spec core.Spec) bool {
	registered, ok := core.DefaultRegistry().Lookup(protocol.SystemMegaDrive)
	return ok && spec.System == registered.System && spec.ExpectedCore == registered.ExpectedCore
}

func validateNativeROM(candidate string) (string, *protocol.APIError) {
	if strings.IndexByte(candidate, 0) >= 0 || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return "", invalidROMPathError()
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "ROM does not exist on the target"}
		}
		return "", invalidROMPathError()
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "ROM does not identify a non-empty regular file"}
	}
	switch strings.ToLower(filepath.Ext(resolved)) {
	case ".md", ".gen", ".bin":
		return resolved, nil
	default:
		return "", invalidROMPathError()
	}
}

func validMegaDriveRunning(response Response) bool {
	return response.Protocol == 1 && response.OK && response.Error == nil &&
		response.State == "running_game" && response.Execution == "game" &&
		response.System != nil && *response.System == "megadrive" &&
		response.Core != nil && *response.Core == "MegaDrive"
}

func validIdle(response Response) bool {
	return response.Protocol == 1 && response.OK &&
		(response.Error == nil || validErrorCode(response.Error.Code)) &&
		response.State == "idle" && response.Execution == "none" &&
		response.System == nil && response.Core == nil
}

func validMegaDriveStarting(response Response) bool {
	return response.Protocol == 1 && response.OK && response.Error == nil &&
		response.State == "starting" && response.Execution == "game" &&
		response.System != nil && *response.System == "megadrive" &&
		response.Core != nil && *response.Core == "MegaDrive"
}

func mapRemoteError(remote *RemoteError) *protocol.APIError {
	if remote == nil {
		return unavailableError()
	}
	switch remote.Code {
	case "invalid_request":
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "target runtime rejected the launch request"}
	case "unknown_system":
		return unsupportedSystemError()
	case "missing_media":
		return &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "target runtime could not open the cartridge"}
	case "busy":
		return &protocol.APIError{Code: protocol.CodeBusy, Message: "target runtime is busy"}
	case "core_mismatch":
		return &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "target runtime observed an unexpected core"}
	case "unsupported_protocol", "program_failed", "io_failed", "idle_failed":
		return unavailableError()
	default:
		return unavailableError()
	}
}
