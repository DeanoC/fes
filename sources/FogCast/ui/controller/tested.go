package controller

// Linux evdev bus IDs tracked for Tested hardware status.
const (
	busUSB       uint16 = 3
	busBluetooth uint16 = 5
)

type testedPad struct {
	bus, vendor, product uint16
}

// testedPads lists controllers fully verified on FES hardware (#1348). USB
// and Bluetooth transports are tracked separately so a wired pass never implies
// Bluetooth verification (or vice versa). Any other GameControllerDB or
// user-mapped pad still works without the badge.
var testedPads = []testedPad{
	// Super Famicom / SNES-style USB gamepad (kit fixture; USB only — Bluetooth pads will be added after #1348 plug-fest testing)
	{bus: busUSB, vendor: 0x081f, product: 0xe401},
}

// Tested reports whether this pad matches a transport and VID:PID verified on
// FES hardware (#1348).
func (d Device) Tested() bool {
	if d.Vendor == 0 && d.Product == 0 {
		return false
	}
	for _, p := range testedPads {
		if d.Bus == p.bus && d.Vendor == p.vendor && d.Product == p.product {
			return true
		}
	}
	return false
}
