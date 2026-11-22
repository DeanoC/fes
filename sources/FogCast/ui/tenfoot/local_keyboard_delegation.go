package tenfoot

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

type boundLocalKeyboard interface {
	SendBound(context.Context, remoteinput.Event, time.Time, localfeed.Binding) error
	CloseBound(localfeed.Binding)
}

func fullLocalIdentity(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// localKeyboardBindingLocked permits a root-local physical HID route while
// another host owns this kit. The target rechecks the immutable lease/core
// binding on each frame; this observation alone never authorizes a write.
func (a *App) localKeyboardBindingLocked() (localfeed.Binding, bool) {
	if !a.kitHost || a.client == nil || a.client.paired || !a.healthHave || a.hostUnreachable || a.localKeyboardUncertain ||
		!a.health.Ready || !a.health.TargetReachable ||
		!a.kitLeaseHave || a.kitLease.Unavailable || a.kitLease.ErrorCode != "" ||
		!fullLocalIdentity(a.kitLease.Generation) || a.kitLease.Generation != a.health.Connection.LeaseGeneration ||
		a.session.State != "active" || !a.session.CoreKeyboardHID || a.session.CorePackage == nil ||
		a.session.TargetID == "" || a.session.TargetID != a.health.Connection.TargetID ||
		(a.kitID != "" && a.kitID != a.session.TargetID) ||
		!fullLocalIdentity(a.session.CorePackage.PackageID) || a.session.CorePackage.Generation == 0 ||
		!a.needsSessionDisplayLocked() || a.sessionDisplayLocked().Focus != protocol.SessionFocusMachine {
		return localfeed.Binding{}, false
	}
	// TargetReady reports idle launch admission, so it is false while a
	// healthy core is running. The active session and its lease/core binding
	// below are the relevant admission checks for an in-play keyboard.
	connection := a.health.Connection
	foreign := connection.State == "busy" && !connection.LeaseOwned && connection.Owner != "" &&
		a.kitLease.State == "held" && a.kitLease.Owner == connection.Owner
	owned := connection.State == "active" && connection.LeaseOwned && a.kitLease.State == "active"
	if !foreign && !owned {
		return localfeed.Binding{}, false
	}
	if _, ok := a.localFeed.(boundLocalKeyboard); !ok {
		return localfeed.Binding{}, false
	}
	return localfeed.Binding{LeaseGeneration: a.kitLease.Generation, PackageID: a.session.CorePackage.PackageID,
		CoreGeneration: a.session.CorePackage.Generation, Epoch: a.playHIDEpoch}, true
}

// retireLocalKeyboardLocked queues neutral on the old binding, then closes
// only that connection after its queue drains. A new lifetime cannot be
// closed by this asynchronous cleanup even if its lease/core are unchanged.
func (a *App) retireLocalKeyboardLocked() {
	binding, ok := a.localKeyboardBindingLocked()
	if !ok {
		return
	}
	feed := a.localFeed.(boundLocalKeyboard)
	a.suppressHeldPlayKeysLocked()
	a.playKeyAdmitted = nil
	a.playKeyPendingRelease = nil
	var releases []remoteinput.Event
	for key, event := range a.playHIDHeld {
		if event.Device != remoteinput.DeviceKeyboard {
			continue
		}
		event.Action = remoteinput.ActionRelease
		event.Value = 0
		releases = append(releases, event)
		delete(a.playHIDHeld, key)
	}
	a.queuePlayHIDBindingLocked(releases, &binding)
	tail := a.playHIDTail
	a.playHIDEpoch++
	a.playHIDPending = 0
	a.localKeyboardUncertain = true
	go func() {
		if tail != nil {
			<-tail
		}
		feed.CloseBound(binding)
	}()
}

func (a *App) keyboardHIDFailClosedLocked() bool {
	if a.kitHost && a.session.CoreKeyboardHID && a.needsSessionDisplayLocked() {
		_, ok := a.localKeyboardBindingLocked()
		return !ok
	}
	return a.playHIDFailClosedLocked()
}

func (a *App) queuePhysicalKeyboardLocked(event remoteinput.Event) {
	if a.kitHost && a.session.CoreKeyboardHID && a.needsSessionDisplayLocked() {
		binding, ok := a.localKeyboardBindingLocked()
		if !ok {
			return
		}
		a.queuePlayHIDBindingLocked([]remoteinput.Event{event}, &binding)
		return
	}
	if a.playHIDFailClosedLocked() {
		return
	}
	a.queuePlayHIDLocked([]remoteinput.Event{event})
}

func (a *App) sendPhysicalKeyboardLocked(event remoteinput.Event) bool {
	if !(a.kitHost && a.session.CoreKeyboardHID && a.needsSessionDisplayLocked()) {
		return a.sendPlayHIDLocked(event)
	}
	binding, ok := a.localKeyboardBindingLocked()
	ready := a.forwardsPlayHIDLocked()
	if a.playHIDInputBlockedLocked() {
		if !a.neutralPlayHIDEventLocked(event) {
			return false
		}
		ready = a.playHIDRouteReadyLocked()
	}
	if !ok || event.Device != remoteinput.DeviceKeyboard || !ready || a.client == nil ||
		a.playHIDPending >= maxPendingPlayHID && !a.neutralPlayHIDEventLocked(event) {
		return false
	}
	a.flushPendingPlayKeyReleasesLocked()
	a.rememberPlayHIDLocked(event)
	a.queuePlayHIDBindingLocked([]remoteinput.Event{event}, &binding)
	return true
}
