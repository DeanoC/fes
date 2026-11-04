//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/dwc2"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"time"
)

type usbRegisters struct{}

func (usbRegisters) Read32(offset uint32) uint32  { return load32(0x10b00000 + uintptr(offset)) }
func (usbRegisters) Write32(offset, value uint32) { store32(0x10b00000+uintptr(offset), value) }
func (usbRegisters) Now() time.Time               { return time.Now() }

var usbAttempted bool

func OpenKeyboard() (*input.USBKeyboard, error) {
	host, err := OpenKeyboardReports()
	if err != nil {
		return nil, err
	}
	return input.NewUSBKeyboard(host)
}

// KeyboardEnabled reports the explicit firmware profile selection. It does not
// establish physical USB qualification or bypass the single-attempt guard.
func KeyboardEnabled() bool { return profileUSBPHYClockHz != 0 }

// OpenKeyboardReports transfers the single controller's raw report source to a
// platform which routes input between its menu and owned core session.
func OpenKeyboardReports() (input.USBReports, error) {
	if profileUSBPHYClockHz == 0 {
		return nil, fmt.Errorf("de25-nano: USB platform explicitly disabled in firmware profile")
	}
	if usbAttempted {
		return nil, fmt.Errorf("de25-nano: USB initialization already attempted")
	}
	usbAttempted = true
	host, err := dwc2.Open(usbRegisters{}, dwc2.Config{PHYClockHz: profileUSBPHYClockHz})
	if err != nil {
		return nil, err
	}
	fmt.Println("FES_USB_READY controller=dwc2 transport=pio")
	return host, nil
}
