package fpgadev

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func makeArtifactFixture(t *testing.T, payload []byte) (string, Manifest) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor contract")
	}
	staging := t.TempDir()
	if err := os.Chmod(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(staging, "top.rbf")
	if err := os.WriteFile(artifactPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest := validManifest()
	manifest.ArtifactSize = uint64(len(payload))
	manifest.ArtifactSHA256 = strings.Repeat("0", 64)
	manifest.ArtifactSHA256 = stringHex(digest[:])
	return staging, manifest
}

func stringHex(raw []byte) string {
	const hex = "0123456789abcdef"
	result := make([]byte, len(raw)*2)
	for i, value := range raw {
		result[i*2] = hex[value>>4]
		result[i*2+1] = hex[value&15]
	}
	return string(result)
}

func testArtifactAccess() ArtifactAccess {
	return ArtifactAccess{ExpectedUID: uint32(os.Getuid())}
}

func TestArtifactBindRetainsAndRevalidatesExactDescriptorBinding(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer binding.Close()
	metadata := binding.Metadata()
	if metadata.Size != int64(manifest.ArtifactSize) || metadata.SHA256 != manifest.ArtifactSHA256 || metadata.Path != filepath.Join(staging, "top.rbf") {
		t.Fatalf("binding metadata = %#v", metadata)
	}
	if err := binding.Revalidate(); err != nil {
		t.Fatalf("initial Revalidate: %v", err)
	}
}

func TestArtifactBindingResourceEvidenceRequiresTheVerifiedBundle(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer binding.Close()
	if _, err := binding.ResourceEvidence(); err == nil {
		t.Fatal("binding without checksum/evidence members was accepted")
	}
}

func TestArtifactRevalidateRejectsMutationAndReplacement(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if err := os.WriteFile(filepath.Join(staging, "top.rbf"), []byte("different!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := binding.Revalidate(); err == nil {
		t.Fatal("same-file mutation was accepted")
	}

	staging, manifest = makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err = testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	replacement := filepath.Join(staging, "replacement.rbf")
	if err := os.WriteFile(replacement, []byte("rbf-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(staging, "top.rbf")); err != nil {
		t.Fatal(err)
	}
	if err := binding.Revalidate(); err == nil {
		t.Fatal("inode replacement was accepted")
	}
}

func TestArtifactBindRejectsHostileFilesystemMatrix(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor contract")
	}
	payload := []byte("rbf-payload")
	_, baseManifest := makeArtifactFixture(t, payload)
	tests := map[string]func(t *testing.T, staging string, manifest Manifest){
		"directory symlink": func(t *testing.T, staging string, manifest Manifest) {
			link := filepath.Join(t.TempDir(), "staging-link")
			if err := os.Symlink(staging, link); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, link); err == nil {
				t.Fatal("directory symlink accepted")
			}
		},
		"artifact symlink": func(t *testing.T, staging string, manifest Manifest) {
			if err := os.Remove(filepath.Join(staging, "top.rbf")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other.rbf", filepath.Join(staging, "top.rbf")); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("artifact symlink accepted")
			}
		},
		"wrong size": func(t *testing.T, staging string, manifest Manifest) {
			manifest.ArtifactSize++
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("declared size mismatch accepted")
			}
		},
		"wrong hash": func(t *testing.T, staging string, manifest Manifest) {
			manifest.ArtifactSHA256 = strings.Repeat("b", 64)
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("declared hash mismatch accepted")
			}
		},
		"zero size": func(t *testing.T, staging string, manifest Manifest) {
			manifest.ArtifactSize = 0
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("zero-size artifact accepted")
			}
		},
		"oversize": func(t *testing.T, staging string, manifest Manifest) {
			manifest.ArtifactSize = MaxRBFSize + 1
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("oversize artifact accepted")
			}
		},
		"wrong mode": func(t *testing.T, staging string, manifest Manifest) {
			if err := os.Chmod(filepath.Join(staging, "top.rbf"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("world-readable artifact accepted")
			}
		},
		"directory wrong mode": func(t *testing.T, staging string, manifest Manifest) {
			if err := os.Chmod(staging, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("world-readable staging directory accepted")
			}
		},
		"directory extra link": func(t *testing.T, staging string, manifest Manifest) {
			if err := os.Mkdir(filepath.Join(staging, "nested"), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("staging directory with an unexpected subdirectory was accepted")
			}
		},
		"hard link": func(t *testing.T, staging string, manifest Manifest) {
			if err := os.Link(filepath.Join(staging, "top.rbf"), filepath.Join(staging, "alias.rbf")); err != nil {
				t.Fatal(err)
			}
			if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
				t.Fatal("multi-link artifact accepted")
			}
		},
		"unsafe staging path": func(t *testing.T, staging string, manifest Manifest) {
			unsafePath := staging + string(os.PathSeparator) + ".." + string(os.PathSeparator) + filepath.Base(staging)
			if _, err := testArtifactAccess().Bind(manifest, unsafePath); err == nil {
				t.Fatal("noncanonical staging path accepted")
			}
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			staging := filepath.Join(t.TempDir(), "bundle")
			if err := os.Mkdir(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(staging, "top.rbf"), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			manifest := baseManifest
			test(t, staging, manifest)
		})
	}
}

