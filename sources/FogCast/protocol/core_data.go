package protocol

import (
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
	PackageID   string           `json:"package_id"`
	CoreID      string           `json:"core_id"`
	Layout      *RuntimeContract `json:"layout"`
	Mode        string           `json:"mode"`
	Revision    string           `json:"revision"`
	PaddleSpeed PaddleSpeed      `json:"paddle_speed"`
	BestRally   uint16           `json:"best_rally"`
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
