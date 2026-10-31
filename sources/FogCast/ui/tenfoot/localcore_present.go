package tenfoot

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// presentHold pauses a menu-display device while a local core owns HDMI.
// The first paused snapshot is still presented so the starting line is
// submitted once. Later frames are skipped. Resume forgets the last frame
// so the next present is sent on the new menu generation. A launch waits
// until that loading frame was presented and aborts if it was not. Late
// adoption submits nothing: it drains outstanding work and pauses, and an
// older menu failure does not block that pause.
type presentHold struct {
	armed  bool
	paused bool
}

func (h *presentHold) skip(ctx context.Context, dev gfx.Device, snap Snapshot) (bool, error) {
	if h == nil {
		return false, nil
	}
	if !snap.LocalCorePresentsPaused {
		if h.paused {
			if md, ok := dev.(*gfx.MenuDisplay); ok {
				md.Resume()
			}
		}
		h.paused = false
		h.armed = false
		return false, nil
	}
	if !h.armed {
		if snap.LocalCoreLateAdopt {
			if !h.paused {
				if md, ok := dev.(*gfx.MenuDisplay); ok {
					parent := ctx
					if parent == nil {
						parent = context.Background()
					}
					pauseCtx, cancel := context.WithTimeout(parent, 2*time.Second)
					err := md.WaitPending(pauseCtx)
					if err == nil {
						err = md.Pause(pauseCtx)
					}
					cancel()
					if err != nil {
						return true, err
					}
				}
				h.paused = true
			}
			return true, nil
		}
		h.armed = true
		return false, nil
	}
	if !h.paused {
		if md, ok := dev.(*gfx.MenuDisplay); ok {
			parent := ctx
			if parent == nil {
				parent = context.Background()
			}
			pauseCtx, cancel := context.WithTimeout(parent, 2*time.Second)
			err := md.WaitIdle(pauseCtx)
			if err == nil {
				err = md.PauseForLaunch(pauseCtx)
			}
			cancel()
			if err != nil {
				return true, err
			}
		}
		h.paused = true
	}
	return true, nil
}