func TestArtifactBindRejectsAncestorSymlinkInsertedAtTrustedRootBoundary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor contract")
	}
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	staging := filepath.Join(parent, "bundle")
	hostile := filepath.Join(root, "hostile")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(hostile, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("rbf-payload")
	if err := os.WriteFile(filepath.Join(staging, ManifestArtifact), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	manifest := validManifest()
	manifest.ArtifactSize = uint64(len(payload))
	manifest.ArtifactSHA256 = stringHex(digest[:])
	access := testArtifactAccess()
	access.BeforeDirectoryOpen = func() {
		if err := os.Rename(parent, parent+".retained"); err != nil {
			t.Fatalf("move trusted-root parent: %v", err)
		}
		if err := os.Symlink(hostile, parent); err != nil {
			t.Fatalf("insert ancestor symlink: %v", err)
		}
	}
	if _, err := access.Bind(manifest, staging); err == nil {
		t.Fatal("ancestor symlink inserted at openat2 boundary was accepted")
	}
	if _, err := os.Lstat(filepath.Join(hostile, ManifestArtifact)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile directory was touched: %v", err)
	}
}

func TestArtifactCloseIsIdempotentAndUnsupportedPathFailsClosed(t *testing.T) {
	var binding ArtifactBinding
	if err := binding.Close(); err != nil {
		t.Fatal(err)
	}
	if err := binding.Release(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" {
		_, err := (ArtifactAccess{}).Bind(validManifest(), "/tmp/bundle")
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("non-Linux Bind error = %v", err)
		}
	}
}

func TestArtifactBindDoesNotBlockOnFIFOArtifact(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux descriptor contract")
	}
	staging := t.TempDir()
	if err := os.Chmod(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(staging, "top.rbf")
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := validManifest()
	manifest.ArtifactSize = 1
	manifest.ArtifactSHA256 = strings.Repeat("0", 64)
	result := make(chan error, 1)
	go func() {
		_, err := testArtifactAccess().Bind(manifest, staging)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("FIFO artifact was accepted")
		}
	case <-time.After(500 * time.Millisecond):
		writerFD, err := unix.Open(fifoPath, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err != nil {
			t.Fatalf("unblock FIFO reader: %v", err)
		}
		_ = unix.Close(writerFD)
		select {
		case <-result:
			t.Fatal("FIFO artifact open blocked without O_NONBLOCK")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("FIFO artifact bind remained blocked after reader release")
		}
	}
}

