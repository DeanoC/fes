package kitpackages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) validateRoot(r *os.Root) error {
	if err := realDirectories(s.root, false); err != nil {
		return err
	}
	info, err := os.Lstat(s.root)
	if err != nil || !os.SameFile(s.info, info) || info.Mode().Perm() != 0o700 {
		return errors.New("kit packages: root changed")
	}
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(s.info, opened) {
		return errors.New("kit packages: opened root changed")
	}
	for name, expected := range s.children {
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, info) || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("kit packages: %s root changed", name)
		}
	}
	if s.persistent {
		return s.CheckPersistent()
	}
	return nil
}

func realDirectories(path string, create bool) error {
	return realDirectoriesContext(context.Background(), path, create)
}

func realDirectoriesContext(ctx context.Context, path string, create bool) error {
	if ctx == nil {
		return errors.New("kit packages: missing directory context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := realDirectoriesContext(ctx, parent, create); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, os.ErrNotExist) && create {
		if err := ctx.Err(); err != nil {
			return err
		}
		mkdirErr := os.Mkdir(path, 0o700)
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return mkdirErr
		}
		// Persist a newly created library's directory entry as well as its
		// contents, including when its private parent had to be created.
		if err := ctx.Err(); err != nil {
			return err
		}
		parentDirectory, openErr := os.Open(parent)
		if contextErr := ctx.Err(); contextErr != nil {
			if parentDirectory != nil {
				_ = parentDirectory.Close()
			}
			return contextErr
		}
		if openErr != nil {
			return openErr
		}
		if err := ctx.Err(); err != nil {
			_ = parentDirectory.Close()
			return err
		}
		syncErr := parentDirectory.Sync()
		contextErr := ctx.Err()
		closeErr := parentDirectory.Close()
		if contextErr != nil {
			return contextErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err = os.Lstat(path)
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("kit packages: path %s must be a real directory", path)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func directoryEntries(r *os.Root, name string) ([]os.DirEntry, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func syncDirectory(r *os.Root, name string) error {
	return syncDirectoryContext(context.Background(), r, name)
}

func syncDirectoryContext(ctx context.Context, r *os.Root, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := r.Open(name)
	if err != nil {
		return err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		_ = f.Close()
		return contextErr
	}
	err = f.Sync()
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

// Only private, recognizable incomplete publications are reclaimed under the
// cross-process lock. Immutable installed packages are never removed.
func cleanTemporaries(r *os.Root) error {
	entries, err := directoryEntries(r, "packages")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !stageRE.MatchString(entry.Name()) {
			continue
		}
		name := filepath.Join("packages", entry.Name())
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("kit packages: invalid staging entry")
		}
		if err := r.Chmod(name, 0o700); err != nil {
			return err
		}
		if err := r.RemoveAll(name); err != nil {
			return err
		}
	}
	entries, err = directoryEntries(r, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !selectionTemporaryRE.MatchString(entry.Name()) {
			continue
		}
		info, err := r.Lstat(entry.Name())
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("kit packages: invalid selection temporary")
		}
		if err := r.Remove(entry.Name()); err != nil {
			return err
		}
	}
	entries, err = directoryEntries(r, "archives")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !archiveTemporaryRE.MatchString(entry.Name()) {
			continue
		}
		name := filepath.Join("archives", entry.Name())
		info, err := r.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
			return errors.New("kit packages: invalid archive temporary")
		}
		if err := r.Remove(name); err != nil {
			return err
		}
	}
	return nil
}
