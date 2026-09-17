package fogcast

import (
	"context"
	"errors"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaCatalog interface {
	ImportCoreMedia(context.Context, []byte) (catalog.CoreMedia, bool, error)
	CoreMedia(context.Context, string) (catalog.CoreMedia, []byte, error)
	CreateCoreMediaEntry(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntryMedia(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

// ImportCoreMedia snapshots a bounded asset into the existing catalog. Import
// never selects media for an entry or mutates a running session.
func (s *Service) ImportCoreMedia(ctx context.Context, size int64, body io.Reader) (catalog.CoreMedia, bool, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	data, err := protocol.ReadDevelopmentMedia(size, body)
	if err != nil {
		return catalog.CoreMedia{}, false, err
	}
	value, created, storeErr := store.ImportCoreMedia(ctx, data)
	return value, created, mapCoreMediaError(storeErr)
}

func (s *Service) CoreMedia(ctx context.Context, id string) (catalog.CoreMedia, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, _, err := store.CoreMedia(ctx, id)
	return value, mapCoreMediaError(err)
}

// SelectCoreEntryMedia changes only the next launch. The expected package and
// media identities prevent stale clients overwriting another selection.
func (s *Service) SelectCoreEntryMedia(parent context.Context, gameID, expectedPackage, expectedMedia, role, mediaID string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackage) != nil ||
		(expectedMedia != "" && protocol.ValidateDigest(expectedMedia) != nil) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if entry.PackageID != expectedPackage || entry.MediaID != expectedMedia {
		return catalog.CoreEntry{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspection, _, err := s.readInstalledCore(ctx, entry.PackageID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if inspection.Descriptor.Core.ID != entry.CoreID {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	if _, err := s.readCoreEntryMedia(ctx, inspection.Descriptor, role, mediaID); err != nil {
		return catalog.CoreEntry{}, err
	}
	selected, err := store.SelectCoreEntryMedia(ctx, gameID, expectedPackage, expectedMedia, role, mediaID)
	return selected, mapCoreMediaError(err)
}

// readCoreEntryMedia validates and snapshots the selected bytes before package
// activation. This is capability-based: a core ID never chooses content.
func (s *Service) readCoreEntryMedia(ctx context.Context, descriptor corepackage.Descriptor, role, id string) ([]byte, error) {
	if role == "" && id == "" {
		return nil, nil
	}
	if role != "blob" || protocol.ValidateDigest(id) != nil {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	capable := false
	if descriptor.ABI.ID == "fes.simple-computer" && descriptor.ABI.Major == 1 && descriptor.ABI.Minor == 0 {
		for _, contract := range descriptor.Interfaces {
			if contract.ID == "fes.media.blob" && contract.Major == 1 && contract.Minor == 0 {
				capable = true
				break
			}
		}
	}
	if !capable {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	_, data, err := store.CoreMedia(ctx, id)
	return data, mapCoreMediaError(err)
}

func mapCoreMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalog.ErrInvalidCoreMedia):
		return canonicalError(protocol.CodeBadRequest, nil)
	case errors.Is(err, catalog.ErrCoreMediaNotFound):
		return canonicalError(protocol.CodeROMNotFound, nil)
	default:
		return mapCoreEntryError(err)
	}
}
