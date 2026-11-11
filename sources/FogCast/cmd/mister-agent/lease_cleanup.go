package main

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

// kitLeaseCleanup stops the runtime to idle before a lease can be claimed.
// If the runtime socket stays closed for the observe window, the lease is
// freed: there is no live session to clean, and blocking it permanently
// strands the kit after an agent restart.
func kitLeaseCleanup(ctx context.Context, probe func(context.Context) bool, observe time.Duration, peripherals func(context.Context) error, stop func(context.Context) (protocol.Status, *protocol.APIError)) error {
	var cleanupErr error
	if peripherals != nil {
		cleanupErr = peripherals(ctx)
	}
	if probe != nil && !runtimeSocketOpened(ctx, probe, observe) {
		if cleanupErr != nil {
			return cleanupErr
		}
		return kitlease.ErrRuntimeUnreachable
	}
	status, stopErr := stop(ctx)
	if stopErr != nil || status.State != protocol.StateIdle {
		cleanupErr = errors.Join(cleanupErr, errors.New("kit runtime did not become idle"))
	}
	return cleanupErr
}

func runtimeSocketOpened(ctx context.Context, probe func(context.Context) bool, observe time.Duration) bool {
	deadline := time.Now().Add(observe)
	for {
		if ctx.Err() != nil {
			return false
		}
		probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		open := probe(probeCtx)
		cancel()
		if open {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
