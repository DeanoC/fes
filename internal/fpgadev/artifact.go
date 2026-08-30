package fpgadev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrUnsupported = errors.New("unsupported on this platform")

// ArtifactAccess carries the expected owner identity for a staging bundle.
// Production construction leaves ExpectedUID at zero (root); tests may inject
// their process UID for ordinary temporary directories.
type ArtifactAccess struct {
	ExpectedUID uint32

	// BeforeDirectoryOpen is a test seam at the trusted-root/openat2
	// boundary. Production callers leave it nil.
	BeforeDirectoryOpen func()

	// BeforeDispatchPathVerify is a test seam immediately before the published
	// named capability is re-resolved through its absolute staging path.
	// Production callers leave it nil.
	BeforeDispatchPathVerify func()
}

// NewArtifactAccess constructs descriptor-bound access with an optional
// expected owner UID. Omitting it enforces root ownership.
func NewArtifactAccess(expectedUID ...uint32) *ArtifactAccess {
	uid := uint32(0)
	if len(expectedUID) != 0 {
		uid = expectedUID[0]
	}
	return &ArtifactAccess{ExpectedUID: uid}
}

// ArtifactMetadata is the immutable identity retained by an ArtifactBinding.
type ArtifactMetadata struct {
	Device uint64
	Inode  uint64
	Size   int64
	SHA256 string
	Path   string
}

// artifactBindingState is the shared lifecycle object behind every copied
// ArtifactBinding value. A binding is intentionally cheap to copy while its
// descriptors and lock remain singletons.
type artifactBindingState struct {
	mu       sync.Mutex
	dir      *os.File
	artifact *os.File
	// dispatchPath is a stable, named hard-link capability published only
	// immediately before the synchronous Main FIFO write. dispatchLeaf is
	// retained separately so cleanup stays descriptor-relative.
	dispatchPath string
	dispatchLeaf string
	metadata     ArtifactMetadata
	manifest     Manifest
	// resourceEvidence is immutable evidence verified against the retained
	// descriptor-bound staging directory. Package-local fixtures may provide it
	// directly; production bindings populate it on their first read.
	resourceEvidence *ResourceEvidenceV2
	expectedUID      uint32
	dispatchVerify   func()
	closed           bool
}

// ArtifactBinding protects the staging directory descriptor, the retained
// artifact descriptor, and the exact artifact identity until FIFO dispatch.
// Its state pointer makes value copies share one lock and one close lifecycle.
type ArtifactBinding struct {
	state *artifactBindingState
}