func TestArtifactBindRejectsOwnerMismatches(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	wrongUID := uint32(os.Getuid()) + 1
	if wrongUID == uint32(os.Getuid()) {
		t.Fatal("test UID arithmetic wrapped")
	}
	if _, err := (ArtifactAccess{ExpectedUID: wrongUID}).Bind(manifest, staging); err == nil {
		t.Fatal("staging directory owner mismatch was accepted")
	}
	if os.Getuid() == 0 {
		artifactPath := filepath.Join(staging, "top.rbf")
		if err := os.Chown(artifactPath, 1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := testArtifactAccess().Bind(manifest, staging); err == nil {
			t.Fatal("artifact owner mismatch was accepted")
		}
	}
}

func TestArtifactDispatchPathRetainsRevalidatedBytesAfterPathReplacement(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	dispatchPath, err := binding.DispatchPath()
	if err != nil {
		t.Fatalf("DispatchPath: %v", err)
	}
	if !strings.HasPrefix(dispatchPath, "/proc/") || strings.Contains(dispatchPath, staging) {
		t.Fatalf("dispatch path = %q", dispatchPath)
	}
	replacement := filepath.Join(staging, "replacement.rbf")
	if err := os.WriteFile(replacement, []byte("new-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(staging, "top.rbf")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dispatchPath)
	if err != nil {
		t.Fatalf("read dispatch capability: %v", err)
	}
	if string(got) != "rbf-payload" {
		t.Fatalf("dispatch capability followed replaced pathname: %q", got)
	}
}

func TestArtifactOpenUsesRetainedDescriptorAfterPathReplacement(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if err := binding.Revalidate(); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(staging, "replacement.rbf")
	if err := os.WriteFile(replacement, []byte("new-payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(staging, "top.rbf")); err != nil {
		t.Fatal(err)
	}
	opened, err := binding.OpenArtifact()
	if err != nil {
		t.Fatalf("OpenArtifact: %v", err)
	}
	defer opened.Close()
	if _, err := opened.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(opened)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "rbf-payload" {
		t.Fatalf("opened capability bytes = %q", got)
	}
}

func TestArtifactDispatchPathStaysStableAcrossRevalidationAndFDReuse(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	dispatchPath, err := binding.DispatchPath()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if err := binding.Revalidate(); err != nil {
			t.Fatalf("Revalidate(%d): %v", i, err)
		}
		currentPath, err := binding.DispatchPath()
		if err != nil {
			t.Fatal(err)
		}
		if currentPath != dispatchPath {
			t.Fatalf("dispatch path changed after Revalidate(%d): %q -> %q", i, dispatchPath, currentPath)
		}
		for j := 0; j < 8; j++ {
			file, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
		got, err := os.ReadFile(dispatchPath)
		if err != nil || string(got) != "rbf-payload" {
			t.Fatalf("dispatch bytes after Revalidate(%d) = %q, %v", i, got, err)
		}
	}
}

func TestArtifactConcurrentRevalidationKeepsCapabilityStable(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	wantPath, err := binding.DispatchPath()
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 8*16)
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := 0; i < 16; i++ {
				if err := binding.Revalidate(); err != nil {
					errorsSeen <- err
					return
				}
				path, err := binding.DispatchPath()
				if err != nil {
					errorsSeen <- err
					return
				}
				if path != wantPath {
					errorsSeen <- fmt.Errorf("dispatch path changed: %q -> %q", wantPath, path)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatal(err)
	}
	got, err := os.ReadFile(wantPath)
	if err != nil || string(got) != "rbf-payload" {
		t.Fatalf("concurrent revalidation bytes = %q, %v", got, err)
	}
}

func TestArtifactBindingValueCopiesShareLifecycleState(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	copyBinding := binding
	if _, err := copyBinding.DispatchPath(); err != nil {
		t.Fatal(err)
	}
	if err := copyBinding.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.DispatchPath(); err == nil {
		t.Fatal("value copy close did not close the shared capability")
	}
	if err := binding.Close(); err != nil {
		t.Fatalf("original close after copied close: %v", err)
	}
	if err := binding.Revalidate(); err == nil {
		t.Fatal("revalidation after copied close succeeded")
	}
}

func TestArtifactBindingConcurrentCloseAndRevalidateShareOneLock(t *testing.T) {
	staging, manifest := makeArtifactFixture(t, []byte("rbf-payload"))
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatal(err)
	}
	copyBinding := binding
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		for i := 0; i < 32; i++ {
			_ = copyBinding.Revalidate()
		}
		close(done)
	}()
	<-started
	if err := binding.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if err := copyBinding.Close(); err != nil {
		t.Fatal(err)
	}
}
