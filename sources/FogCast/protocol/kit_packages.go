package protocol

import "github.com/DeanoC/FogCast/corepackage"

const (
	CodeCorePackageStoreUnavailable ErrorCode = "CORE_PACKAGE_STORE_UNAVAILABLE"
	CodeCorePackageNotFound         ErrorCode = "CORE_PACKAGE_NOT_FOUND"
	CodeCoreSelectionConflict       ErrorCode = "CORE_SELECTION_CONFLICT"
)

// KitCorePackages lists durable installations, independently of the active FPGA.
// Factory packages remain part of the base image rather than this store.
type KitCorePackages struct {
	Packages   []corepackage.Inspection `json:"packages"`
	Selections []KitCoreSelection       `json:"selections"`
}

type KitCoreInstall struct {
	Package corepackage.Inspection `json:"package"`
	Created bool                   `json:"created"`
}

// KitCoreSelection affects future launches only. It conveys no execution lease.
type KitCoreSelection struct {
	CoreID    string `json:"core_id"`
	PackageID string `json:"package_id"`
}

type KitCoreSelectionRequest struct {
	ExpectedPackageID string `json:"expected_package_id"`
	PackageID         string `json:"package_id"`
}
