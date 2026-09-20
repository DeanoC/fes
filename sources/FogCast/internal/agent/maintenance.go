package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/DeanoC/FogCast/protocol"
)

// SetUpdateBlocked gates the existing launch and development health admission.
func (c *Coordinator) SetUpdateBlocked(blocked bool) {
	c.mu.Lock()
	c.updateBlocked = blocked
	c.mu.Unlock()
}

// RawIdleReady bypasses the trial gate but requires an actual native idle probe.
func (c *Coordinator) RawIdleReady(ctx context.Context) bool {
	runtime, ok := c.runtime.(idleConfirmingRuntime)
	return ok && ctx.Err() == nil && c.Status().State == protocol.StateIdle && runtime.ConfirmIdle(ctx)
}

// BeginMaintenance retains the ordinary transition after Stop/save completes.
// The caller releases it on an error, or retains it until process reboot.
func (c *Coordinator) BeginMaintenance(ctx context.Context) (func(), error) {
	if !c.begin() {
		return nil, &protocol.APIError{Code: protocol.CodeBusy, Message: "another transition is running"}
	}
	var once sync.Once
	release := func() { once.Do(c.end) }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	status, err := c.stopLocked(ctx)
	if err != nil {
		release()
		return nil, err
	}
	if status.State != protocol.StateIdle || !c.RawIdleReady(ctx) {
		release()
		return nil, errors.New("native runtime did not confirm idle")
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
