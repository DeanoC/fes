package tenfoot

import (
	"errors"

	"github.com/DeanoC/FogCast/internal/localhid"
)

// nativeKeyboardUIRawLedger is independent of guest authority. It tracks
// physical keyboard incarnations and raw Linux codes, including UI keys that
// have no HID mapping. All access is under App.mu.
type nativeKeyboardUIRawLedger struct {
	devices  map[localhid.DeviceID]*nativeKeyboardUIRawDevice
	revision uint64
}

type nativeKeyboardUIRawDevice struct {
	fd         int
	unknown    bool
	armed      [256]bool
	down       [256]bool
	suppressed [256]bool
	revision   [256]uint64
	commands   [256]Command
}

type nativeKeyboardUIAction struct {
	generation   uint64
	id           localhid.DeviceID
	code         uint16
	down, repeat bool
	revision     uint64
}

const maxNativeUIActions = 64

// replace validates the complete boundary before changing any state. Every
// key held at its device's snapshot starts suppressed until a real release.
func (l *nativeKeyboardUIRawLedger) replace(b nativeRawBoundary) error {
	if b.Publisher == [16]byte{} || len(b.Devices) == 0 || len(b.Devices) > localhid.MaxDevices || l.revision == ^uint64(0) {
		return errors.New("invalid native UI boundary")
	}
	devices := make(map[localhid.DeviceID]*nativeKeyboardUIRawDevice, len(b.Devices))
	for _, device := range b.Devices {
		if device.ID == (localhid.DeviceID{}) || devices[device.ID] != nil || len(device.Held) > 255 {
			return errors.New("invalid native UI device")
		}
		d := &nativeKeyboardUIRawDevice{fd: device.FD}
		for _, code := range device.Held {
			if code == 0 || code >= 256 || d.down[code] {
				return errors.New("invalid native UI held key")
			}
			d.down[code], d.suppressed[code] = true, true
			d.revision[code] = l.revision + 1
		}
		devices[device.ID] = d
	}
	l.devices = devices
	l.revision++
	return nil
}

// quarantine never infers that an unseen key is up. Every source must see a
// real later release before a code can be armed, unless a new complete,
// synchronized boundary proves its state.
func (l *nativeKeyboardUIRawLedger) quarantine() {
	for _, d := range l.devices {
		d.unknown = true
		d.armed = [256]bool{}
		for code, down := range d.down {
			if down {
				d.suppressed[code] = true
			}
		}
	}
}

func (l *nativeKeyboardUIRawLedger) quarantineDevice(id localhid.DeviceID) error {
	d := l.devices[id]
	if d == nil {
		return errors.New("unknown native UI device")
	}
	d.unknown = true
	d.armed = [256]bool{}
	for code, down := range d.down {
		if down {
			d.suppressed[code] = true
		}
	}
	return nil
}

func (l *nativeKeyboardUIRawLedger) addUnknown(id localhid.DeviceID) error {
	if id == (localhid.DeviceID{}) || len(l.devices) >= localhid.MaxDevices || l.devices[id] != nil {
		return errors.New("invalid native UI device")
	}
	if l.devices == nil {
		l.devices = make(map[localhid.DeviceID]*nativeKeyboardUIRawDevice)
	}
	l.devices[id] = &nativeKeyboardUIRawDevice{unknown: true, fd: -1}
	return nil
}

// edge receives every physical edge, including key-ups consumed by UI focus.
// uiAllowed records only whether this press may contribute to local UI state;
// it never posts guest input or synthesizes a press.
func (l *nativeKeyboardUIRawLedger) edge(id localhid.DeviceID, code uint16, down, uiAllowed bool) error {
	d := l.devices[id]
	if d == nil || code == 0 || code >= 256 || l.revision == ^uint64(0) {
		return errors.New("unknown native UI edge")
	}
	if !down {
		l.revision++
		d.revision[code] = l.revision
		d.down[code], d.suppressed[code], d.armed[code] = false, false, true
		return nil
	}
	if d.down[code] {
		return errors.New("duplicate native UI press")
	}
	d.down[code] = true
	l.revision++
	d.revision[code] = l.revision
	if !uiAllowed || (d.unknown && !d.armed[code]) {
		d.suppressed[code] = true
	}
	return nil
}

func (l *nativeKeyboardUIRawLedger) retire(id localhid.DeviceID) { delete(l.devices, id) }

