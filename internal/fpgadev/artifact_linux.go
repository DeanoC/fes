//go:build linux

package fpgadev

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const artifactOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK

// dispatchArtifactLeaf is deliberately fixed and hidden. It is published as
// a hard link to the retained descriptor immediately before Main receives the
// load_core command, so Main opens an ordinary named RBF while the bytes stay
// bound to the descriptor admitted by the artifact validator.
const dispatchArtifactLeaf = ".fogcast-load.rbf"

func resultPersistenceSupported() bool { return true }

// Bind opens the staging directory without following its final symlink, then
// opens the fixed top.rbf member below that descriptor with openat2's strict
// no-symlink/beneath resolution. The directory descriptor remains owned by
// the returned binding.
func (a ArtifactAccess) Bind(manifest Manifest, staging string) (ArtifactBinding, error) {
	if err := validateManifestForBinding(manifest); err != nil {
		return ArtifactBinding{}, fmt.Errorf("validate artifact manifest: %w", err)
	}
	if err := validateArtifactPath(staging); err != nil {
		return ArtifactBinding{}, err
	}
	directoryFD, err := openArtifactDirectory(staging, a.BeforeDirectoryOpen)
	if err != nil {
		return ArtifactBinding{}, fmt.Errorf("open staging directory: %w", err)
	}
	directory := os.NewFile(uintptr(directoryFD), staging)
	if directory == nil {
		_ = unix.Close(directoryFD)
		return ArtifactBinding{}, errors.New("open staging directory returned no descriptor")
	}
	keepDirectory := false
	defer func() {
		if !keepDirectory {
			_ = directory.Close()
		}
	}()

	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil {
		return ArtifactBinding{}, fmt.Errorf("stat staging directory: %w", err)
	}
	if err := validateDirectoryStat(&directoryStat, a.ExpectedUID); err != nil {
		return ArtifactBinding{}, err
	}

	artifactPath := filepath.Join(staging, manifest.ArtifactFilename)
	firstArtifact, firstMetadata, err := openAndInspectArtifact(directoryFD, manifest, a.ExpectedUID, artifactPath)
	if err != nil {
		return ArtifactBinding{}, err
	}
	secondArtifact, secondMetadata, err := openAndInspectArtifact(directoryFD, manifest, a.ExpectedUID, artifactPath)
	if err != nil {
		_ = firstArtifact.Close()
		return ArtifactBinding{}, err
	}
	if !sameArtifactMetadata(firstMetadata, secondMetadata) {
		_ = firstArtifact.Close()
		_ = secondArtifact.Close()
		return ArtifactBinding{}, errors.New("artifact changed during immediate revalidation")
	}
	if err := firstArtifact.Close(); err != nil {
		_ = secondArtifact.Close()
		return ArtifactBinding{}, fmt.Errorf("close initial artifact descriptor: %w", err)
	}
	secondMetadata.Path = artifactPath
	keepDirectory = true
	return ArtifactBinding{
		state: &artifactBindingState{
			dir:            directory,
			artifact:       secondArtifact,
			metadata:       secondMetadata,
			manifest:       manifest,
			expectedUID:    a.ExpectedUID,
			dispatchVerify: a.BeforeDispatchPathVerify,
		},
	}, nil
}

