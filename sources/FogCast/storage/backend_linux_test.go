//go:build linux

package storage

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if got.Source != "/dev/sda1" || got.MajorMinor != "8:17" || got.FSType != "ext4" {
		t.Fatalf("mount identity %+v", got)
	}
	device := blockDevice{Path: "/dev/sda1", FSType: "ext4", MajorMinor: "8:17"}
	if mountMatchesDevice(got, blockDevice{Path: device.Path, FSType: device.FSType, MajorMinor: "8:33"}) {
		t.Fatal("replugged device with a new block identity reused its stale mount")
	}
	if !mountMatchesDevice(got, device) {
		t.Fatal("mount did not match its current block identity")
	}
	if got := parseMountInfo(data, "/run/fogcast/drives/other"); got.Source != "" || got.MajorMinor != "" {
		t.Fatalf("matched unrelated mount %+v", got)
	}
}

func TestNTFSFUSEMountMatchesResolvedCurrentDevicePath(t *testing.T) {
	dir := t.TempDir()
	devicePath := filepath.Join(dir, "sda1")
	otherDevicePath := filepath.Join(dir, "sdb1")
	if err := os.WriteFile(devicePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherDevicePath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	byUUIDPath := filepath.Join(dir, "by-uuid")
	if err := os.Symlink(devicePath, byUUIDPath); err != nil {
		t.Fatal(err)
	}
	device := blockDevice{Path: devicePath, FSType: "ntfs", MajorMinor: "8:1"}
	fuseMount := mountedVolume{Source: byUUIDPath, MajorMinor: "0:54", FSType: "fuseblk"}
	if !mountMatchesDevice(fuseMount, device) {
		t.Fatal("NTFS FUSE mount from the current resolved block device was rejected")
	}
	fuseMount.Source = otherDevicePath
	if mountMatchesDevice(fuseMount, device) {
		t.Fatal("NTFS FUSE mount from another block device was accepted")
	}
	fuseMount.Source = devicePath
	device.FSType = "exfat"
	if mountMatchesDevice(fuseMount, device) {
		t.Fatal("non-NTFS FUSE mount was accepted by source-path matching")
	}
	device.FSType = "ntfs"
	fuseMount.FSType = "ext4"
	if mountMatchesDevice(fuseMount, device) {
		t.Fatal("kernel filesystem mount bypassed its block identity check")
	}
}
