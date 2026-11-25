package misterruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

func (r *Runtime) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	observed, _, attempted, apiErr := r.loadDevelopmentRBF(ctx, ctx, ctx, size, content, false)
	return observed, attempted, apiErr
}

// LoadDevelopmentRBFOwned keeps staging and admission caller-bound, then
// transfers the sole runtime mutation to the agent process owner. Observation
// remains separately bounded and never replays the mutation.
func (r *Runtime) LoadDevelopmentRBFOwned(admission, observation, operationOwner context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	observed, _, attempted, apiErr := r.LoadDevelopmentRBFOwnedWithRecovery(admission, observation, operationOwner, size, content)
	return observed, attempted, apiErr
}

// LoadDevelopmentRBFOwnedWithRecovery preserves an explicit native recovery
// marker while retaining the ordinary load API's unavailable error.
func (r *Runtime) LoadDevelopmentRBFOwnedWithRecovery(admission, observation, operationOwner context.Context, size int64, content io.Reader) (string, string, bool, *protocol.APIError) {
	return r.loadDevelopmentRBF(admission, observation, operationOwner, size, content, true)
}

func (r *Runtime) loadDevelopmentRBF(admission, observation, operationOwner context.Context, size int64, content io.Reader, owned bool) (string, string, bool, *protocol.APIError) {
	if r.developmentRBFPath == "" {
		return "", "", false, unsupportedOperationError()
	}
	if !validDevelopmentRBFPath(r.developmentRBFPath) || size < 1 || size > protocol.MaxDevelopmentRBFBytes || content == nil {
		return "", "", false, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "development RBF input is invalid"}
	}
	if admission.Err() != nil {
		return "", "", false, unavailableError()
	}
	if err := writeAtomicDevelopmentRBF(r.developmentRBFPath, size, &contextReader{ctx: admission, reader: content}); err != nil {
		if admission.Err() != nil {
			return "", "", false, unavailableError()
		}
		return "", "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "development RBF could not be installed"}
	}
	response, err := r.boundedStatus(admission)
	if err != nil || !validIdle(response) {
		return "", "", false, unavailableError()
	}
	// Observation only bounds post-dispatch reconciliation; staging may consume it.
	if admission.Err() != nil || (owned && operationOwner.Err() != nil) {
		return "", "", false, unavailableError()
	}
	dispatchContext := observation
	if owned {
		dispatchContext = operationOwner
	}
	response, err = r.control.Protocol2LoadDevelopmentRBF(dispatchContext, r.developmentRBFPath)
	r.noteDispatch("development_rbf", err == nil)
	if err != nil {
		var mutation protocol2MutationError
		if errors.As(err, &mutation) && !mutation.attempted {
			return "", "", false, unavailableError()
		}
		if owned {
			observed, attempted, apiErr := r.reconcileOwnedLostDevelopment(observation, operationOwner)
			return observed, "", attempted, apiErr
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(admission), r.healthTimeout)
		defer cancel()
		observed, attempted, apiErr := r.reconcileLostDevelopmentWithin(ctx)
		return observed, "", attempted, apiErr
	}
	if validDevelopmentRunning(response) {
		return developmentObservation(response), "", true, nil
	}
	if rebootRequiredResponse(response) {
		return "", protocol.RecoveryRebootRequired, true, mapProtocol2Error(response.Error)
	}
	if response.Error != nil || !response.OK {
		return "", "", true, mapProtocol2Error(response.Error)
	}
	if validDevelopmentStarting(response) || validCleanIdle(response) {
		if owned {
			observed, attempted, apiErr := r.reconcileOwnedLostDevelopment(observation, operationOwner)
			return observed, "", attempted, apiErr
		}
		observed, attempted, apiErr := r.reconcileLostDevelopmentWithin(observation)
		return observed, "", attempted, apiErr
	}
	return "", "", true, unavailableError()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func (r *Runtime) reconcileOwnedLostDevelopment(observation, operationOwner context.Context) (string, bool, *protocol.APIError) {
	if operationOwner.Err() != nil {
		return "", true, unavailableError()
	}
	if !contextExhausted(observation) {
		observed, attempted, apiErr, exhausted := r.observeLostDevelopment(observation)
		if !exhausted {
			return observed, attempted, apiErr
		}
	}
	if operationOwner.Err() != nil {
		return "", true, unavailableError()
	}
	ctx, cancel := context.WithTimeout(operationOwner, r.healthTimeout)
	defer cancel()
	return r.reconcileLostDevelopmentWithin(ctx)
}

func (r *Runtime) reconcileLostDevelopmentWithin(ctx context.Context) (string, bool, *protocol.APIError) {
	observed, attempted, apiErr, _ := r.observeLostDevelopment(ctx)
	return observed, attempted, apiErr
}

