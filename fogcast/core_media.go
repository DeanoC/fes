package fogcast

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaCatalog interface {
	ImportCoreMediaStream(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
	CoreMediaInfo(context.Context, string) (catalog.CoreMedia, error)
	OpenCoreMedia(context.Context, string) (catalog.CoreMedia, io.ReadCloser, error)
	CreateCoreMediaEntry(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntryMedia(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

// ImportCoreMedia snapshots a bounded asset into the existing catalog. Import
// never selects media for an entry or mutates a running session.
func (s *Service) ImportCoreMedia(parent context.Context, size int64, body io.Reader) (catalog.CoreMedia, bool, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	value, created, storeErr := store.ImportCoreMediaStream(ctx, size, body)
	return value, created, mapCoreMediaError(storeErr)
}

func (s *Service) CoreMedia(ctx context.Context, id string) (catalog.CoreMedia, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, err := store.CoreMediaInfo(ctx, id)
	return value, mapCoreMediaError(err)
}

// CoreMediaCapabilities is an offline declaration projection. The running
// target remains the authority for actual active capability and delivery.
func (s *Service) CoreMediaCapabilities(ctx context.Context, id string) (protocol.CoreMediaCapabilities, error) {
	inspection, _, err := s.readInstalledCore(ctx, id)
	if err != nil {
		return protocol.CoreMediaCapabilities{}, err
	}
	return protocol.CoreMediaCapabilities{
		PackageID: inspection.PackageID, Source: "declared-contract",
		Compatibility: "unknown", ImportMaxBytes: catalog.MaxCoreMediaBytes,
		Media: protocol.DeclaredCoreMediaCapabilities(inspection.Descriptor),
	}, nil
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
	var capability *protocol.CoreMediaCapability
	for _, declared := range protocol.DeclaredCoreMediaCapabilities(descriptor) {
		if declared.Role == role {
			capability = &declared
			break
		}
	}
	if capability == nil {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, err := store.CoreMediaInfo(ctx, id)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if media.Size < capability.MinBytes || media.Size > capability.MaxBytes {
		return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request",
			Message: fmt.Sprintf("selected %s media is %d bytes; package contract accepts %d..%d bytes", role, media.Size, capability.MinBytes, capability.MaxBytes)}
	}
	opened, reader, err := store.OpenCoreMedia(ctx, id)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	defer reader.Close()
	if opened != media {
		return nil, canonicalError(protocol.CodeInternal, nil)
	}
	// The legacy target transport still needs a bounded small snapshot; larger
	// stored assets were rejected above, before activation or allocation.
	data, err := io.ReadAll(io.LimitReader(reader, capability.MaxBytes+1))
	if err != nil || int64(len(data)) != media.Size {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if err := reader.Close(); err != nil {
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return data, nil
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
