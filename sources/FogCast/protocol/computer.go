package protocol

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/spectrumdisks"
	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
	"github.com/DeanoC/FogCast/internal/spectrumtapes"
	"github.com/DeanoC/FogCast/protocol/internal/generated"
)

// Home-computer ABI (fes.computer 1.0) contracts. Values come from the
// mister-packages emitter; FogCast never infers them from a core ID.
const (
	ComputerABIID = generated.FesComputerABIID
	// KeyboardHIDRows is the fes.keyboard.hid 1.0 row count: eight
	// Keyboard/Keypad usage rows followed by the modifier row.
	KeyboardHIDRows = int(generated.FesComputerKeyboardRowCount)
	// MaxComputerMediaBytes bounds one media unit under this ABI. A unit's
	// own limits come only from its interface and the live runtime report.
	MaxComputerMediaBytes int64 = int64(generated.FesComputerMediaMaxBytes)
	// ComputerMediaChunkBytes is the only chunk size the ABI admits.
	ComputerMediaChunkBytes uint32 = generated.FesComputerMediaChunkMaxBytes
	// DiskRole is the library media role of a home-computer floppy image.
	DiskRole = "disk"
	// Apple2FloppyBytes is the exact DOS 3.3 order image size.
	Apple2FloppyBytes int64 = int64(generated.FesComputerApple2FloppyBytes)
	// Apple2FloppyUnit is the media unit fes.media.apple2-floppy occupies.
	Apple2FloppyUnit uint8 = uint8(generated.FesComputerApple2FloppyUnit)
	// CassetteRole is the library media role of a Spectrum tape or ZX81 .p image.
	CassetteRole = "cassette"
	// SpectrumTapeMinBytes is the smallest fes.media.spectrum-tape PZX stream.
	SpectrumTapeMinBytes int64 = int64(generated.FesComputerSpectrumTapeMinBytes)
	// SpectrumTapeMaxBytes is the largest fes.media.spectrum-tape PZX stream.
	SpectrumTapeMaxBytes int64 = int64(generated.FesComputerSpectrumTapeMaxBytes)
	// SpectrumTapeImageMaxBytes is the largest .tzx, .tap, .pzx or .csw image
	// the host converts into that stream.
	SpectrumTapeImageMaxBytes int64 = spectrumtapes.MaxInputBytes
	// SpectrumTapeUnit is the media unit fes.media.spectrum-tape occupies.
	SpectrumTapeUnit uint8 = uint8(generated.FesComputerSpectrumTapeUnit)
	// SpectrumDiskUnit is the media unit fes.media.spectrum-disk occupies,
	// beside the unit-0 cassette.
	SpectrumDiskUnit uint8 = uint8(generated.FesComputerSpectrumDiskUnit)
	// SpectrumDiskMinBytes and MaxBytes bound the raw track image the unit
	// holds (internal/spectrumdisks).
	SpectrumDiskMinBytes int64 = int64(generated.FesComputerSpectrumDiskMinBytes)
	SpectrumDiskMaxBytes int64 = int64(generated.FesComputerSpectrumDiskMaxBytes)
	// SpectrumDiskImageMaxBytes is the largest .trd or .scl image the host
	// converts into that raw track image.
	SpectrumDiskImageMaxBytes int64 = spectrumdisks.MaxInputBytes
	// Zx81TapeMinBytes is the smallest fes.media.zx81-tape image.
	Zx81TapeMinBytes int64 = int64(generated.FesComputerZx81TapeMinBytes)
	// Zx81TapeMaxBytes is the largest fes.media.zx81-tape image.
	Zx81TapeMaxBytes int64 = int64(generated.FesComputerZx81TapeMaxBytes)
	// Zx81TapeUnit is the media unit fes.media.zx81-tape occupies.
	Zx81TapeUnit uint8 = uint8(generated.FesComputerZx81TapeUnit)
	// C64DiskBytes is the exact 35-track D64 image size.
	C64DiskBytes int64 = int64(generated.FesComputerC64DiskBytes)
	// C64DiskUnit is the media unit fes.media.c64-disk occupies.
	C64DiskUnit uint8 = uint8(generated.FesComputerC64DiskUnit)
	// AtariStFloppyBytes is an 80-track, two-sided, nine-sector raw ST image.
	AtariStFloppyBytes int64 = int64(generated.FesComputerAtariStFloppyBytes)
	// AtariStFloppyGeometryMinBytes and MaxBytes bound the opt-in layouts.
	AtariStFloppyGeometryMinBytes int64 = int64(generated.FesComputerAtariStFloppyGeometryMinBytes)
	AtariStFloppyGeometryMaxBytes int64 = int64(generated.FesComputerAtariStFloppyGeometryMaxBytes)
	// AtariStFloppyUnit is the media unit fes.media.atari-st-floppy occupies.
	AtariStFloppyUnit uint8 = uint8(generated.FesComputerAtariStFloppyUnit)
	// AtaDiskMinBytes and AtaDiskMaxBytes are the fixed ATA disk status limits.
	AtaDiskMinBytes int64 = int64(generated.FesComputerAtaDiskMinBytes)
	AtaDiskMaxBytes int64 = int64(generated.FesComputerAtaDiskMaxBytes)
	// AtaDiskChunkBytes is one fixed-disk transfer chunk.
	AtaDiskChunkBytes uint32 = generated.FesComputerAtaDiskChunkBytes
	// AtaDiskUnit is the media unit fes.media.ata-disk occupies.
	AtaDiskUnit uint8 = uint8(generated.FesComputerAtaDiskUnit)
	// ComputerMediaTransport names the fes.computer media-unit delivery.
	ComputerMediaTransport = "fes-computer-media-unit-v1"
	// MediaUnitHeader carries the addressed unit on target media requests.
	MediaUnitHeader = "X-FogCast-Media-Unit"
)

