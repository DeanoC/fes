package corepackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/DeanoC/misteross/expansion"
)

const MaxFactoryVideoIndexSize = 64 * 1024
const MaxFactoryVideoArchiveSize = 32 * 1024 * 1024

// Field order follows the sorted-key canonical JSON shared with the producer.
type FactoryVideoReference struct {
	ArchivePath   string `json:"archive_path"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveSize   int64  `json:"archive_size"`
	PartID        string `json:"part_id"`
	Profile       string `json:"profile"`
}

type FactoryVideoPackage struct {
	PackageID string                  `json:"package_id"`
	Parts     []FactoryVideoReference `json:"parts"`
}

type FactoryVideoIndex struct {
	Packages []FactoryVideoPackage `json:"packages"`
	Version  int                   `json:"version"`
}

type FactoryVideoPart struct {
	PackageID string
	Reference FactoryVideoReference
	Archive   []byte
	Asset     expansion.Asset
}

type FactoryVideoSet struct {
	Index      FactoryVideoIndex
	IndexBytes []byte
	SHA256     string
	Parts      []FactoryVideoPart
}

func validateFactoryVideoReference(packageID string, ref FactoryVideoReference) error {
	if !hex64RE.MatchString(packageID) || !hex64RE.MatchString(ref.PartID) ||
		!hex64RE.MatchString(ref.ArchiveSHA256) || ref.ArchiveSize < 1 || ref.ArchiveSize > MaxFactoryVideoArchiveSize ||
		(ref.Profile != "direct" && ref.Profile != "scanlines") ||
		ref.ArchivePath == "" || path.IsAbs(ref.ArchivePath) || path.Clean(ref.ArchivePath) != ref.ArchivePath ||
		strings.Contains(ref.ArchivePath, "\\") || strings.IndexFunc(ref.ArchivePath, unicode.IsControl) >= 0 {
		return errors.New("invalid factory video reference")
	}
	for _, component := range strings.Split(ref.ArchivePath, "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("invalid factory video archive path")
		}
	}
	return nil
}

func DecodeFactoryVideoIndex(data []byte) (FactoryVideoIndex, error) {
	var index FactoryVideoIndex
	if len(data) < 1 || len(data) > MaxFactoryVideoIndexSize {
		return index, errors.New("invalid factory video index size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&index); err != nil {
		return index, fmt.Errorf("decode factory video index: %w", err)
	}
	if index.Version != 1 || len(index.Packages) < 1 || len(index.Packages) > 32 {
		return index, errors.New("unsupported factory video index")
	}
	partIDs := map[string]bool{}
	for i, pkg := range index.Packages {
		if !hex64RE.MatchString(pkg.PackageID) || (i > 0 && index.Packages[i-1].PackageID >= pkg.PackageID) || len(pkg.Parts) != 2 {
			return index, errors.New("factory video packages must be sorted unique exact shell IDs with two profiles")
		}
		for j, ref := range pkg.Parts {
			if err := validateFactoryVideoReference(pkg.PackageID, ref); err != nil {
				return index, err
			}
			if ref.ArchivePath != pkg.PackageID+"/"+ref.PartID+".tar" {
				return index, errors.New("factory video tree archive path differs from exact shell and part IDs")
			}
			if partIDs[ref.PartID] {
				return index, errors.New("factory video part IDs must be globally unique")
			}
			partIDs[ref.PartID] = true
			if ref.Profile != []string{"direct", "scanlines"}[j] {
				return index, errors.New("factory video profiles must be distinct direct and scanlines mappings")
			}
		}
	}
	canonical, err := json.Marshal(index)
	if err != nil || !bytes.Equal(data, append(canonical, '\n')) {
		return index, errors.New("factory video index is not canonical")
	}
	return index, nil
}

// AdmitFactoryVideoPart validates a catalog reference and the exact downloaded
// archive against a canonical sealed shell. Labels never establish resource fit.
func AdmitFactoryVideoPart(ctx context.Context, packageID string, ref FactoryVideoReference, archive, canonicalShell []byte) (FactoryVideoPart, error) {
	if ctx == nil {
		return FactoryVideoPart{}, errors.New("factory video admission requires a context")
	}
	if err := validateFactoryVideoReference(packageID, ref); err != nil {
		return FactoryVideoPart{}, err
	}
	if int64(len(archive)) != ref.ArchiveSize {
		return FactoryVideoPart{}, errors.New("factory video archive differs from reference")
	}
	digest := sha256.Sum256(archive)
	if fmt.Sprintf("%x", digest) != ref.ArchiveSHA256 {
		return FactoryVideoPart{}, errors.New("factory video archive differs from reference")
	}
	asset, err := expansion.ReadAsset(bytes.NewReader(archive))
	if err != nil {
		return FactoryVideoPart{}, fmt.Errorf("read factory video part: %w", err)
	}
	if asset.ID != ref.PartID || asset.Manifest.ShellPackageID != packageID || asset.Manifest.Slot != expansion.VideoSlot {
		return FactoryVideoPart{}, errors.New("factory video part identity or shell differs from reference")
	}
	if _, err := ComposePartsArchive(ctx, canonicalShell, []expansion.Asset{asset}); err != nil {
		return FactoryVideoPart{}, fmt.Errorf("admit factory video composition: %w", err)
	}
	return FactoryVideoPart{PackageID: packageID, Reference: ref, Archive: bytes.Clone(archive), Asset: asset}, nil
}

// ReadFactoryVideoParts pins a closed immutable tree and fully admits every
// indexed part before returning any bytes for installation or import.
func ReadFactoryVideoParts(ctx context.Context, directory string, resolve func(context.Context, string) ([]byte, error)) (FactoryVideoSet, error) {
	if ctx == nil || resolve == nil || !filepath.IsAbs(directory) {
		return FactoryVideoSet{}, errors.New("invalid factory video tree request")
	}
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() || before.Mode().Perm() != 0o555 || before.Mode()&os.ModeSymlink != 0 {
		return FactoryVideoSet{}, errors.New("factory video tree must be a sealed non-symlink directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return FactoryVideoSet{}, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return FactoryVideoSet{}, errors.New("factory video root changed while opening")
	}
	data, err := readFactoryVideoMember(root, "index.json", MaxFactoryVideoIndexSize)
	if err != nil {
		return FactoryVideoSet{}, err
	}
	index, err := DecodeFactoryVideoIndex(data)
	if err != nil {
		return FactoryVideoSet{}, err
	}
	expected := map[string]bool{"index.json": true}
	set := FactoryVideoSet{Index: index, IndexBytes: data, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	for _, pkg := range index.Packages {
		if err := ctx.Err(); err != nil {
			return FactoryVideoSet{}, err
		}
		expected[pkg.PackageID] = true
		dirInfo, err := root.Lstat(pkg.PackageID)
		if err != nil || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o555 || dirInfo.Mode()&os.ModeSymlink != 0 {
			return FactoryVideoSet{}, errors.New("factory video package directory must be sealed and non-symlink")
		}
		shell, err := resolve(ctx, pkg.PackageID)
		if err != nil {
			return FactoryVideoSet{}, err
		}
		members := map[string]bool{}
		for _, ref := range pkg.Parts {
			members[ref.PartID+".tar"] = true
			archive, err := readFactoryVideoMember(root, ref.ArchivePath, MaxFactoryVideoArchiveSize)
			if err != nil {
				return FactoryVideoSet{}, err
			}
			part, err := AdmitFactoryVideoPart(ctx, pkg.PackageID, ref, archive, shell)
			if err != nil {
				return FactoryVideoSet{}, err
			}
			set.Parts = append(set.Parts, part)
		}
		if err := factoryVideoEntries(root, pkg.PackageID, members); err != nil {
			return FactoryVideoSet{}, err
		}
		after, err := root.Lstat(pkg.PackageID)
		if err != nil || !os.SameFile(dirInfo, after) {
			return FactoryVideoSet{}, errors.New("factory video package directory changed while reading")
		}
	}
	if err := factoryVideoEntries(root, ".", expected); err != nil {
		return FactoryVideoSet{}, err
	}
	after, err := os.Lstat(directory)
	if err != nil || !os.SameFile(opened, after) {
		return FactoryVideoSet{}, errors.New("factory video tree changed while reading")
	}
	return set, nil
}

func factoryVideoEntries(root *os.Root, name string, expected map[string]bool) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) != len(expected) {
		return errors.New("factory video tree contains missing or extra entries")
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return errors.New("factory video tree contains an unlisted entry")
		}
	}
	return nil
}

func readFactoryVideoMember(root *os.Root, name string, maximum int64) ([]byte, error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != 0o444 {
		return nil, fmt.Errorf("factory video member %s must be a sealed regular file", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || opened.Size() < 1 || opened.Size() > maximum {
		return nil, errors.New("factory video member changed or has invalid size")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(data)) != opened.Size() {
		return nil, errors.New("cannot read factory video member")
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(opened, after) {
		return nil, errors.New("factory video member changed while reading")
	}
	return data, nil
}
