// SPDX-License-Identifier: GPL-3.0-or-later
package input

import "context"

// PollingControl drains and suspends the exclusively owned report transport
// without resetting the device or releasing its firmware lease. Resume starts
// fresh transactions; reports drained by PausePolling are discarded.
// Callers must still fence menu/core input at every transition boundary.
type PollingControl interface {
	PausePolling(context.Context) error
	ResumePolling(context.Context) error
}
