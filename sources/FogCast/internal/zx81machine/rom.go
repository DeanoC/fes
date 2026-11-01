// Package zx81machine contains the default machine firmware used by the
// supported FES ZX81 package.
package zx81machine

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
)

const (
	MachineROMBytes  = 8192
	machineROMSHA256 = "14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa"
)

//go:embed machine.rom
var files embed.FS

// MachineROM returns a verified copy of the pinned 8 KiB ZX81 BASIC image.
func MachineROM() ([]byte, error) {
	image, err := files.ReadFile("machine.rom")
	if err != nil || len(image) != MachineROMBytes {
		return nil, errors.New("bundled ZX81 machine ROM has an invalid size")
	}
	if fmt.Sprintf("%x", sha256.Sum256(image)) != machineROMSHA256 {
		return nil, errors.New("bundled ZX81 machine ROM failed its SHA-256 check")
	}
	return bytes.Clone(image), nil
}
