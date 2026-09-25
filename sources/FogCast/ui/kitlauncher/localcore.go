package kitlauncher

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
)

// localCoreProbe bounds one runtime status read. It matches the kit-local
// input observation budget in mister-agent so a stalled runtime cannot sit
// on the launcher loop.
const localCoreProbeTimeout = 250 * time.Millisecond

// readLocalCore reports whether the runtime on this kit has a core bound.
// Play input follows this bit. It does not read launcher.json or the host
// session. A nil override uses the local runtime socket.
func (c *Client) readLocalCore(ctx context.Context) (bool, error) {
	if c != nil && c.localCore != nil {
		return c.localCore(ctx)
	}
	return probeRuntimeCore(ctx, misterruntime.DefaultSocketPath)
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