// MediaUnitStates are the runtime's reported unit states.
const (
	MediaUnitEmpty   = "empty"
	MediaUnitLoading = "loading"
	MediaUnitReady   = "ready"
)

// KeyboardHIDInterface is the exact USB HID key-state contract.
func KeyboardHIDInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceKeyboardHidID, Major: generated.FesComputerInterfaceKeyboardHidMajor, Minor: generated.FesComputerInterfaceKeyboardHidMinor}
}

// Apple2FloppyInterface is the unit-0 Apple II DOS-order floppy contract.
func Apple2FloppyInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaApple2FloppyID, Major: generated.FesComputerInterfaceMediaApple2FloppyMajor, Minor: generated.FesComputerInterfaceMediaApple2FloppyMinor}
}

// SpectrumTapeInterface is the unit-0 ZX Spectrum .tap contract.
func SpectrumTapeInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaSpectrumTapeID, Major: generated.FesComputerInterfaceMediaSpectrumTapeMajor, Minor: generated.FesComputerInterfaceMediaSpectrumTapeMinor}
}

// Spectrum snapshot interfaces. A package whose cassette unit also runs
// restore streams (internal/spectrumsnapshots) declares them as optional
// interfaces: fes.media.spectrum-snapshot 1.0 for 48K snapshots, plus
// -128 1.0 for 128K ones and -plus3 1.0 for +2A/+3 ones. They add no unit
// and no capability bit; the unit still carries fes.media.spectrum-tape.
const (
	SpectrumSnapshotInterfaceID      = "fes.media.spectrum-snapshot"
	SpectrumSnapshot128InterfaceID   = "fes.media.spectrum-snapshot-128"
	SpectrumSnapshotPlus3InterfaceID = "fes.media.spectrum-snapshot-plus3"
)

// SpectrumSnapshotRuns reports whether the descriptor's Spectrum cassette
// unit runs restore streams of machine.
func SpectrumSnapshotRuns(descriptor corepackage.Descriptor, machine spectrumsnapshots.Machine) bool {
	if !DeclaresSpectrumTape(descriptor) {
		return false
	}
	declared := func(id string) bool {
		for _, i := range descriptor.Interfaces {
			if i.ID == id && i.Major == 1 && i.Minor == 0 {
				return true
			}
		}
		return false
	}
	switch machine {
	case spectrumsnapshots.Machine48K:
		return declared(SpectrumSnapshotInterfaceID)
	case spectrumsnapshots.Machine128K:
		return declared(SpectrumSnapshotInterfaceID) && declared(SpectrumSnapshot128InterfaceID)
	case spectrumsnapshots.MachinePlus3:
		return declared(SpectrumSnapshotInterfaceID) && declared(SpectrumSnapshot128InterfaceID) &&
			declared(SpectrumSnapshotPlus3InterfaceID)
	}
	return false
}

