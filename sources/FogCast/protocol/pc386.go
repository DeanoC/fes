package protocol

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol/internal/generated"
)

// fes.computer 1.1 contracts the pc386 uses: the ATA hard disk the runtime
// writes into the DDR window, and the per-game fes.pc386.machine record.
const (
	// HardDiskRole is the library media role of a raw ATA hard-disk image.
	HardDiskRole = "hard-disk"
	// AtaDiskUnit is the unit fes.media.ata-disk occupies.
	AtaDiskUnit uint8 = uint8(generated.FesComputerAtaDiskUnit)
	// AtaDiskMinBytes and AtaDiskMaxBytes bound a raw image, a whole number
	// of AtaDiskSectorBytes sectors.
	AtaDiskMinBytes    int64 = int64(generated.FesComputerAtaDiskMinBytes)
	AtaDiskMaxBytes    int64 = int64(generated.FesComputerAtaDiskMaxBytes)
	AtaDiskSectorBytes int64 = int64(generated.FesComputerAtaDiskSectorBytes)
	// AtaDiskChunkBytes is the dirty-map and delta chunk size.
	AtaDiskChunkBytes uint32 = generated.FesComputerAtaDiskChunkBytes
	// AtaDiskTransport names delivery through the runtime's window writes.
	AtaDiskTransport = "fes-computer-ata-disk-window-v1"
	// DiskImageHeader names a staged image by its lowercase SHA-256.
	DiskImageHeader = "X-FogCast-Disk-Image"
	// DiskGeometryHeader carries cylinders/heads/sectors per track.
	DiskGeometryHeader = "X-FogCast-Disk-Geometry"
	// LibraryGameHeader names the library game that keys per-game core data.
	LibraryGameHeader = "X-FogCast-Library-Game"
	// MachineSettingsHeader carries a pc386 settings update as seven decimals.
	MachineSettingsHeader = "X-FogCast-Machine-Settings"
)

func AtaDiskInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfaceMediaAtaDiskID, Major: generated.FesComputerInterfaceMediaAtaDiskMajor, Minor: generated.FesComputerInterfaceMediaAtaDiskMinor}
}

func PersistenceWordsInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfacePersistenceWordsID, Major: generated.FesComputerInterfacePersistenceWordsMajor, Minor: generated.FesComputerInterfacePersistenceWordsMinor}
}

func Pc386MachineInterface() RuntimeContract {
	return RuntimeContract{ID: generated.FesComputerInterfacePc386MachineID, Major: generated.FesComputerInterfacePc386MachineMajor, Minor: generated.FesComputerInterfacePc386MachineMinor}
}

func declaresRequired(descriptor corepackage.Descriptor, want RuntimeContract) bool {
	for _, i := range descriptor.Interfaces {
		if i.Required && i.ID == want.ID && i.Major == int64(want.Major) && i.Minor == int64(want.Minor) {
			return true
		}
	}
	return false
}

// DeclaresAtaDisk reports a fes.computer 1.1 descriptor with the hard disk.
func DeclaresAtaDisk(descriptor corepackage.Descriptor) bool {
	return descriptor.ABI.ID == ComputerABIID && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 1 &&
		declaresRequired(descriptor, AtaDiskInterface())
}

// DeclaresPc386Machine reports a fes.computer 1.1 descriptor whose
// persistence words use the fes.pc386.machine layout, keyed per library game.
func DeclaresPc386Machine(descriptor corepackage.Descriptor) bool {
	return descriptor.ABI.ID == ComputerABIID && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 1 &&
		declaresRequired(descriptor, PersistenceWordsInterface()) && declaresRequired(descriptor, Pc386MachineInterface())
}

// HardDiskCapable reports an active generation that negotiated the hard disk.
func HardDiskCapable(p *CorePackageStatus) bool {
	return activeComputer(p) && activeInterface(p, AtaDiskInterface())
}

// HardDiskBound reports a ready hard disk bound to a library game's delta.
func HardDiskBound(p *CorePackageStatus) bool {
	u, ok := MediaUnit(p, AtaDiskUnit)
	return ok && u.Interface == AtaDiskInterface() && u.Persistence != nil
}

// StopSavesMedia reports a generation whose Stop publishes durable media, a
// writable ST floppy or a hard-disk delta, and so needs the longer budget.
func StopSavesMedia(p *CorePackageStatus) bool {
	return MediaDataBound(p) || HardDiskBound(p)
}

// MachineRecordCapable reports an active generation whose persistence words
// carry a per-game fes.pc386.machine record.
func MachineRecordCapable(p *CorePackageStatus) bool {
	return activeComputer(p) && activeInterface(p, PersistenceWordsInterface()) && activeInterface(p, Pc386MachineInterface())
}