// Revalidate opens a temporary descriptor beneath the retained directory,
// requires its complete device/inode/size/hash identity to remain unchanged,
// and closes only that temporary descriptor. The retained descriptor and any
// published named dispatch capability remain stable until shared-state Close.
func (b *ArtifactBinding) Revalidate() error {
	if b == nil || b.state == nil {
		return errors.New("nil artifact binding")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.closed || b.state.dir == nil || b.state.artifact == nil {
		return errors.New("artifact binding is closed")
	}
	expectedNlink := uint64(1)
	if b.state.dispatchLeaf != "" {
		expectedNlink = 2
	}
	artifact, metadata, err := openAndInspectArtifactWithNlink(int(b.state.dir.Fd()), b.state.manifest, b.state.expectedUID, b.state.metadata.Path, expectedNlink)
	if err != nil {
		return fmt.Errorf("reopen artifact: %w", err)
	}
	metadata.Path = b.state.metadata.Path
	if !sameArtifactMetadata(metadata, b.state.metadata) {
		_ = artifact.Close()
		return errors.New("artifact binding changed")
	}
	if err := artifact.Close(); err != nil {
		return fmt.Errorf("close revalidation descriptor: %w", err)
	}
	return nil
}

// dispatchPathLocked publishes one descriptor-anchored named hard link. The
// caller holds state.mu. A collision, source replacement, link identity
// mismatch, or directory-sync failure leaves the binding unpublished and
// fails closed.
func (b *ArtifactBinding) dispatchPathLocked() (string, error) {
	if b.state.dir == nil || b.state.artifact == nil {
		return "", errors.New("artifact binding is closed")
	}
	if b.state.dispatchPath != "" {
		if err := verifyDispatchArtifactLocked(b); err != nil {
			return "", err
		}
		return b.state.dispatchPath, nil
	}
	directoryFD := int(b.state.dir.Fd())
	artifactPath := b.state.metadata.Path
	artifact, metadata, err := openAndInspectArtifactWithNlink(directoryFD, b.state.manifest, b.state.expectedUID, artifactPath, 1)
	if err != nil {
		return "", fmt.Errorf("validate dispatch source: %w", err)
	}
	if closeErr := artifact.Close(); closeErr != nil {
		return "", fmt.Errorf("close dispatch source: %w", closeErr)
	}
	if !sameArtifactMetadata(metadata, b.state.metadata) {
		return "", errors.New("dispatch source differs from retained artifact")
	}
	if err := linkRetainedArtifact(int(b.state.artifact.Fd()), directoryFD, dispatchArtifactLeaf); err != nil {
		return "", fmt.Errorf("publish dispatch capability: %w", err)
	}
	b.state.dispatchLeaf = dispatchArtifactLeaf
	b.state.dispatchPath = filepath.Join(filepath.Dir(artifactPath), dispatchArtifactLeaf)
	cleanup := func(cause error) (string, error) {
		return "", errors.Join(cause, b.state.releaseDispatchPathLocked())
	}
	if err := unix.Fsync(directoryFD); err != nil {
		return cleanup(fmt.Errorf("sync dispatch capability directory: %w", err))
	}
	if err := verifyDispatchArtifactLocked(b); err != nil {
		return cleanup(err)
	}
	if b.state.dispatchVerify != nil {
		b.state.dispatchVerify()
	}
	if err := verifyAbsoluteDispatchPathLocked(b); err != nil {
		return cleanup(err)
	}
	return b.state.dispatchPath, nil
}

func linkRetainedArtifact(artifactFD, directoryFD int, leaf string) error {
	if err := unix.Linkat(artifactFD, "", directoryFD, leaf, unix.AT_EMPTY_PATH); err == nil {
		return nil
	} else if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	// AT_EMPTY_PATH requires CAP_DAC_READ_SEARCH even when the caller owns the
	// file. Following this process's own retained descriptor creates the same
	// exact-inode hard link without imposing that capability on ordinary test
	// and development environments. Main never receives this procfs path.
	selfDescriptor := fmt.Sprintf("/proc/self/fd/%d", artifactFD)
	return unix.Linkat(unix.AT_FDCWD, selfDescriptor, directoryFD, leaf, unix.AT_SYMLINK_FOLLOW)
}

func verifyDispatchArtifactLocked(b *ArtifactBinding) error {
	if b.state.dispatchLeaf == "" || b.state.dispatchPath == "" || b.state.dir == nil || b.state.artifact == nil {
		return errors.New("dispatch capability is unavailable")
	}
	directoryFD := int(b.state.dir.Fd())
	capability, metadata, err := openAndInspectArtifactWithNlink(directoryFD, b.state.manifest, b.state.expectedUID, b.state.dispatchLeaf, 2)
	if err != nil {
		return fmt.Errorf("verify dispatch capability: %w", err)
	}
	if closeErr := capability.Close(); closeErr != nil {
		return fmt.Errorf("close dispatch capability: %w", closeErr)
	}
	metadata.Path = b.state.metadata.Path
	if !sameArtifactMetadata(metadata, b.state.metadata) {
		return errors.New("dispatch capability differs from retained artifact")
	}
	source, sourceMetadata, err := openAndInspectArtifactWithNlink(directoryFD, b.state.manifest, b.state.expectedUID, b.state.metadata.Path, 2)
	if err != nil {
		return fmt.Errorf("verify dispatch source: %w", err)
	}
	if closeErr := source.Close(); closeErr != nil {
		return fmt.Errorf("close dispatch source: %w", closeErr)
	}
	if !sameArtifactMetadata(sourceMetadata, b.state.metadata) {
		return errors.New("dispatch source changed after capability publication")
	}
	return nil
}

func verifyAbsoluteDispatchPathLocked(b *ArtifactBinding) error {
	staging := filepath.Dir(b.state.metadata.Path)
	reopenedFD, err := openArtifactDirectory(staging, nil)
	if err != nil {
		return fmt.Errorf("reopen dispatch staging path: %w", err)
	}
	reopened := os.NewFile(uintptr(reopenedFD), staging)
	if reopened == nil {
		_ = unix.Close(reopenedFD)
		return errors.New("reopen dispatch staging path returned no descriptor")
	}
	defer reopened.Close()

	var retainedStat, reopenedStat unix.Stat_t
	if err := unix.Fstat(int(b.state.dir.Fd()), &retainedStat); err != nil {
		return fmt.Errorf("stat retained dispatch directory: %w", err)
	}
	if err := unix.Fstat(reopenedFD, &reopenedStat); err != nil {
		return fmt.Errorf("stat reopened dispatch directory: %w", err)
	}
	if err := validateDirectoryStat(&reopenedStat, b.state.expectedUID); err != nil {
		return fmt.Errorf("validate reopened dispatch directory: %w", err)
	}
	if retainedStat.Dev != reopenedStat.Dev || retainedStat.Ino != reopenedStat.Ino {
		return errors.New("dispatch staging path differs from retained directory")
	}

	capability, metadata, err := openAndInspectArtifactWithNlink(reopenedFD, b.state.manifest, b.state.expectedUID, b.state.dispatchLeaf, 2)
	if err != nil {
		return fmt.Errorf("open absolute dispatch capability: %w", err)
	}
	if closeErr := capability.Close(); closeErr != nil {
		return fmt.Errorf("close absolute dispatch capability: %w", closeErr)
	}
	metadata.Path = b.state.metadata.Path
	if !sameArtifactMetadata(metadata, b.state.metadata) {
		return errors.New("absolute dispatch capability differs from retained artifact")
	}
	return nil
}

func (s *artifactBindingState) releaseDispatchPathLocked() error {
	if s.dispatchLeaf == "" {
		s.dispatchPath = ""
		return nil
	}
	if s.dir == nil {
		return errors.New("dispatch capability directory is unavailable")
	}
	directoryFD := int(s.dir.Fd())
	leaf := s.dispatchLeaf
	if err := unix.Unlinkat(directoryFD, leaf, 0); err != nil {
		return fmt.Errorf("remove dispatch capability: %w", err)
	}
	s.dispatchLeaf = ""
	s.dispatchPath = ""
	if err := unix.Fsync(directoryFD); err != nil {
		return fmt.Errorf("sync dispatch capability removal: %w", err)
	}
	return nil
}

// OpenArtifact returns a caller-owned duplicate of the retained, revalidated
// artifact descriptor. It never re-resolves the mutable staging pathname, so
// the descriptor and the dispatch capability identify the same bytes held by
// this binding until Close.
func (b *ArtifactBinding) OpenArtifact() (*os.File, error) {
	if b == nil || b.state == nil {
		return nil, errors.New("nil artifact binding")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.closed || b.state.dir == nil || b.state.artifact == nil {
		return nil, errors.New("artifact binding is closed")
	}
	if _, err := b.state.artifact.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind retained artifact descriptor: %w", err)
	}
	fd, err := unix.FcntlInt(b.state.artifact.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate retained artifact descriptor: %w", err)
	}
	artifact := os.NewFile(uintptr(fd), b.state.metadata.Path)
	if artifact == nil {
		_ = unix.Close(fd)
		return nil, errors.New("duplicate retained artifact descriptor returned no file")
	}
	return artifact, nil
}

func openAndInspectArtifact(directoryFD int, manifest Manifest, expectedUID uint32, path string) (*os.File, ArtifactMetadata, error) {
	return openAndInspectArtifactWithNlink(directoryFD, manifest, expectedUID, path, 1)
}

func openAndInspectArtifactWithNlink(directoryFD int, manifest Manifest, expectedUID uint32, path string, expectedNlink uint64) (*os.File, ArtifactMetadata, error) {
	name := manifest.ArtifactFilename
	if filepath.Base(path) != name {
		name = filepath.Base(path)
	}
	artifactFD, err := openArtifactMemberAt(directoryFD, name)
	if err != nil {
		return nil, ArtifactMetadata{}, err
	}
	artifact := os.NewFile(uintptr(artifactFD), path)
	if artifact == nil {
		_ = unix.Close(artifactFD)
		return nil, ArtifactMetadata{}, errors.New("open artifact returned no descriptor")
	}
	metadata, err := inspectArtifactFileWithNlink(artifact, manifest, expectedUID, expectedNlink)
	if err != nil {
		_ = artifact.Close()
		return nil, ArtifactMetadata{}, err
	}
	if _, err := artifact.Seek(0, io.SeekStart); err != nil {
		_ = artifact.Close()
		return nil, ArtifactMetadata{}, fmt.Errorf("rewind artifact descriptor: %w", err)
	}
	metadata.Path = path
	return artifact, metadata, nil
}

func sameArtifactMetadata(left, right ArtifactMetadata) bool {
	return left.Device == right.Device && left.Inode == right.Inode && left.Size == right.Size && left.SHA256 == right.SHA256 && left.Path == right.Path
}

func openArtifactAt(directoryFD int, name string) (int, error) {
	if name != ManifestArtifact {
		return -1, errors.New("artifact filename is not the fixed top.rbf")
	}
	return openArtifactMemberAt(directoryFD, name)
}

func openArtifactMemberAt(directoryFD int, name string) (int, error) {
	how := &unix.OpenHow{
		Flags:   uint64(artifactOpenFlags),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(directoryFD, name, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return -1, fmt.Errorf("openat2 artifact resolution unsupported: %w", ErrUnsupported)
		}
		return -1, fmt.Errorf("open artifact member with openat2: %w", err)
	}
	return fd, nil
}

// verifyArtifactBundleAtPath opens the staging directory once and delegates
// every bundle-member read to that retained descriptor.  The path is used
// only to establish the initial directory capability; sibling members are
// never reopened through the mutable pathname.
func verifyArtifactBundleAtPath(staging string, expectedUID uint32) (bundle ArtifactBundle, err error) {
	if err := validateArtifactPath(staging); err != nil {
		return ArtifactBundle{}, err
	}
	directoryFD, err := openArtifactDirectory(staging, nil)
	if err != nil {
		return ArtifactBundle{}, fmt.Errorf("open bundle staging directory: %w", err)
	}
	directory := os.NewFile(uintptr(directoryFD), staging)
	if directory == nil {
		_ = unix.Close(directoryFD)
		return ArtifactBundle{}, errors.New("open bundle staging directory returned no descriptor")
	}
	defer func() {
		err = errors.Join(err, directory.Close())
	}()
	return verifyArtifactBundleAtDirectory(directory, staging, expectedUID, nil, nil, nil)
}

// verifyArtifactBundleForBinding consumes bundle members relative to the
// directory descriptor and retained RBF descriptor owned by the binding.
// Holding the binding lock across this bounded verification prevents Close
// from invalidating either descriptor while evidence is being consumed.
func verifyArtifactBundleForBinding(binding *ArtifactBinding) (ArtifactBundle, error) {
	if binding == nil || binding.state == nil {
		return ArtifactBundle{}, errors.New("nil artifact binding")
	}
	binding.state.mu.Lock()
	defer binding.state.mu.Unlock()
	if binding.state.closed || binding.state.dir == nil || binding.state.artifact == nil {
		return ArtifactBundle{}, errors.New("artifact binding is closed")
	}
	return verifyArtifactBundleAtDirectory(
		binding.state.dir,
		filepath.Dir(binding.state.metadata.Path),
		binding.state.expectedUID,
		&binding.state.metadata,
		&binding.state.manifest,
		binding.state.artifact,
	)
}

func verifyArtifactBundleAtDirectory(directory *os.File, staging string, expectedUID uint32, boundMetadata *ArtifactMetadata, boundManifest *Manifest, boundArtifact *os.File) (ArtifactBundle, error) {
	var zero ArtifactBundle
	if directory == nil {
		return zero, errors.New("bundle staging directory descriptor is unavailable")
	}
	directoryFD := int(directory.Fd())
	var directoryStat unix.Stat_t
	if err := unix.Fstat(directoryFD, &directoryStat); err != nil {
		return zero, fmt.Errorf("stat bundle staging directory: %w", err)
	}
	if err := validateDirectoryStat(&directoryStat, expectedUID); err != nil {
		return zero, err
	}
	if err := validateArtifactBundleDirectory(directory); err != nil {
		return zero, err
	}

	checksumRaw, _, err := readArtifactBundleMemberAt(directoryFD, ManifestChecksums, expectedUID, maxBundleChecksumBytes)
	if err != nil {
		return zero, err
	}
	checksums, err := ParseBundleSHA256(checksumRaw)
	if err != nil {
		return zero, err
	}
	contents := make(map[string][]byte, len(requiredBundleMembers))
	memberStats := make(map[string]unix.Stat_t, len(requiredBundleMembers))
	for _, name := range requiredBundleMembers {
		member, stat, err := readArtifactBundleMemberAt(directoryFD, name, expectedUID, bundleMemberMaxBytes(name))
		if err != nil {
			return zero, err
		}
		contents[name] = member
		memberStats[name] = stat
		sum := sha256.Sum256(member)
		if hex.EncodeToString(sum[:]) != checksums[name] {
			return zero, fmt.Errorf("bundle checksum mismatch for %s", name)
		}
	}

	manifest, err := ParseManifest(contents["manifest.json"])
	if err != nil {
		return zero, fmt.Errorf("manifest: %w", err)
	}
	if boundManifest != nil && manifest != *boundManifest {
		return zero, errors.New("resource bundle manifest differs from bound manifest")
	}
	evidence, err := ParseResourceEvidenceV2(contents[ManifestEvidence])
	if err != nil {
		return zero, fmt.Errorf("resource evidence: %w", err)
	}
	if err := evidence.MatchesManifest(manifest); err != nil {
		return zero, err
	}

	artifactPath := filepath.Join(staging, ManifestArtifact)
	var openedMetadata ArtifactMetadata
	if boundArtifact != nil {
		openedMetadata, err = inspectArtifactFile(boundArtifact, manifest, expectedUID)
		if err != nil {
			return zero, fmt.Errorf("inspect retained artifact descriptor: %w", err)
		}
		openedMetadata.Path = artifactPath
		if boundMetadata == nil || !sameArtifactMetadata(openedMetadata, *boundMetadata) {
			return zero, errors.New("retained artifact descriptor differs from binding")
		}
	} else {
		firstArtifact, firstMetadata, openErr := openAndInspectArtifact(directoryFD, manifest, expectedUID, artifactPath)
		if openErr != nil {
			return zero, openErr
		}
		secondArtifact, secondMetadata, openErr := openAndInspectArtifact(directoryFD, manifest, expectedUID, artifactPath)
		if openErr != nil {
			return zero, errors.Join(openErr, firstArtifact.Close())
		}
		if !sameArtifactMetadata(firstMetadata, secondMetadata) {
			return zero, errors.Join(errors.New("artifact changed during bundle verification"), firstArtifact.Close(), secondArtifact.Close())
		}
		if closeErr := firstArtifact.Close(); closeErr != nil {
			return zero, errors.Join(fmt.Errorf("close initial artifact descriptor: %w", closeErr), secondArtifact.Close())
		}
		if closeErr := secondArtifact.Close(); closeErr != nil {
			return zero, fmt.Errorf("close artifact descriptor: %w", closeErr)
		}
		openedMetadata = secondMetadata
	}

	topStat := memberStats[ManifestArtifact]
	if uint64(topStat.Dev) != openedMetadata.Device || uint64(topStat.Ino) != openedMetadata.Inode || topStat.Size != openedMetadata.Size {
		return zero, errors.New("opened RBF descriptor differs from bundle member")
	}
	if openedMetadata.SHA256 != manifest.ArtifactSHA256 || openedMetadata.SHA256 != evidence.ArtifactSHA256 || uint64(openedMetadata.Size) != manifest.ArtifactSize {
		return zero, errors.New("opened RBF metadata does not match manifest/evidence")
	}
	return ArtifactBundle{Manifest: manifest, Evidence: evidence, Artifact: openedMetadata, BundleSHA256: checksums}, nil
}

func validateArtifactBundleDirectory(directory *os.File) (err error) {
	if directory == nil {
		return errors.New("bundle staging directory descriptor is unavailable")
	}
	fd, openErr := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if openErr != nil {
		return fmt.Errorf("open bundle staging directory for enumeration: %w", openErr)
	}
	enumerator := os.NewFile(uintptr(fd), "bundle-staging-enumerator")
	if enumerator == nil {
		_ = unix.Close(fd)
		return errors.New("open bundle staging directory enumeration returned no descriptor")
	}
	defer func() {
		err = errors.Join(err, enumerator.Close())
	}()
	expected := make(map[string]struct{}, len(requiredBundleMembers)+1)
	for _, name := range requiredBundleMembers {
		expected[name] = struct{}{}
	}
	expected[ManifestChecksums] = struct{}{}
	entries, err := enumerator.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read bundle staging directory: %w", err)
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("bundle staging directory contains %d members; want %d", len(entries), len(expected))
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return fmt.Errorf("bundle staging directory contains unlisted member %q", entry.Name())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("bundle staging member %q is a symlink", entry.Name())
		}
	}
	return nil
}

