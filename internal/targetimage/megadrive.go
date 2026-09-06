package targetimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

const (
	megaDriveBundleManifest = "megadrive-rbf.toml"
	megaDriveArtifact       = "megadrive.rbf"
	megaDriveInstallPath    = "/usr/share/mister-runtime/cores/megadrive.rbf"
	megaDriveRepository     = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
	megaDriveRevision       = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
	megaDriveUpstreamPath   = "releases/MegaDrive_20260603.rbf"
	megaDriveRecipe         = "scripts/rebuild_core.py"
)

// MegaDriveBundleManifest is the closed manifest emitted by MisterOSS for a
// sealed source-built Mega Drive RBF bundle.
type MegaDriveBundleManifest struct {
	Format       int    `toml:"format"`
	ABI          string `toml:"abi"`
	System       string `toml:"system"`
	Artifact     string `toml:"artifact"`
	SHA256       string `toml:"sha256"`
	Size         int64  `toml:"size"`
	Repository   string `toml:"repository"`
	Revision     string `toml:"revision"`
	Recipe       string `toml:"recipe"`
	RecipeSHA256 string `toml:"recipe_sha256"`
	Toolchain    string `toml:"toolchain"`
	Label        string `toml:"label,omitempty"`
}

// MegaDriveSelection is the normalized provenance record consumed by native
// image construction, independent of whether the selected bytes came from a
// sealed source-built bundle or the locked upstream release.
type MegaDriveSelection struct {
	Format       int    `toml:"format"`
	Origin       string `toml:"origin"`
	ABI          string `toml:"abi"`
	System       string `toml:"system"`
	Repository   string `toml:"repository"`
	Revision     string `toml:"revision"`
	Artifact     string `toml:"artifact"`
	SHA256       string `toml:"sha256"`
	Size         int64  `toml:"size"`
	InstallPath  string `toml:"install_path"`
	Recipe       string `toml:"recipe,omitempty"`
	RecipeSHA256 string `toml:"recipe_sha256,omitempty"`
	Toolchain    string `toml:"toolchain,omitempty"`
	Label        string `toml:"label,omitempty"`
}

// MegaDriveSelectionRequest describes one explicit build-time selection.
// Empty Source is treated as source-built, matching the documented native
// image default. Callers must provide absolute cache and output paths.
type MegaDriveSelectionRequest struct {
	Source       string
	Bundle       string
	Artifact     string
	UpstreamLock string
	Cache        string
	Output       string
}

type nativeRuntimeInputLock struct {
	Format        int                   `toml:"format"`
	MisterRuntime nativeRuntimeIdentity `toml:"mister_runtime"`
	IdleRBF       nativeRBFLock         `toml:"idle_rbf"`
	MegaDriveRBF  nativeRBFLock         `toml:"megadrive_rbf"`
}

type nativeRuntimeIdentity struct {
	Commit    string `toml:"commit"`
	MountPath string `toml:"mount_path"`
}

type nativeRBFLock struct {
	Repository  string `toml:"repository"`
	Commit      string `toml:"commit"`
	Path        string `toml:"path"`
	SHA256      string `toml:"sha256"`
	Size        int64  `toml:"size"`
	InstallPath string `toml:"install_path"`
}

