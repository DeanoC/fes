package main

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/ui/kitlauncher"
)

// localCoreProbeTimeout bounds one runtime status read. It matches the
// kit-local input observation budget in mister-agent so a stalled runtime
// cannot sit on the launcher loop.
const localCoreProbeTimeout = 250 * time.Millisecond

// localInputConfig is the play path fogcast-kit injects into the launcher.
// The launcher does not import the runtime or input packages. The socket is
// the agent listener. One status read supplies the bound-core condition used
// by mister-agent's observeRuntimeInput and the idle-menu confirmation used
// by the host-absent HDMI handoff.
func localInputConfig() (string, func(context.Context) (kitlauncher.LocalCoreSample, error)) {
	return input.DefaultLocalInputSocket, func(ctx context.Context) (kitlauncher.LocalCoreSample, error) {
		return probeRuntimeCore(ctx, misterruntime.DefaultSocketPath)
	}
}

// probeRuntimeCore reads one runtime status. Bound matches mister-agent's
// observeRuntimeInput: an ok running_development status with an active
// package and a non-zero generation. MenuIdle is a completed idle menu.
// starting and recovery, including reboot_required, are neither.
func probeRuntimeCore(ctx context.Context, socketPath string) (kitlauncher.LocalCoreSample, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, localCoreProbeTimeout)
	defer cancel()
	status, err := misterruntime.NewClient(socketPath).Protocol2Status(probeCtx)
	if err != nil {
		return kitlauncher.LocalCoreSample{}, err
	}
	return runtimeCoreSample(status), nil
}

func runtimeCoreSample(status misterruntime.Protocol2Response) kitlauncher.LocalCoreSample {
	bound := runtimeCoreBound(status)
	return kitlauncher.LocalCoreSample{Bound: bound, MenuIdle: !bound && runtimeMenuIdle(status)}
}

func runtimeCoreBound(status misterruntime.Protocol2Response) bool {
	return status.OK && status.State == "running_development" && status.ActivePackage != nil && status.Generation != nil && *status.Generation != 0
}

// runtimeMenuIdle reports a completed idle menu the launcher may scan out:
// state idle, an available menu display, and a non-zero generation that is
// not a session binding. starting, reboot_required, and an idle status
// without that menu are not ready.
func runtimeMenuIdle(status misterruntime.Protocol2Response) bool {
	menu := status.MenuDisplay
	return status.OK && status.State == "idle" && menu != nil && menu.Available && !menu.Session && menu.Generation != 0
}