func bundleMemberMaxBytes(name string) int64 {
	switch name {
	case ManifestChecksums:
		return maxBundleChecksumBytes
	case ManifestEvidence:
		return MaxResourceEvidenceBytes
	case ManifestArtifact:
		return int64(MaxRBFSize)
	case "manifest.json":
		return maxStagedManifestBytes
	default:
		return 0
	}
}

func readArtifactBundleMemberAt(directoryFD int, name string, expectedUID uint32, maxBytes int64) (raw []byte, stat unix.Stat_t, err error) {
	var zero unix.Stat_t
	if bundleMemberMaxBytes(name) == 0 || maxBytes <= 0 {
		return nil, zero, errors.New("invalid bundle member")
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(directoryFD, name, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return nil, zero, fmt.Errorf("open bundle member resolution unsupported: %w", ErrUnsupported)
		}
		return nil, zero, fmt.Errorf("open bundle member %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, zero, errors.New("open bundle member returned no descriptor")
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
	}()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, zero, fmt.Errorf("stat bundle member %s: %w", name, err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, zero, fmt.Errorf("bundle member %s is not a regular file", name)
	}
	if uint32(before.Uid) != expectedUID {
		return nil, zero, fmt.Errorf("bundle member %s owner is not authorized", name)
	}
	if before.Mode&0o7777 != 0o600 {
		return nil, zero, fmt.Errorf("bundle member %s mode must be 0600", name)
	}
	if before.Nlink != 1 {
		return nil, zero, fmt.Errorf("bundle member %s link count must be one", name)
	}
	if before.Size <= 0 || before.Size > maxBytes {
		return nil, zero, fmt.Errorf("bundle member %s size is outside its bound", name)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, zero, fmt.Errorf("seek bundle member %s: %w", name, err)
	}
	var readErr error
	raw, readErr = io.ReadAll(io.LimitReader(file, maxBytes+1))
	if readErr != nil {
		return nil, zero, fmt.Errorf("read bundle member %s: %w", name, readErr)
	}
	if int64(len(raw)) != before.Size || int64(len(raw)) > maxBytes {
		return nil, zero, fmt.Errorf("bundle member %s changed size while reading", name)
	}
	var after unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, zero, fmt.Errorf("restat bundle member %s: %w", name, err)
	}
	if !sameBundleMemberStat(before, after) {
		return nil, zero, fmt.Errorf("bundle member %s changed while reading", name)
	}
	closeErr := file.Close()
	closed = true
	if closeErr != nil {
		return nil, zero, fmt.Errorf("close bundle member %s: %w", name, closeErr)
	}
	return raw, before, nil
}

func sameBundleMemberStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Size == right.Size && left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink && left.Rdev == right.Rdev && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

// openArtifactDirectory resolves the absolute staging path below a trusted
// root descriptor. The single openat2 operation applies both beneath and
// no-symlink resolution to every ancestor and the final directory component.
func openArtifactDirectory(path string, beforeOpen func()) (int, error) {
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open trusted root: %w", err)
	}
	defer unix.Close(rootFD)
	if beforeOpen != nil {
		beforeOpen()
	}
	relative := strings.TrimPrefix(path, string(filepath.Separator))
	how := &unix.OpenHow{
		Flags:   uint64(artifactOpenFlags | unix.O_DIRECTORY),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return -1, fmt.Errorf("openat2 staging resolution unsupported: %w", ErrUnsupported)
		}
		return -1, fmt.Errorf("open staging directory with openat2: %w", err)
	}
	return fd, nil
}

func validateDirectoryStat(stat *unix.Stat_t, expectedUID uint32) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("staging path is not a directory")
	}
	if uint32(stat.Uid) != expectedUID {
		return errors.New("staging directory owner is not authorized")
	}
	if stat.Mode&0o7777 != 0o700 {
		return errors.New("staging directory mode must be 0700")
	}
	if stat.Nlink != 2 {
		return errors.New("staging directory link count must be exactly two")
	}
	return nil
}