// SpectrumDiskInterface is the unit-1 Spectrum raw track disk contract.
func SpectrumDiskInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaSpectrumDiskID, Major: generated.FesComputerInterfaceMediaSpectrumDiskMajor, Minor: generated.FesComputerInterfaceMediaSpectrumDiskMinor}
}

// SpectrumDiskTrdosInterfaceID and SpectrumDiskPlus3InterfaceID name the
// disk unit's controller: a package whose unit-1 disk is a Beta 128 running
// TR-DOS, or the +3's uPD765A, declares one as an optional 1.0 interface, and
// the host then converts .trd and .scl, or .dsk, images for it. They add no
// unit and no capability bit.
const (
	SpectrumDiskTrdosInterfaceID = "fes.media.spectrum-disk-trdos"
	SpectrumDiskPlus3InterfaceID = "fes.media.spectrum-disk-plus3"
)

// SpectrumDiskSystem reports the disk system of the descriptor's unit-1
// Spectrum disk: the one controller it declares beside the cassette.
func SpectrumDiskSystem(descriptor corepackage.Descriptor) (spectrumdisks.System, bool) {
	if !ComputerABI(descriptor.ABI.ID, descriptor.ABI.Major, descriptor.ABI.Minor) {
		return 0, false
	}
	tape, disk, trdos, plus3 := false, false, false, false
	wantTape, wantDisk := SpectrumTapeInterface(), SpectrumDiskInterface()
	for _, i := range descriptor.Interfaces {
		if i.ID == wantTape.ID && i.Major == int64(wantTape.Major) && i.Minor == int64(wantTape.Minor) {
			tape = true
		}
		if i.ID == wantDisk.ID && i.Major == int64(wantDisk.Major) && i.Minor == int64(wantDisk.Minor) && i.Required {
			disk = true
		}
		if i.Major == 1 && i.Minor == 0 {
			trdos = trdos || i.ID == SpectrumDiskTrdosInterfaceID
			plus3 = plus3 || i.ID == SpectrumDiskPlus3InterfaceID
		}
	}
	if !tape || !disk || trdos == plus3 {
		return 0, false
	}
	if plus3 {
		return spectrumdisks.Plus3, true
	}
	return spectrumdisks.TRDOS, true
}

// Zx81TapeInterface is the unit-0 ZX81 .p contract.
func Zx81TapeInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaZx81TapeID, Major: generated.FesComputerInterfaceMediaZx81TapeMajor, Minor: generated.FesComputerInterfaceMediaZx81TapeMinor}
}

// Zx81TapeWriteInterface is the snapshot contract for a ROM SAVE .p.
func Zx81TapeWriteInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaZx81TapeWriteID, Major: generated.FesComputerInterfaceMediaZx81TapeWriteMajor, Minor: generated.FesComputerInterfaceMediaZx81TapeWriteMinor}
}

// Zx81TapeWriteCapable reports an active computer that can publish a SAVE image
// even when the cassette unit itself is empty.
func Zx81TapeWriteCapable(p *CorePackageStatus) bool {
	return p != nil && ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor)) &&
		activeInterface(p, Zx81TapeInterface()) && activeInterface(p, Zx81TapeWriteInterface())
}

// C64DiskInterface is the unit-0 Commodore D64 contract.
func C64DiskInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaC64DiskID, Major: generated.FesComputerInterfaceMediaC64DiskMajor, Minor: generated.FesComputerInterfaceMediaC64DiskMinor}
}

// AtariStFloppyInterface is the unit-0 Atari ST raw floppy contract.
func AtariStFloppyInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtariStFloppyID, Major: generated.FesComputerInterfaceMediaAtariStFloppyMajor, Minor: generated.FesComputerInterfaceMediaAtariStFloppyMinor}
}

// AtaDiskInterface is the fixed pc386 disk contract.
func AtaDiskInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtaDiskID, Major: generated.FesComputerInterfaceMediaAtaDiskMajor, Minor: generated.FesComputerInterfaceMediaAtaDiskMinor}
}

func AtariStFloppyGeometryInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtariStFloppyGeometryID, Major: generated.FesComputerInterfaceMediaAtariStFloppyGeometryMajor, Minor: generated.FesComputerInterfaceMediaAtariStFloppyGeometryMinor}
}
func AtariStFloppyGeometryCapable(p *CorePackageStatus) bool {
	return p != nil && ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor)) &&
		activeInterface(p, AtariStFloppyInterface()) && activeInterface(p, AtariStFloppyGeometryInterface())
}
func AdmitAtariStFloppySize(size int64) bool {
	_, ok := corepackage.AtariStGeometryForSize(size)
	return ok
}

