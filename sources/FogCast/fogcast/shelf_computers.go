package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// ShelfComputer is one host-launched computer Home can offer beside the
// kit-local shelf. Ready follows the same composition gates as a library
// launch. The ZX81 workshop does not read this list.
type ShelfComputer struct {
	GameID            string `json:"game_id"`
	Title             string `json:"title"`
	CoreID            string `json:"core_id"`
	PackageID         string `json:"package_id"`
	Ready             bool   `json:"ready"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

// ShelfComputers lists the prepared ZX81 and C64 library entries. Those
// machines launch through the host catalog, so a kit image that only selected
// the menu and a local shell would otherwise omit them from Home. One card
// per core: a ready bare machine wins over another title for the same core.
func (s *Service) ShelfComputers(ctx context.Context) ([]ShelfComputer, error) {
	entries, err := s.CoreEntries(ctx)
	if err != nil {
		return nil, err
	}
	picked := make([]catalog.CoreEntry, 0)
	ids := make([]string, 0)
	for _, entry := range entries {
		if entry.CoreID != "fes.zx81" && entry.CoreID != "fes.c64" {
			continue
		}
		picked = append(picked, entry)
		ids = append(ids, entry.GameID)
	}
	if len(picked) == 0 {
		return []ShelfComputer{}, nil
	}
	comps, err := s.CoreCompositions(ctx, ids)
	if err != nil {
		return nil, err
	}
	best := map[string]shelfCandidate{}
	for _, entry := range picked {
		inspection, inspectErr := s.installedCoreInspection(ctx, entry.PackageID)
		packageReady := inspectErr == nil && inspection.Descriptor.Core.ID == entry.CoreID
		ready := packageReady && shelfCompositionReady(comps[entry.GameID])
		if ready {
			ready = s.shelfComputerLaunchReady(ctx, entry, inspection)
		}
		reason := ""
		if !ready {
			if !packageReady {
				reason = "The selected machine package is unavailable."
			} else {
				reason = "This machine is not ready to launch from the library."
			}
		}
		next := shelfCandidate{card: ShelfComputer{
			GameID: entry.GameID, Title: entry.Title, CoreID: entry.CoreID, PackageID: entry.PackageID,
			Ready: ready, UnavailableReason: reason,
		}, mediaID: entry.MediaID}
		if prev, ok := best[entry.CoreID]; !ok || betterShelfComputer(next, prev) {
			best[entry.CoreID] = next
		}
	}
	out := make([]ShelfComputer, 0, len(best))
	for _, id := range []string{"fes.c64", "fes.zx81"} {
		if card, ok := best[id]; ok {
			out = append(out, card.card)
		}
	}
	return out, nil
}

// shelfComputerLaunchReady checks launch admissions that are not represented
// by CoreComposition. Keep this projection aligned with the library launch
// path so Home does not offer Play for a title that launch will reject.
func (s *Service) shelfComputerLaunchReady(ctx context.Context, entry catalog.CoreEntry, inspection corepackage.Inspection) bool {
	descriptor := inspection.Descriptor
	if err := libraryVideoAdmission(inspection); err != nil {
		return false
	}
	if protocol.RequiresCoreMedia(descriptor) && !(descriptor.ROM != nil && descriptor.ROM.Role == "cartridge") && entry.MediaID == "" {
		return false
	}
	if entry.MediaID != "" || entry.MediaRole != "" {
		if err := s.validateCoreEntryMedia(ctx, descriptor, entry.MediaRole, entry.MediaID); err != nil {
			return false
		}
	}
	if entry.CoreID == "fes.zx81" && descriptor.Format == 2 && !s.zx81FirmwareReady(ctx, entry, descriptor) {
		return false
	}
	if hasVideoSocket(inspection) {
		_, base, err := s.readInstalledCore(ctx, entry.PackageID)
		if err != nil {
			return false
		}
		if _, err := s.composeVideoEntry(ctx, entry, inspection, base); err != nil {
			return false
		}
	}
	return true
}

type shelfCandidate struct {
	card    ShelfComputer
	mediaID string
}

func betterShelfComputer(next, prev shelfCandidate) bool {
	if next.card.Ready != prev.card.Ready {
		return next.card.Ready
	}
	nextBare, prevBare := next.mediaID == "", prev.mediaID == ""
	if nextBare != prevBare {
		return nextBare
	}
	return next.card.Title < prev.card.Title
}

func shelfCompositionReady(comp protocol.CoreComposition) bool {
	if comp.ROMRequired && !comp.ROMReady {
		return false
	}
	if comp.FirmwareRequired && !comp.FirmwareReady {
		return false
	}
	if comp.ExpansionID != "" && !comp.ExpansionReady {
		return false
	}
	for _, slot := range comp.SlotExpansions {
		if !slot.Ready {
			return false
		}
	}
	return true
}
