package fogcast

import (
	"context"
	"fmt"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

const maxSMSMatchBytes = 4 << 20
const maxSMSMatchArchiveBytes = 8 << 20

// NativeSMSROMHash identifies the selected cartridge bytes without preparing
// unbounded sources or changing the catalog's stored content identity.
func (s *Service) NativeSMSROMHash(ctx context.Context, game catalog.Game) (string, error) {
	if game.System != protocol.SystemSMS || (game.Kind != catalog.SourceKindRaw && game.Kind != catalog.SourceKindZIP) || !game.RootOnline || game.State != catalog.SourceStateAvailable {
		return "", nil
	}
	size := game.Fingerprint.SourceSize
	if game.Kind == catalog.SourceKindZIP {
		if game.Fingerprint.ZIPSize < 1 || game.Fingerprint.ZIPSize > maxSMSMatchBytes {
			return "", nil
		}
		if size < 1 || size > maxSMSMatchArchiveBytes {
			return "", nil
		}
	} else if size < 1 || size > maxSMSMatchBytes {
		return "", nil
	}
	if game.Content != nil && game.Content.Size > 0 && game.Content.Size <= maxSMSMatchBytes && protocol.ValidateDigest(game.Content.SHA256) == nil {
		return game.Content.SHA256, nil
	}
	root, ok := s.libraryRoot(game.LibraryID)
	if !ok {
		return "", fmt.Errorf("SMS library root unavailable")
	}
	prepared, err := s.preparer.Prepare(ctx, root, game)
	if err != nil {
		return "", err
	}
	defer prepared.Remove()
	if prepared.Content.Size < 1 || prepared.Content.Size > maxSMSMatchBytes {
		return "", nil
	}
	return prepared.Content.SHA256, nil
}

// maxLocalCartridgeBytes bounds a kit-local ROM read. Master System carts
// are far smaller. The file is not uploaded anywhere.
const maxLocalCartridgeBytes = 8 << 20
