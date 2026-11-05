package fogcast

import (
	"context"
	"os"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/zx81family"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
	"github.com/DeanoC/FogCast/protocol"
)

// HardwareMachine is a saved library setup. Ready describes local package,
// firmware and expansion admission, not target availability or hardware tests.
// The active session's target receipt is returned separately by the host API.
type HardwareMachine struct {
	GameID            string              `json:"game_id"`
	Title             string              `json:"title"`
	CoreID            string              `json:"core_id"`
	PackageID         string              `json:"package_id"`
	LaunchPackageID   string              `json:"launch_package_id,omitempty"`
	PackageReady      bool                `json:"package_ready"`
	FirmwareReady     bool                `json:"firmware_ready"`
	Ready             bool                `json:"ready"`
	UnavailableReason string              `json:"unavailable_reason,omitempty"`
	Socket            HardwareSocket      `json:"socket"`
	MediaID           string              `json:"media_id,omitempty"`
	MediaName         string              `json:"media_name,omitempty"`
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
	InProgress        bool   `json:"in_progress,omitempty"`
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
	var packages []corepackage.Inspection
	if s.corePackages != nil {
		packages, err = s.corePackages.List(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, canonicalError(protocol.CodeInternal, safeContextError(ctx.Err()))
			}
			// The inventory is used only to suggest an optional selector package.
			// A damaged unrelated archive must not hide an otherwise usable setup;
			// the selected package is inspected independently below.
			packages = nil
		}
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
		// ZX81, ZX80 and the Timex models share this rear connector. Other
		// families use their own admitted topology when added to this projection.
		if !zx81family.Known(entry.CoreID) {
			continue
		}
		machine := HardwareMachine{
			GameID: entry.GameID, Title: entry.Title, CoreID: entry.CoreID, PackageID: entry.PackageID,
			MediaID: entry.MediaID, MediaName: hardwareTapeName(entry),
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
		selectorFirmwareReady := false
		if selection.ExpansionID == "" {
			for _, candidate := range zx81SessionDisplayCandidates(packages, entry.PackageID, inspection.Descriptor) {
				if !s.zx81FirmwareReady(ctx, entry, candidate.Descriptor) ||
					s.validateCoreEntryMedia(ctx, candidate.Descriptor, entry.MediaRole, entry.MediaID) != nil {
					continue
				}
				machine.LaunchPackageID = candidate.PackageID
				selectorFirmwareReady = true
				break
			}
		}
		machine.Socket.Supported = corepackage.RearExpansionSocket(inspection.Descriptor)
		machine.FirmwareReady = s.zx81FirmwareReady(ctx, entry, inspection.Descriptor) || selectorFirmwareReady
		if !machine.FirmwareReady {
			machine.UnavailableReason = "The BASIC firmware for this setup is unavailable."
		}
		selectedReady := selection.ExpansionID == ""
		selectedFound := selectedReady
		for _, row := range expansions {
			if row.Slot != 0 || row.PackageID != entry.PackageID {
				continue
			}
			choice := HardwareExpansion{ExpansionID: row.ExpansionID, Label: row.Label, Description: row.Description, InProgress: row.InProgress}
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

// zx81SessionDisplayCandidates returns installed packages for the same ZX81
// target that can open the running media selector. The caller checks firmware
// and selected media for each candidate in order, continuing after rejection.
func zx81SessionDisplayCandidates(packages []corepackage.Inspection, selected string, current corepackage.Descriptor) []corepackage.Inspection {
	if zx81SessionDisplayDescriptor(current, nil) {
		return nil
	}
	var candidates []corepackage.Inspection
	for _, candidate := range packages { // CorePackageStore.List is sorted by package ID.
		if candidate.PackageID == selected || candidate.Descriptor.Core.ID != "fes.zx81" || candidate.Descriptor.Target != current.Target ||
			!zx81SessionDisplayDescriptor(candidate.Descriptor, nil) || !sameZX81MachineROM(current.ROM, candidate.Descriptor.ROM) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func sameZX81MachineROM(current, candidate *corepackage.ROM) bool {
	if current == nil || candidate == nil {
		return current == nil && candidate == nil
	}
	return current.ID == "machine-rom" && current.Role == "firmware" &&
		candidate.ID == current.ID && candidate.Role == current.Role && candidate.SourceSize == current.SourceSize
}

func zx81SessionDisplayDescriptor(descriptor corepackage.Descriptor, activeInterfaces []protocol.RuntimeInterface) bool {
	if descriptor.Core.ID != "fes.zx81" || !corepackage.RearExpansionSocket(descriptor) {
		return false
	}
	interfaceAvailable := func(id string) bool {
		for _, iface := range descriptor.Interfaces {
			if iface.ID != id || iface.Major != 1 || iface.Minor != 0 {
				continue
			}
			if iface.Required {
				return true
			}
			for _, active := range activeInterfaces {
				if active.ID == id && active.Major == 1 && active.Minor == 0 {
					return true
				}
			}
		}
		return false
	}
	display := interfaceAvailable("fes.video.session-display")
	ddr := interfaceAvailable("fes.memory.hps-ddr")
	blob := interfaceAvailable("fes.media.blob")
	tape := interfaceAvailable("fes.media.zx81-tape")
	media := (descriptor.ABI == (corepackage.Contract{ID: "fes.simple-computer", Major: 1}) && blob) ||
		(corepackage.SupportedComputerABI(descriptor.ABI) && tape)
	return display && ddr && media
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

func (s *Service) SetCoreExpansionPresentationWithProgress(ctx context.Context, id, label, description string, inProgress bool) (catalog.CoreExpansionPresentation, error) {
	store, ok := s.catalog.(interface {
		SetCoreExpansionPresentationWithProgress(context.Context, string, string, string, bool) (catalog.CoreExpansionPresentation, error)
	})
	if !ok {
		return catalog.CoreExpansionPresentation{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, err := store.SetCoreExpansionPresentationWithProgress(ctx, id, label, description, inProgress)
	return value, expansionError(err)
}

func hardwareTapeName(entry catalog.CoreEntry) string {
	id := entry.MediaID
	if id == "" {
		return "No cassette"
	}
	for _, tape := range zx81tapes.Entries() {
		if tape.SHA256 == id {
			return tape.Name
		}
	}
	if entry.MediaName != "" {
		return entry.MediaName
	}
	return "Imported cassette " + id[:8]
}