// Metadata returns a copy of the originally validated binding identity.
func (b *ArtifactBinding) Metadata() ArtifactMetadata {
	if b == nil || b.state == nil {
		return ArtifactMetadata{}
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	return b.state.metadata
}

// Path returns the canonical staging artifact path retained by the binding.
func (b *ArtifactBinding) Path() string {
	return b.Metadata().Path
}

// ArtifactPath is an explicit alias for Path.
func (b *ArtifactBinding) ArtifactPath() string {
	return b.Path()
}

// Open is an alias for OpenArtifact. It returns a duplicate of the retained
// descriptor, not a reopen of the mutable staging pathname.
func (b *ArtifactBinding) Open() (*os.File, error) {
	return b.OpenArtifact()
}

// DispatchPath publishes and returns a named capability for the retained
// descriptor. The platform implementation anchors publication to the
// retained staging directory and artifact descriptor; callers dispatch this
// capability rather than a procfs path or mutable staging pathname.
func (b *ArtifactBinding) DispatchPath() (string, error) {
	if b == nil || b.state == nil {
		return "", errors.New("nil artifact binding")
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.closed || b.state.artifact == nil {
		return "", errors.New("artifact binding is closed")
	}
	return b.dispatchPathLocked()
}

// ReleaseDispatchPath removes the named Main-load capability while retaining
// the artifact binding. It is used after stable Main absence so qualification
// can continue to require the original top.rbf link count and identity.
func (b *ArtifactBinding) ReleaseDispatchPath() error {
	if b == nil || b.state == nil {
		return nil
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	return b.state.releaseDispatchPathLocked()
}

// CapabilityPath is an explicit alias for DispatchPath.
func (b *ArtifactBinding) CapabilityPath() (string, error) {
	return b.DispatchPath()
}

// ArtifactDispatchPath is a descriptive alias for DispatchPath.
func (b *ArtifactBinding) ArtifactDispatchPath() (string, error) {
	return b.DispatchPath()
}

// ResourceEvidence verifies and returns the unsigned schema-2 evidence bound
// to this artifact. Production callers must have a real descriptor-backed
// staging path; the in-memory hook exists only for package-local qualification
// fixtures that deliberately do not touch the filesystem.
func (b *ArtifactBinding) ResourceEvidence() (ResourceEvidenceV2, error) {
	var zero ResourceEvidenceV2
	if b == nil || b.state == nil {
		return zero, errors.New("nil artifact binding")
	}
	b.state.mu.Lock()
	if b.state.closed {
		b.state.mu.Unlock()
		return zero, errors.New("artifact binding is closed")
	}
	metadata := b.state.metadata
	manifest := b.state.manifest
	cachedEvidence := b.state.resourceEvidence
	hasDescriptor := b.state.dir != nil
	b.state.mu.Unlock()
	if cachedEvidence != nil {
		evidence := *cachedEvidence
		if hasDescriptor {
			if err := b.Revalidate(); err != nil {
				return zero, fmt.Errorf("revalidate artifact before cached evidence: %w", err)
			}
		}
		if err := evidence.MatchesManifest(manifest); err != nil {
			return zero, err
		}
		if evidence.ArtifactSHA256 != metadata.SHA256 {
			return zero, errors.New("resource evidence artifact hash does not match opened artifact")
		}
		return evidence, nil
	}
	if err := b.Revalidate(); err != nil {
		return zero, fmt.Errorf("revalidate artifact before evidence load: %w", err)
	}
	bundle, err := verifyArtifactBundleForBinding(b)
	if err != nil {
		return zero, err
	}
	if err := b.Revalidate(); err != nil {
		return zero, fmt.Errorf("revalidate artifact after evidence load: %w", err)
	}
	b.state.mu.Lock()
	if b.state.closed {
		b.state.mu.Unlock()
		return zero, errors.New("artifact binding closed before evidence publication")
	}
	cached := bundle.Evidence
	b.state.resourceEvidence = &cached
	b.state.mu.Unlock()
	return bundle.Evidence, nil
}

// Evidence is an explicit alias for ResourceEvidence.
func (b *ArtifactBinding) Evidence() (ResourceEvidenceV2, error) {
	return b.ResourceEvidence()
}

// Close releases the protected staging descriptor. It is idempotent.
func (b *ArtifactBinding) Close() error {
	if b == nil || b.state == nil {
		return nil
	}
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	if b.state.closed {
		return nil
	}
	b.state.closed = true
	dispatchErr := b.state.releaseDispatchPathLocked()
	var artifactErr error
	if b.state.artifact != nil {
		if err := b.state.artifact.Close(); err != nil {
			artifactErr = fmt.Errorf("close artifact descriptor: %w", err)
		}
		b.state.artifact = nil
	}
	var directoryErr error
	if b.state.dir != nil {
		if err := b.state.dir.Close(); err != nil {
			directoryErr = fmt.Errorf("close staging directory: %w", err)
		}
		b.state.dir = nil
	}
	if dispatchErr != nil || artifactErr != nil || directoryErr != nil {
		return errors.Join(dispatchErr, artifactErr, directoryErr)
	}
	return nil
}

// Release is the lifecycle-oriented alias for Close.
func (b *ArtifactBinding) Release() error {
	return b.Close()
}

func validateArtifactPath(staging string) error {
	if staging == "" || !filepath.IsAbs(staging) || filepath.Clean(staging) != staging || staging == string(filepath.Separator) {
		return errors.New("staging path must be an absolute canonical directory path")
	}
	return nil
}

func validateManifestForBinding(manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if manifest.ArtifactSize > MaxRBFSize {
		return errors.New("artifact exceeds maximum RBF size")
	}
	return nil
}