// ComputerABI reports the exact fes.computer 1.0 contract.
func ComputerABI(id string, major, minor int64) bool {
	return id == ComputerABIID && major == int64(generated.FesComputerABIMajor) && minor == 0
}

// ComputerMediaABI recognizes fes.computer 1.0 and 1.1 in runtime status.
// Features with a 1.0-only contract continue to use ComputerABI.
func ComputerMediaABI(id string, major, minor int64) bool {
	return id == ComputerABIID && major == int64(generated.FesComputerABIMajor) && minor >= 0 && minor <= int64(generated.FesComputerABIMinor)
}

// ComputerAtaDiskABI reports a supported fes.computer minor that includes ATA disks.
func ComputerAtaDiskABI(id string, major, minor int64) bool {
	return ComputerMediaABI(id, major, minor) && minor >= 1
}

func activeComputer(p *CorePackageStatus) bool {
	return p != nil && ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor))
}

func activeComputerMedia(p *CorePackageStatus) bool {
	return p != nil && ComputerMediaABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor))
}

func activeInterface(p *CorePackageStatus, want RuntimeContract) bool {
	if p == nil {
		return false
	}
	for _, i := range p.ActiveInterfaces {
		if (RuntimeContract{ID: i.ID, Major: i.Major, Minor: i.Minor}) == want {
			return true
		}
	}
	return false
}

func MouseRelativeInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMouseRelativeID, Major: generated.FesComputerInterfaceMouseRelativeMajor, Minor: generated.FesComputerInterfaceMouseRelativeMinor}
}

func MouseRelativeCapable(p *CorePackageStatus) bool {
	return activeComputerMedia(p) && activeInterface(p, MouseRelativeInterface())
}

// KeyboardHIDCapable reports an active supported fes.computer generation that
// negotiated fes.keyboard.hid 1.0. Such sessions forward physical key state as
// USB HID usages; the core owns every character mapping.
func KeyboardHIDCapable(p *CorePackageStatus) bool {
	return activeComputerMedia(p) && activeInterface(p, KeyboardHIDInterface())
}

// MediaUnitStatus is one runtime-reported media unit. The runtime reads its
// live state while a fes.computer generation is active.
type MediaUnitStatus struct {
	Unit        uint8            `json:"unit"`
	Interface   RuntimeContract  `json:"interface"`
	MinBytes    uint32           `json:"min_bytes"`
	MaxBytes    uint32           `json:"max_bytes"`
	ChunkBytes  uint32           `json:"chunk_bytes"`
	State       string           `json:"state"`
	Saveable    *bool            `json:"saveable,omitempty"`
	Persistence *MediaDataStatus `json:"persistence,omitempty"`
}

// CanSave reports whether this unit supports an explicit in-session save.
// Older status responses omit the field and retain their existing behavior.
func (u MediaUnitStatus) CanSave() bool { return u.Saveable == nil || *u.Saveable }

