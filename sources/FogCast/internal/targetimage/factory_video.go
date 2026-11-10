package targetimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

const FactoryVideoSelectionName = "fes-core-video-parts.json"

// factoryVideoShellProfile applies the same closed admission rule as the FES
// catalog and core-dev tools. No marker is an ordinary package; an unsupported
// marker is an error and must not silently bypass the video-parts inventory.
func factoryVideoShellProfile(d corepackage.Descriptor) (string, error) {
	var marker corepackage.Interface
	count := 0
	for _, iface := range d.Interfaces {
		if iface.ID == expansion.VideoSlot || iface.ID == expansion.NativeVideoSlot {
			marker = iface
			count++
		}
	}
	if count == 0 {
		return "", nil
	}
	invalid := func() (string, error) { return "", errors.New("unsupported factory video shell profile") }
	if count != 1 || marker.Major != 1 || marker.Minor != 0 || marker.Required {
		return invalid()
	}
	switch d.Core.ID {
	case "fes.coleco":
		if d.ABI != (corepackage.Contract{ID: "fes.application", Major: 1, Minor: 0}) {
			return invalid()
		}
		if d.Format != 2 {
			if d.Format != 4 || len(d.ROMs) != 2 ||
				d.ROMs[0] != (corepackage.ROMRequirement{ID: "coleco-bios", Role: "firmware", SourceSize: 8192, SourceOffset: 0}) ||
				d.ROMs[1] != (corepackage.ROMRequirement{ID: "coleco-cart", Role: "cartridge", SourceSize: 16384, SourceOffset: 8192}) {
				return invalid()
			}
		}
		if marker.ID == expansion.NativeVideoSlot {
			return expansion.ColecoNativeVideoMap, nil
		}
		return expansion.ColecoVideoMap, nil
	case "fes.atari-st":
		if d.Format != 3 || d.ABI != (corepackage.Contract{ID: "fes.computer", Major: 1, Minor: 0}) ||
			d.ROM == nil || d.ROM.Role != "firmware" || marker.ID != expansion.VideoSlot {
			return invalid()
		}
		bus := corepackage.Interface{ID: expansion.AtariStSlot, Major: 1, Minor: 0, Required: false}
		for _, iface := range d.Interfaces {
			if iface == bus {
				return expansion.AtariStVideoMap, nil
			}
		}
	}
	return invalid()
}

// InspectFactoryVideoParts also checks coverage: every selected marked shell
// has an indexed pair, and no indexed shell lies outside the selected packages.
// An absent tree is permitted only when no selected package has a video socket.
func InspectFactoryVideoParts(ctx context.Context, directory, packages string) (*corepackage.FactoryVideoSet, error) {
	if ctx == nil {
		return nil, errors.New("factory video inspection requires a context")
	}
	if directory != "" {
		if err := validateAbsoluteDestination(directory, "video parts root"); err != nil {
			return nil, err
		}
	}
	if err := validateAbsoluteDestination(packages, "package root"); err != nil {
		return nil, err
	}
	info, err := os.Lstat(packages)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("factory package root must be a non-symlink directory")
	}
	entries, err := os.ReadDir(packages)
	if err != nil {
		return nil, err
	}
	expected := map[string]bool{}
	for _, entry := range entries {
		if !validSHA256(entry.Name()) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("factory package root contains an invalid entry")
		}
		if err := verifyClosedPackage(filepath.Join(packages, entry.Name()), true); err != nil {
			return nil, err
		}
		inspection, err := corepackage.InspectPackage(filepath.Join(packages, entry.Name()))
		if err != nil || inspection.PackageID != entry.Name() {
			return nil, errors.New("factory package identity differs from directory")
		}
		profile, err := factoryVideoShellProfile(inspection.Descriptor)
		if err != nil {
			return nil, fmt.Errorf("factory package %s: %w", entry.Name(), err)
		}
		if profile != "" {
			expected[entry.Name()] = true
		}
	}
	if directory == "" {
		if len(expected) > 0 {
			return nil, errors.New("marked factory video shell requires a video parts tree")
		}
		return nil, nil
	}
	if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		if len(expected) > 0 {
			return nil, errors.New("marked factory video shell requires a video parts tree")
		}
		return nil, nil
	}
	set, err := corepackage.ReadFactoryVideoParts(ctx, directory, func(ctx context.Context, id string) ([]byte, error) {
		if !expected[id] {
			return nil, errors.New("factory video index refers to an unselected or unmarked shell")
		}
		return corepackage.CanonicalArchive(filepath.Join(packages, id))
	})
	if err != nil {
		return nil, err
	}
	if len(set.Index.Packages) != len(expected) {
		return nil, errors.New("factory video index does not cover all selected marked shells")
	}
	return &set, nil
}

// PrepareFactoryVideoParts stages only validated bytes and replaces the cache
// tree and its external index together, restoring both if publication fails.
func PrepareFactoryVideoParts(ctx context.Context, directory, packages, cache string) error {
	return prepareFactoryVideoParts(ctx, directory, packages, cache, os.Rename)
}

