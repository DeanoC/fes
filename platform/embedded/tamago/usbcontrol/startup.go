// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import "fmt"

// StartupFailure retains the outcome of an attempted Open, including the
// existing bounded cleanup. It is historical evidence, never permission to
// poll, retry, adopt a lease or omit the original startup error. Failed partial
// drivers and sources must still be retained until reboot.
type StartupFailure struct {
	stage         string
	stopAttempted bool
	stopVerified  bool
	stopped       Receipt
	cause         error
}

func (e *StartupFailure) Error() string {
	return fmt.Sprintf("usbcontrol: startup stage=%s stop_attempted=%t stop_verified=%t: %v", e.stage, e.stopAttempted, e.stopVerified, e.cause)
}
func (e *StartupFailure) Unwrap() error       { return e.cause }
func (e *StartupFailure) Stage() string       { return e.stage }
func (e *StartupFailure) StopAttempted() bool { return e.stopAttempted }

// StopReceipt returns a copy of the inactive receipt observed after this Open's
// successful Stop response, before terminal local retirement. An absent/lost/
// failed response supplies no stopped receipt, even if hardware may have stopped.
func (e *StartupFailure) StopReceipt() (Receipt, bool) {
	return e.stopped, e.stopVerified
}
