// SPDX-License-Identifier: GPL-3.0-or-later

package usb

import (
	"errors"
	"fmt"
)

var (
	ErrBootReport       = errors.New("invalid USB boot-keyboard report")
	ErrKeyboardRollover = errors.New("USB keyboard rollover or error report")
)

// KeyEvent is a changed HID usage, including modifiers as usages E0..E7.
// Modifiers is the new report's complete modifier mask on every edge. It lets
// a consumer interpret key presses without depending on modifier event order.
type KeyEvent struct {
	Usage     uint8
	Pressed   bool
	Modifiers uint8
}

// BootKeyboard compares eight-byte HID boot-protocol keyboard snapshots. It
// generates edges, not typematic repeats. The caller selects boot protocol
// using SET_PROTOCOL before passing reports here and owns repeat/text policy.
type BootKeyboard struct {
	keys [256]bool
}

// Feed retains the last valid state on a malformed or rollover report, so a
// transient bad snapshot cannot release held keys or invent presses. Byte 1
// is OEM-reserved and ignored as specified by HID 1.11 Appendix B.
func (k *BootKeyboard) Feed(report []byte) ([]KeyEvent, error) {
	if len(report) != 8 {
		return nil, fmt.Errorf("%w: expected 8 bytes, got %d", ErrBootReport, len(report))
	}
	var next [256]bool
	for _, usage := range report[2:] {
		if usage == 0 {
			continue
		}
		if usage <= 3 {
			return nil, ErrKeyboardRollover
		}
		if usage >= 0xe0 && usage <= 0xe7 {
			return nil, fmt.Errorf("%w: modifier in key array", ErrBootReport)
		}
		if next[usage] {
			return nil, fmt.Errorf("%w: duplicate key usage", ErrBootReport)
		}
		next[usage] = true
	}
	for bit := uint8(0); bit < 8; bit++ {
		next[0xe0+bit] = report[0]&(1<<bit) != 0
	}
	events := make([]KeyEvent, 0, 14)
	// Releases precede presses; order inside each set is deterministic, since
	// HID key-array order does not describe chronological event order.
	for usage, wasDown := range k.keys {
		if wasDown && !next[usage] {
			events = append(events, KeyEvent{Usage: uint8(usage), Modifiers: report[0]})
		}
	}
	for usage, down := range next {
		if down && !k.keys[usage] {
			events = append(events, KeyEvent{Usage: uint8(usage), Pressed: true, Modifiers: report[0]})
		}
	}
	k.keys = next
	return events, nil
}

// Reset releases every held usage after a confirmed disconnect and clears the
// decoder for the next device. Error or timeout alone must not imply unplug.
func (k *BootKeyboard) Reset() []KeyEvent {
	events, _ := k.Feed(make([]byte, 8))
	return events
}
