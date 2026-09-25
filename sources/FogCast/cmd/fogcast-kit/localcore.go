package main

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

// localCoreProbeTimeout bounds one runtime status read. It matches the
// kit-local input observation budget in mister-agent so a stalled runtime
// cannot sit on the launcher loop.
const localCoreProbeTimeout = 250 * time.Millisecond

// localInputConfig is the play path fogcast-kit injects into the launcher.
// The launcher does not import the runtime or input packages. The socket is
// the agent listener. The probe is the same bound-core condition as
// mister-agent's observeRuntimeInput.
func localInputConfig() (string, func(context.Context) (bool, error)) {
	return input.DefaultLocalInputSocket, func(ctx context.Context) (bool, error) {
		return probeRuntimeCore(ctx, misterruntime.DefaultSocketPath)
	}
}

// probeRuntimeCore uses the same bound-core condition as mister-agent's
// observeRuntimeInput: an ok running_development status with an active
// package and a non-zero generation. Frames the agent would drop are not
// play input.
func probeRuntimeCore(ctx context.Context, socketPath string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, localCoreProbeTimeout)
	defer cancel()
	status, err := misterruntime.NewClient(socketPath).Protocol2Status(probeCtx)
	if err != nil {
		return false, err
	}
	return runtimeCoreBound(status), nil
}

func runtimeCoreBound(status misterruntime.Protocol2Response) bool {
	return status.OK && status.State == "running_development" && status.ActivePackage != nil && status.Generation != nil && *status.Generation != 0
}
