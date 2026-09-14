package targetimage

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/pelletier/go-toml/v2"
)

const corePackageSelectionName = "fes-pong.package-selection.toml"

var lowerRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

func packageCoreName(coreID string) (string, error) {
	switch coreID {
	case "fes.pong":
		return "pong", nil
	case "fes.zx81":
		return "zx81", nil
	case "fes.coleco":
		return "coleco", nil
	default:
		return "", fmt.Errorf("unsupported format-2 package core %q", coreID)
	}
}

func packageSelectionNameForCore(coreID string) (string, error) {
	coreName, err := packageCoreName(coreID)
	if err != nil {
		return "", err
	}
	if coreName == "pong" {
		return corePackageSelectionName, nil
	}
	return "fes-" + coreName + ".package-selection.toml", nil
}

// CorePackageSelection is the closed producer/package projection consumed by
// native image assembly. The package manifest remains the descriptor authority.
type CorePackageSelection struct {
	Format                 int    `toml:"format"`
	Kind                   string `toml:"kind"`
	CoreID                 string `toml:"core_id"`
	PackageID              string `toml:"package_id"`
	PayloadSHA256          string `toml:"payload_sha256"`
	MisterossRevision      string `toml:"misteross_revision"`
	MisterPackagesRevision string `toml:"mister_packages_revision"`
	InstallPath            string `toml:"install_path"`
}

// PrepareCorePackageSelection pins the selected record and package members,
// validates their combined identity, and publishes one sealed cache pair.
func PrepareCorePackageSelection(directory, record, cache, output string) (CorePackageSelection, error) {
	return PrepareCorePackageSelectionForCore(directory, record, cache, output, "fes.pong")
}

// PrepareCorePackageSelectionForCore pins one selected record/package pair
// while binding it to the expected format-2 core ID.
func PrepareCorePackageSelectionForCore(directory, record, cache, output, expectedCoreID string) (CorePackageSelection, error) {
	var selection CorePackageSelection
	coreName, err := packageCoreName(expectedCoreID)
	if err != nil {
		return selection, err
	}
	selectionName, err := packageSelectionNameForCore(expectedCoreID)
	if err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(directory, "package directory"); err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(record, "package selection"); err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(cache, "cache"); err != nil {
		return selection, err
	}
	if err := validateAbsoluteDestination(output, "output"); err != nil {
		return selection, err
	}
	if filepath.Clean(output) != filepath.Join(filepath.Clean(cache), selectionName) {
		return selection, fmt.Errorf("package selection output must use the closed cache filename")
	}
	recordBytes, selection, err := readCorePackageSelection(record, true, expectedCoreID)
	if err != nil {
		return CorePackageSelection{}, err
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return CorePackageSelection{}, fmt.Errorf("create package cache: %w", err)
	}
	cacheInfo, err := os.Lstat(cache)
	if err != nil || !cacheInfo.IsDir() || cacheInfo.Mode()&os.ModeSymlink != 0 {
		return CorePackageSelection{}, fmt.Errorf("package cache must be a non-symlink directory")
	}
	packageParent := filepath.Join(cache, "core-packages")
	temporaryRoot, err := os.MkdirTemp(cache, ".fes-"+coreName+"-package-root.*")
	if err != nil {
		return CorePackageSelection{}, fmt.Errorf("create temporary package root: %w", err)
	}
	temporary := filepath.Join(temporaryRoot, selection.PackageID)
	if err := os.Mkdir(temporary, 0o700); err != nil {
		_ = os.RemoveAll(temporaryRoot)
		return CorePackageSelection{}, fmt.Errorf("create temporary package: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = removePath(temporaryRoot)
		}
	}()
	if err := copyClosedPackage(directory, temporary); err != nil {
		return CorePackageSelection{}, err
	}
	inspection, err := corepackage.InspectPackage(temporary)
	if err != nil {
		return CorePackageSelection{}, fmt.Errorf("inspect selected package: %w", err)
	}
	if err := validateCorePackageSelection(selection, inspection); err != nil {
		return CorePackageSelection{}, err
	}
	if err := os.Chmod(temporary, 0o555); err != nil {
		return CorePackageSelection{}, fmt.Errorf("seal package directory: %w", err)
	}
	recordTemporary, err := os.CreateTemp(cache, ".fes-"+coreName+"-selection.*")
	if err != nil {
		return CorePackageSelection{}, fmt.Errorf("create temporary selection: %w", err)
	}
	recordTemporaryPath := recordTemporary.Name()
	removeRecordTemporary := true
	defer func() {
		if removeRecordTemporary {
			_ = os.Remove(recordTemporaryPath)
		}
	}()
	if _, err := recordTemporary.Write(recordBytes); err != nil {
		_ = recordTemporary.Close()
		return CorePackageSelection{}, fmt.Errorf("copy package selection: %w", err)
	}
	if err := recordTemporary.Sync(); err != nil {
		_ = recordTemporary.Close()
		return CorePackageSelection{}, fmt.Errorf("sync package selection: %w", err)
	}
	if err := recordTemporary.Close(); err != nil {
		return CorePackageSelection{}, fmt.Errorf("close package selection: %w", err)
	}
	if err := os.Chmod(recordTemporaryPath, 0o444); err != nil {
		return CorePackageSelection{}, fmt.Errorf("seal package selection: %w", err)
	}
	if _, _, err := InspectCorePackageSelectionForCore(temporary, recordTemporaryPath, expectedCoreID); err != nil {
		return CorePackageSelection{}, fmt.Errorf("verify staged package selection: %w", err)
	}
	if err := replacePackagePair(temporary, recordTemporaryPath, packageParent, output, expectedCoreID); err != nil {
		return CorePackageSelection{}, err
	}
	removeRecordTemporary = false
	return selection, nil
}