// AdmitAtaDiskSize reports a raw image length the disk admits.
func AdmitAtaDiskSize(size int64) bool {
	return size >= AtaDiskMinBytes && size <= AtaDiskMaxBytes && size%AtaDiskSectorBytes == 0
}

// DiskGeometry is the CHS geometry the disk reports in IDENTIFY DEVICE and
// the partition table was written with.
type DiskGeometry struct {
	Cylinders       uint16 `json:"cylinders"`
	Heads           uint16 `json:"heads"`
	SectorsPerTrack uint16 `json:"sectors_per_track"`
}

// Valid checks the DiskCommit rules for an image of size bytes.
func (g DiskGeometry) Valid(size int64) bool {
	return AdmitAtaDiskSize(size) && g.Cylinders >= 1 && uint32(g.Cylinders) <= generated.FesComputerAtaDiskMaxCylinders &&
		g.Heads >= 1 && uint32(g.Heads) <= generated.FesComputerAtaDiskMaxHeads &&
		g.SectorsPerTrack >= 1 && uint32(g.SectorsPerTrack) <= generated.FesComputerAtaDiskMaxSectorsPerTrack &&
		int64(g.Cylinders)*int64(g.Heads)*int64(g.SectorsPerTrack) <= size/AtaDiskSectorBytes
}

// String is the DiskGeometryHeader form, cylinders/heads/sectors.
func (g DiskGeometry) String() string {
	return fmt.Sprintf("%d/%d/%d", g.Cylinders, g.Heads, g.SectorsPerTrack)
}

var diskGeometryRE = regexp.MustCompile(`^([1-9][0-9]{0,4})/([1-9][0-9]?)/([1-9][0-9]?)$`)

// ParseDiskGeometry reads the DiskGeometryHeader form; Valid still applies.
func ParseDiskGeometry(value string) (DiskGeometry, bool) {
	parts := diskGeometryRE.FindStringSubmatch(value)
	if parts == nil {
		return DiskGeometry{}, false
	}
	var fields [3]uint16
	for i := range fields {
		n, err := strconv.ParseUint(parts[i+1], 10, 16)
		if err != nil {
			return DiskGeometry{}, false
		}
		fields[i] = uint16(n)
	}
	return DiskGeometry{Cylinders: fields[0], Heads: fields[1], SectorsPerTrack: fields[2]}, true
}

// InferDiskGeometry derives geometry from an image's first sector. Partition
// entries whose end CHS is filled in name the heads and sectors per track the
// table was written with; without one the image takes 16 heads and 63
// sectors. Cylinders follow from the size, capped at 16383.
func InferDiskGeometry(mbr []byte, size int64) (DiskGeometry, error) {
	if !AdmitAtaDiskSize(size) {
		return DiskGeometry{}, fmt.Errorf("hard-disk image must be 1 to 128 MiB in whole 512-byte sectors")
	}
	heads, sectors := uint16(16), uint16(63)
	if len(mbr) >= 512 && mbr[510] == 0x55 && mbr[511] == 0xaa {
		for entry := 0; entry < 4; entry++ {
			e := mbr[446+16*entry : 462+16*entry]
			if e[4] == 0 || binary.LittleEndian.Uint32(e[12:]) == 0 {
				continue
			}
			endHead, endSector := uint16(e[5]), uint16(e[6]&0x3f)
			if endSector == 0 || endHead >= uint16(generated.FesComputerAtaDiskMaxHeads) {
				continue
			}
			heads, sectors = endHead+1, endSector
			break
		}
	}
	cylinders := size / AtaDiskSectorBytes / int64(heads) / int64(sectors)
	if max := int64(generated.FesComputerAtaDiskMaxCylinders); cylinders > max {
		cylinders = max
	}
	geometry := DiskGeometry{Cylinders: uint16(cylinders), Heads: heads, SectorsPerTrack: sectors}
	if !geometry.Valid(size) {
		return DiskGeometry{}, fmt.Errorf("hard-disk image is too small for its partition geometry")
	}
	return geometry, nil
}

// MachineSettings are the settings words 0..6 of the fes.pc386.machine
// record that FogCast writes for the next launch.
type MachineSettings struct {
	Preset      uint16 `json:"preset"`
	Frequency   uint16 `json:"frequency"`
	CodeProfile uint16 `json:"code_profile"`
	Turbo       uint16 `json:"turbo"`
	SBBase      uint16 `json:"sb_base"`
	SBIRQ       uint16 `json:"sb_irq"`
	SBDMA       uint16 `json:"sb_dma"`
}

// DefaultMachineSettings is a missing record: max, coherent, 220h, IRQ 7, DMA 1.
func DefaultMachineSettings() MachineSettings {
	return MachineSettings{SBBase: uint16(generated.FesComputerPc386SbBase220),
		SBIRQ: uint16(generated.FesComputerPc386SbIrqDefault), SBDMA: uint16(generated.FesComputerPc386SbDma)}
}

