package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type coreSlotExpansionCatalog interface {
	coreExpansionCatalog
	CoreEntrySlotExpansions(context.Context, string) ([]catalog.CoreEntrySlotExpansion, error)
	SelectCoreEntrySlotExpansion(context.Context, string, string, int, string, string) ([]catalog.CoreEntrySlotExpansion, error)
}

// CoreEntrySlotExpansions reports a title's per-slot card selection for a
// multi-socket shell: the shell's physical sockets and each selected card
// with its readiness. Shells without a multi-socket bus report no sockets.
type CoreEntrySlotExpansions struct {
	GameID     string                         `json:"game_id"`
	PackageID  string                         `json:"package_id"`
	Bus        string                         `json:"bus,omitempty"`
	Map        string                         `json:"map,omitempty"`
	Sockets    []int                          `json:"sockets"`
	Expansions []protocol.SlotExpansionStatus `json:"expansions"`
	Ready      bool                           `json:"ready"`
}

func slotExpansionUnavailable() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "selected slot card is unavailable or incompatible with the exact core package and slot"}
}

func slotExpansionError(err error) error {
	if errors.Is(err, catalog.ErrCoreExpansionNotFound) || errors.Is(err, catalog.ErrInvalidCoreExpansion) {
		return slotExpansionUnavailable()
	}
	return mapCoreEntryError(err)
}

