package fogcast

import (
	"context"
	"errors"
	"fmt"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// CoreFirmware returns the household firmware slot. Unset slots have empty media_id.
func (s *Service) CoreFirmware(parent context.Context, slot string) (catalog.CoreFirmware, error) {
	store, ok := s.catalog.(coreFirmwareCatalog)
	if !ok {
		return catalog.CoreFirmware{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	value, err := store.CoreFirmware(ctx, slot)
	return value, mapCoreFirmwareError(err)
}

// SelectCoreFirmware binds an already imported core-media object into the household slot.
func (s *Service) SelectCoreFirmware(parent context.Context, slot, mediaID string) (catalog.CoreFirmware, error) {
	store, ok := s.catalog.(coreFirmwareCatalog)
	if !ok {
		return catalog.CoreFirmware{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreFirmware{}, err
	}
	defer release()
	value, err := store.SelectCoreFirmware(ctx, slot, mediaID)
	return value, mapCoreFirmwareError(err)
}

// CoreCompositions reports title-level firmware readiness for catalog rows.
func (s *Service) CoreCompositions(ctx context.Context, ids []string) (map[string]protocol.CoreComposition, error) {
	out := make(map[string]protocol.CoreComposition, len(ids))
	entries, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return out, nil
	}
	store, hasFirmware := s.catalog.(coreFirmwareCatalog)
	filled := false
	if hasFirmware {
		var err error
		filled, err = store.HouseholdFirmwareFilled(ctx)
		if err != nil {
			return nil, mapCoreFirmwareError(err)
		}
	}
	declares := map[string]bool{}
	for _, id := range ids {
		entry, err := entries.CoreEntry(ctx, id)
		if errors.Is(err, catalog.ErrCoreEntryNotFound) {
			continue
		}
		if err != nil {
			return nil, mapCoreEntryError(err)
		}
		comp := protocol.CoreComposition{FirmwareRequired: entry.FirmwareRequired}
		if expansions, ok := s.catalog.(coreExpansionCatalog); ok {
			selected, err := expansions.CoreEntryExpansion(ctx, id)
			if err != nil {
				return nil, expansionError(err)
			}
			comp.ExpansionID = selected.ExpansionID
			if selected.ExpansionID != "" {
				asset, assetErr := expansions.ReadCoreExpansion(ctx, selected.ExpansionID)
				inspection, _, inspectErr := s.readInstalledCore(ctx, entry.PackageID)
				comp.ExpansionReady = assetErr == nil && inspectErr == nil && asset.Manifest.ShellPackageID == entry.PackageID && asset.Manifest.ShellBuildID == inspection.Descriptor.Build.ID && asset.Manifest.ShellSHA256 == inspection.Descriptor.Payload.SHA256
			}
		}
		if !entry.FirmwareRequired {
			comp.FirmwareReady = true
			out[id] = comp
			continue
		}
		declared, known := declares[entry.PackageID]
		if !known {
			inspection, _, inspectErr := s.readInstalledCore(ctx, entry.PackageID)
			declared = inspectErr == nil && protocol.DeclaresFirmwareSlot(inspection.Descriptor)
			declares[entry.PackageID] = declared
		}
		comp.FirmwareReady = protocol.FirmwareReady(true, filled, declared)
		out[id] = comp
	}
	return out, nil
}

func (s *Service) readCoreFirmware(ctx context.Context, descriptor corepackage.Descriptor, mediaID string) (*coreEntryMedia, error) {
	if !protocol.DeclaresFirmwareSlot(descriptor) {
		return nil, protocol.FirmwareAdmissionError("required firmware cannot be bound; package does not declare fes.firmware.blob 1.0")
	}
	if protocol.ValidateDigest(mediaID) != nil {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	store, ok := s.catalog.(coreFirmwareCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, err := store.CoreMediaInfo(ctx, mediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if media.Size != protocol.FirmwareBytes {
		return nil, protocol.FirmwareAdmissionError(fmt.Sprintf("household firmware is %d bytes; Coleco BIOS must be exactly %d bytes", media.Size, protocol.FirmwareBytes))
	}
	opened, reader, err := store.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if opened != media {
		closeErr := (&coreEntryMedia{ReadCloser: reader}).Close()
		return nil, errors.Join(canonicalError(protocol.CodeInternal, nil), closeErr)
	}
	return &coreEntryMedia{ReadCloser: reader, size: media.Size}, nil
}

func mapCoreFirmwareError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalog.ErrInvalidCoreFirmware):
		return canonicalError(protocol.CodeBadRequest, nil)
	case errors.Is(err, catalog.ErrCoreFirmwareNotFound), errors.Is(err, catalog.ErrCoreMediaNotFound):
		return canonicalError(protocol.CodeROMNotFound, nil)
	default:
		return mapCoreMediaError(err)
	}
}