// Valid applies the layout's commit rules (mister-packages computer-io.md).
func (m MachineSettings) Valid() bool {
	if uint32(m.Preset) >= generated.FesComputerPc386PresetCount || m.CodeProfile > 1 || m.Turbo > 1 {
		return false
	}
	if m.Frequency != 0 && (uint32(m.Preset) == generated.FesComputerPc386PresetMax ||
		uint32(m.Frequency) < generated.FesComputerPc386FrequencyMin || uint32(m.Frequency) > generated.FesComputerPc386FrequencyMax) {
		return false
	}
	base := uint32(m.SBBase)
	return (base == generated.FesComputerPc386SbBase220 || base == generated.FesComputerPc386SbBase240) &&
		m.SBIRQ < 16 && generated.FesComputerPc386SbIrqMask&(1<<m.SBIRQ) != 0 && uint32(m.SBDMA) == generated.FesComputerPc386SbDma
}

// String is the MachineSettingsHeader form.
func (m MachineSettings) String() string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d", m.Preset, m.Frequency, m.CodeProfile, m.Turbo, m.SBBase, m.SBIRQ, m.SBDMA)
}

// ParseMachineSettings reads the MachineSettingsHeader form; Valid still applies.
func ParseMachineSettings(value string) (MachineSettings, bool) {
	parts := strings.Split(value, ",")
	if len(parts) != 7 {
		return MachineSettings{}, false
	}
	var fields [7]uint16
	for i, part := range parts {
		if part == "" || len(part) > 5 || (len(part) > 1 && part[0] == '0') {
			return MachineSettings{}, false
		}
		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return MachineSettings{}, false
		}
		fields[i] = uint16(n)
	}
	return MachineSettings{Preset: fields[0], Frequency: fields[1], CodeProfile: fields[2], Turbo: fields[3],
		SBBase: fields[4], SBIRQ: fields[5], SBDMA: fields[6]}, true
}

// StagedDisk names a hard-disk image the kit holds by digest, with the
// geometry its library entry launches it with.
type StagedDisk struct {
	SHA256   string       `json:"sha256"`
	Size     int64        `json:"size"`
	Geometry DiskGeometry `json:"geometry"`
}

func (d StagedDisk) Valid() bool {
	return ValidateDigest(d.SHA256) == nil && d.Geometry.Valid(d.Size)
}

// LibraryMachineBinding carries a pc386 library launch's game, which keys its
// machine record and disk delta, and its staged disk, if any.
type LibraryMachineBinding struct {
	GameID string
	Disk   *StagedDisk
}

func (b LibraryMachineBinding) Valid() bool {
	return ValidMediaGameID(b.GameID) && (b.Disk == nil || b.Disk.Valid())
}

// SetHeaders writes the binding onto a library load request. The size is the
// staged file's; the kit reads it, not a header.
func (b LibraryMachineBinding) SetHeaders(h http.Header) {
	h.Set(LibraryGameHeader, b.GameID)
	if b.Disk != nil {
		h.Set(DiskImageHeader, b.Disk.SHA256)
		h.Set(DiskGeometryHeader, b.Disk.Geometry.String())
	}
}

// LibraryMachineHeaders reads an optional binding. It returns nil and true
// when the request carries none; a partial or invalid binding is false.
// Disk.Size is left zero for the kit to fill from its staged file.
func LibraryMachineHeaders(h http.Header) (*LibraryMachineBinding, bool) {
	games, images, geometries := h.Values(LibraryGameHeader), h.Values(DiskImageHeader), h.Values(DiskGeometryHeader)
	if len(games) == 0 && len(images) == 0 && len(geometries) == 0 {
		return nil, true
	}
	if len(games) != 1 || !ValidMediaGameID(games[0]) || len(images) != len(geometries) || len(images) > 1 {
		return nil, false
	}
	binding := &LibraryMachineBinding{GameID: games[0]}
	if len(images) == 1 {
		geometry, ok := ParseDiskGeometry(geometries[0])
		if !ok || ValidateDigest(images[0]) != nil {
			return nil, false
		}
		binding.Disk = &StagedDisk{SHA256: images[0], Geometry: geometry}
	}
	return binding, true
}

// HardDiskRequestError refuses a hard-disk image or geometry the disk cannot hold.
func HardDiskRequestError() *APIError {
	return &APIError{Code: CodeBadRequest, Phase: "request",
		Message: "hard-disk media requires a raw image of 1 to 128 MiB in whole 512-byte sectors, with at most 16383 cylinders, 16 heads and 63 sectors per track inside it"}
}

// DiskImageStatus reports one image the kit holds for hard-disk launches.
type DiskImageStatus struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Staged bool   `json:"staged"`
}