// Valid checks the unit against the ABI and, for known media interfaces, the
// exact unit and size the interface defines.
func (u MediaUnitStatus) Valid() bool {
	if u.Interface == AtaDiskInterface() && u.CanSave() {
		return false
	}
	if u.Persistence != nil {
		if u.Unit >= uint8(generated.FesComputerMediaUnitCount) || u.State != MediaUnitReady || !u.Persistence.Valid() {
			return false
		}
		switch u.Interface {
		case AtariStFloppyInterface():
			if u.Unit != AtariStFloppyUnit {
				return false
			}
		case AtaDiskInterface():
			if u.Unit != AtaDiskUnit || u.CanSave() {
				return false
			}
		default:
			return false
		}
	}
	maxBytes := MaxComputerMediaBytes
	chunkBytes := ComputerMediaChunkBytes
	if u.Interface.ID == AtaDiskInterface().ID {
		maxBytes = AtaDiskMaxBytes
		chunkBytes = AtaDiskChunkBytes
	}
	if u.Unit >= uint8(generated.FesComputerMediaUnitCount) || u.MinBytes < generated.FesComputerMediaMinBytes ||
		u.MinBytes > u.MaxBytes || int64(u.MaxBytes) > maxBytes || u.ChunkBytes != chunkBytes ||
		u.Interface.ID == "" || u.Interface.Major == 0 {
		return false
	}
	switch u.State {
	case MediaUnitEmpty, MediaUnitLoading, MediaUnitReady:
	default:
		return false
	}
	switch u.Interface.ID {
	case Apple2FloppyInterface().ID:
		return u.Interface == Apple2FloppyInterface() && u.Unit == Apple2FloppyUnit &&
			int64(u.MinBytes) == Apple2FloppyBytes && int64(u.MaxBytes) == Apple2FloppyBytes
	case C64DiskInterface().ID:
		return u.Interface == C64DiskInterface() && u.Unit == C64DiskUnit &&
			int64(u.MinBytes) == C64DiskBytes && int64(u.MaxBytes) == C64DiskBytes
	case AtariStFloppyInterface().ID:
		return u.Interface == AtariStFloppyInterface() && u.Unit == AtariStFloppyUnit &&
			((int64(u.MinBytes) == AtariStFloppyBytes && int64(u.MaxBytes) == AtariStFloppyBytes) ||
				(int64(u.MinBytes) == AtariStFloppyGeometryMinBytes && int64(u.MaxBytes) == AtariStFloppyGeometryMaxBytes))
	case AtaDiskInterface().ID:
		return u.Interface == AtaDiskInterface() && u.Unit == AtaDiskUnit &&
			int64(u.MinBytes) == AtaDiskMinBytes && int64(u.MaxBytes) == AtaDiskMaxBytes &&
			u.ChunkBytes == AtaDiskChunkBytes &&
			((u.State == MediaUnitReady && u.Persistence != nil) ||
				(u.Persistence == nil && (u.State == MediaUnitEmpty || u.State == MediaUnitLoading)))
	}
	if u.Interface.ID == SpectrumTapeInterface().ID {
		return u.Interface == SpectrumTapeInterface() && u.Unit == SpectrumTapeUnit &&
			int64(u.MinBytes) == SpectrumTapeMinBytes && int64(u.MaxBytes) == SpectrumTapeMaxBytes
	}
	if u.Interface.ID == Zx81TapeInterface().ID {
		return u.Interface == Zx81TapeInterface() && u.Unit == Zx81TapeUnit &&
			int64(u.MinBytes) == Zx81TapeMinBytes && int64(u.MaxBytes) == Zx81TapeMaxBytes
	}
	if u.Interface.ID == SpectrumDiskInterface().ID {
		return u.Interface == SpectrumDiskInterface() && u.Unit == SpectrumDiskUnit &&
			int64(u.MinBytes) == SpectrumDiskMinBytes && int64(u.MaxBytes) == SpectrumDiskMaxBytes
	}
	return true
}

// MediaUnit returns the observed unit of an active fes.computer generation
// when its interface is also active. Unknown or absent units return false.
func MediaUnit(p *CorePackageStatus, unit uint8) (MediaUnitStatus, bool) {
	if !activeComputerMedia(p) {
		return MediaUnitStatus{}, false
	}
	for _, u := range p.MediaUnits {
		if u.Unit == unit && u.Valid() && activeInterface(p, u.Interface) {
			if u.Interface == AtaDiskInterface() && !ComputerAtaDiskABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor)) {
				continue
			}
			if !ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor)) && u.Interface != AtaDiskInterface() {
				continue
			}
			if u.Interface == AtariStFloppyInterface() && int64(u.MaxBytes) != AtariStFloppyBytes && !AtariStFloppyGeometryCapable(p) {
				continue
			}
			return u, true
		}
	}
	return MediaUnitStatus{}, false
}

