// SPDX-License-Identifier: GPL-3.0-or-later

// Package input decodes interactive QEMU serial-console input into normalized
// key names. It does not implement USB enumeration or a DE25 controller.
package input

import "time"

// Event separates key actions from printable text so the application decides
// whether a letter is a shortcut or text for an active field.
type Event struct {
	Key  string
	Text string
}

// Decoder accepts ASCII and the usual terminal arrow/home sequences. Escape
// is emitted after a short bounded ambiguity interval, not by blocking input.
type Decoder struct {
	state     uint8
	escapedAt time.Time
	lastCR    bool
}

const EscapeDelay = 30 * time.Millisecond

func (d *Decoder) Feed(value byte, now time.Time) (Event, bool) {
	if value == 0x1b {
		d.state = 1
		d.escapedAt = now
		return Event{}, false
	}
	if d.state == 1 {
		if value == '[' || value == 'O' {
			d.state = 2
			return Event{}, false
		}
		d.state = 0
	}
	if d.state == 2 {
		d.state = 0
		keys := map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end", 'Z': "shift-tab"}
		key, ok := keys[value]
		return Event{Key: key}, ok
	}
	if value == '\n' && d.lastCR {
		d.lastCR = false
		return Event{}, false
	}
	d.lastCR = value == '\r'
	switch value {
	case '\r', '\n':
		return Event{Key: "return"}, true
	case '\t':
		return Event{Key: "tab"}, true
	case 8, 127:
		return Event{Key: "backspace"}, true
	case ' ':
		return Event{Key: "space", Text: " "}, true
	}
	if value >= 32 && value <= 126 {
		key := value
		if key >= 'A' && key <= 'Z' {
			key += 'a' - 'A'
		}
		return Event{Key: string(key), Text: string(value)}, true
	}
	return Event{}, false
}

func (d *Decoder) Flush(now time.Time) (Event, bool) {
	if d.state != 0 && now.Sub(d.escapedAt) >= EscapeDelay {
		d.state = 0
		return Event{Key: "escape"}, true
	}
	return Event{}, false
}