// CoreEntrySlotExpansions reads the selection without contacting the target.
func (s *Service) CoreEntrySlotExpansions(ctx context.Context, gameID string) (CoreEntrySlotExpansions, error) {
	store, ok := s.catalog.(coreSlotExpansionCatalog)
	if !ok {
		return CoreEntrySlotExpansions{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return CoreEntrySlotExpansions{}, err
	}
	inspection, base, err := s.readInstalledCore(ctx, entry.PackageID)
	if err != nil {
		return CoreEntrySlotExpansions{}, err
	}
	return s.slotExpansionView(ctx, store, entry, inspection, base)
}

func (s *Service) slotExpansionView(ctx context.Context, store coreSlotExpansionCatalog, entry catalog.CoreEntry, inspection corepackage.Inspection, base []byte) (CoreEntrySlotExpansions, error) {
	view := CoreEntrySlotExpansions{GameID: entry.GameID, PackageID: entry.PackageID, Sockets: []int{}, Expansions: []protocol.SlotExpansionStatus{}}
	if sockets := corepackage.SlotSockets(inspection.Descriptor); sockets != nil {
		view.Bus, view.Map, view.Sockets = expansion.Apple2Slot, expansion.Apple2Map, sockets
	}
	_, statuses, err := s.readSlotCards(ctx, store, entry, inspection, base)
	if err != nil && statuses == nil {
		return CoreEntrySlotExpansions{}, err
	}
	view.Expansions = statuses
	view.Ready = err == nil
	return view, nil
}

// readSlotCards returns the title's selected cards for its exact shell and a
// readiness row per selection. A missing, damaged, stale or incompatible card
// returns an admission error together with the rows, so launch blocks before
// any target mutation and readiness reports which slot failed.
func (s *Service) readSlotCards(ctx context.Context, store coreSlotExpansionCatalog, entry catalog.CoreEntry, inspection corepackage.Inspection, base []byte) ([]expansion.Asset, []protocol.SlotExpansionStatus, error) {
	selected, err := store.CoreEntrySlotExpansions(ctx, entry.GameID)
	if err != nil {
		return nil, nil, slotExpansionError(err)
	}
	statuses := make([]protocol.SlotExpansionStatus, 0, len(selected))
	if len(selected) == 0 {
		return nil, statuses, nil
	}
	sockets := corepackage.SlotSockets(inspection.Descriptor)
	var assets []expansion.Asset
	failed := false
	for _, row := range selected {
		status := protocol.SlotExpansionStatus{Slot: row.Slot, ExpansionID: row.ExpansionID}
		asset, readErr := store.ReadCoreExpansion(ctx, row.ExpansionID)
		if readErr != nil && !errors.Is(readErr, catalog.ErrCoreExpansionNotFound) && !errors.Is(readErr, catalog.ErrInvalidCoreExpansion) {
			return nil, nil, slotExpansionError(readErr)
		}
		if readErr == nil && sockets != nil && asset.Manifest.SlotIndex == row.Slot &&
			corepackage.ValidateSlotExpansions(base, []expansion.Asset{asset}) == nil {
			status.Ready = true
			assets = append(assets, asset)
		} else {
			failed = true
		}
		statuses = append(statuses, status)
	}
	if !failed && corepackage.ValidateSlotExpansions(base, assets) != nil {
		for index := range statuses {
			statuses[index].Ready = false
		}
		failed = true
	}
	if failed {
		return nil, statuses, slotExpansionUnavailable()
	}
	return assets, statuses, nil
}

// SelectCoreEntrySlotExpansion changes one slot of the next launch. The card
// must be imported for the entry's exact package and built for that slot.
func (s *Service) SelectCoreEntrySlotExpansion(ctx context.Context, gameID, packageID string, slot int, expected, id string) (CoreEntrySlotExpansions, error) {
	store, ok := s.catalog.(coreSlotExpansionCatalog)
	if !ok {
		return CoreEntrySlotExpansions{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return CoreEntrySlotExpansions{}, err
	}
	defer release()
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return CoreEntrySlotExpansions{}, err
	}
	if entry.PackageID != packageID {
		return CoreEntrySlotExpansions{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspection, base, err := s.readInstalledCore(ctx, packageID)
	if err != nil {
		return CoreEntrySlotExpansions{}, err
	}
	sockets := corepackage.SlotSockets(inspection.Descriptor)
	valid := false
	for _, socket := range sockets {
		valid = valid || socket == slot
	}
	if !valid {
		return CoreEntrySlotExpansions{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request", Message: "slot is not a physical socket of the selected core package"}
	}
	if id != "" {
		asset, err := store.ReadCoreExpansion(ctx, id)
		if err != nil {
			return CoreEntrySlotExpansions{}, slotExpansionError(err)
		}
		if asset.Manifest.SlotIndex != slot || corepackage.ValidateSlotExpansions(base, []expansion.Asset{asset}) != nil {
			return CoreEntrySlotExpansions{}, slotExpansionUnavailable()
		}
	}
	if _, err := store.SelectCoreEntrySlotExpansion(ctx, gameID, packageID, slot, expected, id); err != nil {
		return CoreEntrySlotExpansions{}, slotExpansionError(err)
	}
	return s.slotExpansionView(ctx, store, entry, inspection, base)
}

// importSlotCard validates a multi-socket card against its installed shell,
// including one trial link, before the immutable archive is stored.
func (s *Service) importSlotCard(ctx context.Context, asset expansion.Asset) (catalog.CoreExpansion, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return catalog.CoreExpansion{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	_, base, err := s.readInstalledCore(ctx, asset.Manifest.ShellPackageID)
	if err != nil {
		return catalog.CoreExpansion{}, err
	}
	if err := corepackage.ValidateSlotCards(ctx, base, []expansion.Asset{asset}); err != nil {
		return catalog.CoreExpansion{}, slotExpansionUnavailable()
	}
	result, err := store.ImportCoreExpansion(ctx, asset)
	return result, expansionError(err)
}

// slotCompositionFor returns the readiness rows of a title's selected slot
// cards for its already read package; nil when none is selected.
func (s *Service) slotCompositionFor(ctx context.Context, entry catalog.CoreEntry, inspection corepackage.Inspection, base []byte) ([]protocol.SlotExpansionStatus, error) {
	store, ok := s.catalog.(coreSlotExpansionCatalog)
	if !ok {
		return nil, nil
	}
	_, statuses, err := s.readSlotCards(ctx, store, entry, inspection, base)
	if err != nil && statuses == nil {
		return nil, err
	}
	if len(statuses) == 0 {
		return nil, nil
	}
	return statuses, nil
}