// declaredComputerMedia projects known media interfaces of an exact
// fes.computer 1.0 descriptor.
func declaredComputerMedia(descriptor corepackage.Descriptor) []CoreMediaCapability {
	result := make([]CoreMediaCapability, 0)
	if !ComputerABI(descriptor.ABI.ID, descriptor.ABI.Major, descriptor.ABI.Minor) {
		return result
	}
	floppy := Apple2FloppyInterface()
	tape := SpectrumTapeInterface()
	disk := C64DiskInterface()
	stFloppy := AtariStFloppyInterface()
	for _, contract := range descriptor.Interfaces {
		switch {
		case contract.ID == floppy.ID && contract.Major == int64(floppy.Major) && contract.Minor == int64(floppy.Minor):
			unit := Apple2FloppyUnit
			result = append(result, CoreMediaCapability{Role: DiskRole, Format: "apple2-dos-order",
				MinBytes: Apple2FloppyBytes, MaxBytes: Apple2FloppyBytes, Interface: floppy,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".dsk", ".do"}})
		case contract.ID == disk.ID && contract.Major == int64(disk.Major) && contract.Minor == int64(disk.Minor):
			unit := C64DiskUnit
			result = append(result, CoreMediaCapability{Role: DiskRole, Format: "c64-d64",
				MinBytes: C64DiskBytes, MaxBytes: C64DiskBytes, Interface: disk,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".d64"}})
		case contract.ID == stFloppy.ID && contract.Major == int64(stFloppy.Major) && contract.Minor == int64(stFloppy.Minor):
			unit := AtariStFloppyUnit
			minBytes, maxBytes := AtariStFloppyBytes, AtariStFloppyBytes
			if corepackage.DeclaresAtariStGeometry(descriptor) {
				minBytes, maxBytes = AtariStFloppyGeometryMinBytes, AtariStFloppyGeometryMaxBytes
			}
			result = append(result, CoreMediaCapability{Role: DiskRole, Format: "atari-st-floppy",
				MinBytes: minBytes, MaxBytes: maxBytes, Interface: stFloppy,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".st"}})
		}
		if contract.ID == tape.ID && contract.Major == int64(tape.Major) && contract.Minor == int64(tape.Minor) {
			// The library holds tape images, and snapshots where the package
			// runs them; the host converts one to the unit's PZX or restore
			// stream when it is inserted.
			unit := SpectrumTapeUnit
			extensions := append([]string(nil), spectrumtapes.Extensions...)
			if declaresSpectrumSnapshots(descriptor) {
				extensions = append(extensions, spectrumsnapshots.Extensions...)
			}
			result = append(result, CoreMediaCapability{Role: CassetteRole, Format: "spectrum-tape",
				MinBytes: 1, MaxBytes: SpectrumTapeImageMaxBytes, Interface: tape,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: extensions})
		}
		spectrumDisk := SpectrumDiskInterface()
		system, declared := SpectrumDiskSystem(descriptor)
		if contract.ID == spectrumDisk.ID && contract.Major == int64(spectrumDisk.Major) &&
			contract.Minor == int64(spectrumDisk.Minor) && declared {
			// The library holds the system's .trd and .scl, or .dsk, images;
			// the host converts one to the unit's raw track image when it is
			// inserted.
			unit := SpectrumDiskUnit
			format := "spectrum-disk-trdos"
			if system == spectrumdisks.Plus3 {
				format = "spectrum-disk-plus3"
			}
			result = append(result, CoreMediaCapability{Role: DiskRole, Format: format,
				MinBytes: 1, MaxBytes: SpectrumDiskImageMaxBytes, Interface: spectrumDisk,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: system.Extensions()})
		}
		zx81 := Zx81TapeInterface()
		if contract.ID == zx81.ID && contract.Major == int64(zx81.Major) && contract.Minor == int64(zx81.Minor) {
			unit := Zx81TapeUnit
			result = append(result, CoreMediaCapability{Role: CassetteRole, Format: "zx81-p",
				MinBytes: Zx81TapeMinBytes, MaxBytes: Zx81TapeMaxBytes, Interface: zx81,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".p"}})
		}
	}
	return result
}

func declaresSpectrumSnapshots(descriptor corepackage.Descriptor) bool {
	for _, i := range descriptor.Interfaces {
		if i.ID == SpectrumSnapshotInterfaceID && i.Major == 1 && i.Minor == 0 {
			return true
		}
	}
	return false
}

// DeclaresDiskMedia reports whether the descriptor has a disk unit (Apple II
// floppy, C64 D64 or Atari ST floppy on unit 0, or a TR-DOS disk on unit 1).
func DeclaresDiskMedia(descriptor corepackage.Descriptor) bool {
	for _, capability := range declaredComputerMedia(descriptor) {
		if capability.Role == DiskRole {
			return true
		}
	}
	return false
}

// DeclaresSpectrumTape reports whether the descriptor has the Spectrum cassette unit.
func DeclaresSpectrumTape(descriptor corepackage.Descriptor) bool {
	for _, capability := range declaredComputerMedia(descriptor) {
		if capability.Format == "spectrum-tape" {
			return true
		}
	}
	return false
}

