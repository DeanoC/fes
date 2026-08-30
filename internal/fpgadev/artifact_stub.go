//go:build !linux

package fpgadev

import "os"

func resultPersistenceSupported() bool { return false }

func (a ArtifactAccess) Bind(manifest Manifest, staging string) (ArtifactBinding, error) {
	return ArtifactBinding{}, ErrUnsupported
}

func (b *ArtifactBinding) Revalidate() error {
	return ErrUnsupported
}

func (b *ArtifactBinding) dispatchPathLocked() (string, error) {
	return "", ErrUnsupported
}

func (s *artifactBindingState) releaseDispatchPathLocked() error {
	if s == nil || s.dispatchLeaf == "" {
		if s != nil {
			s.dispatchPath = ""
		}
		return nil
	}
	return ErrUnsupported
}

func (b *ArtifactBinding) OpenArtifact() (*os.File, error) {
	return nil, ErrUnsupported
}

func verifyArtifactBundleAtPath(string, uint32) (ArtifactBundle, error) {
	return ArtifactBundle{}, ErrUnsupported
}

func verifyArtifactBundleForBinding(*ArtifactBinding) (ArtifactBundle, error) {
	return ArtifactBundle{}, ErrUnsupported
}

func openResultDirectory(string, uint32, ...func()) (*resultDirectoryHandle, error) {
	return nil, ErrUnsupported
}

func openResultFile(*resultDirectoryHandle, string) (*os.File, error) {
	return nil, ErrUnsupported
}

func createResultTemp(*resultDirectoryHandle, string) (*os.File, string, error) {
	return nil, "", ErrUnsupported
}

func linkResult(*resultDirectoryHandle, string, string) error {
	return ErrUnsupported
}

func exchangeResult(*resultDirectoryHandle, string, string) error {
	return ErrUnsupported
}

func unlinkResult(*resultDirectoryHandle, string) error {
	return ErrUnsupported
}

func snapshotResultEntry(*resultDirectoryHandle, string) (resultEntrySnapshot, error) {
	return resultEntrySnapshot{}, ErrUnsupported
}

func syncResultEntryFile(*resultDirectoryHandle, string, func(*os.File) error) error {
	return ErrUnsupported
}

func lockResultDirectory(*resultDirectoryHandle) error {
	return ErrUnsupported
}

func unlockResultDirectory(*resultDirectoryHandle) error {
	return ErrUnsupported
}
