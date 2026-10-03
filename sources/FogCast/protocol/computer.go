package protocol

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
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
	// CassetteRole is the library media role of a ZX Spectrum .tap image.
	CassetteRole = "cassette"
	// SpectrumTapeMinBytes is the smallest fes.media.spectrum-tape image.
	SpectrumTapeMinBytes int64 = int64(generated.FesComputerSpectrumTapeMinBytes)
	// SpectrumTapeMaxBytes is the largest fes.media.spectrum-tape image.
	SpectrumTapeMaxBytes int64 = int64(generated.FesComputerSpectrumTapeMaxBytes)
	// SpectrumTapeUnit is the media unit fes.media.spectrum-tape occupies.
	SpectrumTapeUnit uint8 = uint8(generated.FesComputerSpectrumTapeUnit)
	// C64DiskBytes is the exact 35-track D64 image size.
	C64DiskBytes int64 = int64(generated.FesComputerC64DiskBytes)
	// C64DiskUnit is the media unit fes.media.c64-disk occupies.
	C64DiskUnit uint8 = uint8(generated.FesComputerC64DiskUnit)
	// AtariStFloppyBytes is an 80-track, two-sided, nine-sector raw ST image.
	AtariStFloppyBytes int64 = int64(generated.FesComputerAtariStFloppyBytes)
	// AtariStFloppyUnit is the media unit fes.media.atari-st-floppy occupies.
	AtariStFloppyUnit uint8 = uint8(generated.FesComputerAtariStFloppyUnit)
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

// C64DiskInterface is the unit-0 Commodore D64 contract.
func C64DiskInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaC64DiskID, Major: generated.FesComputerInterfaceMediaC64DiskMajor, Minor: generated.FesComputerInterfaceMediaC64DiskMinor}
}

// AtariStFloppyInterface is the unit-0 Atari ST raw floppy contract.
func AtariStFloppyInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtariStFloppyID, Major: generated.FesComputerInterfaceMediaAtariStFloppyMajor, Minor: generated.FesComputerInterfaceMediaAtariStFloppyMinor}
}

// ComputerABI reports the exact fes.computer 1.0 contract.
func ComputerABI(id string, major, minor int64) bool {
	return id == ComputerABIID && major == int64(generated.FesComputerABIMajor) && minor == int64(generated.FesComputerABIMinor)
}

func activeComputer(p *CorePackageStatus) bool {
	return p != nil && ComputerABI(p.ABI.ID, int64(p.ABI.Major), int64(p.ABI.Minor))
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

// KeyboardHIDCapable reports an active fes.computer 1.0 generation that
// negotiated fes.keyboard.hid 1.0. Such sessions forward physical key state as
// USB HID usages; the core owns every character mapping.
func KeyboardHIDCapable(p *CorePackageStatus) bool {
	return activeComputer(p) && activeInterface(p, KeyboardHIDInterface())
}

// MediaUnitStatus is one runtime-reported removable-media drive. The runtime
// reads it from live MediaInfo while a fes.computer generation is active.
type MediaUnitStatus struct {
	Unit       uint8           `json:"unit"`
	Interface  RuntimeContract `json:"interface"`
	MinBytes   uint32          `json:"min_bytes"`
	MaxBytes   uint32          `json:"max_bytes"`
	ChunkBytes uint32          `json:"chunk_bytes"`
	State      string          `json:"state"`
}

// Valid checks the unit against the ABI and, for known media interfaces, the
// exact unit and size the interface defines.
func (u MediaUnitStatus) Valid() bool {
	if u.Unit >= uint8(generated.FesComputerMediaUnitCount) || u.MinBytes < generated.FesComputerMediaMinBytes ||
		u.MinBytes > u.MaxBytes || int64(u.MaxBytes) > MaxComputerMediaBytes || u.ChunkBytes != ComputerMediaChunkBytes ||
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
			int64(u.MinBytes) == AtariStFloppyBytes && int64(u.MaxBytes) == AtariStFloppyBytes
	}
	if u.Interface.ID == SpectrumTapeInterface().ID {
		return u.Interface == SpectrumTapeInterface() && u.Unit == SpectrumTapeUnit &&
			int64(u.MinBytes) == SpectrumTapeMinBytes && int64(u.MaxBytes) == SpectrumTapeMaxBytes
	}
	return true
}

// MediaUnit returns the observed unit of an active fes.computer generation
// when its interface is also active. Unknown or absent units return false.
func MediaUnit(p *CorePackageStatus, unit uint8) (MediaUnitStatus, bool) {
	if !activeComputer(p) {
		return MediaUnitStatus{}, false
	}
	for _, u := range p.MediaUnits {
		if u.Unit == unit && u.Valid() && activeInterface(p, u.Interface) {
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
			result = append(result, CoreMediaCapability{Role: DiskRole, Format: "atari-st-floppy",
				MinBytes: AtariStFloppyBytes, MaxBytes: AtariStFloppyBytes, Interface: stFloppy,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".st"}})
		}
		if contract.ID == tape.ID && contract.Major == int64(tape.Major) && contract.Minor == int64(tape.Minor) {
			unit := SpectrumTapeUnit
			result = append(result, CoreMediaCapability{Role: CassetteRole, Format: "spectrum-tap",
				MinBytes: SpectrumTapeMinBytes, MaxBytes: SpectrumTapeMaxBytes, Interface: tape,
				Transport: ComputerMediaTransport, Unit: &unit, Extensions: []string{".tap"}})
		}
	}
	return result
}

// DeclaresDiskMedia reports whether the descriptor has a unit-0 disk
// (Apple II floppy, C64 D64 or Atari ST floppy).
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
		if capability.Role == CassetteRole {
			return true
		}
	}
	return false
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

// AdmitSpectrumTapeName accepts a ZX Spectrum .tap basename.
func AdmitSpectrumTapeName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	return strings.ToLower(filepath.Ext(name)) == ".tap"
}

// AdmitSpectrumTapeSize reports whether a .tap image fits the unit.
func AdmitSpectrumTapeSize(size int64) bool {
	return size >= SpectrumTapeMinBytes && size <= SpectrumTapeMaxBytes
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
// home-computer disks and Spectrum cassettes. The active package then chooses
// the exact contract.
func AdmitLiveMediaName(name string) bool {
	return AdmitTapeMediaName(name) || AdmitComputerDiskName(name) || AdmitSpectrumTapeName(name)
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
	_, ok := MediaUnit(s.CorePackage, b.Unit)
	return ok
}

// AcceptsSize checks the exact byte count against the observed unit limits.
func (b MediaUnitBinding) AcceptsSize(s Status, size int64) bool {
	if !b.Matches(s) {
		return false
	}
	unit, _ := MediaUnit(s.CorePackage, b.Unit)
	return size >= int64(unit.MinBytes) && size <= int64(unit.MaxBytes)
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
	return &APIError{Code: CodeBadRequest, Message: "live disk media requires .dsk/.do (143360 bytes), .d64 (174848 bytes), or .st (737280 bytes)", Phase: "request"}
}

// CassetteMediaRequestError is the live-media refusal for Spectrum .tap images.
func CassetteMediaRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Message: "live cassette media requires a .tap image of 1..65536 bytes", Phase: "request"}
}
