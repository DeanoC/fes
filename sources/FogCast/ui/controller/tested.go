package controller

import "slices"

// Linux evdev bus IDs tracked for Tested hardware status.
const (
	busUSB       uint16 = 3
	busBluetooth uint16 = 5
)

type testedPad struct {
	bus, vendor, product, version uint16
	name                          string
	buttons, hats, axes           []uint16
}

// testedPads lists controllers fully verified on FES hardware (#1348). Each
// entry matches the full evdev identity (transport, VID:PID, version, name, and
// EVIOCGBIT key/hat/axis capabilities) so unrelated devices reusing a generic
// commodity VID:PID do not receive a hardware-verification badge, and USB vs.
// Bluetooth paths remain independent. Any other GameControllerDB or
// user-mapped pad still works without the badge.
var testedPads = []testedPad{
	// Super Famicom / SNES-style USB gamepad (kit fixture; USB only — Bluetooth pads will be added after #1348 plug-fest testing)
	{
		bus:     busUSB,
		vendor:  0x081f,
		product: 0xe401,
		version: 0x0110,
		name:    "USB Gamepad ",
		buttons: []uint16{288, 289, 290, 291, 292, 293, 294, 295, 296, 297},
		axes:    []uint16{0, 1},
	},
}

// Tested reports whether this pad matches an exact hardware-verified evdev
// identity and capability layout (#1348).
func (d Device) Tested() bool {
	if d.Vendor == 0 && d.Product == 0 {
		return false
	}
	buttons, hats, axes := sdlControls(d.Keys, d.Abs)
	for _, p := range testedPads {
		if d.Bus == p.bus && d.Vendor == p.vendor && d.Product == p.product &&
			d.Version == p.version && d.Name == p.name &&
			slices.Equal(buttons, p.buttons) && slices.Equal(hats, p.hats) && slices.Equal(axes, p.axes) {
			return true
		}
	}
	return false
}