func inspectArtifactFile(file *os.File, manifest Manifest, expectedUID uint32) (ArtifactMetadata, error) {
	return inspectArtifactFileWithNlink(file, manifest, expectedUID, 1)
}

func inspectArtifactFileWithNlink(file *os.File, manifest Manifest, expectedUID uint32, expectedNlink uint64) (ArtifactMetadata, error) {
	var before unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return ArtifactMetadata{}, fmt.Errorf("stat artifact: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return ArtifactMetadata{}, errors.New("artifact is not a regular file")
	}
	if uint32(before.Uid) != expectedUID {
		return ArtifactMetadata{}, errors.New("artifact owner is not authorized")
	}
	if before.Mode&0o7777 != 0o600 {
		return ArtifactMetadata{}, errors.New("artifact mode must be 0600")
	}
	if uint64(before.Nlink) != expectedNlink {
		return ArtifactMetadata{}, fmt.Errorf("artifact link count must be %d", expectedNlink)
	}
	if before.Size <= 0 || uint64(before.Size) > MaxRBFSize {
		return ArtifactMetadata{}, fmt.Errorf("artifact size must be between 1 and %d", MaxRBFSize)
	}
	if uint64(before.Size) != manifest.ArtifactSize {
		return ArtifactMetadata{}, errors.New("artifact size differs from manifest")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ArtifactMetadata{}, fmt.Errorf("seek artifact: %w", err)
	}
	hasher := sha256.New()
	count, err := io.CopyN(hasher, file, int64(MaxRBFSize)+1)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ArtifactMetadata{}, fmt.Errorf("hash artifact: %w", err)
	}
	if count > int64(MaxRBFSize) {
		return ArtifactMetadata{}, fmt.Errorf("artifact exceeds maximum RBF size %d", MaxRBFSize)
	}
	after := before
	if err := unix.Fstat(int(file.Fd()), &after); err != nil {
		return ArtifactMetadata{}, fmt.Errorf("restat artifact: %w", err)
	}
	if after.Dev != before.Dev || after.Ino != before.Ino || after.Size != before.Size {
		return ArtifactMetadata{}, errors.New("artifact changed while hashing")
	}
	digest := fmt.Sprintf("%x", hasher.Sum(nil))
	if digest != manifest.ArtifactSHA256 {
		return ArtifactMetadata{}, errors.New("artifact hash differs from manifest")
	}
	return ArtifactMetadata{Device: uint64(before.Dev), Inode: uint64(before.Ino), Size: before.Size, SHA256: digest}, nil
}

