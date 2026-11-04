package kitpackages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DeanoC/FogCast/corepackage"
)

// FactoryRoot holds the image's factory packages: one sealed, unpacked
// directory per package, named by its package ID.
const FactoryRoot = "/usr/share/mister-runtime/core-packages"

const menuCoreID = "fes.menu"

// Factory reads the read-only factory packages under one root. A package
// directory hashes to the same ID as its canonical archive, so the kit host
// can serve it as if it were an installed archive.
type Factory struct {
	root string

	verifyMu sync.Mutex
	verified map[string]verifiedPackage
}

// factoryVerifications counts factory package reads that hash the members.
var factoryVerifications atomic.Int64

func NewFactory(root string) *Factory {
	return &Factory{root: root, verified: make(map[string]verifiedPackage)}
}

// List returns the factory packages sorted by ID. An absent root is an empty
// list. As in kit-local inventory, the menu package is not listed, and a
// directory that fails inspection or does not hash to its name is omitted.
// Inspect and Read still find the menu package by ID.
func (f *Factory) List(ctx context.Context) ([]corepackage.Inspection, error) {
	entries, err := os.ReadDir(f.root)
	if errors.Is(err, os.ErrNotExist) {
		return []corepackage.Inspection{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []corepackage.Inspection{}
	for _, entry := range entries {
		if !packageIDRE.MatchString(entry.Name()) {
			continue
		}
		inspection, err := f.Inspect(ctx, entry.Name())
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if inspection.Descriptor.Core.ID == menuCoreID {
			continue
		}
		result = append(result, inspection)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PackageID < result[j].PackageID })
	return result, nil
}

// Inspect validates one factory package. An unchanged directory is not read
// or hashed again.
func (f *Factory) Inspect(ctx context.Context, id string) (corepackage.Inspection, error) {
	if !packageIDRE.MatchString(id) {
		return corepackage.Inspection{}, ErrInvalidPackageID
	}
	if err := ctx.Err(); err != nil {
		return corepackage.Inspection{}, err
	}
	before, err := f.statKey(id)
	if err != nil {
		return corepackage.Inspection{}, err
	}
	if inspection, ok := f.cachedVerified(id, before); ok {
		return inspection, nil
	}
	factoryVerifications.Add(1)
	inspection, err := corepackage.InspectListing(ctx, filepath.Join(f.root, id))
	if err != nil {
		return corepackage.Inspection{}, err
	}
	return f.verify(id, before, inspection)
}

// Read returns the package and its canonical archive bytes. An unchanged
// package reuses its verified inspection but reads archive bytes from disk.
func (f *Factory) Read(ctx context.Context, id string) (corepackage.Inspection, []byte, error) {
	if !packageIDRE.MatchString(id) {
		return corepackage.Inspection{}, nil, ErrInvalidPackageID
	}
	if err := ctx.Err(); err != nil {
		return corepackage.Inspection{}, nil, err
	}
	before, err := f.statKey(id)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	if inspection, ok := f.cachedVerified(id, before); ok {
		data, err := corepackage.CanonicalArchive(filepath.Join(f.root, id))
		if err != nil {
			return corepackage.Inspection{}, nil, err
		}
		return inspection, data, nil
	}
	factoryVerifications.Add(1)
	inspection, data, err := corepackage.ReadPackage(filepath.Join(f.root, id))
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	inspection, err = f.verify(id, before, inspection)
	if err != nil {
		return corepackage.Inspection{}, nil, err
	}
	return inspection, data, nil
}

// verify rejects a directory that does not hash to its name, and remembers
// an inspection only when the directory did not change while it was read.
func (f *Factory) verify(id, before string, inspection corepackage.Inspection) (corepackage.Inspection, error) {
	if inspection.PackageID != id {
		return corepackage.Inspection{}, fmt.Errorf("%w: factory package %s hashes to %s", ErrInvalidPackage, id, inspection.PackageID)
	}
	if after, err := f.statKey(id); err == nil && after == before {
		f.verifyMu.Lock()
		f.verified[id] = verifiedPackage{key: after, inspection: inspection.Clone()}
		f.verifyMu.Unlock()
	}
	return inspection, nil
}

func (f *Factory) cachedVerified(id, key string) (corepackage.Inspection, bool) {
	f.verifyMu.Lock()
	defer f.verifyMu.Unlock()
	hit, ok := f.verified[id]
	if !ok || hit.key != key {
		return corepackage.Inspection{}, false
	}
	return hit.inspection.Clone(), true
}

// statKey is the package directory and each member's size, mtime, mode,
// device, inode and ctime. A missing directory is ErrPackageNotFound.
func (f *Factory) statKey(id string) (string, error) {
	r, err := os.OpenRoot(f.root)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrPackageNotFound
	}
	if err != nil {
		return "", err
	}
	defer r.Close()
	info, err := r.Lstat(id)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrPackageNotFound
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: factory package %s is not a real directory", ErrInvalidPackage, id)
	}
	var b strings.Builder
	if err := writeStat(&b, r, id); err != nil {
		return "", err
	}
	entries, err := directoryEntries(r, id)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		fmt.Fprintf(&b, "entry %s\n", entry.Name())
		if err := writeStat(&b, r, filepath.Join(id, entry.Name())); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}
