package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type coreROMCatalog interface {
	CoreEntryROM(context.Context, string) (catalog.CoreEntryROM, error)
	SelectCoreEntryROM(context.Context, string, string, string, int64, string, string) (catalog.CoreEntryROM, error)
}

func romAdmissionError() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "required ROM must select an exact-size binary for this package and named ROM requirement"}
}
func romSelectionError(err error) error {
	if errors.Is(err, catalog.ErrInvalidCoreROM) || errors.Is(err, catalog.ErrInvalidCoreMedia) || errors.Is(err, catalog.ErrCoreMediaNotFound) {
		return romAdmissionError()
	}
	return mapCoreEntryError(err)
}
func (s *Service) CoreEntryROM(ctx context.Context, gameID string) (catalog.CoreEntryROM, error) {
	store, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if _, err := s.CoreEntry(ctx, gameID); err != nil {
		return catalog.CoreEntryROM{}, err
	}
	result, err := store.CoreEntryROM(ctx, gameID)
	return result, romSelectionError(err)
}
func (s *Service) SelectCoreEntryROM(parent context.Context, gameID, packageID, romID, expected, mediaID string) (catalog.CoreEntryROM, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	defer release()
	store, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	if entry.PackageID != packageID {
		return catalog.CoreEntryROM{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspected, _, err := s.readInstalledCore(ctx, packageID)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	req := inspected.Descriptor.ROM
	if req == nil || req.ID != romID {
		return catalog.CoreEntryROM{}, romAdmissionError()
	}
	value, err := store.SelectCoreEntryROM(ctx, gameID, packageID, romID, req.SourceSize, expected, mediaID)
	return value, romSelectionError(err)
}
func (s *Service) readCoreEntryROM(ctx context.Context, entry catalog.CoreEntry, descriptor corepackage.Descriptor) (catalog.CoreEntryROM, []byte, error) {
	if err := validateROMMediaContract(descriptor); err != nil {
		return catalog.CoreEntryROM{}, nil, err
	}
	store, ok := s.catalog.(coreROMCatalog)
	media, mediaOK := s.catalog.(coreMediaCatalog)
	if !ok || !mediaOK || descriptor.ROM == nil {
		return catalog.CoreEntryROM{}, nil, romAdmissionError()
	}
	selected, err := store.CoreEntryROM(ctx, entry.GameID)
	if err != nil {
		return selected, nil, romSelectionError(err)
	}
	req := descriptor.ROM
	if selected.MediaID == "" || selected.PackageID != entry.PackageID || selected.ROMID != req.ID || selected.SourceSize != req.SourceSize {
		return selected, nil, romAdmissionError()
	}
	object, reader, err := media.OpenCoreMedia(ctx, selected.MediaID)
	if err != nil {
		return selected, nil, romSelectionError(err)
	}
	defer reader.Close()
	if object.Size != req.SourceSize {
		return selected, nil, romAdmissionError()
	}
	data, err := io.ReadAll(io.LimitReader(reader, req.SourceSize+1))
	if err != nil || int64(len(data)) != req.SourceSize {
		return selected, nil, romAdmissionError()
	}
	return selected, data, nil
}
func (s *Service) romLaunchSource(ctx context.Context, entry catalog.CoreEntry, descriptor corepackage.Descriptor, base []byte) (coreLoadSource, error) {
	selected, rom, err := s.readCoreEntryROM(ctx, entry, descriptor)
	if err != nil {
		return coreLoadSource{}, err
	}
	var asset *expansion.Asset
	if store, ok := s.catalog.(coreExpansionCatalog); ok {
		selection, err := store.CoreEntryExpansion(ctx, entry.GameID)
		if err != nil {
			return coreLoadSource{}, expansionError(err)
		}
		if selection.ExpansionID != "" {
			value, err := store.ReadCoreExpansion(ctx, selection.ExpansionID)
			if err != nil {
				return coreLoadSource{}, expansionError(err)
			}
			if corepackage.ValidateExpansionArchive(base, value) != nil {
				return coreLoadSource{}, expansionError(catalog.ErrInvalidCoreExpansion)
			}
			asset = &value
		}
	}
	data, err := corepackage.WriteROMInput(corepackage.ROMInput{Package: base, ROM: rom, Expansion: asset})
	if err != nil {
		return coreLoadSource{}, romAdmissionError()
	}
	source := coreLoadSource{size: int64(len(data)), body: bytes.NewReader(data), entry: &entry, romID: selected.ROMID, romMediaID: selected.MediaID, romMapSHA256: descriptor.ROM.SHA256, romSourceSize: descriptor.ROM.SourceSize}
	if asset != nil {
		source.expansionID = asset.ID
	}
	return source, nil
}

// The target computes linked and composed identities. The host binds the
// returned receipt to its selected source identities, not a host-built image.
func (s coreLoadSource) matchesLoadedIdentity(status *protocol.CorePackageStatus) bool {
	if s.romID == "" {
		return reflect.DeepEqual(status.Composition, s.composition)
	}
	link := status.ROMLink
	if link == nil || link.ROMID != s.romID || link.SourceSHA256 != s.romMediaID || link.MapSHA256 != s.romMapSHA256 || link.SourceSize != s.romSourceSize {
		return false
	}
	if s.expansionID == "" {
		return status.Composition == nil
	}
	return status.Composition != nil && status.Composition.ExpansionID == s.expansionID
}

// Cartridge linking supplies startup content before activation. A mailbox that
// holds the application in reset for a later media upload cannot also own it.
func validateROMMediaContract(descriptor corepackage.Descriptor) error {
	if descriptor.ROM == nil || descriptor.ROM.Role != "cartridge" {
		return nil
	}
	application := descriptor.ABI.ID == "fes.application" && descriptor.ABI.Major == 1
	for _, endpoint := range descriptor.Interfaces {
		if endpoint.Major == 1 && endpoint.Minor == 0 && (endpoint.ID == "fes.media.blob-stream" || endpoint.ID == "fes.firmware.blob" || application && endpoint.ID == "fes.media.blob") {
			return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Phase: "compatibility", Message: "linked cartridge packages must omit media endpoints that wait for a separate startup upload"}
		}
	}
	return nil
}
