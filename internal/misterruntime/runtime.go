package misterruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/protocol"
)

const unavailableMessage = "target runtime is unavailable"
const unsupportedOperationMessage = "requested operation is unsupported"

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
	ctx, cancel := context.WithTimeout(context.Background(), r.healthTimeout)
	defer cancel()
	response, err := r.control.Status(ctx)
	health.Ready = err == nil && response.OK && response.State == "idle"
	return health
}

func (r *Runtime) Reconcile(ctx context.Context) protocol.Status {
	for {
		response, err := r.control.Status(ctx)
		if err == nil {
			if !response.OK {
				return unavailableStatus()
			}
			switch response.State {
			case "idle":
				return protocol.Status{State: protocol.StateIdle}
			case "starting":
			default:
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

func (r *Runtime) Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError) {
	return mister.PreparedLaunch{}, &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is unsupported by target runtime"}
}

func (r *Runtime) Launch(context.Context, mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return "", false, &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is unsupported by target runtime"}
}

func (r *Runtime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, unsupportedOperationError()
}

func (r *Runtime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", unsupportedOperationError()
}

func (r *Runtime) Stop(ctx context.Context) (string, *protocol.APIError) {
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