// VerifyCorePackageSelection re-reads one sealed directory/record pair and
// requires the closed selection to match the package descriptor and bytes.
func VerifyCorePackageSelection(directory, record string) error {
	return VerifyCorePackageSelectionForCore(directory, record, "fes.pong")
}

// VerifyCorePackageSelectionForCore re-reads one pair with an expected core ID.
func VerifyCorePackageSelectionForCore(directory, record, expectedCoreID string) error {
	_, _, err := InspectCorePackageSelectionForCore(directory, record, expectedCoreID)
	return err
}

// InspectCorePackageSelection verifies one sealed directory/record pair and
// returns its closed selection plus the SHA-256 of the exact record bytes.
func InspectCorePackageSelection(directory, record string) (CorePackageSelection, string, error) {
	return InspectCorePackageSelectionForCore(directory, record, "fes.pong")
}

// InspectCorePackageSelectionForCore verifies one pair and binds its record
// and manifest to the expected format-2 core ID.
func InspectCorePackageSelectionForCore(directory, record, expectedCoreID string) (CorePackageSelection, string, error) {
	recordBytes, selection, err := readCorePackageSelection(record, true, expectedCoreID)
	if err != nil {
		return CorePackageSelection{}, "", err
	}
	if err := verifyClosedPackage(directory, true); err != nil {
		return CorePackageSelection{}, "", err
	}
	if filepath.Base(filepath.Clean(directory)) != selection.PackageID {
		return CorePackageSelection{}, "", fmt.Errorf("package directory name differs from selected package ID")
	}
	inspection, err := corepackage.InspectPackage(directory)
	if err != nil {
		return CorePackageSelection{}, "", fmt.Errorf("inspect package: %w", err)
	}
	if err := validateCorePackageSelection(selection, inspection); err != nil {
		return CorePackageSelection{}, "", err
	}
	digest := sha256.Sum256(recordBytes)
	return selection, fmt.Sprintf("%x", digest), nil
}

