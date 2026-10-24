package zx81machine

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestMachineROMHasPinnedSizeAndDigest(t *testing.T) {
	image, err := MachineROM()
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != MachineROMBytes || fmt.Sprintf("%x", sha256.Sum256(image)) != machineROMSHA256 {
		t.Fatalf("machine ROM size=%d sha256=%x", len(image), sha256.Sum256(image))
	}
}