// resultDirectoryHandle is deliberately descriptor-only: once a result
// operation has opened and validated the protected directory, all result
// entries are reached relative to this descriptor rather than by resolving
// the configured path again.
func openResultDirectory(path string, expectedUID uint32, beforeOpen ...func()) (*resultDirectoryHandle, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) {
		return nil, errors.New("result directory must be an absolute canonical non-root path")
	}
	var hook func()
	if len(beforeOpen) != 0 {
		hook = beforeOpen[0]
	}
	fd, err := openDirectoryBelowTrustedRoot(path, unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, hook)
	if err != nil {
		return nil, fmt.Errorf("open result directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), path)
	if directory == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open result directory returned no descriptor")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = directory.Close()
		return nil, fmt.Errorf("stat result directory: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = directory.Close()
		return nil, errors.New("result path is not a directory")
	}
	if uint32(stat.Uid) != expectedUID {
		_ = directory.Close()
		return nil, errors.New("result directory owner is not authorized")
	}
	if stat.Mode&0o7777 != 0o700 {
		_ = directory.Close()
		return nil, errors.New("result directory mode must be 0700")
	}
	if stat.Nlink < 2 {
		_ = directory.Close()
		return nil, errors.New("result directory link count is invalid")
	}
	return &resultDirectoryHandle{file: directory}, nil
}

func openDirectoryBelowTrustedRoot(path string, flags int, beforeOpen func()) (int, error) {
	rootFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open trusted root: %w", err)
	}
	defer unix.Close(rootFD)
	if beforeOpen != nil {
		beforeOpen()
	}
	relative := strings.TrimPrefix(path, string(filepath.Separator))
	how := &unix.OpenHow{
		Flags:   uint64(flags),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	}
	fd, err := unix.Openat2(rootFD, relative, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return -1, fmt.Errorf("openat2 result resolution unsupported: %w", ErrUnsupported)
		}
		return -1, err
	}
	return fd, nil
}

