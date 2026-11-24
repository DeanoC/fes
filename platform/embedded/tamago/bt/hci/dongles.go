// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import "github.com/DeanoC/fes/platform/embedded/tamago/usb"

// Quirk captures hardware-specific initialization or scan constraints for
// curated USB Bluetooth dongles.
type Quirk uint32

const (
	QuirkNone          Quirk = 0
	QuirkRTL8761BU     Quirk = 1 << 0 // Realtek RTL8761B/BU EPATCH v1 via 0xFC6D + 0xFC20
	QuirkBCM20702      Quirk = 1 << 1 // Broadcom BCM20702 (vendor class 0xFF allowlist)
	QuirkBrokenExtScan Quirk = 1 << 2 // Avoid LE Extended Scan commands (e.g. 0bda:a728)
)

// DongleSpec describes a curated USB Bluetooth adapter.
type DongleSpec struct {
	VendorID     uint16
	ProductID    uint16
	Name         string
	Quirks       Quirk
	FirmwareFile string
	ConfigFile   string
}

var curatedDongles = []DongleSpec{
	// Realtek RTL8761B / RTL8761BU / RTL8761BUV (BT 5.0–5.4)
	{0x0bda, 0xa728, "Realtek RTL8761BU BT 5.4", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x0bda, 0x8771, "Realtek RTL8761BU BT 5.0", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x0bda, 0x877b, "Realtek RTL8761BUV BT 5.3", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x0bda, 0xa725, "Realtek RTL8761BU BT 5.3", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x0bda, 0xa729, "Realtek RTL8761BU BT 5.4", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x2357, 0x0604, "TP-Link UB500 (RTL8761BU)", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x0b05, 0x190e, "ASUS USB-BT500 (RTL8761BU)", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	{0x2550, 0x8761, "MPOW BH456A (RTL8761BU)", QuirkRTL8761BU | QuirkBrokenExtScan, "rtl8761bu_fw.bin", "rtl8761bu_config.bin"},
	// ROM-only / zero-blob standard HCI dongles
	{0x0a12, 0x0001, "CSR8510 A10 BT 4.0", QuirkNone, "", ""},
	{0x33fa, 0x0001, "Barrot BR8041A02 BT 5.1", QuirkNone, "", ""},
	{0x33fa, 0x0010, "Barrot BR8041A02 BT 5.3", QuirkNone, "", ""},
	// Broadcom vendor-class allowlist
	{0x0a5c, 0x21e8, "Broadcom BCM20702A0 BT 4.0", QuirkBCM20702, "", ""},
}

// LookupDongle returns the curated DongleSpec for vid:pid, if listed.
func LookupDongle(vid, pid uint16) (DongleSpec, bool) {
	for _, d := range curatedDongles {
		if d.VendorID == vid && d.ProductID == pid {
			return d, true
		}
	}
	return DongleSpec{}, false
}

// FindBluetoothInterface locates the primary HCI USB interface (Interrupt-IN
// Events + Bulk-IN ACL + Bulk-OUT ACL) on dev. It accepts standard Wireless
// Controller / Bluetooth interfaces (0xE0/0x01/0x01) and explicit vendor-class
// dongles in the curated allowlist (such as Broadcom BCM20702).
func FindBluetoothInterface(dev usb.BusDevice) (usb.InterfaceDescriptor, DongleSpec, bool) {
	spec, curated := LookupDongle(dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	if !curated {
		spec = DongleSpec{
			VendorID:  dev.Descriptor.VendorID,
			ProductID: dev.Descriptor.ProductID,
			Name:      "Generic USB Bluetooth HCI",
		}
	}
	// First prefer standard Bluetooth HCI interface class 0xE0/0x01/0x01.
	for _, iface := range dev.Config.Interfaces {
		if iface.AlternateSetting != 0 {
			continue
		}
		if iface.Class == 0xe0 && iface.SubClass == 0x01 && iface.Protocol == 0x01 && hasHCIEndpoints(iface) {
			return iface, spec, true
		}
	}
	// Fallback for curated vendor-class adapters (e.g. Broadcom 0a5c:21e8).
	if curated && spec.Quirks&QuirkBCM20702 != 0 {
		for _, iface := range dev.Config.Interfaces {
			if iface.AlternateSetting != 0 {
				continue
			}
			if iface.Class == 0xff && hasHCIEndpoints(iface) {
				return iface, spec, true
			}
		}
	}
	return usb.InterfaceDescriptor{}, DongleSpec{}, false
}

func hasHCIEndpoints(iface usb.InterfaceDescriptor) bool {
	_, okEv := iface.FindEndpoint(usb.TransferInterrupt, true)
	_, okIn := iface.FindEndpoint(usb.TransferBulk, true)
	_, okOut := iface.FindEndpoint(usb.TransferBulk, false)
	return okEv && okIn && okOut
}
