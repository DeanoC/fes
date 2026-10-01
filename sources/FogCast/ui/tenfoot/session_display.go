package tenfoot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
)

// SetMenuDisplay connects physical presentation to the session controls. The
// host window keeps its existing independent display.
func (a *App) SetMenuDisplay(display *gfx.MenuDisplay) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.menuDisplay = display
}

func (a *App) needsSessionDisplayLocked() bool {
	return a.menuDisplay != nil || a.localCores != nil
}

func (a *App) sessionDisplayOfferedLocked() bool {
	return !a.needsSessionDisplayLocked() || hostclient.SessionDisplayCapable(a.session.CorePackage)
}

func (a *App) suppressHeldPlayKeysLocked() {
	if a.playKeySuppressed == nil {
		a.playKeySuppressed = make(map[string]bool)
	}
	for key := range a.playKeyHeld {
		a.playKeySuppressed[key] = true
	}
}

// Every physical key passes here, including room navigation. A held key must
// be released before either the room or the machine can see another press.
func (a *App) consumeSuppressedPlayKey(name string, down bool) bool {
	if a == nil {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.playKeyHeld == nil {
		a.playKeyHeld = make(map[string]bool)
	}
	if down {
		a.playKeyHeld[name] = true
	} else {
		delete(a.playKeyHeld, name)
	}
	suppressed := a.playKeySuppressed[name]
	if !down {
		delete(a.playKeySuppressed, name)
	}
	if a.sessionDisplayBusy || a.sessionDisplayUncertain && name != "home" && name != "escape" && name != "backspace" {
		if down {
			if a.playKeySuppressed == nil {
				a.playKeySuppressed = make(map[string]bool)
			}
			a.playKeySuppressed[name] = true
		}
		return true
	}
	return suppressed
}

func (a *App) beginSessionDisplayLocked(visible, openTape bool) {
	if a.sessionDisplayBusy || a.client == nil || a.session.State != "active" || !hostclient.SessionDisplayCapable(a.session.CorePackage) || a.playHIDFailClosedLocked() {
		return
	}
	a.releasePlayHIDLocked()
	a.suppressHeldPlayKeysLocked()
	a.suppressHeldSessionPadLocked()
	a.repeat.Clear()
	a.hold.Clear()
	a.sessionDisplayEpoch++
	a.sessionDisplayBusy = true
	prior, epoch := a.session, a.sessionDisplayEpoch
	done := make(chan struct{})
	a.sessionDisplayWait = done
	a.sessionDisplayBinding = prior
	display, tail, feed := a.menuDisplay, a.playHIDTail, a.localFeed
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	if visible {
		a.status = "Opening HDMI controls…"
	} else {
		a.status = "Returning to play…"
	}
	a.bumpSessionGenLocked()
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		var err error
		if display != nil {
			err = display.Pause(ctx)
		}
		if err == nil && tail != nil {
			select {
			case <-tail:
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if feed != nil {
			// Disconnecting releases just this local source in the existing
			// agent input controller, including buttons held before Home.
			feed.Close()
		}
		var result hostclient.SessionResult
		if err == nil {
			result, err = a.client.SetSessionDisplayForSession(ctx, prior, visible)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if epoch != a.sessionDisplayEpoch || !samePlayHIDSession(a.session, prior) {
			return
		}
		a.sessionDisplayBusy = false
		a.sessionDisplayWait = nil
		a.bumpSessionGenLocked()
		if err != nil {
			if visible {
				a.sessionDisplayUncertain = true
			}
			if display != nil {
				display.Resume()
			}
			a.status = fmt.Sprintf("HDMI controls unavailable: %s. Back returns to play; Home retries.", err)
			a.roomSessionNotice = a.status
			a.kickSessionPollLocked()
			return
		}
		a.applySessionLocked(result)
		a.sessionDisplayVisible = visible
		a.sessionDisplayUncertain = false
		a.roomSessionNotice = ""
		if display != nil {
			if visible {
				display.BindSession(prior.CorePackage.PackageID, prior.CorePackage.Generation)
			} else {
				display.ClearSessionBinding()
			}
			display.Resume()
		}
		if visible {
			a.openPlayingHardwareRoomLocked()
			if !a.roomDuringPlay {
				a.beginSessionDisplayLocked(false, false)
				return
			}
			if openTape {
				a.openTapePickerLocked()
			}
		} else {
			a.finishRoomSessionResumeLocked()
		}
		a.kickSessionPollLocked()
	}()
}

func (a *App) invalidateSessionDisplayLocked() {
	a.sessionDisplayEpoch++
	a.sessionDisplayBusy = false
	a.sessionDisplayVisible = false
	a.sessionDisplayUncertain = false
	a.sessionDisplayBinding = hostclient.SessionResult{}
	a.roomDuringPlay = false
	a.playPadChordSince = time.Time{}
	a.playPadChordFired = false
	a.suppressHeldPlayKeysLocked()
	a.suppressHeldSessionPadLocked()
	if a.menuDisplay != nil {
		a.menuDisplay.ClearSessionBinding()
		a.menuDisplay.Resume()
	}
}

// Shell exit returns the captured HDMI plane without stopping the machine.
func (a *App) returnSessionDisplayOnExit() {
	a.mu.Lock()
	visible, prior, display, client := a.sessionDisplayVisible || a.sessionDisplayUncertain, a.sessionDisplayBinding, a.menuDisplay, a.client
	a.mu.Unlock()
	if !visible || client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if display != nil {
		if err := display.Pause(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "tenfoot: returning HDMI on exit: %v\n", err)
			return
		}
	}
	if _, err := client.SetSessionDisplayForSession(ctx, prior, false); err != nil {
		fmt.Fprintf(os.Stderr, "tenfoot: returning HDMI on exit: %v\n", err)
	}
}
