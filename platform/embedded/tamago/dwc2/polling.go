// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// PausePolling completes the existing packet/path-health operation, discarding
// any report, and leaves no pending channel to expire during a long core load.
// Enumeration, endpoint PID, device address and firmware ownership are retained.
// Like PollKeyboard, it requires the host's exclusive polling owner.
func (h *Host) PausePolling(ctx context.Context) error {
	if h == nil || ctx == nil || h.io == nil || !h.owned || h.initializing {
		return fmt.Errorf("dwc2: polling context required")
	}
	if h.fault != nil {
		return h.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.pollingPaused {
		return fmt.Errorf("dwc2: polling already paused")
	}
	start := h.io.Now()
	until := start.Add(500 * time.Millisecond)
	for n := 0; h.pending.active || h.healthStage != 0; n++ {
		if err := ctx.Err(); err != nil {
			return h.fail(err)
		}
		if n >= 4096 || !h.io.Now().Before(until) {
			return h.fail(h.pollingDrainFault(start, n))
		}
		if _, err := h.pollInput(); err != nil {
			return err
		}
		runtime.Gosched()
	}
	if err := h.pio(); err != nil {
		return h.fail(err)
	}
	if err := h.link(); err != nil {
		return h.fail(err)
	}
	for n := uint32(0); n < h.channels; n++ {
		if h.io.Read32(hcchar+n*0x20)&channelEnable != 0 {
			return h.fail(fmt.Errorf("dwc2: polling drain left an active channel"))
		}
	}
	if h.io.Read32(gint)&(1<<4) != 0 {
		return h.fail(fmt.Errorf("dwc2: polling drain left unread RX status"))
	}
	h.pollingPaused = true
	return nil
}

// ResumePolling performs no register access and cannot repair a failed host.
// The next poll validates the link and gives new packets fresh deadlines.
func (h *Host) ResumePolling(ctx context.Context) error {
	if h == nil || ctx == nil || h.io == nil || !h.owned || h.initializing {
		return fmt.Errorf("dwc2: polling context required")
	}
	if h.fault != nil {
		return h.fault
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !h.pollingPaused || h.pending.active || h.healthStage != 0 {
		return fmt.Errorf("dwc2: polling is not drained")
	}
	h.pollingPaused = false
	return nil
}
func (g *GamepadHost) PausePolling(ctx context.Context) error {
	if g == nil || g.host == nil {
		return fmt.Errorf("dwc2: gamepad host absent")
	}
	return g.host.PausePolling(ctx)
}
func (g *GamepadHost) ResumePolling(ctx context.Context) error {
	if g == nil || g.host == nil {
		return fmt.Errorf("dwc2: gamepad host absent")
	}
	return g.host.ResumePolling(ctx)
}
