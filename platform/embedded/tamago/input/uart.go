//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package input

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/serial"
	"time"
)

// UART is the selected, already-configured console. Poll never waits for
// a byte and reads at most 64 bytes, allowing the UI/network goroutines to run.
type UART struct{ decoder Decoder }

func (u *UART) Poll(now time.Time) []Event {
	events := make([]Event, 0, 8)
	// Resolve an old standalone Escape before consuming a newly arrived key.
	// A slow frame must not turn "Escape, then a" into an Alt-a sequence.
	if event, ok := u.decoder.Flush(now); ok {
		events = append(events, event)
	}
	for i := 0; i < 64; i++ {
		value, ready := serial.Read()
		if !ready {
			break
		}
		if event, ok := u.decoder.Feed(value, now); ok {
			events = append(events, event)
		}
	}
	if event, ok := u.decoder.Flush(now); ok {
		events = append(events, event)
	}
	return events
}
