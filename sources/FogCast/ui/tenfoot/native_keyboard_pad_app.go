package tenfoot

import (
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// All helpers here run under App.mu and never perform I/O. The Feed's
// connection, mapped-generation and device fences are one-way atomics.
func (a *App) nativePadStartMappingLocked(epoch uint64, binding padOnlyAuthority) error {
	if !a.nativePadRetiring || epoch == 0 || epoch != a.nativePadRetirementEpoch || binding != a.nativePadRetirementBinding || a.nativePadConnectionFence == nil || a.nativePadConnectionFence.Revoked() {
		return errNativeGuestState
	}
	if err := a.nativeMappedPad.begin(epoch, binding); err != nil {
		return err
	}
	a.nativePadMappingFence = &localfeed.PadWriteFence{}
	a.nativePadMappingFenced = false
	a.nativePadDeviceFences = make(map[localhid.DeviceID]*localfeed.PadWriteFence, len(a.nativeUIRaw.devices))
	a.nativePadDeviceBlocked = make(map[localhid.DeviceID]bool)
	for id := range a.nativeUIRaw.devices {
		a.nativePadDeviceFences[id] = &localfeed.PadWriteFence{}
	}
	return nil
}

func (a *App) nativePadWakeLocked() {
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
}

// A fresh lease result can independently revoke local pad authority even
// when its generation is unchanged. Fence the same connection under App.mu
// before any in-flight writer can observe the newly applied result.
func (a *App) nativePadObserveLeaseLocked() {
	if a.nativePadRetiring && a.playHIDFailClosedLocked() {
		a.nativePadRevokeConnectionLocked()
	}
}

// Focus changes fence all keyboard-mapped contributors, but not physical pad
// or mouse sources. One ordered retirement action performs the Feed update.
func (a *App) nativePadFenceMappingLocked() {
	if !a.nativeMappedPad.active || a.nativeMappedPad.invalid || a.nativePadMappingFenced {
		return
	}
	if a.nativePadMappingFence != nil {
		a.nativePadMappingFence.Revoke()
	}
	if err := a.nativeMappedPad.fence(a.nativeMappedPad.epoch, a.nativeMappedPad.binding); err != nil {
		a.nativePadRevokeConnectionLocked()
		return
	}
	a.nativePadMappingFenced = true
	a.nativePadWakeLocked()
}

func (a *App) nativePadRetireDeviceLocked(id localhid.DeviceID) {
	if !a.nativeMappedPad.active || a.nativeMappedPad.invalid || a.nativePadDeviceBlocked[id] {
		return
	}
	if f := a.nativePadDeviceFences[id]; f != nil {
		f.Revoke()
	}
	if err := a.nativeMappedPad.retire(a.nativeMappedPad.epoch, a.nativeMappedPad.binding, id); err != nil {
		a.nativePadRevokeConnectionLocked()
		return
	}
	if a.nativePadDeviceBlocked == nil {
		a.nativePadDeviceBlocked = make(map[localhid.DeviceID]bool)
	}
	a.nativePadDeviceBlocked[id] = true
	a.nativePadWakeLocked()
}

// A changed session/lease or shutdown revokes all pad writes immediately.
// Any in-flight write is ambiguous; only exact close plus RECOVER can retire.
func (a *App) nativePadRevokeConnectionLocked() {
	if a.nativePadConnectionFence != nil {
		a.nativePadConnectionFence.Revoke()
	}
	if a.nativePadMappingFence != nil {
		a.nativePadMappingFence.Revoke()
	}
	for _, f := range a.nativePadDeviceFences {
		f.Revoke()
	}
	if a.nativeMappedPad.active {
		a.nativeMappedPad.invalidate()
	}
	a.nativePadMappingFenced = true
	a.nativePadWakeLocked()
}

func (a *App) nativePadCompleteMappedLocked(action nativeMappedPadAction, err error) bool {
	if a.nativeMappedPad.pending == nil || a.nativeMappedPad.pending.epoch != action.epoch || a.nativeMappedPad.pending.serial != action.serial {
		return false
	}
	if !a.nativeMappedPad.complete(action, err) {
		if err != nil {
			a.nativePadRevokeConnectionLocked()
		}
		return false
	}
	if action.retire {
		if action.device == (localhid.DeviceID{}) {
			a.nativePadMappingFence = &localfeed.PadWriteFence{}
			a.nativePadMappingFenced = false
			a.nativePadDeviceBlocked = make(map[localhid.DeviceID]bool)
			for id := range a.nativeUIRaw.devices {
				a.nativePadDeviceFences[id] = &localfeed.PadWriteFence{}
			}
		} else {
			delete(a.nativePadDeviceBlocked, action.device)
			if a.nativeUIRaw.devices[action.device] != nil {
				a.nativePadDeviceFences[action.device] = &localfeed.PadWriteFence{}
			} else {
				delete(a.nativePadDeviceFences, action.device)
			}
		}
	}
	a.nativePadWakeLocked()
	return true
}
