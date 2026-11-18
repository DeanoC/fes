package controller

import "testing"

func TestFamilyFromVendorAndName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		d    Device
		want Family
	}{
		{Device{Bus: 3, Vendor: 0x045e, Product: 0x0b12, Name: "Microsoft Xbox Series S|X Controller"}, FamilyXbox},
		{Device{Bus: 5, Vendor: 0x054c, Product: 0x0ce6, Name: "DualSense Wireless Controller"}, FamilyPlayStation},
		{Device{Bus: 3, Vendor: 0x057e, Product: 0x2009, Name: "Pro Controller"}, FamilyNintendo},
		// Another maker's pad takes its family from what it calls itself.
		{Device{Bus: 3, Vendor: 0x0f0d, Product: 0x00c1, Name: "HORI CO.,LTD. HORIPAD for Nintendo Switch"}, FamilyNintendo},
		{Device{Bus: 3, Vendor: 0x24c6, Product: 0x543a, Name: "PowerA Xbox One wired controller"}, FamilyXbox},
		{Device{Bus: 3, Vendor: 0x0e6f, Product: 0x0000, Name: "Wired PS4 pad"}, FamilyPlayStation},
		{Device{Bus: 3, Vendor: 0x081f, Product: 0xe401, Name: "USB gamepad"}, FamilyGeneric},
		{Device{Name: "Wireless business pad"}, FamilyGeneric},
	} {
		if got := c.d.Family(); got != c.want {
			t.Errorf("%q %04x:%04x is %v, want %v", c.d.Name, c.d.Vendor, c.d.Product, got, c.want)
		}
	}
}