// PrepareMegaDriveSelection validates one source and stages the selected RBF
// plus its normalized provenance record before installing the pair. A failed
// installation restores the previous pair, so callers never accept a partial
// selection.
func PrepareMegaDriveSelection(request MegaDriveSelectionRequest) (MegaDriveSelection, error) {
	if request.Source == "" {
		request.Source = "source-built"
	}
	if request.Source != "source-built" && request.Source != "upstream" {
		return MegaDriveSelection{}, fmt.Errorf("source must be source-built or upstream")
	}
	if err := validateAbsoluteDestination(request.Cache, "cache"); err != nil {
		return MegaDriveSelection{}, err
	}
	if err := validateAbsoluteDestination(request.Output, "output"); err != nil {
		return MegaDriveSelection{}, err
	}
	if filepath.Clean(request.Output) == filepath.Join(filepath.Clean(request.Cache), megaDriveArtifact) {
		return MegaDriveSelection{}, fmt.Errorf("output must not replace the cached artifact")
	}

	var (
		selection MegaDriveSelection
		source    string
		digest    string
		size      int64
	)
	switch request.Source {
	case "source-built":
		if request.Artifact != "" {
			return MegaDriveSelection{}, fmt.Errorf("artifact is forbidden for source-built")
		}
		manifest, artifact, err := loadMegaDriveBundle(request.Bundle)
		if err != nil {
			return MegaDriveSelection{}, fmt.Errorf("source-built bundle: %w", err)
		}
		selection = MegaDriveSelection{
			Format:       1,
			Origin:       "source-built",
			ABI:          manifest.ABI,
			System:       manifest.System,
			Repository:   manifest.Repository,
			Revision:     manifest.Revision,
			Artifact:     manifest.Artifact,
			SHA256:       manifest.SHA256,
			Size:         manifest.Size,
			InstallPath:  megaDriveInstallPath,
			Recipe:       manifest.Recipe,
			RecipeSHA256: manifest.RecipeSHA256,
			Toolchain:    manifest.Toolchain,
			Label:        manifest.Label,
		}
		source = artifact
		digest = manifest.SHA256
		size = manifest.Size
	case "upstream":
		if request.Bundle != "" {
			return MegaDriveSelection{}, fmt.Errorf("bundle is forbidden for upstream")
		}
		if request.Artifact == "" || request.UpstreamLock == "" {
			return MegaDriveSelection{}, fmt.Errorf("artifact and upstream lock are required for upstream")
		}
		lock, err := loadNativeRuntimeInputLock(request.UpstreamLock)
		if err != nil {
			return MegaDriveSelection{}, fmt.Errorf("upstream lock: %w", err)
		}
		if err := validateUpstreamMegaDrive(lock.MegaDriveRBF); err != nil {
			return MegaDriveSelection{}, err
		}
		selection = MegaDriveSelection{
			Format:      1,
			Origin:      "upstream",
			ABI:         "mister",
			System:      "megadrive",
			Repository:  lock.MegaDriveRBF.Repository,
			Revision:    lock.MegaDriveRBF.Commit,
			Artifact:    lock.MegaDriveRBF.Path,
			SHA256:      lock.MegaDriveRBF.SHA256,
			Size:        lock.MegaDriveRBF.Size,
			InstallPath: lock.MegaDriveRBF.InstallPath,
		}
		source = request.Artifact
		digest = lock.MegaDriveRBF.SHA256
		size = lock.MegaDriveRBF.Size
	}

	if err := installMegaDriveSelection(source, digest, size, request.Cache, request.Output, request.Source == "source-built", selection); err != nil {
		return MegaDriveSelection{}, err
	}
	return selection, nil
}

func loadMegaDriveBundle(bundlePath string) (MegaDriveBundleManifest, string, error) {
	if err := validateBundleDirectory(bundlePath); err != nil {
		return MegaDriveBundleManifest{}, "", err
	}
	bundle := filepath.Clean(bundlePath)
	manifestPath := filepath.Join(bundle, megaDriveBundleManifest)
	artifactPath := filepath.Join(bundle, megaDriveArtifact)
	manifestFile, _, err := openRegularNoFollow(manifestPath, true)
	if err != nil {
		return MegaDriveBundleManifest{}, "", fmt.Errorf("manifest: %w", err)
	}
	var manifest MegaDriveBundleManifest
	decoder := toml.NewDecoder(manifestFile)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&manifest)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		return MegaDriveBundleManifest{}, "", fmt.Errorf("decode manifest: %w", decodeErr)
	}
	if closeErr != nil {
		return MegaDriveBundleManifest{}, "", fmt.Errorf("close manifest: %w", closeErr)
	}
	if err := validateMegaDriveBundleManifest(manifest); err != nil {
		return MegaDriveBundleManifest{}, "", err
	}
	artifactInfo, err := os.Lstat(artifactPath)
	if err != nil {
		return MegaDriveBundleManifest{}, "", fmt.Errorf("artifact: %w", err)
	}
	if artifactInfo.Mode()&os.ModeSymlink != 0 || !artifactInfo.Mode().IsRegular() {
		return MegaDriveBundleManifest{}, "", fmt.Errorf("artifact must be a regular non-symlink file")
	}
	return manifest, artifactPath, nil
}

func validateBundleDirectory(path string) error {
	return validateCoreBundleDirectory(path, "megadrive")
}

func validateCoreBundleDirectory(path, system string) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("bundle must be an absolute directory")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("bundle: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("bundle must be a regular non-symlink directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}
	manifestName, artifactName := system+"-rbf.toml", system+".rbf"
	wanted := map[string]bool{manifestName: true, artifactName: true}
	if len(entries) != len(wanted) {
		return fmt.Errorf("bundle must contain exactly %s and %s", manifestName, artifactName)
	}
	for _, entry := range entries {
		if !wanted[entry.Name()] {
			return fmt.Errorf("bundle contains unexpected entry %q", entry.Name())
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect bundle entry %q: %w", entry.Name(), err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("bundle entry %q must be a regular non-symlink file", entry.Name())
		}
	}
	return nil
}

