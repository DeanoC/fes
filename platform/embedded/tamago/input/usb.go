// SPDX-License-Identifier: GPL-3.0-or-later

package input

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// USBReports is a nonblocking boot-keyboard interrupt report source. A physical
// USB host owns enumeration and transfers; this adapter never fabricates them.
type USBReports interface{ PollKeyboard() ([]byte, error) }

// USBKeyboard tracks report edges and translates key presses for tenfoot.
// Releases update the decoder so the next real press can fire again. Held-key
// repeat and layouts other than the US keyboard mapping are not implemented.
type USBKeyboard struct {
	source  USBReports
	decoder usb.BootKeyboard
}

func NewUSBKeyboard(source USBReports) (*USBKeyboard, error) {
	if source == nil {
		return nil, fmt.Errorf("USB keyboard report source is nil")
	}
	return &USBKeyboard{source: source}, nil
}

// Poll drains at most eight reports, then yields control to the application.
// Rollover reports are ignored while preserving the last valid state. Other
// invalid reports return an error.
func (k *USBKeyboard) Poll() ([]Event, error) {
	var result []Event
	for n := 0; n < 8; n++ {
		report, err := k.source.PollKeyboard()
		if err != nil {
			return nil, err
		}
		if report == nil {
			return result, nil
		}
		events, err := k.Decode(report)
		if errors.Is(err, usb.ErrKeyboardRollover) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, events...)
	}
	return result, nil
}

// Decode translates one snapshot without polling the source. A platform which
// shares a keyboard between menu and core owns polling and chooses the decoder.
func (k *USBKeyboard) Decode(report []byte) ([]Event, error) {
	changes, err := k.decoder.Feed(report)
	if err != nil {
		return nil, err
	}
	var events []Event
	for _, change := range changes {
		if event, ok := usbPress(change); ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func usbPress(change usb.KeyEvent) (Event, bool) {
	if !change.Pressed {
		return Event{}, false
	}
	shifted := change.Modifiers&(1<<1|1<<5) != 0
	if change.Usage >= 4 && change.Usage <= 29 {
		key := byte('a' + change.Usage - 4)
		text := key
		if shifted {
			text = key - 'a' + 'A'
		}
		return Event{Key: string(key), Text: string(text)}, true
	}
	if change.Usage >= 30 && change.Usage <= 39 {
		index := change.Usage - 30
		key := "1234567890"[index]
		text := key
		if shifted {
			text = "!@#$%^&*()"[index]
		}
		return Event{Key: string(key), Text: string(text)}, true
	}
	names := map[uint8]string{40: "return", 41: "escape", 42: "backspace", 43: "tab", 44: "space", 45: "minus", 46: "equals", 47: "leftbracket", 48: "rightbracket", 49: "backslash", 51: "semicolon", 52: "apostrophe", 53: "grave", 54: "comma", 55: "period", 56: "slash", 74: "home", 77: "end", 79: "right", 80: "left", 81: "down", 82: "up"}
	name, ok := names[change.Usage]
	if !ok {
		return Event{}, false
	}
	if name == "tab" && shifted {
		name = "shift-tab"
	}
	plain := map[uint8]byte{44: ' ', 45: '-', 46: '=', 47: '[', 48: ']', 49: '\\', 51: ';', 52: '\'', 53: '`', 54: ',', 55: '.', 56: '/'}
	alternate := map[uint8]byte{44: ' ', 45: '_', 46: '+', 47: '{', 48: '}', 49: '|', 51: ':', 52: '"', 53: '~', 54: '<', 55: '>', 56: '?'}
	event := Event{Key: name}
	if text, ok := plain[change.Usage]; ok {
		if shifted {
			text = alternate[change.Usage]
		}
		event.Text = string(text)
	}
	return event, true
}
