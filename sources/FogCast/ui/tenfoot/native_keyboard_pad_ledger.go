package tenfoot

import (
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// This ledger is App-owned inert bookkeeping: it never writes a pad or opens
// a device. The one worker drains actions serially on its exact recovered
// connection. Feed owns the separate union with physical pad contributions.
const maxNativeMappedPadActions = 256

type nativeMappedPadKey struct {
	device localhid.DeviceID
	raw    uint16
}

type nativeMappedPadAction struct {
	epoch, serial uint64
	device        localhid.DeviceID
	raw           uint16
	event         remoteinput.Event
	retire        bool // zero device retires all mapped-key contributors
	mappingFence  *localfeed.PadWriteFence
	deviceFence   *localfeed.PadWriteFence
}

type nativeMappedPadLedger struct {
	epoch, serial uint64
	binding       padOnlyAuthority
	keys          map[nativeMappedPadKey]remoteinput.Event
	queue         []nativeMappedPadAction
	pending       *nativeMappedPadAction
	invalid       bool
	active        bool
}

func (l *nativeMappedPadLedger) begin(epoch uint64, binding padOnlyAuthority) error {
	if l.active || epoch == 0 || epoch <= l.epoch || l.pending != nil || l.serial == ^uint64(0) {
		return errNativeGuestState
	}
	l.epoch, l.binding = epoch, binding
	l.keys = make(map[nativeMappedPadKey]remoteinput.Event)
	l.queue = nil
	l.invalid = false
	l.active = true
	return nil
}

// confirmedRetired is allowed only after exact old-connection drain has been
// acknowledged. Socket close, an error, or a merely queued barrier is not proof.
func (l *nativeMappedPadLedger) confirmedRetired(epoch uint64, binding padOnlyAuthority) error {
	if !l.active || l.pending != nil || epoch == 0 || epoch != l.epoch || binding != l.binding {
		return errNativeGuestState
	}
	l.active = false
	l.invalid = false
	l.keys = nil
	l.queue = nil
	l.pending = nil
	return nil
}

// edge admits only a fresh non-UI mapped down. Its eventual physical release
// retires that exact mapping even if UI subsequently consumes the key-up.
func (l *nativeMappedPadLedger) edge(epoch uint64, binding padOnlyAuthority, id localhid.DeviceID, raw uint16, down, admit bool, event remoteinput.Event) error {
	if !l.active || epoch != l.epoch || binding != l.binding {
		return errNativeGuestState
	}
	if l.invalid || l.epoch == 0 || id == (localhid.DeviceID{}) || raw == 0 || raw >= 256 {
		l.invalidate()
		return errNativeGuestState
	}
	key := nativeMappedPadKey{id, raw}
	if down {
		if _, exists := l.keys[key]; exists {
			l.invalidate()
			return errNativeGuestState
		}
		if !admit {
			return nil
		}
		if event.Device != remoteinput.DeviceGamepad || event.Kind != remoteinput.KindButton || event.Action != remoteinput.ActionPress {
			l.invalidate()
			return errNativeGuestState
		}
		if len(l.keys) >= localhid.MaxDevices*255 {
			l.invalidate()
			return errNativeGuestState
		}
		l.keys[key] = event
	} else {
		previous, exists := l.keys[key]
		if !exists {
			return nil
		}
		delete(l.keys, key)
		event = previous
		event.Action = remoteinput.ActionRelease
		event.Value = 0
	}
	if len(l.queue) >= maxNativeMappedPadActions {
		l.invalidate()
		return errNativeGuestState
	}
	l.queue = append(l.queue, nativeMappedPadAction{epoch: epoch, device: id, raw: raw, event: event})
	return nil
}

// retire removes unsent edges for only the lost source, then queues an
// ordered Feed barrier after any already-dispatched operation has completed.
func (l *nativeMappedPadLedger) retire(epoch uint64, binding padOnlyAuthority, id localhid.DeviceID) error {
	if !l.active || epoch != l.epoch || binding != l.binding {
		return errNativeGuestState
	}
	if l.invalid || l.epoch == 0 || id == (localhid.DeviceID{}) {
		l.invalidate()
		return errNativeGuestState
	}
	for key := range l.keys {
		if key.device == id {
			delete(l.keys, key)
		}
	}
	kept := l.queue[:0]
	for _, action := range l.queue {
		if action.device != id {
			kept = append(kept, action)
		}
	}
	l.queue = kept
	if len(l.queue) >= maxNativeMappedPadActions {
		l.invalidate()
		return errNativeGuestState
	}
	l.queue = append(l.queue, nativeMappedPadAction{epoch: l.epoch, device: id, retire: true})
	return nil
}

// fence cancels unsent key actions and queues retirement of all mapped
// contributors, preserving any already-dispatched result and physical pads.
func (l *nativeMappedPadLedger) fence(epoch uint64, binding padOnlyAuthority) error {
	if !l.active || epoch != l.epoch || binding != l.binding {
		return errNativeGuestState
	}
	if l.invalid || l.epoch == 0 {
		l.invalidate()
		return errNativeGuestState
	}
	l.keys = make(map[nativeMappedPadKey]remoteinput.Event)
	l.queue = []nativeMappedPadAction{{epoch: l.epoch, retire: true}}
	return nil
}

func (l *nativeMappedPadLedger) next(epoch uint64, binding padOnlyAuthority) (nativeMappedPadAction, bool) {
	if !l.active || l.invalid || epoch != l.epoch || binding != l.binding || l.pending != nil || len(l.queue) == 0 {
		return nativeMappedPadAction{}, false
	}
	if l.serial == ^uint64(0) {
		l.invalidate()
		return nativeMappedPadAction{}, false
	}
	l.serial++
	action := l.queue[0]
	l.queue = l.queue[1:]
	action.serial = l.serial
	l.pending = &action
	return action, true
}

// complete never replays an ambiguous write. Stale completion cookies cannot
// retire a newer action or source; errors require exact connection recovery.
func (l *nativeMappedPadLedger) complete(action nativeMappedPadAction, err error) bool {
	if l.pending == nil || action.epoch != l.pending.epoch || action.serial != l.pending.serial {
		return false
	}
	l.pending = nil
	if err != nil {
		l.invalidate()
		return false
	}
	return true
}

func (l *nativeMappedPadLedger) invalidate() { l.invalid = true; l.keys = nil; l.queue = nil }