func validateMegaDriveBundleManifest(manifest MegaDriveBundleManifest) error {
	return validateCoreManifest(manifest, "megadrive", megaDriveRecipe)
}

func validateCoreManifest(manifest MegaDriveBundleManifest, system, recipe string) error {
	if manifest.Format != 1 {
		return fmt.Errorf("manifest format must be 1")
	}
	for name, value := range map[string]string{
		"abi":           manifest.ABI,
		"system":        manifest.System,
		"artifact":      manifest.Artifact,
		"sha256":        manifest.SHA256,
		"repository":    manifest.Repository,
		"revision":      manifest.Revision,
		"recipe":        manifest.Recipe,
		"recipe_sha256": manifest.RecipeSHA256,
		"toolchain":     manifest.Toolchain,
		"label":         manifest.Label,
	} {
		if hasControl(value) {
			return fmt.Errorf("manifest %s contains a control character", name)
		}
	}
	if manifest.ABI != "mister" || manifest.System != system {
		return fmt.Errorf("manifest ABI and system must be mister/megadrive")
	}
	if manifest.Artifact != system+".rbf" || filepath.IsAbs(manifest.Artifact) || filepath.Clean(manifest.Artifact) != manifest.Artifact {
		return fmt.Errorf("manifest artifact must be the relative file %q", megaDriveArtifact)
	}
	if !validSHA256(manifest.SHA256) || manifest.Size <= 0 {
		return fmt.Errorf("manifest artifact digest and size are invalid")
	}
	parsed, err := url.Parse(manifest.Repository)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("manifest repository must be an HTTPS URL")
	}
	if !commitPattern.MatchString(manifest.Revision) {
		return fmt.Errorf("manifest revision must be a 40-character lowercase hexadecimal SHA")
	}
	if manifest.Recipe != recipe || !validSHA256(manifest.RecipeSHA256) {
		return fmt.Errorf("manifest recipe identity is invalid")
	}
	if strings.TrimSpace(manifest.Toolchain) == "" || len(manifest.Toolchain) > 256 {
		return fmt.Errorf("manifest toolchain identity is invalid")
	}
	if len(manifest.Label) > 128 {
		return fmt.Errorf("manifest label is too long")
	}
	return nil
}

func loadNativeRuntimeInputLock(path string) (nativeRuntimeInputLock, error) {
	file, _, err := openRegularNoFollow(path, false)
	if err != nil {
		return nativeRuntimeInputLock{}, err
	}
	var lock nativeRuntimeInputLock
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	decodeErr := decoder.Decode(&lock)
	closeErr := file.Close()
	if decodeErr != nil {
		return nativeRuntimeInputLock{}, fmt.Errorf("decode: %w", decodeErr)
	}
	if closeErr != nil {
		return nativeRuntimeInputLock{}, fmt.Errorf("close: %w", closeErr)
	}
	if lock.Format != 1 {
		return nativeRuntimeInputLock{}, fmt.Errorf("format must be 1")
	}
	return lock, nil
}

func validateUpstreamMegaDrive(lock nativeRBFLock) error {
	if lock.Repository != megaDriveRepository || lock.Commit != megaDriveRevision || lock.Path != megaDriveUpstreamPath {
		return fmt.Errorf("upstream Mega Drive lock does not match the authoritative release")
	}
	if !validSHA256(lock.SHA256) || lock.Size <= 0 || lock.InstallPath != megaDriveInstallPath {
		return fmt.Errorf("upstream Mega Drive lock artifact identity is invalid")
	}
	if filepath.IsAbs(lock.Path) || filepath.Clean(lock.Path) != lock.Path {
		return fmt.Errorf("upstream Mega Drive lock path must be relative and clean")
	}
	return nil
}

