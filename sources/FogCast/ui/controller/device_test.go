package controller

import (
	"encoding/binary"
	"testing"
)

func TestOnlyPhysicalGamepadEligible(t *testing.T) {
	if eligible(6, "FogCast Virtual Gamepad", true) {
		t.Fatal("virtual feedback")
	}
	if eligible(3, "keyboard", false) {
		t.Fatal("keyboard selected")
	}
	if !eligible(3, "USB gamepad", true) {
		t.Fatal("physical pad rejected")
	}
	if !eligible(5, "Bluetooth gamepad", true) {
		t.Fatal("bluetooth pad rejected")
	}
}

func TestPhysicalKeyboardEligibleForPlayHID(t *testing.T) {
	if eligibleKeyboard(6, "FogCast Virtual Gamepad", true) {
		t.Fatal("virtual keyboard")
	}
	if eligibleKeyboard(3, "USB gamepad", false) {
		t.Fatal("pad without keys")
	}
	if !eligibleKeyboard(3, "USB Keyboard", true) {
		t.Fatal("physical keyboard rejected")
	}
}
func TestDecodeArchitectureAndDroppedEvents(t *testing.T) {
	for _, size := range []int{16, 24} {
		b := make([]byte, size)
		binary.LittleEndian.PutUint16(b[size-8:], 1)
		binary.LittleEndian.PutUint16(b[size-6:], 304)
		binary.LittleEndian.PutUint32(b[size-4:], 1)
		typ, code, v, err := decode(b)
		if err != nil || typ != 1 || code != 304 || v != 1 {
			t.Fatalf("%d decode: %d %d %d %v", size, typ, code, v, err)
		}
	}
	if _, _, _, err := decode(make([]byte, 15)); err == nil {
		t.Fatal("short event accepted")
	}
}

// SDL_CreateJoystickGUID: bus, CRC-16/ARC of the evdev name, then vendor,
// product and version. Without a vendor, the name follows the CRC.
func TestGUIDIsSDLs(t *testing.T) {
	pad := Device{Name: "123456789", Bus: 3, Vendor: 0x045e, Product: 0x028e, Version: 0x0114}
	if got := pad.GUID(); got != "03003dbb5e0400008e02000014010000" {
		t.Fatalf("guid %s", got)
	}
	if got := (Device{Name: "123456789", Bus: 5}).GUID(); got != "05003dbb313233343536373839000000" {
		t.Fatalf("vendorless guid %s", got)
	}
	long := Device{Name: "Arcade Stick Deluxe", Bus: 5}
	if got := long.GUID(); got[8:] != "417263616465205374696300" {
		t.Fatalf("vendorless guid keeps more than 11 name bytes: %s", got)
	}
}
