package controller

import (
	"fmt"
	"testing"
)

func TestDeviceTested(t *testing.T) {
	verified := Device{
		Bus: busUSB, Vendor: 0x081f, Product: 0xe401, Version: 0x0110,
		Name: "USB gamepad           ",
		Keys: bits(96, codeRange(288, 297)...), Abs: bits(8, 0, 1),
	}
	cases := []struct {
		name string
		dev  Device
		want bool
	}{
		{
			name: "live SNES-style USB pad identity",
			dev:  verified,
			want: true,
		},
		{
			name: "old USB Gamepad spelling is not Tested",
			dev: func() Device {
				d := verified
				d.Name = "USB Gamepad "
				return d
			}(),
			want: false,
		},
		{
			name: "same VID:PID over Bluetooth is not Tested",
			dev: func() Device {
				d := verified
				d.Bus = busBluetooth
				return d
			}(),
			want: false,
		},
		{
			name: "same VID:PID with different name is not Tested",
			dev: func() Device {
				d := verified
				d.Name = "Other Commodity USB Pad"
				return d
			}(),
			want: false,
		},
		{
			name: "same VID:PID with different version is not Tested",
			dev:  superFamicomPad(0x0111),
			want: false,
		},
		{
			name: "same VID:PID without matching capabilities is not Tested",
			dev:  Device{Bus: busUSB, Vendor: 0x081f, Product: 0xe401, Version: 0x0110, Name: "USB gamepad           "},
			want: false,
		},
		{
			name: "same VID:PID with extra capability bits is not Tested",
			dev: func() Device {
				d := verified
				d.Keys = bits(96, codeRange(288, 298)...)
				return d
			}(),
			want: false,
		},
		{
			name: "zero vendor and product",
			dev:  Device{Bus: busUSB},
			want: false,
		},
		{
			name: "Xbox 360 USB pad not yet Tested",
			dev:  Device{Bus: busUSB, Vendor: 0x045e, Product: 0x028e, Version: 0x0114, Name: "Xbox 360 Controller"},
			want: false,
		},
		{
			name: "DualSense Bluetooth pad not yet Tested",
			dev:  Device{Bus: busBluetooth, Vendor: 0x054c, Product: 0x0ce6, Version: 0x8100, Name: "DualSense Wireless Controller"},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.dev.Tested(); got != tc.want {
				t.Fatalf("Tested() = %v, want %v (%+v)", got, tc.want, tc.dev)
			}
		})
	}
}

func TestTestedPadsRegistryInvariants(t *testing.T) {
	seen := make(map[string]bool, len(testedPads))
	for _, p := range testedPads {
		if p.bus != busUSB && p.bus != busBluetooth {
			t.Fatalf("unexpected bus %d in testedPads: %+v", p.bus, p)
		}
		if p.vendor == 0 || p.product == 0 || p.version == 0 || p.name == "" {
			t.Fatalf("incomplete identity in testedPads: %+v", p)
		}
		if len(p.buttons) == 0 {
			t.Fatalf("testedPads entry must declare expected buttons: %+v", p)
		}
		key := fmt.Sprintf("%d:%04x:%04x:%04x:%s", p.bus, p.vendor, p.product, p.version, p.name)
		if seen[key] {
			t.Fatalf("duplicate identity key %q in testedPads", key)
		}
		seen[key] = true
	}
}