func prepareFactoryVideoParts(ctx context.Context, directory, packages, cache string, rename func(string, string) error) error {
	if directory == "" {
		return errors.New("factory video source tree is required")
	}
	set, err := InspectFactoryVideoParts(ctx, directory, packages)
	if err != nil {
		return err
	}
	if set == nil {
		return errors.New("factory video source tree is missing")
	}
	if err := validateAbsoluteDestination(cache, "cache"); err != nil {
		return err
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(cache)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("factory video cache must be a non-symlink directory")
	}
	temporary, err := os.MkdirTemp(cache, ".factory-video.*")
	if err != nil {
		return err
	}
	defer removePath(temporary)
	staged := filepath.Join(temporary, "tree")
	if err := os.Mkdir(staged, 0o755); err != nil {
		return err
	}
	for _, pkg := range set.Index.Packages {
		if err := os.Mkdir(filepath.Join(staged, pkg.PackageID), 0o755); err != nil {
			return err
		}
	}
	for _, part := range set.Parts {
		if err := os.WriteFile(filepath.Join(staged, part.Reference.ArchivePath), part.Archive, 0o444); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(staged, "index.json"), set.IndexBytes, 0o444); err != nil {
		return err
	}
	for _, pkg := range set.Index.Packages {
		if err := os.Chmod(filepath.Join(staged, pkg.PackageID), 0o555); err != nil {
			return err
		}
	}
	if err := os.Chmod(staged, 0o555); err != nil {
		return err
	}
	record := filepath.Join(temporary, "record")
	if err := os.WriteFile(record, set.IndexBytes, 0o444); err != nil {
		return err
	}
	destination := filepath.Join(cache, "core-video-parts")
	selection := filepath.Join(cache, FactoryVideoSelectionName)
	for _, name := range []string{destination, selection} {
		info, err := os.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (name == destination && !info.IsDir()) || (name == selection && !info.Mode().IsRegular()) {
			return errors.New("unsafe previous factory video cache entry")
		}
	}
	movedTree, movedRecord, publishedTree := false, false, false
	rollback := func() {
		if publishedTree {
			_ = removePath(destination)
		}
		if movedTree {
			_ = os.Chmod(filepath.Join(temporary, "previous-tree"), 0o755)
			_ = rename(filepath.Join(temporary, "previous-tree"), destination)
			_ = os.Chmod(destination, 0o555)
		}
		if movedRecord {
			_ = rename(filepath.Join(temporary, "previous-record"), selection)
		}
	}
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Chmod(destination, 0o755); err != nil {
			return err
		}
		if err := rename(destination, filepath.Join(temporary, "previous-tree")); err != nil {
			_ = os.Chmod(destination, 0o555)
			return err
		}
		movedTree = true
	}
	if _, err := os.Lstat(selection); err == nil {
		if err := rename(selection, filepath.Join(temporary, "previous-record")); err != nil {
			rollback()
			return err
		}
		movedRecord = true
	}
	if err := os.Chmod(staged, 0o755); err != nil {
		rollback()
		return err
	}
	if err := rename(staged, destination); err != nil {
		rollback()
		return err
	}
	publishedTree = true
	if err := os.Chmod(destination, 0o555); err != nil {
		rollback()
		return err
	}
	if err := rename(record, selection); err != nil {
		rollback()
		return err
	}
	return nil
}

func VerifyFactoryVideoSelection(set *corepackage.FactoryVideoSet, selection string) error {
	if set == nil {
		return errors.New("factory video tree is unavailable")
	}
	file, info, err := openRegularNoFollow(selection, true)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Size() != int64(len(set.IndexBytes)) {
		return errors.New("factory video selection size differs from index")
	}
	data := make([]byte, info.Size())
	if _, err := file.ReadAt(data, 0); err != nil {
		return err
	}
	if !bytes.Equal(data, set.IndexBytes) {
		return errors.New("factory video selection differs from tree index")
	}
	return nil
}

func FactoryVideoBuildInputs(set *corepackage.FactoryVideoSet) string {
	if set == nil {
		return ""
	}
	result := fmt.Sprintf("factory_video_index_sha256=%s\nfactory_video_install_path=/usr/share/mister-runtime/core-video-parts\n", set.SHA256)
	parts := append([]corepackage.FactoryVideoPart(nil), set.Parts...)
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PackageID+parts[i].Reference.Profile < parts[j].PackageID+parts[j].Reference.Profile
	})
	for i, part := range parts {
		prefix := fmt.Sprintf("factory_video_%d", i)
		result += fmt.Sprintf("%s_package_id=%s\n%s_profile=%s\n%s_part_id=%s\n%s_archive_sha256=%s\n%s_archive_size=%d\n", prefix, part.PackageID, prefix, part.Reference.Profile, prefix, part.Reference.PartID, prefix, part.Reference.ArchiveSHA256, prefix, part.Reference.ArchiveSize)
	}
	return result
}
