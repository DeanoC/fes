package fogcast

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

// librarySourceCatalog is deliberately independent of the active target.
// A source lists local cassette entries and never selects or launches a kit.
type librarySourceCatalog interface {
	CoreEntries(context.Context) ([]catalog.CoreEntry, error)
	CoreMediaInfo(context.Context, string) (catalog.CoreMedia, error)
	OpenCoreMedia(context.Context, string) (catalog.CoreMedia, io.ReadCloser, error)
	CoreEntryExpansion(context.Context, string) (catalog.CoreEntryExpansion, error)
	CoreEntrySlotExpansions(context.Context, string) ([]catalog.CoreEntrySlotExpansion, error)
}

// LibrarySourceEntries returns the supported, locally owned ZX81 cassette
// entries. Firmware and expansion selections remain with their own host.
func (s *Service) LibrarySourceEntries(ctx context.Context) ([]catalog.CoreEntry, error) {
	if s == nil || s.catalog == nil {
		return nil, errors.New("library source catalog unavailable")
	}
	store, ok := s.catalog.(librarySourceCatalog)
	if !ok {
		return nil, errors.New("library source catalog unavailable")
	}
	entries, err := store.CoreEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]catalog.CoreEntry, 0)
	for _, entry := range entries {
		if entry.CoreID != "fes.zx81" || entry.MediaRole != "cassette" ||
			protocol.ValidateGameID(entry.GameID) != nil || protocol.ValidateDigest(entry.PackageID) != nil ||
			protocol.ValidateDigest(entry.MediaID) != nil || strings.TrimSpace(entry.Title) == "" {
			continue
		}
		if remote, ok := s.catalog.(interface {
			IsRemoteSourceEntry(context.Context, string) (bool, error)
		}); ok {
			isRemote, err := remote.IsRemoteSourceEntry(ctx, entry.GameID)
			if err != nil {
				return nil, err
			}
			if isRemote {
				continue
			}
		}
		legacy, err := store.CoreEntryExpansion(ctx, entry.GameID)
		if err != nil {
			return nil, err
		}
		slots, err := store.CoreEntrySlotExpansions(ctx, entry.GameID)
		if err != nil {
			return nil, err
		}
		if legacy.ExpansionID != "" || len(slots) != 0 {
			continue
		}
		media, err := store.CoreMediaInfo(ctx, entry.MediaID)
		if errors.Is(err, catalog.ErrCoreMediaNotFound) || errors.Is(err, catalog.ErrInvalidCoreMedia) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if media.MediaID != entry.MediaID || media.Size < 1 || media.Size > 16384 {
			continue
		}
		// Limit the wire to identities needed by the receiving kit. In
		// particular, never forward a source-host firmware selection.
		result = append(result, catalog.CoreEntry{GameID: entry.GameID, Title: entry.Title,
			CoreID: entry.CoreID, PackageID: entry.PackageID, MediaID: entry.MediaID, MediaRole: entry.MediaRole})
	}
	return result, nil
}

// LibrarySourceContentAdvertises restricts object access to exported titles.
func (s *Service) LibrarySourceContentAdvertises(ctx context.Context, id meshcontent.ContentID) (bool, error) {
	if id.Validate() != nil {
		return false, nil
	}
	entries, err := s.LibrarySourceEntries(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.MediaID == id.Digest {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) OpenLibrarySourceContent(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error) {
	allowed, err := s.LibrarySourceContentAdvertises(ctx, id)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, os.ErrNotExist
	}
	store := s.catalog.(librarySourceCatalog)
	media, body, err := store.OpenCoreMedia(ctx, id.Digest)
	if err != nil {
		if errors.Is(err, catalog.ErrCoreMediaNotFound) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	if media.MediaID != id.Digest || media.Size < 1 || media.Size > 16384 {
		body.Close()
		return nil, os.ErrNotExist
	}
	return body, nil
}
