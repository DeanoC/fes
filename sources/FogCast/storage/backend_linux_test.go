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

func TestParseMountInfoRetainsCurrentDeviceIdentity(t *testing.T) {
	data := "36 25 8:17 / /run/fogcast/drives/usb\\040games rw,relatime - ext4 /dev/sda1 rw\n"
	got := parseMountInfo(data, "/run/fogcast/drives/usb games")
	if got.Source != "/dev/sda1" || got.MajorMinor != "8:17" {
		t.Fatalf("mount identity %+v", got)
	}
	if mountMatchesDevice(got, "8:33") {
		t.Fatal("replugged device with a new block identity reused its stale mount")
	}
	if !mountMatchesDevice(got, "8:17") {
		t.Fatal("mount did not match its current block identity")
	}
	if got := parseMountInfo(data, "/run/fogcast/drives/other"); got.Source != "" || got.MajorMinor != "" {
		t.Fatalf("matched unrelated mount %+v", got)
	}
}
