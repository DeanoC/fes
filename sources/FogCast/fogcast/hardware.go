package fogcast

import (
	"context"
	"os"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// HardwareMachine is a saved library setup. Ready describes local package,
// firmware and expansion admission, not target availability or hardware tests.
// The active session's target receipt is returned separately by the host API.
type HardwareMachine struct {
	GameID            string              `json:"game_id"`
	Title             string              `json:"title"`
	CoreID            string              `json:"core_id"`
	PackageID         string              `json:"package_id"`
	PackageReady      bool                `json:"package_ready"`
	FirmwareReady     bool                `json:"firmware_ready"`
	Ready             bool                `json:"ready"`
	UnavailableReason string              `json:"unavailable_reason,omitempty"`
	Socket            HardwareSocket      `json:"socket"`
	DraftExpansionID  string              `json:"draft_expansion_id"`
	Choices           []HardwareExpansion `json:"choices"`
}

type HardwareSocket struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Supported bool   `json:"supported"`
}

type HardwareExpansion struct {
	ExpansionID       string `json:"expansion_id"`
	Label             string `json:"label"`
	Description       string `json:"description"`
	Ready             bool   `json:"ready"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// Hardware returns the supported ZX81 setups under the same lifecycle admission
// used by selection and launch. Package, selected card and admitted choices
// cannot be paired across concurrent host configuration edits.
func (s *Service) Hardware(ctx context.Context) ([]HardwareMachine, error) {
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	entries, err := s.CoreEntries(ctx)
	if err != nil {
		return nil, err
	}
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	expansions, err := store.CoreExpansions(ctx)
	if err != nil {
		return nil, expansionError(err)
	}
	result := make([]HardwareMachine, 0)
	for _, entry := range entries {
		// This room currently supports the one real ZX81 rear connector. Other
		// families use their own admitted topology when added to this projection.
		if entry.CoreID != "fes.zx81" {
			continue
		}
		machine := HardwareMachine{
			GameID: entry.GameID, Title: entry.Title, CoreID: entry.CoreID, PackageID: entry.PackageID,
			Socket: HardwareSocket{ID: "rear", Label: "Rear expansion socket"}, Choices: []HardwareExpansion{},
		}
		selection, err := store.CoreEntryExpansion(ctx, entry.GameID)
		if err != nil {
			return nil, expansionError(err)
		}
		machine.DraftExpansionID = selection.ExpansionID
		inspection, base, inspectErr := s.readInstalledCore(ctx, entry.PackageID)
		machine.PackageReady = inspectErr == nil && inspection.Descriptor.Core.ID == entry.CoreID
		if !machine.PackageReady {
			machine.UnavailableReason = "The selected machine package is unavailable."
			result = append(result, machine)
			continue
		}
		machine.Socket.Supported = zx81RearSocket(inspection.Descriptor)
		machine.FirmwareReady = s.zx81FirmwareReady(ctx, entry, inspection.Descriptor)
		if !machine.FirmwareReady {
			machine.UnavailableReason = "The BASIC firmware for this setup is unavailable."
		}
		selectedReady := selection.ExpansionID == ""
		selectedFound := selectedReady
		for _, row := range expansions {
			if row.Slot != 0 || row.PackageID != entry.PackageID {
				continue
			}
			choice := HardwareExpansion{ExpansionID: row.ExpansionID, Label: row.Label, Description: row.Description}
			if choice.Label == "" {
				shortID := row.ExpansionID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				choice.Label = "Expansion " + shortID
			}
			if choice.Description == "" {
				choice.Description = "No feature description has been supplied for this expansion."
			}
			asset, readErr := store.ReadCoreExpansion(ctx, row.ExpansionID)
			choice.Ready = machine.Socket.Supported && readErr == nil && validateExpansionSelection(inspection, base, asset) == nil
			if !choice.Ready {
				choice.UnavailableReason = "This expansion is unavailable or does not fit this exact machine package."
			}
			if choice.ExpansionID == selection.ExpansionID {
				selectedFound, selectedReady = true, choice.Ready
			}
			machine.Choices = append(machine.Choices, choice)
		}
		if !selectedFound {
			machine.Choices = append(machine.Choices, HardwareExpansion{
				ExpansionID: selection.ExpansionID, Label: "Unavailable expansion", Description: "The saved expansion cannot be used with this machine package.",
				UnavailableReason: "Remove it or choose a compatible expansion before launching.",
			})
		}
		if !selectedReady {
			machine.UnavailableReason = "The saved expansion is unavailable for this machine package."
		}
		machine.Ready = machine.PackageReady && machine.FirmwareReady && selectedReady
		result = append(result, machine)
	}
	return result, nil
}

func zx81RearSocket(descriptor corepackage.Descriptor) bool {
	if descriptor.ABI.ID != "fes.simple-computer" || descriptor.ABI.Major != 1 || descriptor.ABI.Minor != 0 {
		return false
	}
	for _, contract := range descriptor.Interfaces {
		if contract.ID == expansion.Slot && contract.Major == 1 && contract.Minor == 0 && !contract.Required {
			return true
		}
	}
	return false
}

func (s *Service) zx81FirmwareReady(ctx context.Context, entry catalog.CoreEntry, descriptor corepackage.Descriptor) bool {
	if descriptor.ROM != nil {
		_, _, err := s.readCoreEntryROM(ctx, entry, descriptor)
		return err == nil
	}
	if s.machineROM == nil {
		return false
	}
	// The legacy host linker config names the local image and tools. Checking
	// those files is a readiness read; only launch actually links the firmware.
	if linker, ok := s.machineROM.(PythonMachineROM); ok {
		for _, path := range []string{linker.Script, linker.Image, linker.MistralCV} {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
				return false
			}
		}
	}
	return true
}

type coreExpansionPresentationCatalog interface {
	CoreExpansionPresentation(context.Context, string) (catalog.CoreExpansionPresentation, error)
	SetCoreExpansionPresentation(context.Context, string, string, string) (catalog.CoreExpansionPresentation, error)
}

func (s *Service) CoreExpansionPresentation(ctx context.Context, id string) (catalog.CoreExpansionPresentation, error) {
	store, ok := s.catalog.(coreExpansionPresentationCatalog)
	if !ok {
		return catalog.CoreExpansionPresentation{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, err := store.CoreExpansionPresentation(ctx, id)
	return value, expansionError(err)
}

func (s *Service) SetCoreExpansionPresentation(ctx context.Context, id, label, description string) (catalog.CoreExpansionPresentation, error) {
	store, ok := s.catalog.(coreExpansionPresentationCatalog)
	if !ok {
		return catalog.CoreExpansionPresentation{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, err := store.SetCoreExpansionPresentation(ctx, id, label, description)
	return value, expansionError(err)
}
