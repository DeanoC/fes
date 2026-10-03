package fogcast

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// LocalContentPath is the regular file for one catalog row, under its
// library root. Symlinks and paths that leave the root are refused. It does
// not dial a target.
func (s *Service) LocalContentPath(ctx context.Context, gameID string) (string, error) {
	if s == nil {
		return "", errors.New("local catalog is unavailable")
	}
	game, err := s.Game(ctx, gameID)
	if err != nil {
		return "", err
	}
	if !game.RootOnline || game.State != catalog.SourceStateAvailable {
		return "", errors.New("local catalog file is unavailable")
	}
	root, ok := s.libraryRoot(game.LibraryID)
	if !ok || strings.TrimSpace(root.Path) == "" {
		return "", errors.New("local catalog root is unavailable")
	}
	rel, err := catalog.NormalizeRelativePath(game.RelativePath)
	if err != nil {
		return "", err
	}
	rootPath, err := filepath.Abs(root.Path)
	if err != nil {
		return "", err
	}
	full := filepath.Join(rootPath, filepath.FromSlash(rel))
	relBack, err := filepath.Rel(rootPath, full)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(os.PathSeparator)) {
		return "", errors.New("local catalog path escapes its root")
	}
	info, err := os.Lstat(full)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("local catalog file is unavailable")
	}
	if info.Size() <= 0 || info.Size() > maxLocalCartridgeBytes {
		return "", errors.New("local catalog file is unavailable")
	}
	return full, nil
}
