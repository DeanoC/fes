//go:build linux

package storage

import "testing"

func TestDiscoverOnlyUSBFilesystemsAndInheritTransport(t *testing.T) {
	devices := []blockDevice{
		{Path: "/dev/mmcblk0", Transport: "mmc", Children: []blockDevice{{Path: "/dev/mmcblk0p1", UUID: "kit-boot", FSType: "vfat"}}},
		{Path: "/dev/sda", Transport: "sata", UUID: "host", FSType: "ext4"},
		{Path: "/dev/sdb", Transport: "usb", Children: []blockDevice{{Path: "/dev/sdb1", UUID: "games", FSType: "exfat"}, {Path: "/dev/sdb2", UUID: "swap", FSType: "swap"}}},
	}
	found := collectUSB(devices, false)
	if len(found) != 1 || found[0].UUID != "games" {
		t.Fatalf("discovered internal or unusable volumes: %+v", found)
	}
}
