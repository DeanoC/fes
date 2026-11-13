package protocol

const (
	// FirmwareRole is the composition slot name. It is not cartridge media.
	FirmwareRole = "firmware"
	// FirmwareBytes is the Coleco BIOS aperture at 0x0000–0x1fff.
	FirmwareBytes int64 = 8192
)

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
	ROMRequired      bool   `json:"rom_required,omitempty"`
	ROMReady         bool   `json:"rom_ready,omitempty"`
	ROMID            string `json:"rom_id,omitempty"`
	ROMMediaID       string `json:"rom_media_id,omitempty"`
	ROMRole          string `json:"rom_role,omitempty"`
	ROMSourceSize    int64  `json:"rom_source_size,omitempty"`
	ExpansionID      string `json:"expansion_id,omitempty"`
	ExpansionReady   bool   `json:"expansion_ready,omitempty"`
	FirmwareRequired bool   `json:"firmware_required"`
	FirmwareReady    bool   `json:"firmware_ready"`
	// SlotExpansions lists the cards a multi-socket shell title selects, in
	// ascending slot order. Every card must be ready for the title to launch.
	SlotExpansions []SlotExpansionStatus `json:"slot_expansions,omitempty"`
}

// SlotExpansionStatus is one selected card for a physical slot. Ready means
// the card is stored intact and admitted against the title's exact shell
// together with the title's other selected cards.
type SlotExpansionStatus struct {
	Slot        int    `json:"slot"`
	ExpansionID string `json:"expansion_id"`
	Ready       bool   `json:"ready"`
}
