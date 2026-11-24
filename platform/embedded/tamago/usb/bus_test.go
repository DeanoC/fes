// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import "testing"

func TestParseFullConfigurationCompositeBTAndHID(t *testing.T) {
	// Bluetooth HCI dongle descriptor:
	// IF0 alt0: class=0xE0 sub=0x01 proto=0x01 (EP1 Int-IN 16B, EP2 Bulk-IN 64B, EP2 Bulk-OUT 64B)
	// IF1 alt0: class=0xE0 sub=0x01 proto=0x01 (Isochronous SCO - parsed cleanly, ignored by BT input stack)
	btCfg := []byte{
		9, 2, 48, 0, 2, 1, 0, 0xe0, 50,
		// Interface 0 alt 0: HCI
		9, 4, 0, 0, 3, 0xe0, 0x01, 0x01, 0,
		7, 5, 0x81, 0x03, 16, 0, 1, // EP1 Interrupt IN, MPS 16, interval 1
		7, 5, 0x82, 0x02, 0x00, 0x02, 0, // EP2 Bulk IN, MPS 512 (HS!)
		7, 5, 0x02, 0x02, 0x00, 0x02, 0, // EP2 Bulk OUT, MPS 512 (HS!)
		// Interface 1 alt 0: SCO
		9, 4, 1, 0, 0, 0xe0, 0x01, 0x01, 0,
	}
	cfg, err := ParseFullConfiguration(btCfg)
	if err != nil {
		t.Fatalf("ParseFullConfiguration(BT): %v", err)
	}
	if cfg.Value != 1 || len(cfg.Interfaces) != 2 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	hci, ok := cfg.FindInterface(0xe0, 0x01, 0x01)
	if !ok || hci.Number != 0 || len(hci.Endpoints) != 3 {
		t.Fatalf("unexpected HCI interface: ok=%t iface=%+v", ok, hci)
	}
	intIn, ok := hci.FindEndpoint(TransferInterrupt, true)
	if !ok || intIn.Address != 0x81 || intIn.MaxPacketSize != 16 || intIn.Interval != 1 {
		t.Fatalf("unexpected HCI Int-IN: %+v", intIn)
	}
	bulkIn, ok := hci.FindEndpoint(TransferBulk, true)
	if !ok || bulkIn.Address != 0x82 || bulkIn.MaxPacketSize != 512 {
		t.Fatalf("unexpected HCI Bulk-IN: %+v", bulkIn)
	}
	bulkOut, ok := hci.FindEndpoint(TransferBulk, false)
	if !ok || bulkOut.Address != 0x02 || bulkOut.MaxPacketSize != 512 {
		t.Fatalf("unexpected HCI Bulk-OUT: %+v", bulkOut)
	}

	// Composite gaming keyboard + DualSense-style HID with Interrupt IN + OUT:
	composite := []byte{
		9, 2, 66, 0, 2, 1, 0, 0xa0, 250,
		// IF0: Boot keyboard
		9, 4, 0, 0, 1, 3, 1, 1, 0,
		9, 0x21, 0x11, 1, 0, 1, 0x22, 63, 0,
		7, 5, 0x81, 3, 8, 0, 1,
		// IF1: Gamepad HID with IN (64B) + OUT (64B)
		9, 4, 1, 0, 2, 3, 0, 0, 0,
		9, 0x21, 0x11, 1, 0, 1, 0x22, 0x90, 0x01, // 400-byte report descriptor
		7, 5, 0x84, 3, 64, 0, 1,
		7, 5, 0x03, 3, 64, 0, 1,
	}
	ccfg, err := ParseFullConfiguration(composite)
	if err != nil {
		t.Fatalf("ParseFullConfiguration(composite): %v", err)
	}
	if len(ccfg.Interfaces) != 2 || ccfg.Interfaces[0].HIDReportBytes != 63 || ccfg.Interfaces[1].HIDReportBytes != 400 {
		t.Fatalf("unexpected composite interfaces: %+v", ccfg.Interfaces)
	}
	if outEp, ok := ccfg.Interfaces[1].FindEndpoint(TransferInterrupt, false); !ok || outEp.Address != 0x03 || outEp.MaxPacketSize != 64 {
		t.Fatalf("missing HID Interrupt OUT: ok=%t ep=%+v", ok, outEp)
	}
}

func TestParseDeviceDescriptorMultiConfigAccepted(t *testing.T) {
	raw := []byte{18, 1, 0x00, 0x02, 0x00, 0x00, 0x00, 64, 0x4c, 0x05, 0xe6, 0x0c, 0x00, 0x01, 1, 2, 3, 2}
	d, err := ParseDeviceDescriptor(raw)
	if err != nil {
		t.Fatalf("ParseDeviceDescriptor: %v", err)
	}
	if d.VendorID != 0x054c || d.ProductID != 0x0ce6 || d.NumConfigs != 2 || d.MaxPacketSize0 != 64 {
		t.Fatalf("unexpected descriptor: %+v", d)
	}
}
