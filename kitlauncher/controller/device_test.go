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