func readCorePackageSelection(path string, sealed bool, expectedCoreID string) ([]byte, CorePackageSelection, error) {
	if _, err := packageCoreName(expectedCoreID); err != nil {
		return nil, CorePackageSelection{}, err
	}
	file, info, err := openRegularNoFollow(path, sealed)
	if err != nil {
		return nil, CorePackageSelection{}, fmt.Errorf("open package selection: %w", err)
	}
	defer file.Close()
	if info.Size() <= 0 || info.Size() > 16*1024 {
		return nil, CorePackageSelection{}, fmt.Errorf("package selection size is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil || int64(len(data)) != info.Size() {
		return nil, CorePackageSelection{}, fmt.Errorf("read package selection")
	}
	var selection CorePackageSelection
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		return nil, CorePackageSelection{}, fmt.Errorf("decode package selection: %w", err)
	}
	if selection.Format != 2 || selection.Kind != "core-package" || selection.CoreID != expectedCoreID ||
		!validSHA256(selection.PackageID) || !validSHA256(selection.PayloadSHA256) ||
		!lowerRevision.MatchString(selection.MisterossRevision) ||
		!lowerRevision.MatchString(selection.MisterPackagesRevision) ||
		selection.InstallPath != "/usr/share/mister-runtime/core-packages/"+selection.PackageID {
		return nil, CorePackageSelection{}, fmt.Errorf("invalid core package selection")
	}
	return data, selection, nil
}

func validateCorePackageSelection(selection CorePackageSelection, inspection corepackage.Inspection) error {
	if selection.PackageID != inspection.PackageID || selection.CoreID != inspection.Descriptor.Core.ID ||
		selection.PayloadSHA256 != inspection.Descriptor.Payload.SHA256 ||
		selection.MisterossRevision != inspection.Descriptor.Build.Revision {
		return fmt.Errorf("package bytes differ from selection identity")
	}
	return nil
}

