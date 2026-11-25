package protocol

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol/internal/generated"
)

// fes.media.pc-floppy 1.0: pc386's drive A: (unit 1, 3.5" 1.44M) and B:
// (unit 2, 5.25" 1.2M). A raw image's exact size names its format; no size
// fits both drives. A writable disk's saved image is keyed by core, game,
// base digest and PcFloppyRecordUnit, whichever drive holds it.
const (
	// FloppyRole is the library media role of a raw PC floppy image.
	FloppyRole         = "floppy"
	PcFloppyUnitA      = uint8(generated.FesComputerPcFloppyUnitA)
	PcFloppyUnitB      = uint8(generated.FesComputerPcFloppyUnitB)
	PcFloppyMinBytes   = int64(generated.FesComputerPcFloppyMinBytes)
	PcFloppyMaxBytes   = int64(generated.FesComputerPcFloppyMaxBytes)
	PcFloppyRecordUnit = uint8(1)
	// Pc386CoreID is the only core whose records use PcFloppyRecordUnit.
	Pc386CoreID = "fes.pc386"
	// FloppyImageHeader carries one launch disk: unit, digest and, for a
	// write-protected disk, "wp", as "1:<sha256>" or "2:<sha256>:wp".
	FloppyImageHeader = "X-FogCast-Floppy-Image"
	// MediaWriteProtectHeader marks a live pc386 floppy insert write-protected.
	MediaWriteProtectHeader = "X-FogCast-Media-Write-Protect"
)

// PcFloppyFormat is one admitted image size, its geometry and the drives that
// read it. scripts/consistency.py checks this table against the runtime's.
type PcFloppyFormat struct {
	Bytes                     int64
	Cylinders, Heads, Sectors int
	DriveA, DriveB            bool
}

var pcFloppyFormats = []PcFloppyFormat{
	{163840, 40, 1, 8, false, true},
	{184320, 40, 1, 9, false, true},
	{327680, 40, 2, 8, false, true},
	{368640, 40, 2, 9, false, true},
	{737280, 80, 2, 9, true, false},
	{1228800, 80, 2, 15, false, true},
	{1474560, 80, 2, 18, true, false},
	{1720320, 80, 2, 21, true, false},
	{2949120, 80, 2, 36, false, false},
}

func PcFloppyInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaPcFloppyID, Major: generated.FesComputerInterfaceMediaPcFloppyMajor, Minor: generated.FesComputerInterfaceMediaPcFloppyMinor}
}

// DeclaresPcFloppy reports a fes.computer 1.1 descriptor with drives A: and B:.
func DeclaresPcFloppy(descriptor corepackage.Descriptor) bool {
	return descriptor.ABI.ID == ComputerABIID && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 1 &&
		declaresRequired(descriptor, PcFloppyInterface())
}

// PcFloppyFormatFor returns the format of an image of size bytes.
func PcFloppyFormatFor(size int64) (PcFloppyFormat, bool) {
	for _, f := range pcFloppyFormats {
		if f.Bytes == size {
			return f, true
		}
	}
	return PcFloppyFormat{}, false
}

// AdmitPcFloppySize reports an image size some drive reads.
func AdmitPcFloppySize(size int64) bool {
	f, ok := PcFloppyFormatFor(size)
	return ok && (f.DriveA || f.DriveB)
}

// AdmitStagedDiskImageSize reports a size the disk-image store holds: a hard
// disk or a PC floppy. Each launch path checks its own role's size after.
func AdmitStagedDiskImageSize(size int64) bool {
	return AdmitAtaDiskSize(size) || AdmitPcFloppySize(size)
}

// PcFloppyDrive is the one drive that reads an image of size bytes.
func PcFloppyDrive(size int64) (uint8, bool) {
	f, ok := PcFloppyFormatFor(size)
	switch {
	case ok && f.DriveA:
		return PcFloppyUnitA, true
	case ok && f.DriveB:
		return PcFloppyUnitB, true
	}
	return 0, false
}

// AdmitPcFloppyName accepts a raw PC floppy image's file name.
func AdmitPcFloppyName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".img") || strings.HasSuffix(lower, ".ima") || strings.HasSuffix(lower, ".vfd")
}

// PcFloppyUnit reports unit 1 or 2.
func PcFloppyUnit(unit uint8) bool { return unit == PcFloppyUnitA || unit == PcFloppyUnitB }

// PcFloppyDriveName is "A:" or "B:".
func PcFloppyDriveName(unit uint8) string {
	if unit == PcFloppyUnitB {
		return "B:"
	}
	return "A:"
}

// PcFloppySizeError refuses an image the chosen drive cannot read, naming
// the drive it needs.
func PcFloppySizeError(unit uint8, size int64) *APIError {
	message := "a PC floppy image must be 160K, 180K, 320K, 360K, 720K, 1.2M, 1.44M or 1.68M exactly"
	if drive, ok := PcFloppyDrive(size); ok && drive != unit {
		message = fmt.Sprintf("drive %s cannot read this disk; put it in %s", PcFloppyDriveName(unit), PcFloppyDriveName(drive))
	} else if f, known := PcFloppyFormatFor(size); known && !f.DriveA && !f.DriveB {
		message = "neither drive reads a 2.88M disk"
	}
	return &APIError{Code: CodeBadRequest, Message: message, Phase: "admission"}
}

// StagedFloppy names a launch disk the kit holds by digest, the drive it
// goes in and whether the guest may write it. Size is the staged file's.
type StagedFloppy struct {
	Unit           uint8  `json:"unit"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	WriteProtected bool   `json:"write_protected"`
}

func (f StagedFloppy) Valid() bool {
	drive, ok := PcFloppyDrive(f.Size)
	return ValidateDigest(f.SHA256) == nil && ok && drive == f.Unit
}

func (f StagedFloppy) header() string {
	value := strconv.Itoa(int(f.Unit)) + ":" + f.SHA256
	if f.WriteProtected {
		value += ":wp"
	}
	return value
}

func parseFloppyHeader(value string) (StagedFloppy, bool) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 || (len(parts) == 3 && parts[2] != "wp") ||
		(parts[0] != "1" && parts[0] != "2") || ValidateDigest(parts[1]) != nil {
		return StagedFloppy{}, false
	}
	return StagedFloppy{Unit: uint8(parts[0][0] - '0'), SHA256: parts[1], WriteProtected: len(parts) == 3}, true
}