func installMegaDriveSelection(source, expectedDigest string, expectedSize int64, cache, output string, sealedSource bool, selection MegaDriveSelection) error {
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return fmt.Errorf("create cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create selection directory: %w", err)
	}
	sourceFile, sourceInfo, err := openRegularNoFollow(source, sealedSource)
	if err != nil {
		return fmt.Errorf("open selected artifact: %w", err)
	}
	defer sourceFile.Close()
	if sourceInfo.Size() != expectedSize {
		return fmt.Errorf("selected artifact size mismatch: got %d, want %d", sourceInfo.Size(), expectedSize)
	}

	cachePath := filepath.Join(cache, selection.System+".rbf")
	temporary, err := os.CreateTemp(cache, ".megadrive.rbf.*")
	if err != nil {
		return fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), sourceFile)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("copy selected artifact: %w", err)
	}
	if written != expectedSize {
		_ = temporary.Close()
		return fmt.Errorf("selected artifact size mismatch while copying: got %d, want %d", written, expectedSize)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if actualDigest != expectedDigest {
		_ = temporary.Close()
		return fmt.Errorf("selected artifact SHA-256 mismatch: got %s, want %s", actualDigest, expectedDigest)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary artifact: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o444); err != nil {
		return fmt.Errorf("seal temporary artifact: %w", err)
	}
	selectionTemporaryPath, err := stageMegaDriveSelection(output, selection)
	if err != nil {
		return err
	}
	if err := installMegaDrivePair(
		temporaryPath,
		selectionTemporaryPath,
		cachePath,
		output,
		expectedDigest,
		expectedSize,
	); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

type megaDriveInstallBackup struct {
	target string
	backup string
	moved  bool
}

func prepareMegaDriveInstallBackup(path string) (megaDriveInstallBackup, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return megaDriveInstallBackup{target: path}, nil
	}
	if err != nil {
		return megaDriveInstallBackup{}, err
	}
	if info.IsDir() {
		return megaDriveInstallBackup{}, fmt.Errorf("install target is a directory: %s", path)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".megadrive.previous.*")
	if err != nil {
		return megaDriveInstallBackup{}, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return megaDriveInstallBackup{}, err
	}
	return megaDriveInstallBackup{target: path, backup: temporaryPath}, nil
}

func (backup *megaDriveInstallBackup) move() error {
	if backup.backup == "" {
		return nil
	}
	if err := os.Rename(backup.target, backup.backup); err != nil {
		return err
	}
	backup.moved = true
	return nil
}

func (backup *megaDriveInstallBackup) restore() error {
	if backup.backup == "" {
		return nil
	}
	if !backup.moved {
		err := os.Remove(backup.backup)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(backup.target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(backup.backup, backup.target); err != nil {
		return err
	}
	backup.backup = ""
	backup.moved = false
	return nil
}

func (backup *megaDriveInstallBackup) discard() {
	if backup.backup != "" {
		_ = os.Remove(backup.backup)
	}
}

func rollbackMegaDrivePair(
	artifactPath, selectionPath string,
	artifactInstalled, selectionInstalled bool,
	artifactBackup, selectionBackup *megaDriveInstallBackup,
) error {
	var rollbackErrs []error
	if selectionInstalled {
		if err := os.Remove(selectionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("remove new selection: %w", err))
		}
	}
	if artifactInstalled {
		if err := os.Remove(artifactPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("remove new artifact: %w", err))
		}
	}
	if err := selectionBackup.restore(); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore selection: %w", err))
	}
	if err := artifactBackup.restore(); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("restore artifact: %w", err))
	}
	return errors.Join(rollbackErrs...)
}

func installMegaDrivePair(
	artifactTemporaryPath, selectionTemporaryPath, artifactPath, selectionPath string,
	expectedDigest string, expectedSize int64,
) error {
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(artifactTemporaryPath)
			_ = os.Remove(selectionTemporaryPath)
		}
	}()
	artifactBackup, err := prepareMegaDriveInstallBackup(artifactPath)
	if err != nil {
		_ = os.Remove(selectionTemporaryPath)
		return fmt.Errorf("prepare artifact replacement: %w", err)
	}
	selectionBackup, err := prepareMegaDriveInstallBackup(selectionPath)
	if err != nil {
		artifactBackup.discard()
		_ = os.Remove(selectionTemporaryPath)
		return fmt.Errorf("prepare selection replacement: %w", err)
	}
	artifactInstalled := false
	selectionInstalled := false
	rollback := func(cause error) error {
		if rollbackErr := rollbackMegaDrivePair(
			artifactPath,
			selectionPath,
			artifactInstalled,
			selectionInstalled,
			&artifactBackup,
			&selectionBackup,
		); rollbackErr != nil {
			return errors.Join(cause, rollbackErr)
		}
		return cause
	}
	if err := artifactBackup.move(); err != nil {
		artifactBackup.discard()
		selectionBackup.discard()
		_ = os.Remove(artifactTemporaryPath)
		_ = os.Remove(selectionTemporaryPath)
		return fmt.Errorf("backup selected artifact: %w", err)
	}
	if err := selectionBackup.move(); err != nil {
		return rollback(fmt.Errorf("backup selection: %w", err))
	}
	if err := os.Rename(artifactTemporaryPath, artifactPath); err != nil {
		return rollback(fmt.Errorf("install selected artifact: %w", err))
	}
	artifactInstalled = true
	if err := os.Rename(selectionTemporaryPath, selectionPath); err != nil {
		return rollback(fmt.Errorf("install Mega Drive selection: %w", err))
	}
	selectionInstalled = true
	if err := verifySealedFile(artifactPath, expectedDigest, expectedSize); err != nil {
		return rollback(fmt.Errorf("verify installed artifact: %w", err))
	}
	artifactBackup.discard()
	selectionBackup.discard()
	removeTemporary = false
	return nil
}