func verifyClosedPackage(directory string, sealed bool) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (sealed && info.Mode().Perm()&0o222 != 0) {
		return fmt.Errorf("package must be a sealed non-symlink directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "core.rbf" || names[1] != "manifest.toml" {
		return fmt.Errorf("package must contain exactly manifest.toml and core.rbf")
	}
	for _, name := range names {
		file, _, err := openRegularNoFollow(filepath.Join(directory, name), sealed)
		if err != nil {
			return fmt.Errorf("open package member %s: %w", name, err)
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func copyClosedPackage(source, destination string) error {
	if err := verifyClosedPackage(source, true); err != nil {
		return err
	}
	for _, name := range []string{"manifest.toml", "core.rbf"} {
		input, _, err := openRegularNoFollow(filepath.Join(source, name), true)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(filepath.Join(destination, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
		if err == nil {
			_, err = io.Copy(output, input)
		}
		closeOutput := error(nil)
		if output != nil {
			closeOutput = output.Close()
		}
		closeInput := input.Close()
		if err != nil || closeOutput != nil || closeInput != nil {
			return fmt.Errorf("copy package member %s", name)
		}
	}
	return nil
}

func replacePackagePair(packageTemporary, recordTemporary, packageDestination, recordDestination, expectedCoreID string) error {
	packageID := filepath.Base(filepath.Clean(packageTemporary))
	if !validSHA256(packageID) {
		return fmt.Errorf("temporary package path has an invalid package ID")
	}

	packageParentCreated := false
	packageInfo, err := os.Lstat(packageDestination)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(packageDestination, 0o700); err != nil {
			return fmt.Errorf("create package cache root: %w", err)
		}
		packageParentCreated = true
	} else if err != nil {
		return err
	} else if !packageInfo.IsDir() || packageInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("package cache root must be a non-symlink directory")
	}

	previousPackageID := ""
	recordExisted := false
	if recordInfo, err := os.Lstat(recordDestination); err == nil {
		if !recordInfo.Mode().IsRegular() || recordInfo.Mode()&os.ModeSymlink != 0 {
			if packageParentCreated {
				_ = os.Remove(packageDestination)
			}
			return fmt.Errorf("previous package selection must be a regular non-symlink file")
		}
		_, previous, err := readCorePackageSelection(recordDestination, true, expectedCoreID)
		if err != nil {
			if packageParentCreated {
				_ = os.Remove(packageDestination)
			}
			return fmt.Errorf("inspect previous package selection: %w", err)
		}
		previousPackageID = previous.PackageID
		recordExisted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
		return err
	}

	currentPackage := filepath.Join(packageDestination, packageID)
	currentInfo, err := os.Lstat(currentPackage)
	currentExisted := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
		return err
	}
	if currentExisted && previousPackageID != packageID {
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
		return fmt.Errorf("package ID already exists without a matching selection")
	}
	if currentExisted && (!currentInfo.IsDir() || currentInfo.Mode()&os.ModeSymlink != 0) {
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
		return fmt.Errorf("current package must be a non-symlink entry")
	}

	previousPackage := ""
	if previousPackageID != "" && previousPackageID != packageID {
		previousPackage = filepath.Join(packageDestination, previousPackageID)
		previousInfo, err := os.Lstat(previousPackage)
		if err == nil && (!previousInfo.IsDir() || previousInfo.Mode()&os.ModeSymlink != 0) {
			if packageParentCreated {
				_ = os.Remove(packageDestination)
			}
			return fmt.Errorf("superseded package must be a non-symlink directory")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			if packageParentCreated {
				_ = os.Remove(packageDestination)
			}
			return err
		}
	}

	backupRoot := filepath.Join(filepath.Dir(packageTemporary), ".previous")
	if err := os.Mkdir(backupRoot, 0o700); err != nil {
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
		return fmt.Errorf("create package replacement backup: %w", err)
	}
	packageBackup := filepath.Join(backupRoot, "current-package")
	previousBackup := filepath.Join(backupRoot, "previous-package")
	recordBackup := filepath.Join(backupRoot, "selection")
	currentMoved, previousMoved, recordMoved, packagePublished, recordPublished := false, false, false, false, false
	rollback := func() {
		if recordPublished {
			_ = removePath(recordDestination)
		}
		if recordMoved {
			_ = os.Rename(recordBackup, recordDestination)
		}
		if packagePublished {
			_ = removePath(currentPackage)
		}
		if currentMoved {
			_ = os.Rename(packageBackup, currentPackage)
			_ = os.Chmod(currentPackage, 0o555)
		}
		if previousMoved {
			_ = os.Rename(previousBackup, previousPackage)
			_ = os.Chmod(previousPackage, 0o555)
		}
		_ = removePath(backupRoot)
		if packageParentCreated {
			_ = os.Remove(packageDestination)
		}
	}

	if err := os.Chmod(packageTemporary, 0o700); err != nil {
		rollback()
		return fmt.Errorf("prepare package for publication: %w", err)
	}
	if currentExisted {
		if err := os.Chmod(currentPackage, 0o700); err != nil {
			rollback()
			return fmt.Errorf("prepare previous package: %w", err)
		}
		if err := os.Rename(currentPackage, packageBackup); err != nil {
			rollback()
			return fmt.Errorf("retain previous package: %w", err)
		}
		currentMoved = true
	}
	if previousPackage != "" {
		if _, err := os.Lstat(previousPackage); err == nil {
			if err := os.Chmod(previousPackage, 0o700); err != nil {
				rollback()
				return fmt.Errorf("prepare superseded package: %w", err)
			}
			if err := os.Rename(previousPackage, previousBackup); err != nil {
				rollback()
				return fmt.Errorf("retain superseded package: %w", err)
			}
			previousMoved = true
		} else if !errors.Is(err, os.ErrNotExist) {
			rollback()
			return err
		}
	}
	if err := os.Rename(packageTemporary, currentPackage); err != nil {
		rollback()
		return fmt.Errorf("publish package: %w", err)
	}
	packagePublished = true
	if err := os.Chmod(currentPackage, 0o555); err != nil {
		rollback()
		return fmt.Errorf("seal published package: %w", err)
	}

	if recordExisted {
		if err := os.Rename(recordDestination, recordBackup); err != nil {
			rollback()
			return fmt.Errorf("retain previous selection: %w", err)
		}
		recordMoved = true
	}
	if err := os.Rename(recordTemporary, recordDestination); err != nil {
		rollback()
		return fmt.Errorf("publish package selection: %w", err)
	}
	recordPublished = true
	_ = removePath(backupRoot)
	return nil
}

func removePath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if err := filepath.Walk(path, func(current string, currentInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentInfo.IsDir() && currentInfo.Mode()&os.ModeSymlink == 0 {
			return os.Chmod(current, 0o700)
		}
		return nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