func (l *nativeKeyboardUIRawLedger) ctrlHeld() bool {
	for _, d := range l.devices {
		for _, code := range [...]uint16{29, 97} {
			if d.down[code] && !d.suppressed[code] {
				return true
			}
		}
	}
	return false
}

func (a *App) replaceNativeUIRawLocked(b nativeRawBoundary) error {
	if err := a.nativeUIRaw.replace(b); err != nil {
		return err
	}
	a.repeat.ClearNative()
	a.nativeUIActions = nil
	return nil
}
func (a *App) recordNativeUIRawLocked(id localhid.DeviceID, code uint16, down, allowed bool) error {
	if err := a.nativeUIRaw.edge(id, code, down, allowed); err != nil {
		return err
	}
	if !down {
		// Every physical release retires this exact repeat contributor, even
		// if UI dispatch is consumed, delayed or loses its action token.
		a.repeat.UpNative([16]byte(id), code)
	}
	return nil
}
func (a *App) retireNativeUIRawLocked(id localhid.DeviceID) {
	a.repeat.RetireNative([16]byte(id))
	a.nativeUIRaw.retire(id)
	for token, action := range a.nativeUIActions {
		if action.id == id {
			delete(a.nativeUIActions, token)
		}
	}
}

// quarantineNativeUIRawDeviceLocked cancels only this keyboard's local
// repeat contributions; other keyboards and the legacy pad lane survive.
func (a *App) quarantineNativeUIRawDeviceLocked(id localhid.DeviceID) error {
	if err := a.nativeUIRaw.quarantineDevice(id); err != nil {
		return err
	}
	a.repeat.RetireNative([16]byte(id))
	for token, action := range a.nativeUIActions {
		if action.id == id {
			delete(a.nativeUIActions, token)
		}
	}
	return nil
}

// issueNativeUIActionLocked is called only after the boundary has accepted an
// ordered raw edge or UI-only repeat. It does not authorize guest input.
func (a *App) issueNativeUIActionLocked(gen uint64, id localhid.DeviceID, code uint16, down, repeat bool) uint64 {
	d := a.nativeUIRaw.devices[id]
	if !a.delegatedV2Active || a.nativeKeyboardGenerationExhausted || gen == 0 || gen != a.nativeKeyboardGeneration ||
		d == nil || code == 0 || code >= 256 || d.revision[code] == 0 || (repeat && !down) || d.down[code] != down || (down && d.suppressed[code]) {
		return 0
	}
	if a.nativeUIActionNext == ^uint64(0) || len(a.nativeUIActions) >= maxNativeUIActions {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultOverflow)
		return 0
	}
	a.nativeUIActionNext++
	if a.nativeUIActions == nil {
		a.nativeUIActions = make(map[uint64]nativeKeyboardUIAction)
	}
	a.nativeUIActions[a.nativeUIActionNext] = nativeKeyboardUIAction{generation: gen, id: id, code: code, down: down, repeat: repeat, revision: d.revision[code]}
	return a.nativeUIActionNext
}

func (a *App) discardNativeKeyboardUIAction(token uint64) error {
	if a == nil || token == 0 {
		return errors.New("invalid native UI action")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.nativeUIActions[token]; !ok {
		return errors.New("unknown native UI action")
	}
	delete(a.nativeUIActions, token)
	return nil
}

// requestNativeKeyboardQuarantineLocked only marks state and wakes a future
// worker. It never waits for IO or an ACK while App.mu is held. The v2 latch
// is deliberately retained until the authority owner confirms retirement.
func (a *App) requestNativeKeyboardQuarantineLocked(reason nativeKeyboardFault) {
	if !a.delegatedV2Active {
		return
	}
	if reason != nativeFaultHotplug && reason != nativeFaultDesync && reason != nativeFaultCapability {
		a.nativePadFenceMappingLocked()
	}
	a.nativeGuestSnapshotAllowed = false
	a.nativeGuest.invalidate()
	a.nativeKeyboardQuarantined = true
	a.nativeUIActions = nil
	if a.nativeKeyboardGeneration == ^uint64(0) {
		a.nativeKeyboardGenerationExhausted = true
	} else {
		a.nativeKeyboardGeneration++
	}
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
}

// retireNativeKeyboardUIOnly removes only this keyboard incarnation.
func (a *App) retireNativeKeyboardUIOnly(id localhid.DeviceID) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nativePadRetireDeviceLocked(id)
	a.retireNativeUIRawLocked(id)
}
