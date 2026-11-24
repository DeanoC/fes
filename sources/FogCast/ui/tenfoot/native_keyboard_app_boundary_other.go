//go:build !linux

package tenfoot

import "github.com/DeanoC/FogCast/internal/localhid"

// Native evdev recovery is available only on Linux. SDL's shared recovery
// service must not promote a native source on other platforms.
func (a *App) nativeKeyboardRetireForPadRecoveryLocked(_ padOnlyAuthority, _ string, _ localhid.Reply) error {
	return errNativeGuestState
}
