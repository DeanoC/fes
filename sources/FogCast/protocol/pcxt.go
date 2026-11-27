package protocol

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol/internal/generated"
)

// PcxtMachineSettingsHeader carries a fes.pcxt.machine settings update as
// two decimals: video_card,mono_tint.
const PcxtMachineSettingsHeader = "X-FogCast-PCXT-Machine-Settings"

// PcxtMachineInterface is the fes.pcxt.machine 1.0 contract.
func PcxtMachineInterface() RuntimeContract {
	return RuntimeContract{
		ID:    generated.FesComputerInterfacePcxtMachineID,
		Major: generated.FesComputerInterfacePcxtMachineMajor,
		Minor: generated.FesComputerInterfacePcxtMachineMinor,
	}
}

// DeclaresPcxtMachine reports a fes.computer 1.1 descriptor whose
// persistence words use the fes.pcxt.machine layout, keyed per library game.
func DeclaresPcxtMachine(descriptor corepackage.Descriptor) bool {
	return descriptor.ABI.ID == ComputerABIID && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 1 &&
		declaresRequired(descriptor, PersistenceWordsInterface()) && declaresRequired(descriptor, PcxtMachineInterface())
}

// DeclaresPcxtFloppy reports a fes.computer 1.1 descriptor with the PC/XT floppy.
func DeclaresPcxtFloppy(descriptor corepackage.Descriptor) bool {
	return descriptor.ABI.ID == ComputerABIID && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 1 &&
		declaresRequired(descriptor, PcxtFloppyInterface())
}

// DeclaresPerGameMachine reports a fes.computer 1.1 descriptor that uses a
// per-game persistent machine record (fes.pc386.machine or fes.pcxt.machine).
func DeclaresPerGameMachine(descriptor corepackage.Descriptor) bool {
	return DeclaresPc386Machine(descriptor) || DeclaresPcxtMachine(descriptor)
}

// PcxtMachineSettings are the settings words 0..1 of the fes.pcxt.machine
// record that FogCast writes for the next launch.
type PcxtMachineSettings struct {
	VideoCard uint16 `json:"video_card"`
	MonoTint  uint16 `json:"mono_tint"`
}

// Valid applies the layout's commit rules (mister-packages computer-io.md).
func (m PcxtMachineSettings) Valid() bool {
	return uint32(m.VideoCard) <= generated.FesComputerPcxtVideoCardMax &&
		uint32(m.MonoTint) <= generated.FesComputerPcxtMonoTintMax
}

// String is the PcxtMachineSettingsHeader form.
func (m PcxtMachineSettings) String() string {
	return fmt.Sprintf("%d,%d", m.VideoCard, m.MonoTint)
}

// ParsePcxtMachineSettings reads the PcxtMachineSettingsHeader form; Valid still applies.
func ParsePcxtMachineSettings(value string) (PcxtMachineSettings, bool) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return PcxtMachineSettings{}, false
	}
	var fields [2]uint16
	for i, part := range parts {
		if part == "" || len(part) > 5 || (len(part) > 1 && part[0] == '0') {
			return PcxtMachineSettings{}, false
		}
		n, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return PcxtMachineSettings{}, false
		}
		fields[i] = uint16(n)
	}
	return PcxtMachineSettings{VideoCard: fields[0], MonoTint: fields[1]}, true
}