// DeclaresZx81Tape reports whether the descriptor has the ZX81 .p unit.
func DeclaresZx81Tape(descriptor corepackage.Descriptor) bool {
	for _, capability := range declaredComputerMedia(descriptor) {
		if capability.Format == "zx81-p" {
			return true
		}
	}
	return false
}

// ZX81TapeSelectionRole is the library role for a .p file on this package.
// A declared fes.media.zx81-tape unit uses cassette. A legacy fes.media.blob
// declaration uses blob. The tape unit wins when a package declares both.
func ZX81TapeSelectionRole(media []CoreMediaCapability) (string, bool) {
	blob := RuntimeContract{ID: "fes.media.blob", Major: 1}
	legacy := false
	for _, cap := range media {
		if cap.Role == CassetteRole && cap.Format == "zx81-p" && cap.Interface == Zx81TapeInterface() {
			return CassetteRole, true
		}
		if cap.Role == "blob" && cap.Interface == blob {
			legacy = true
		}
	}
	if legacy {
		return "blob", true
	}
	return "", false
}

// AdmitDiskMediaName accepts only DOS-order .dsk/.do basenames. ProDOS-order
// .po and nibble .nib images are different formats and are refused.
func AdmitDiskMediaName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".dsk", ".do":
		return true
	default:
		return false
	}
}

// AdmitSpectrumTapeName accepts a ZX Spectrum .tzx, .tap, .pzx or .csw
// basename.
func AdmitSpectrumTapeName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return spectrumtapes.Admit(name)
}

// AdmitSpectrumSnapshotName accepts a Spectrum .z80, .sna or .szx snapshot basename.
func AdmitSpectrumSnapshotName(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\") || name != filepath.Base(name) {
		return false
	}
	return spectrumsnapshots.Admit(name)
}

// AdmitSpectrumCassetteName accepts a Spectrum tape image or a .z80, .sna or .szx
// snapshot basename; the active package decides whether it runs snapshots.
func AdmitSpectrumCassetteName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return spectrumtapes.Admit(name) || spectrumsnapshots.Admit(name)
}

// AdmitSpectrumDiskName accepts a TR-DOS .trd or .scl, or +3 .dsk, basename.
// A .dsk is also an Apple II disk name; the active package decides which.
func AdmitSpectrumDiskName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return spectrumdisks.Admit(name)
}

// AdmitSpectrumDiskSize reports whether a Spectrum disk image may be converted.
func AdmitSpectrumDiskSize(size int64) bool {
	return size >= 1 && size <= SpectrumDiskImageMaxBytes
}

// AdmitSpectrumTapeSize reports whether a Spectrum tape image may be
// converted; the converted stream must then fit the unit.
func AdmitSpectrumTapeSize(size int64) bool {
	return spectrumtapes.AdmitSize(size)
}

// AdmitZx81TapeSize reports whether a .p image fits the ZX81 tape unit.
func AdmitZx81TapeSize(size int64) bool {
	return size >= Zx81TapeMinBytes && size <= Zx81TapeMaxBytes
}

// AdmitC64DiskName accepts a Commodore D64 basename.
func AdmitC64DiskName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return strings.ToLower(filepath.Ext(name)) == ".d64"
}

// AdmitAtariStFloppyName accepts a raw Atari ST .st basename.
func AdmitAtariStFloppyName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return strings.ToLower(filepath.Ext(name)) == ".st"
}

// AdmitComputerDiskName accepts a supported home-computer disk image name.
func AdmitComputerDiskName(name string) bool {
	return AdmitDiskMediaName(name) || AdmitC64DiskName(name) || AdmitAtariStFloppyName(name)
}

// AdmitLiveMediaName accepts the names of every live media form: ZX81 tapes,
// home-computer disks, Spectrum cassettes and snapshots, and TR-DOS disks.
// The active package then chooses the exact contract.
func AdmitLiveMediaName(name string) bool {
	return AdmitTapeMediaName(name) || AdmitComputerDiskName(name) || AdmitSpectrumCassetteName(name) ||
		AdmitSpectrumDiskName(name)
}

// MediaUnitBinding names one unit of the already active target package
// generation. Target and TargetID are host-only bindings.
type MediaUnitBinding struct {
	PackageID  string
	Generation uint64
	Unit       uint8
	Target     string
	TargetID   string
}

