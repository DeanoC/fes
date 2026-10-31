package protocol

import (
	"regexp"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol/internal/generated"
)

const (
	CodeCorruptData      ErrorCode = "CORRUPT_DATA"
	CodeIncompatibleData ErrorCode = "INCOMPATIBLE_DATA"
	CodeStaleRevision    ErrorCode = "STALE_REVISION"
	CodeSaveFailed       ErrorCode = "SAVE_FAILED"
)

type PaddleSpeed uint16

const (
	PaddleSlow PaddleSpeed = iota
	PaddleNormal
	PaddleFast
)

func (s PaddleSpeed) Valid() bool { return s <= PaddleFast }

// Pc386MachineSettings are the next-launch settings carried by a per-game
// fes.pc386.machine record. CMOS bytes are retained by the runtime and omitted.
type Pc386MachineSettings struct {
	Preset      uint16 `json:"preset"`
	Frequency   uint16 `json:"frequency"`
	CodeProfile uint16 `json:"code_profile"`
	Turbo       uint16 `json:"turbo"`
	SBBase      uint16 `json:"sb_base"`
	SBIRQ       uint16 `json:"sb_irq"`
	SBDMA       uint16 `json:"sb_dma"`
}

func (m Pc386MachineSettings) Valid() bool {
	if uint32(m.Preset) >= generated.FesComputerPc386PresetCount ||
		uint32(m.CodeProfile) > generated.FesComputerPc386CodeProfileQueue16 ||
		uint32(m.Turbo) > generated.FesComputerPc386TurboMax ||
		uint32(m.SBDMA) != generated.FesComputerPc386SbDma {
		return false
	}
	if m.Frequency != 0 && (uint32(m.Preset) == generated.FesComputerPc386PresetMaxPerformance ||
		uint32(m.Frequency) < generated.FesComputerPc386FrequencyMin ||
		uint32(m.Frequency) > generated.FesComputerPc386FrequencyMax) {
		return false
	}
	if uint32(m.SBBase) != generated.FesComputerPc386SbBase220 &&
		uint32(m.SBBase) != generated.FesComputerPc386SbBase240 {
		return false
	}
	return m.SBIRQ < 16 && ((generated.FesComputerPc386SbIrqMask>>m.SBIRQ)&1) != 0
}

// CoreData describes durable target data. It never reports an uncommitted live score.
type CoreData struct {
	PackageID   string                `json:"package_id"`
	CoreID      string                `json:"core_id"`
	Layout      *RuntimeContract      `json:"layout"`
	Mode        string                `json:"mode"`
	Revision    string                `json:"revision"`
	PaddleSpeed PaddleSpeed           `json:"paddle_speed"`
	BestRally   uint16                `json:"best_rally"`
	GameID      string                `json:"game_id,omitempty"`
	Machine     *Pc386MachineSettings `json:"machine,omitempty"`
}

// CoreDataInspection binds data to the exact archive admitted on the target.
type CoreDataInspection struct {
	CoreData
	Descriptor corepackage.Descriptor `json:"descriptor"`
}
type CoreSettingsUpdate struct {
	ExpectedPackageID string      `json:"expected_package_id"`
	ExpectedRevision  string      `json:"expected_revision"`
	PaddleSpeed       PaddleSpeed `json:"paddle_speed"`
}

var coreDataDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var coreDataID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
var coreDataGameID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func ValidCoreDataRevision(value string) bool {
	return value == "absent" || coreDataDigest.MatchString(value)
}
func (u CoreSettingsUpdate) Valid() bool {
	return coreDataDigest.MatchString(u.ExpectedPackageID) && ValidCoreDataRevision(u.ExpectedRevision) && u.PaddleSpeed.Valid()
}
func (d CoreData) Valid() bool {
	if !coreDataDigest.MatchString(d.PackageID) || !coreDataID.MatchString(d.CoreID) || !ValidCoreDataRevision(d.Revision) || !d.PaddleSpeed.Valid() {
		return false
	}
	if d.GameID != "" || d.Machine != nil {
		return d.Mode == "persistent" && d.CoreID == "fes.pc386" &&
			d.GameID != "" && len(d.GameID) <= 256 &&
			coreDataGameID.MatchString(d.GameID) && d.Machine != nil && d.Machine.Valid() &&
			d.Layout != nil && d.Layout.ID == generated.FesComputerInterfacePc386MachineID &&
			d.Layout.Major == generated.FesComputerInterfacePc386MachineMajor &&
			d.Layout.Minor == generated.FesComputerInterfacePc386MachineMinor &&
			d.PaddleSpeed == PaddleSlow && d.BestRally == 0
	}
	switch d.Mode {
	case "persistent":
		return d.Layout != nil && coreDataID.MatchString(d.Layout.ID) && d.Layout.Major > 0
	case "volatile":
		return d.Layout == nil && d.Revision == "absent" && d.PaddleSpeed == PaddleNormal && d.BestRally == 0
	default:
		return false
	}
}
func (d CoreDataInspection) Valid() bool {
	if !d.CoreData.Valid() || corepackage.ValidateDescriptor(d.Descriptor) != nil || d.CoreID != d.Descriptor.Core.ID {
		return false
	}
	if d.Layout == nil {
		return true
	}
	for _, i := range d.Descriptor.Interfaces {
		if i.Required && i.ID == d.Layout.ID && i.Major == int64(d.Layout.Major) && i.Minor == int64(d.Layout.Minor) {
			return true
		}
	}
	return false
}
