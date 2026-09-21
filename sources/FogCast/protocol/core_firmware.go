package protocol

import "github.com/DeanoC/FogCast/corepackage"

const (
	// FirmwareRole is the composition slot name. It is not cartridge media.
	FirmwareRole = "firmware"
	// FirmwareInterfaceID is the exact Coleco Phase 1 firmware contract.
	FirmwareInterfaceID = "fes.firmware.blob"
	// FirmwareBytes is the Coleco BIOS aperture at 0x0000–0x1fff.
	FirmwareBytes int64 = 8192
)

// DeclaredFirmwareCapabilities projects the firmware slot from an admitted
// descriptor. Unknown ABI or interface versions expose no slot. Optional
// declarations still require active runtime support at launch.
func DeclaredFirmwareCapabilities(descriptor corepackage.Descriptor) []CoreMediaCapability {
	if descriptor.ABI.ID != "fes.application" || descriptor.ABI.Major != 1 || descriptor.ABI.Minor != 0 {
		return []CoreMediaCapability{}
	}
	for _, contract := range descriptor.Interfaces {
		if contract.ID == FirmwareInterfaceID && contract.Major == 1 && contract.Minor == 0 {
			return []CoreMediaCapability{{
				Role: FirmwareRole, Format: "raw", MinBytes: FirmwareBytes, MaxBytes: FirmwareBytes,
				Interface: RuntimeContract{ID: FirmwareInterfaceID, Major: 1, Minor: 0},
				Transport: "fes-application-mailbox-firmware-v1",
			}}
		}
	}
	return []CoreMediaCapability{}
}

// DeclaresFirmwareSlot reports whether the package can bind firmware before boot.
func DeclaresFirmwareSlot(descriptor corepackage.Descriptor) bool {
	return len(DeclaredFirmwareCapabilities(descriptor)) > 0
}

// FirmwareAdmissionError is the closed-door launch failure when a title
// requires firmware that is missing or cannot be bound. Callers must not
// program the FPGA after this error.
func FirmwareAdmissionError(message string) *APIError {
	if message == "" {
		message = "required firmware is missing; import Coleco BIOS before launch"
	}
	return &APIError{Code: CodeBadRequest, Message: message, Phase: "admission"}
}

// CoreComposition is catalog-side slot fill for one library title. Ready
// requires every required slot to be fillable on the current setup.
type CoreComposition struct {
	ExpansionID      string `json:"expansion_id,omitempty"`
	ExpansionReady   bool   `json:"expansion_ready,omitempty"`
	FirmwareRequired bool   `json:"firmware_required"`
	FirmwareReady    bool   `json:"firmware_ready"`
}

// FirmwareReady reports whether a title's firmware slot is composition-ready.
// BIOS-free titles are ready without a household object. Required titles need
// both a filled household slot and a package that can bind it.
func FirmwareReady(required, householdFilled, packageDeclares bool) bool {
	if !required {
		return true
	}
	return householdFilled && packageDeclares
}

// FirmwareCapable reports whether the active package generation can bind the
// Coleco firmware slot.
func FirmwareCapable(p *CorePackageStatus) bool {
	if p == nil || p.ABI.ID != "fes.application" || p.ABI.Major != 1 || p.ABI.Minor != 0 {
		return false
	}
	for _, i := range p.ActiveInterfaces {
		if i.ID == FirmwareInterfaceID && i.Major == 1 && i.Minor == 0 {
			return true
		}
	}
	return false
}
