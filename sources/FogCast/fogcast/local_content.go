package fogcast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
)

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