func (b MediaUnitBinding) Valid() bool {
	return ValidateDigest(b.PackageID) == nil && b.Generation != 0 && b.Unit < uint8(generated.FesComputerMediaUnitCount)
}

// Matches requires the exact active, error-free generation and an observed
// unit whose interface is active.
func (b MediaUnitBinding) Matches(s Status) bool {
	if !b.Valid() || s.State != StateActive || !s.Development || s.Recovery != "" || s.LastError != nil ||
		s.CorePackage == nil || s.CorePackage.PackageID != b.PackageID || s.CorePackage.Generation != b.Generation {
		return false
	}
	unit, ok := MediaUnit(s.CorePackage, b.Unit)
	return ok && unit.Interface != AtaDiskInterface()
}

// AcceptsSize checks the exact byte count against the observed unit limits.
func (b MediaUnitBinding) AcceptsSize(s Status, size int64) bool {
	if !b.Matches(s) {
		return false
	}
	unit, _ := MediaUnit(s.CorePackage, b.Unit)
	return size >= int64(unit.MinBytes) && size <= int64(unit.MaxBytes) &&
		(unit.Interface != AtariStFloppyInterface() || AdmitAtariStFloppySize(size))
}

func (b MediaUnitBinding) SetHeaders(h http.Header) {
	DevelopmentMediaBinding{PackageID: b.PackageID, Generation: b.Generation, Target: b.Target, TargetID: b.TargetID}.SetHeaders(h)
	h.Set(MediaUnitHeader, strconv.FormatUint(uint64(b.Unit), 10))
}

// MediaUnitHeaders parses the exact binding headers of a target media request.
func MediaUnitHeaders(h http.Header) (MediaUnitBinding, bool) {
	base, ok := DevelopmentMediaHeaders(h)
	units := h.Values(MediaUnitHeader)
	if !ok || len(units) != 1 {
		return MediaUnitBinding{}, false
	}
	unit, err := strconv.ParseUint(units[0], 10, 8)
	b := MediaUnitBinding{PackageID: base.PackageID, Generation: base.Generation, Unit: uint8(unit), Target: base.Target, TargetID: base.TargetID}
	return b, err == nil && strconv.FormatUint(unit, 10) == units[0] && b.Valid()
}

func MediaUnitRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "media unit delivery requires an exact-size image for a declared unit and a package generation", Phase: "request"}
}

func MediaUnitIdentityError() *APIError {
	return &APIError{Code: CodeBusy, Message: "media unit delivery requires the current active computer package generation with that unit", Phase: "admission"}
}

// DiskMediaRequestError is the live-media refusal for home-computer disks.
func DiskMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live disk media requires .dsk/.do (143360 bytes), .d64 (174848 bytes), or raw .st (720 KiB, or a declared 80..82-track, 1..2-head, 9..10-sector layout)", Phase: "request"}
}

// CassetteMediaRequestError is the live-media refusal for Spectrum tapes.
func CassetteMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live cassette media requires a .tzx, .tap, .pzx or .csw image of at most 32 MiB, or a .z80, .sna or .szx snapshot", Phase: "request"}
}

// CassetteConversionError reports a Spectrum tape image the host could not
// convert to the unit's PZX stream.
func CassetteConversionError(err error) *APIError {
	return &APIError{Code: CodeBadRequest, Message: "Spectrum tape image: " + err.Error(), Phase: "request"}
}

// SpectrumDiskRequestError is the live-media refusal for Spectrum disks.
func SpectrumDiskRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live Spectrum disk media requires a .trd or .scl image on a package with a TR-DOS drive, or a .dsk image on one with a +3 drive, of at most 1 MiB", Phase: "request"}
}

// SpectrumDiskConversionError reports a disk image the host could not
// convert to the unit's raw track image.
func SpectrumDiskConversionError(err error) *APIError {
	return &APIError{Code: CodeBadRequest, Message: "Spectrum disk image: " + err.Error(), Phase: "request"}
}

// SnapshotConversionError reports a Spectrum snapshot the host could not
// convert to a restore stream, or one the active package does not run.
func SnapshotConversionError(err error) *APIError {
	return &APIError{Code: CodeBadRequest, Message: "Spectrum snapshot: " + err.Error(), Phase: "request"}
}
