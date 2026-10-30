package protocol

import (
	"encoding/json"
	"regexp"

	"github.com/DeanoC/FogCast/corepackage"
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

// CoreData describes durable target data. It never reports an uncommitted live score.
type CoreData struct {
	PackageID   string                `json:"package_id"`
	CoreID      string                `json:"core_id"`
	Layout      *RuntimeContract      `json:"layout"`
	Mode        string                `json:"mode"`
	Revision    string                `json:"revision"`
	GameID      string                `json:"game_id,omitempty"`
	Machine     *PC386MachineSettings `json:"machine,omitempty"`
	PaddleSpeed PaddleSpeed           `json:"paddle_speed"`
	BestRally   uint16                `json:"best_rally"`
}

// PC386MachineSettings is the settings portion of fes.pc386.machine 1.0.
// CMOS contents remain runtime-owned progress and are not part of an update.
type PC386MachineSettings struct {
	Preset      uint16 `json:"preset"`
	Frequency   uint16 `json:"frequency"`
	CodeProfile uint16 `json:"code_profile"`
	Turbo       uint16 `json:"turbo"`
	SBBase      uint16 `json:"sb_base"`
	SBIRQ       uint16 `json:"sb_irq"`
	SBDMA       uint16 `json:"sb_dma"`
}

func (m PC386MachineSettings) Valid() bool {
	if m.Preset >= 8 || m.CodeProfile > 1 || m.Turbo > 1 || m.SBDMA != 1 {
		return false
	}
	if m.Frequency != 0 && (m.Preset == 0 || m.Frequency < 10 || m.Frequency > 1000) {
		return false
	}
	if m.SBBase != 0x220 && m.SBBase != 0x240 {
		return false
	}
	return m.SBIRQ < 16 && (uint16(0xac)&(1<<m.SBIRQ)) != 0
}

type coreDataWire struct {
	PackageID   string                `json:"package_id"`
	CoreID      string                `json:"core_id"`
	Layout      *RuntimeContract      `json:"layout"`
	Mode        string                `json:"mode"`
	Revision    string                `json:"revision"`
	GameID      string                `json:"game_id,omitempty"`
	Machine     *PC386MachineSettings `json:"machine,omitempty"`
	PaddleSpeed *PaddleSpeed          `json:"paddle_speed,omitempty"`
	BestRally   *uint16               `json:"best_rally,omitempty"`
}

func (d CoreData) MarshalJSON() ([]byte, error) {
	wire := coreDataWire{PackageID: d.PackageID, CoreID: d.CoreID, Layout: d.Layout, Mode: d.Mode, Revision: d.Revision}
	if d.Machine != nil {
		wire.GameID, wire.Machine = d.GameID, d.Machine
	} else {
		speed, rally := d.PaddleSpeed, d.BestRally
		wire.PaddleSpeed, wire.BestRally = &speed, &rally
	}
	return json.Marshal(wire)
}

// CoreDataInspection binds data to the exact archive admitted on the target.
type CoreDataInspection struct {
	CoreData
	Descriptor corepackage.Descriptor `json:"descriptor"`
}

func (d CoreDataInspection) MarshalJSON() ([]byte, error) {
	if d.Machine != nil {
		return json.Marshal(struct {
			PackageID  string                 `json:"package_id"`
			CoreID     string                 `json:"core_id"`
			Layout     *RuntimeContract       `json:"layout"`
			Mode       string                 `json:"mode"`
			Revision   string                 `json:"revision"`
			GameID     string                 `json:"game_id"`
			Machine    *PC386MachineSettings  `json:"machine"`
			Descriptor corepackage.Descriptor `json:"descriptor"`
		}{d.PackageID, d.CoreID, d.Layout, d.Mode, d.Revision, d.GameID, d.Machine, d.Descriptor})
	}
	return json.Marshal(struct {
		PackageID   string                 `json:"package_id"`
		CoreID      string                 `json:"core_id"`
		Layout      *RuntimeContract       `json:"layout"`
		Mode        string                 `json:"mode"`
		Revision    string                 `json:"revision"`
		PaddleSpeed PaddleSpeed            `json:"paddle_speed"`
		BestRally   uint16                 `json:"best_rally"`
		Descriptor  corepackage.Descriptor `json:"descriptor"`
	}{d.PackageID, d.CoreID, d.Layout, d.Mode, d.Revision, d.PaddleSpeed, d.BestRally, d.Descriptor})
}

type CoreSettingsUpdate struct {
	ExpectedPackageID string                `json:"expected_package_id"`
	ExpectedRevision  string                `json:"expected_revision"`
	GameID            string                `json:"game_id,omitempty"`
	Machine           *PC386MachineSettings `json:"machine,omitempty"`
	PaddleSpeed       PaddleSpeed           `json:"paddle_speed"`
}

var coreDataDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var coreDataID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)

func ValidCoreDataRevision(value string) bool {
	return value == "absent" || coreDataDigest.MatchString(value)
}
func (u CoreSettingsUpdate) Valid() bool {
	if !coreDataDigest.MatchString(u.ExpectedPackageID) || !ValidCoreDataRevision(u.ExpectedRevision) {
		return false
	}
	if u.GameID != "" && ValidateGameID(u.GameID) != nil {
		return false
	}
	if u.Machine != nil {
		return u.Machine.Valid() && u.GameID != ""
	}
	return u.PaddleSpeed.Valid()
}
func (d CoreData) Valid() bool {
	if !coreDataDigest.MatchString(d.PackageID) || !coreDataID.MatchString(d.CoreID) || !ValidCoreDataRevision(d.Revision) {
		return false
	}
	switch d.Mode {
	case "persistent":
		if d.Layout == nil || !coreDataID.MatchString(d.Layout.ID) || d.Layout.Major == 0 {
			return false
		}
		if d.Layout.ID == "fes.pc386.machine" {
			return d.CoreID == "fes.pc386" && ValidateGameID(d.GameID) == nil && d.Machine != nil && d.Machine.Valid()
		}
		return d.GameID == "" && d.Machine == nil && d.PaddleSpeed.Valid()
	case "volatile":
		return d.Layout == nil && d.GameID == "" && d.Machine == nil && d.Revision == "absent" && d.PaddleSpeed == PaddleNormal && d.BestRally == 0
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
