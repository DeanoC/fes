package corecatalog

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

func validateVideoReferences(parts []corepackage.FactoryVideoReference) error {
	if len(parts) > 2 {
		return errors.New("too many published video parts")
	}
	profiles, ids, paths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, part := range parts {
		if (part.Profile != "direct" && part.Profile != "scanlines") || profiles[part.Profile] ||
			!digest.MatchString(part.PartID) || ids[part.PartID] || !validPath(part.ArchivePath) || paths[part.ArchivePath] ||
			strings.ContainsAny(part.ArchivePath, "\x00\r\n\t") ||
			!digest.MatchString(part.ArchiveSHA256) || part.ArchiveSize < 1 || part.ArchiveSize > corepackage.MaxFactoryVideoArchiveSize {
			return errors.New("invalid published video part identity")
		}
		profiles[part.Profile], ids[part.PartID], paths[part.ArchivePath] = true, true, true
	}
	return nil
}

// ReadVideoParts snapshots and admits every declared companion against the
// canonical shell before callers publish any installed package or profile.
func (c Catalog) ReadVideoParts(ctx context.Context, entry Entry, shell []byte) ([]corepackage.FactoryVideoPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateVideoReferences(entry.VideoParts); err != nil {
		return nil, err
	}
	parts := make([]corepackage.FactoryVideoPart, 0, len(entry.VideoParts))
	if len(entry.VideoParts) == 0 {
		return parts, nil
	}
	root, err := os.OpenRoot(c.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, reference := range entry.VideoParts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Relative paths remain contained, and neither files nor their parent
		// components may redirect a publication through a symlink.
		components := strings.Split(reference.ArchivePath, "/")
		for i := range components {
			info, err := root.Lstat(strings.Join(components[:i+1], "/"))
			if err != nil || info.Mode()&os.ModeSymlink != 0 || (i < len(components)-1 && !info.IsDir()) || (i == len(components)-1 && !info.Mode().IsRegular()) {
				return nil, errors.New("invalid published video part path")
			}
		}
		file, err := root.Open(reference.ArchivePath)
		if err != nil {
			return nil, err
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != reference.ArchiveSize {
			_ = file.Close()
			return nil, errors.New("invalid published video part file")
		}
		archive, readErr := io.ReadAll(io.LimitReader(file, reference.ArchiveSize+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		part, err := corepackage.AdmitFactoryVideoPart(ctx, entry.PackageID, reference, archive, shell)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}