func stageMegaDriveSelection(path string, selection MegaDriveSelection) (string, error) {
	if err := validateMegaDriveSelection(selection); err != nil {
		return "", err
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(selection); err != nil {
		return "", fmt.Errorf("encode Mega Drive selection: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".megadrive.selection.*")
	if err != nil {
		return "", fmt.Errorf("create temporary selection: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(encoded.Bytes()); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write temporary selection: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync temporary selection: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary selection: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o444); err != nil {
		return "", fmt.Errorf("seal temporary selection: %w", err)
	}
	removeTemporary = false
	return temporaryPath, nil
}

func validateMegaDriveSelection(selection MegaDriveSelection) error {
	if selection.System == "pong" || selection.System == "snes" {
		if selection.Origin != "source-built" || selection.InstallPath != "/usr/share/mister-runtime/cores/"+selection.System+".rbf" {
			return fmt.Errorf("invalid additional core selection")
		}
		return validateExtraCoreManifest(MegaDriveBundleManifest{Format: selection.Format, ABI: selection.ABI, System: selection.System, Artifact: selection.Artifact, SHA256: selection.SHA256, Size: selection.Size, Repository: selection.Repository, Revision: selection.Revision, Recipe: selection.Recipe, RecipeSHA256: selection.RecipeSHA256, Toolchain: selection.Toolchain, Label: selection.Label}, selection.System)
	}

	if selection.Format != 1 || (selection.Origin != "source-built" && selection.Origin != "upstream") ||
		selection.ABI != "mister" || selection.System != "megadrive" || selection.InstallPath != megaDriveInstallPath ||
		!validSHA256(selection.SHA256) || selection.Size <= 0 || hasControl(selection.Repository) || hasControl(selection.Revision) ||
		hasControl(selection.Artifact) || hasControl(selection.Recipe) || hasControl(selection.Toolchain) || hasControl(selection.Label) {
		return fmt.Errorf("invalid Mega Drive selection")
	}
	if selection.Origin == "source-built" {
		if selection.Artifact != megaDriveArtifact || selection.Recipe != megaDriveRecipe || !validSHA256(selection.RecipeSHA256) || strings.TrimSpace(selection.Toolchain) == "" {
			return fmt.Errorf("invalid source-built Mega Drive selection")
		}
	} else if selection.Recipe != "" || selection.RecipeSHA256 != "" || selection.Toolchain != "" || selection.Label != "" {
		return fmt.Errorf("upstream Mega Drive selection contains source-built provenance")
	}
	return nil
}

func openRegularNoFollow(path string, requireSealed bool) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("not a regular non-symlink file: %s", path)
	}
	if requireSealed && info.Mode().Perm()&0o222 != 0 {
		return nil, nil, fmt.Errorf("file is writable: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, nil, fmt.Errorf("open file: %s", path)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() || (requireSealed && openedInfo.Mode().Perm()&0o222 != 0) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file changed to an invalid type or mode: %s", path)
	}
	return file, openedInfo, nil
}

func verifySealedFile(path, expectedDigest string, expectedSize int64) error {
	file, info, err := openRegularNoFollow(path, true)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Size() != expectedSize {
		return fmt.Errorf("size mismatch: got %d, want %d", info.Size(), expectedSize)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedDigest {
		return fmt.Errorf("SHA-256 mismatch: got %s, want %s", actual, expectedDigest)
	}
	return nil
}

func validateAbsoluteDestination(path, name string) error {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("%s must be an absolute path", name)
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
