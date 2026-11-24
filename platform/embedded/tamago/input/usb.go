// SPDX-License-Identifier: GPL-3.0-or-later

package input

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// USBReports is a nonblocking boot-keyboard interrupt report source. A physical
// USB host owns enumeration and transfers; this adapter never fabricates them.
type USBReports interface {
	PollKeyboard() ([]byte, error)
	PollKeyboardInto(dst []byte) (int, error)
}

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

var (
	usbLetterKeys     = [26]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"}
	usbLetterUpper    = [26]string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z"}
	usbDigitKeys      = [10]string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0"}
	usbDigitShifted   = [10]string{"!", "@", "#", "$", "%", "^", "&", "*", "(", ")"}
	usbKeyNames       = [256]string{40: "return", 41: "escape", 42: "backspace", 43: "tab", 44: "space", 45: "minus", 46: "equals", 47: "leftbracket", 48: "rightbracket", 49: "backslash", 51: "semicolon", 52: "apostrophe", 53: "grave", 54: "comma", 55: "period", 56: "slash", 74: "home", 77: "end", 79: "right", 80: "left", 81: "down", 82: "up"}
	usbPlainText      = [256]string{44: " ", 45: "-", 46: "=", 47: "[", 48: "]", 49: "\\", 51: ";", 52: "'", 53: "`", 54: ",", 55: ".", 56: "/"}
	usbShiftedText    = [256]string{44: " ", 45: "_", 46: "+", 47: "{", 48: "}", 49: "|", 51: ":", 52: "\"", 53: "~", 54: "<", 55: ">", 56: "?"}
)

func usbPress(change usb.KeyEvent) (Event, bool) {
	if !change.Pressed {
		return Event{}, false
	}
	shifted := change.Modifiers&(1<<1|1<<5) != 0
	if change.Usage >= 4 && change.Usage <= 29 {
		i := change.Usage - 4
		text := usbLetterKeys[i]
		if shifted {
			text = usbLetterUpper[i]
		}
		return Event{Key: usbLetterKeys[i], Text: text}, true
	}
	if change.Usage >= 30 && change.Usage <= 39 {
		i := change.Usage - 30
		text := usbDigitKeys[i]
		if shifted {
			text = usbDigitShifted[i]
		}
		return Event{Key: usbDigitKeys[i], Text: text}, true
	}
	name := usbKeyNames[change.Usage]
	if name == "" {
		return Event{}, false
	}
	if name == "tab" && shifted {
		name = "shift-tab"
	}
	text := usbPlainText[change.Usage]
	if shifted && text != "" {
		text = usbShiftedText[change.Usage]
	}
	return Event{Key: name, Text: text}, true
}
