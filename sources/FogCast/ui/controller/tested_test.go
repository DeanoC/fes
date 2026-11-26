package controller

import "testing"

func TestDeviceTested(t *testing.T) {
	cases := []struct {
		name string
		dev  Device
		want bool
	}{
		{
			name: "SNES-style USB pad",
			dev:  Device{Bus: busUSB, Vendor: 0x081f, Product: 0xe401, Name: "USB Gamepad "},
			want: true,
		},
		{
			name: "same VID:PID over Bluetooth is not Tested",
			dev:  Device{Bus: busBluetooth, Vendor: 0x081f, Product: 0xe401, Name: "USB Gamepad "},
			want: false,
		},
		{
			name: "zero vendor and product",
			dev:  Device{Bus: busUSB},
			want: false,
		},
		{
			name: "Xbox 360 USB pad not yet Tested",
			dev:  Device{Bus: busUSB, Vendor: 0x045e, Product: 0x028e, Name: "Xbox 360 Controller"},
			want: false,
		},
		{
			name: "DualSense Bluetooth pad not yet Tested",
			dev:  Device{Bus: busBluetooth, Vendor: 0x054c, Product: 0x0ce6, Name: "DualSense Wireless Controller"},
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
	seen := make(map[testedPad]bool, len(testedPads))
	for _, p := range testedPads {
		if p.bus != busUSB && p.bus != busBluetooth {
			t.Fatalf("unexpected bus %d in testedPads: %+v", p.bus, p)
		}
		if p.vendor == 0 || p.product == 0 {
			t.Fatalf("zero vendor or product in testedPads: %+v", p)
		}
		if seen[p] {
			t.Fatalf("duplicate entry in testedPads: %+v", p)
		}
		seen[p] = true
	}
}