func (r *Runtime) observeLostDevelopment(ctx context.Context) (observed string, attempted bool, apiErr *protocol.APIError, exhausted bool) {
	for {
		response, err := r.control.Protocol2Status(ctx)
		if err != nil {
			return "", true, unavailableError(), contextExhausted(ctx)
		}
		if validDevelopmentRunning(response) {
			return developmentObservation(response), true, nil, false
		}
		if validIdle(response) && response.Error != nil {
			return "", true, mapProtocol2Error(response.Error), false
		}
		if !(validDevelopmentStarting(response) || validCleanIdle(response)) {
			return "", true, unavailableError(), false
		}
		if !waitForPoll(ctx, r.pollInterval) {
			return "", true, unavailableError(), true
		}
	}
}

type protocol2RecoverIdleControl interface {
	Protocol2RecoverIdle(context.Context) (Protocol2Response, error)
}

// RecoverIdle programs idle without rebooting the board. A false result with
// a recovery-phase error means LoadIdle failed and a board reboot is the
// remaining recovery. A missing recover_idle operation is unsupported,
// including an older daemon's unknown-operation or invalid_request reply.
// Transport and busy failures stay unavailable so recovery can fail closed.
func (r *Runtime) RecoverIdle(ctx context.Context) (bool, *protocol.APIError) {
	if err := ctx.Err(); err != nil {
		return false, unavailableError()
	}
	control, ok := r.control.(protocol2RecoverIdleControl)
	if !ok {
		return false, unsupportedOperationError()
	}
	response, err := control.Protocol2RecoverIdle(ctx)
	r.noteDispatch("recover_idle", err == nil)
	if err != nil {
		return false, unavailableError()
	}
	if legacyRecoverIdleResponse(response) {
		return false, unsupportedOperationError()
	}
	if validCleanIdle(response) {
		return true, nil
	}
	if rebootRequiredResponse(response) {
		return false, mapProtocol2Error(response.Error)
	}
	return false, unavailableError()
}

// legacyRecoverIdleRejection recognizes a daemon that does not implement
// recover_idle. Those replies are not valid reboot_required results, so the
// strict decoder rejects them before RecoverIdle can see the error code.
func legacyRecoverIdleRejection(line []byte) (Protocol2Response, bool) {
	var probe struct {
		Error *Protocol2Error `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil || probe.Error == nil {
		return Protocol2Response{}, false
	}
	response := Protocol2Response{Protocol: 2, Error: probe.Error}
	if !legacyRecoverIdleResponse(response) {
		return Protocol2Response{}, false
	}
	return response, true
}

func legacyRecoverIdleResponse(response Protocol2Response) bool {
	if response.Error == nil {
		return false
	}
	switch response.Error.Code {
	case "invalid_request", "unknown_operation":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(response.Error.Message), "unknown operation")
}

// RuntimeSocketOpen reports whether a status call reached the runtime.
// A missing socket or a probe that expires is closed: agent restart must
// free the kit lease instead of leaving it blocked.
func (r *Runtime) RuntimeSocketOpen(ctx context.Context) bool {
	_, err := r.control.Protocol2Status(ctx)
	if err == nil {
		return true
	}
	return ctx.Err() == nil && !errors.Is(err, errRuntimeConnection)
}

func (r *Runtime) RecoverDevelopment(ctx context.Context) (string, *protocol.APIError) {
	if ctx.Err() != nil || r.recoveryReboot == nil {
		return "", unavailableError()
	}
	if err := r.recoveryReboot(ctx); err != nil {
		return "", unavailableError()
	}
	return "", nil
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
	return r.stop(ctx, ctx, false)
}

// StopOwned keeps cancellation caller-bound until dispatch, then gives the
// sole admitted mutation to the separately bounded agent operation context.
func (r *Runtime) StopOwned(admission, operation context.Context) (string, *protocol.APIError) {
	return r.stop(admission, operation, true)
}

// StopOwnedWithRecovery preserves the native runtime's explicit reboot
// requirement without making the coordinator infer it from an error string.
func (r *Runtime) StopOwnedWithRecovery(admission, operation context.Context) (string, string, *protocol.APIError) {
	return r.stopWithRecovery(admission, operation, true)
}

func (r *Runtime) stop(admission, operation context.Context, owned bool) (string, *protocol.APIError) {
	observed, recovery, apiErr := r.stopWithRecovery(admission, operation, owned)
	if apiErr == nil && recovery != "" {
		return observed, unavailableError()
	}
	return observed, apiErr
}

func (r *Runtime) stopWithRecovery(admission, operation context.Context, owned bool) (string, string, *protocol.APIError) {
	if admission.Err() != nil || (owned && operation.Err() != nil) {
		return "", "", unavailableError()
	}
	ctx := admission
	if owned {
		ctx = operation
	}
	return r.stopCorePackage(ctx, r.control)
}

func rebootRequiredResponse(response Protocol2Response) bool {
	return response.Protocol == 2 && !response.OK && response.State == "reboot_required" &&
		response.Execution == "none" && response.System == nil && response.Core == nil &&
		response.Error != nil && response.Error.Code == "idle_failed"
}