func openResultFile(directory *resultDirectoryHandle, name string) (*os.File, error) {
	if directory == nil || directory.file == nil {
		return nil, errors.New("result directory descriptor is unavailable")
	}
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return nil, errors.New("result entry name is unsafe")
	}
	fd, err := unix.Openat(int(directory.file.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open result entry: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open result entry returned no descriptor")
	}
	return file, nil
}

func createResultTemp(directory *resultDirectoryHandle, prefix string) (*os.File, string, error) {
	if directory == nil || directory.file == nil {
		return nil, "", errors.New("result directory descriptor is unavailable")
	}
	if prefix == "" || filepath.Base(prefix) != prefix || prefix == "." || prefix == ".." {
		return nil, "", errors.New("result temporary prefix is unsafe")
	}
	for attempt := 0; attempt < 128; attempt++ {
		randomBytes := make([]byte, 16)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, "", fmt.Errorf("generate result temporary name: %w", err)
		}
		name := "." + prefix + "-" + hex.EncodeToString(randomBytes)
		fd, err := unix.Openat(int(directory.file.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create result temporary: %w", err)
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			return nil, "", errors.New("create result temporary returned no descriptor")
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate a unique result temporary name")
}

func linkResult(directory *resultDirectoryHandle, source, target string) error {
	if err := unix.Linkat(int(directory.file.Fd()), source, int(directory.file.Fd()), target, 0); err != nil {
		return fmt.Errorf("link result entry: %w", err)
	}
	return nil
}

func exchangeResult(directory *resultDirectoryHandle, source, target string) error {
	if err := unix.Renameat2(int(directory.file.Fd()), source, int(directory.file.Fd()), target, unix.RENAME_EXCHANGE); err != nil {
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) {
			return fmt.Errorf("result exchange unsupported: %w", ErrUnsupported)
		}
		return fmt.Errorf("exchange result entries: %w", err)
	}
	return nil
}

func unlinkResult(directory *resultDirectoryHandle, name string) error {
	if err := unix.Unlinkat(int(directory.file.Fd()), name, 0); err != nil {
		return fmt.Errorf("unlink result entry: %w", err)
	}
	return nil
}

func snapshotResultEntry(directory *resultDirectoryHandle, name string) (resultEntrySnapshot, error) {
	if directory == nil || directory.file == nil {
		return resultEntrySnapshot{}, errors.New("result directory descriptor is unavailable")
	}
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return resultEntrySnapshot{}, errors.New("result entry name is unsafe")
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(directory.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return resultEntrySnapshot{}, fmt.Errorf("stat displaced result entry: %w", err)
	}
	snapshot := resultEntrySnapshotFromStat(&stat)
	switch snapshot.kind {
	case resultEntrySymlinkKind:
		buffer := make([]byte, 64<<10)
		n, err := unix.Readlinkat(int(directory.file.Fd()), name, buffer)
		if err != nil {
			return snapshot, fmt.Errorf("read displaced result symlink: %w", err)
		}
		if n == len(buffer) {
			return snapshot, errors.New("displaced result symlink target is too long")
		}
		snapshot.target = string(buffer[:n])
		snapshot.targetKnown = true
	case resultEntryRegularKind:
		fd, err := unix.Openat(int(directory.file.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return snapshot, fmt.Errorf("open displaced result entry: %w", err)
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			return snapshot, errors.New("open displaced result entry returned no file")
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, resultMaxBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return snapshot, fmt.Errorf("read displaced result entry: %w", readErr)
		}
		if closeErr != nil {
			return snapshot, fmt.Errorf("close displaced result entry: %w", closeErr)
		}
		if len(raw) > resultMaxBytes {
			return snapshot, errors.New("displaced result entry exceeds bounded size")
		}
		snapshot.raw = raw
		snapshot.rawKnown = true
	}
	return snapshot, nil
}

func resultEntrySnapshotFromStat(stat *unix.Stat_t) resultEntrySnapshot {
	return resultEntrySnapshot{
		device: uint64(stat.Dev), inode: uint64(stat.Ino), size: stat.Size,
		mode: stat.Mode, uid: uint32(stat.Uid), gid: uint32(stat.Gid),
		nlink: uint64(stat.Nlink), rdev: uint64(stat.Rdev),
		kind: uint32(stat.Mode & unix.S_IFMT),
	}
}

func syncResultEntryFile(directory *resultDirectoryHandle, name string, syncFile func(*os.File) error) error {
	if directory == nil || directory.file == nil {
		return errors.New("result directory descriptor is unavailable")
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(int(directory.file.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("stat result for fsync: %w", err)
	}
	if uint32(stat.Mode&unix.S_IFMT) != resultEntryRegularKind {
		return nil
	}
	fd, err := unix.Openat(int(directory.file.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open result for fsync: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("open result for fsync returned no file")
	}
	var openedStat unix.Stat_t
	if err := unix.Fstat(fd, &openedStat); err != nil {
		_ = file.Close()
		return fmt.Errorf("restat result for fsync: %w", err)
	}
	openedSnapshot := resultEntrySnapshotFromStat(&openedStat)
	wantedSnapshot := resultEntrySnapshotFromStat(&stat)
	if !sameResultEntryMetadata(openedSnapshot, wantedSnapshot) {
		_ = file.Close()
		return errors.New("result changed before fsync")
	}
	syncErr := syncFile(file)
	closeErr := file.Close()
	return combineResultErrors(syncErr, closeErr)
}

// lockResultDirectory serializes cooperating result writers using the
// already-validated directory descriptor. The lock is deliberately scoped to
// the final conditional exchange window; callers perform their test hook
// before acquiring it so an in-process second store can exercise replacement
// detection without recursively deadlocking the hook.
func lockResultDirectory(directory *resultDirectoryHandle) error {
	if directory == nil || directory.file == nil {
		return errors.New("result directory descriptor is unavailable")
	}
	if err := unix.Flock(int(directory.file.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock result directory: %w", err)
	}
	return nil
}

func unlockResultDirectory(directory *resultDirectoryHandle) error {
	if directory == nil || directory.file == nil {
		return errors.New("result directory descriptor is unavailable")
	}
	if err := unix.Flock(int(directory.file.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("unlock result directory: %w", err)
	}
	return nil
}
