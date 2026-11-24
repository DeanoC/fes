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
	var buf [28]KeyEvent
	n, err := k.FeedInto(report, buf[:])
	if err != nil {
		return nil, err
	}
	events := make([]KeyEvent, n)
	copy(events, buf[:n])
	return events, nil
}

// FeedInto is the zero-allocation fast path for Feed. A boot-protocol snapshot
// carries at most 6 key usages + 8 modifiers, so at most 14 edges can occur per
// snapshot (up to 14 releases + 14 presses across transitions, capped by 14
// simultaneous held usages — we accept *[28]KeyEvent or write up to len(dst)).
func (k *BootKeyboard) FeedInto(report []byte, dst []KeyEvent) (int, error) {
	if len(report) != 8 {
		return 0, fmt.Errorf("%w: expected 8 bytes, got %d", ErrBootReport, len(report))
	}
	var next [256]bool
	for _, usage := range report[2:] {
		if usage == 0 {
			continue
		}
		if usage <= 3 {
			return 0, ErrKeyboardRollover
		}
		if usage >= 0xe0 && usage <= 0xe7 {
			return 0, fmt.Errorf("%w: modifier in key array", ErrBootReport)
		}
		if next[usage] {
			return 0, fmt.Errorf("%w: duplicate key usage", ErrBootReport)
		}
		next[usage] = true
	}
	for bit := uint8(0); bit < 8; bit++ {
		next[0xe0+bit] = report[0]&(1<<bit) != 0
	}
	needed := 0
	for usage, wasDown := range k.keys {
		if wasDown && !next[usage] {
			needed++
		}
	}
	for usage, down := range next {
		if down && !k.keys[usage] {
			needed++
		}
	}
	if needed > len(dst) {
		return 0, fmt.Errorf("usb: edge buffer too small: need %d, have %d", needed, len(dst))
	}
	n := 0
	// Releases precede presses; order inside each set is deterministic, since
	// HID key-array order does not describe chronological event order.
	for usage, wasDown := range k.keys {
		if wasDown && !next[usage] {
			dst[n] = KeyEvent{Usage: uint8(usage), Modifiers: report[0]}
			n++
		}
	}
	for usage, down := range next {
		if down && !k.keys[usage] {
			dst[n] = KeyEvent{Usage: uint8(usage), Pressed: true, Modifiers: report[0]}
			n++
		}
	}
	k.keys = next
	return n, nil
}

// Reset releases every held usage after a confirmed disconnect and clears the
// decoder for the next device. Error or timeout alone must not imply unplug.
func (k *BootKeyboard) Reset() []KeyEvent {
	events, _ := k.Feed(make([]byte, 8))
	return events
}
