//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/dwc2"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
	"time"
)

type usbRegisters struct{}

func (usbRegisters) Read32(offset uint32) uint32  { return load32(0x10b00000 + uintptr(offset)) }
func (usbRegisters) Write32(offset, value uint32) { store32(0x10b00000+uintptr(offset), value) }
func (usbRegisters) Now() time.Time               { return time.Now() }

var usbAttempted bool
var ownedUSBSource *usbcontrol.Source // Retain failed/closed resources until reboot.

// OpenKeyboardWithControl uses an explicitly supplied secure USB client. There
// is no fallback to direct controller access if its lease/confirmation fails.
// A selecting boot profile must supply this client's independent boot owner.
func OpenKeyboardWithControl(ctx context.Context, client *usbcontrol.Client) (*usbcontrol.Source, error) {
	if !profileUSBControlEnabled || client == nil {
		return nil, fmt.Errorf("de25-nano: controlled USB profile and client required")
	}
	if usbAttempted {
		return nil, fmt.Errorf("de25-nano: USB initialization already attempted")
	}
	usbAttempted = true
	if _, err := arm64map.Verify(profileRAMStart, profileRAMSize, []arm64map.Range{{Base: 0x10b00000, Bytes: 0x20000, Kind: arm64map.Device}}); err != nil {
		return nil, err
	}
	var err error
	ownedUSBSource, err = usbcontrol.Open(ctx, client, func() (input.USBReports, error) {
		return dwc2.Open(usbRegisters{}, dwc2.Config{PHYClockHz: 60000000})
	})
	if err != nil {
		return nil, err
	}
	return ownedUSBSource, nil
}

func OpenKeyboard() (*input.USBKeyboard, error) {
	host, err := OpenKeyboardReports()
	if err != nil {
		return nil, err
	}
	return input.NewUSBKeyboard(host)
}

// KeyboardEnabled reports the explicit firmware profile selection. It does not
// establish physical USB qualification or bypass the single-attempt guard.
func KeyboardEnabled() bool { return profileUSBControlEnabled || profileUSBPHYClockHz != 0 }

// OpenKeyboardReports transfers the single controller's raw report source to a
// platform which routes input between its menu and owned core session.
func OpenKeyboardReports() (input.USBReports, error) {
	if profileUSBControlEnabled {
		return nil, fmt.Errorf("de25-nano: selected USB lease requires an explicit firmware client")
	}
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
