package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type coreExpansionCatalog interface {
	ImportCoreExpansion(context.Context, expansion.Asset) (catalog.CoreExpansion, error)
	CoreExpansions(context.Context) ([]catalog.CoreExpansion, error)
	ReadCoreExpansion(context.Context, string) (expansion.Asset, error)
	CoreEntryExpansion(context.Context, string) (catalog.CoreEntryExpansion, error)
	SelectCoreEntryExpansion(context.Context, string, string, string, string) (catalog.CoreEntryExpansion, error)
}

func expansionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, catalog.ErrCoreExpansionNotFound) || errors.Is(err, catalog.ErrInvalidCoreExpansion) {
		return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "selected expansion is unavailable or incompatible with the exact core package"}
	}
	return mapCoreEntryError(err)
}
func (s *Service) ImportCoreExpansion(ctx context.Context, size int64, body io.Reader) (catalog.CoreExpansion, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return catalog.CoreExpansion{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if size < 1 || size > expansion.MaxArchiveBytes {
		return catalog.CoreExpansion{}, expansionError(catalog.ErrInvalidCoreExpansion)
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil || int64(len(data)) != size {
		return catalog.CoreExpansion{}, expansionError(catalog.ErrInvalidCoreExpansion)
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(data))
	if err != nil {
		return catalog.CoreExpansion{}, expansionError(catalog.ErrInvalidCoreExpansion)
	}
	inspection, base, err := s.readInstalledCore(ctx, asset.Manifest.ShellPackageID)
	if err != nil {
		return catalog.CoreExpansion{}, err
	}
	if err = validateExpansionSelection(inspection, base, asset); err != nil {
		return catalog.CoreExpansion{}, expansionError(catalog.ErrInvalidCoreExpansion)
	}
	result, err := store.ImportCoreExpansion(ctx, asset)
	return result, expansionError(err)
}
func (s *Service) CoreExpansions(ctx context.Context) ([]catalog.CoreExpansion, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return store.CoreExpansions(ctx)
}
func (s *Service) CoreEntryExpansion(ctx context.Context, gameID string) (catalog.CoreEntryExpansion, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return catalog.CoreEntryExpansion{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return store.CoreEntryExpansion(ctx, gameID)
}
func (s *Service) SelectCoreEntryExpansion(ctx context.Context, gameID, packageID, expected, id string) (catalog.CoreEntryExpansion, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return catalog.CoreEntryExpansion{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntryExpansion{}, err
	}
	defer release()
	if id != "" {
		asset, err := store.ReadCoreExpansion(ctx, id)
		if err != nil {
			return catalog.CoreEntryExpansion{}, expansionError(err)
		}
		inspection, base, err := s.readInstalledCore(ctx, packageID)
		if err != nil {
			return catalog.CoreEntryExpansion{}, err
		}
		if err = validateExpansionSelection(inspection, base, asset); err != nil {
			return catalog.CoreEntryExpansion{}, expansionError(catalog.ErrInvalidCoreExpansion)
		}
	}
	value, err := store.SelectCoreEntryExpansion(ctx, gameID, packageID, expected, id)
	return value, expansionError(err)
}
func (s *Service) composeCoreEntry(ctx context.Context, entry catalog.CoreEntry, base []byte) (*corepackage.CompositionBundle, error) {
	store, ok := s.catalog.(coreExpansionCatalog)
	if !ok {
		return nil, nil
	}
	selected, err := store.CoreEntryExpansion(ctx, entry.GameID)
	if err != nil {
		return nil, expansionError(err)
	}
	if selected.ExpansionID == "" {
		return nil, nil
	}
	asset, err := store.ReadCoreExpansion(ctx, selected.ExpansionID)
	if err != nil {
		return nil, expansionError(err)
	}
	bundle, err := corepackage.ComposeArchive(base, asset)
	if err != nil {
		return nil, expansionError(catalog.ErrInvalidCoreExpansion)
	}
	return &bundle, nil
}

func validateExpansionSelection(inspection corepackage.Inspection, base []byte, asset expansion.Asset) error {
	if inspection.Descriptor.ROM != nil {
		return corepackage.ValidateExpansionArchive(base, asset)
	}
	_, err := corepackage.ComposeArchive(base, asset)
	return err
}
