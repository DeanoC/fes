package romsource

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestPrepareRawContentAndSecureStaging(t *testing.T) {
	root, game := rawFixture(t, "Games/HERO.SFC", []byte("synthetic-raw-rom"))
	staging := filepath.Join(t.TempDir(), "nested", "staging")

	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Remove() })

	wantDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("synthetic-raw-rom")))
	wantContent := protocol.ContentIdentity{SHA256: wantDigest, Size: 17, Extension: "sfc"}
	if prepared.Content != wantContent {
		t.Fatalf("content = %+v, want %+v", prepared.Content, wantContent)
	}
	data, err := os.ReadFile(prepared.Path)
	if err != nil || string(data) != "synthetic-raw-rom" {
		t.Fatalf("staged data = %q, err=%v", data, err)
	}
	assertSecureStagingPath(t, staging, prepared.Path)
}

func TestPreparedOpenRejectsReplacementAndReadsHeldIdentity(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	prepared, err := (Preparer{StagingRoot: t.TempDir(), MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Remove() })

	file, err := prepared.Open()
	if err != nil {
		t.Fatalf("Open(original): %v", err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(data) != "synthetic-original" {
		t.Fatalf("read original = %q, read=%v close=%v", data, readErr, closeErr)
	}

	displaced := prepared.Path + ".original"
	if err := os.Rename(prepared.Path, displaced); err != nil {
		t.Fatalf("displace prepared path: %v", err)
	}
	if err := os.WriteFile(prepared.Path, []byte("private-replacement"), 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if file, err := prepared.Open(); err == nil {
		_ = file.Close()
		t.Fatal("Open accepted replacement staging identity")
	}
	if err := os.Remove(prepared.Path); err != nil {
		t.Fatalf("remove replacement: %v", err)
	}
	if err := os.Rename(displaced, prepared.Path); err != nil {
		t.Fatalf("restore prepared path: %v", err)
	}
}

func TestPreparedRemoveFindsRenamedHeldIdentityAndPreservesDecoy(t *testing.T) {
	for _, withDecoy := range []bool{false, true} {
		name := "rename only"
		if withDecoy {
			name = "rename with decoy"
		}
		t.Run(name, func(t *testing.T) {
			root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
			prepared, err := (Preparer{StagingRoot: t.TempDir(), MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			t.Cleanup(func() { _ = prepared.Remove() })

			renamed := prepared.Path + ".renamed"
			if err := os.Rename(prepared.Path, renamed); err != nil {
				t.Fatalf("rename prepared file: %v", err)
			}
			if withDecoy {
				if err := os.WriteFile(prepared.Path, []byte("private-decoy"), 0o600); err != nil {
					t.Fatalf("write decoy: %v", err)
				}
			}

			if err := prepared.Remove(); err != nil {
				t.Fatalf("Remove: %v", err)
			}
			if _, err := os.Lstat(renamed); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("renamed held identity still exists or cannot be inspected: %v", err)
			}
			if withDecoy {
				decoy, err := os.ReadFile(prepared.Path)
				if err != nil || string(decoy) != "private-decoy" {
					t.Fatalf("decoy = %q, err=%v", decoy, err)
				}
			}
		})
	}
}

func TestPreparedRemoveFindsIdentityRenamedIntoHeldSubdirectory(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	subdirectory := filepath.Join(staging, "moved")
	if err := os.Mkdir(subdirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(subdirectory, filepath.Base(prepared.Path))
	if err := os.Rename(prepared.Path, renamed); err != nil {
		t.Fatal(err)
	}

	if err := prepared.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(renamed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("subdirectory staging identity still exists or cannot be inspected: %v", err)
	}
}

func TestPreparedRemoveFailsWhenIdentityMovedOutsideHeldRoot(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "moved-outside.rom")
	if err := os.Rename(prepared.Path, outside); err != nil {
		t.Fatal(err)
	}

	if err := prepared.Remove(); err == nil {
		t.Fatal("Remove reported success after the staged identity moved outside its held root")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "synthetic-original" {
		t.Fatalf("outside identity = %q, err=%v", data, err)
	}
	if err := os.Rename(outside, prepared.Path); err != nil {
		t.Fatalf("restore staged identity: %v", err)
	}
	if err := prepared.Remove(); err != nil {
		t.Fatalf("Remove(restored): %v", err)
	}
}

func TestPreparedRemoveDoesNotDeleteDecoySwappedAfterIdentityCheck(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	renamed := filepath.Join(staging, "renamed.rom")
	if err := os.Rename(prepared.Path, renamed); err != nil {
		t.Fatal(err)
	}
	var swappedOriginal string
	hookCalled := false
	const quarantine = ".fogcast-remove-deterministic"
	prepared.removeQuarantineName = func() string { return quarantine }
	prepared.beforeRemoveCandidate = func(held *os.Root, candidate string) error {
		if hookCalled {
			return nil
		}
		hookCalled = true
		swappedOriginal = candidate + ".original"
		if err := held.Rename(candidate, swappedOriginal); err != nil {
			return err
		}
		decoy, err := held.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := decoy.Write([]byte("private-decoy"))
		return errors.Join(writeErr, decoy.Close())
	}

	if err := prepared.Remove(); err == nil {
		t.Fatal("Remove reported success after an identity-check swap")
	}
	if !hookCalled {
		t.Fatal("identity-check swap hook was not called")
	}
	original, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(swappedOriginal)))
	if err != nil || string(original) != "synthetic-original" {
		t.Fatalf("retained swapped original = %q, err=%v", original, err)
	}
	decoy, err := os.ReadFile(renamed)
	if err != nil || string(decoy) != "private-decoy" {
		t.Fatalf("decoy = %q, err=%v", decoy, err)
	}
}

func TestPreparedRemoveDoesNotOverwriteQuarantineDestination(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	const quarantine = ".fogcast-remove-deterministic"
	prepared.removeQuarantineName = func() string { return quarantine }
	prepared.beforeCaptureCandidate = func(held *os.Root, _, gotQuarantine string) error {
		if gotQuarantine != quarantine {
			return errors.New("unexpected quarantine name")
		}
		decoy, err := held.OpenFile(gotQuarantine, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := decoy.Write([]byte("private-decoy"))
		return errors.Join(writeErr, decoy.Close())
	}

	if err := prepared.Remove(); err == nil {
		t.Fatal("Remove reported success after quarantine destination insertion")
	}
	original, err := os.ReadFile(prepared.Path)
	if err != nil || string(original) != "synthetic-original" {
		t.Fatalf("staged identity = %q, err=%v", original, err)
	}
	decoy, err := os.ReadFile(filepath.Join(staging, quarantine))
	if err != nil || string(decoy) != "private-decoy" {
		t.Fatalf("quarantine decoy = %q, err=%v", decoy, err)
	}
}

func TestPreparedRemoveFailsWithoutDeletingFinalWindowReplacement(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	const quarantine = ".fogcast-remove-deterministic"
	prepared.removeQuarantineName = func() string { return quarantine }
	var retainedExact string
	prepared.beforeFinalRemove = func(held *os.Root, gotQuarantine string) error {
		retainedExact = gotQuarantine + ".exact"
		if err := held.Rename(gotQuarantine, retainedExact); err != nil {
			return err
		}
		decoy, err := held.OpenFile(gotQuarantine, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := decoy.Write([]byte("private-decoy"))
		return errors.Join(writeErr, decoy.Close())
	}

	if err := prepared.Remove(); err == nil {
		t.Fatal("Remove reported success after final-window replacement")
	}
	original, err := os.ReadFile(filepath.Join(staging, retainedExact))
	if err != nil || string(original) != "synthetic-original" {
		t.Fatalf("retained exact identity = %q, err=%v", original, err)
	}
	decoy, err := os.ReadFile(filepath.Join(staging, quarantine))
	if err != nil || string(decoy) != "private-decoy" {
		t.Fatalf("final-window decoy = %q, err=%v", decoy, err)
	}
}

func TestPreparedRemoveConfinesRenamedCandidateParentToHeldRoot(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("synthetic-original"))
	staging := t.TempDir()
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	base := filepath.Base(prepared.Path)
	insideParent := filepath.Join(staging, "moved")
	if err := os.Mkdir(insideParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(prepared.Path, filepath.Join(insideParent, base)); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideDecoy := filepath.Join(outside, base)
	if err := os.WriteFile(outsideDecoy, []byte("outside-decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared.beforeCaptureCandidate = func(held *os.Root, _, _ string) error {
		if err := held.Rename("moved", "moved-exact"); err != nil {
			return err
		}
		return held.Symlink(outside, "moved")
	}

	if err := prepared.Remove(); err == nil {
		t.Fatal("Remove reported success after candidate parent replacement")
	}
	original, err := os.ReadFile(filepath.Join(staging, "moved-exact", base))
	if err != nil || string(original) != "synthetic-original" {
		t.Fatalf("retained exact identity = %q, err=%v", original, err)
	}
	decoy, err := os.ReadFile(outsideDecoy)
	if err != nil || string(decoy) != "outside-decoy" {
		t.Fatalf("outside decoy = %q, err=%v", decoy, err)
	}
}

func TestPrepareRawRejectsInvalidSizesAndCleansStaging(t *testing.T) {
	tests := []struct {
		name string
		size int64
	}{
		{name: "zero", size: 0},
		{name: "over protocol maximum", size: protocol.MaxContentBytes + 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			library := t.TempDir()
			source := filepath.Join(library, "game.sfc")
			file, err := os.Create(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(test.size); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			root, game := catalogFixture(t, library, "game.sfc", catalog.SourceKindRaw, "", 0, 0, 0)
			staging := t.TempDir()

			_, err = (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
			assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, library)
			assertStagingEmpty(t, staging)
		})
	}
}

func TestPrepareRawCancellationMissingAndReplacementCleanStaging(t *testing.T) {
	t.Run("canceled", func(t *testing.T) {
		root, game := rawFixture(t, "game.sfc", []byte("content"))
		staging := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(ctx, root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
		assertStagingEmpty(t, staging)
	})

	t.Run("missing", func(t *testing.T) {
		root, game := rawFixture(t, "game.sfc", []byte("content"))
		if err := os.Remove(filepath.Join(root.Path, "game.sfc")); err != nil {
			t.Fatal(err)
		}
		staging := t.TempDir()

		_, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("error = %v, want fs.ErrNotExist", err)
		}
		assertStagingEmpty(t, staging)
	})

	t.Run("replaced before open", func(t *testing.T) {
		root, game := rawFixture(t, "game.sfc", []byte("cataloged"))
		sourcePath := filepath.Join(root.Path, "game.sfc")
		originalInfo, err := os.Stat(sourcePath)
		if err != nil {
			t.Fatal(err)
		}
		replacement := filepath.Join(root.Path, "replacement.sfc")
		if err := os.WriteFile(replacement, []byte("different"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(replacement, originalInfo.ModTime(), originalInfo.ModTime()); err != nil {
			t.Fatal(err)
		}
		staging := t.TempDir()
		preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
		preparer.openFile = func(root *os.Root, name string) (sourceFile, error) {
			if err := os.Rename(replacement, sourcePath); err != nil {
				return nil, err
			}
			return root.Open(name)
		}

		_, err = preparer.Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
		assertStagingEmpty(t, staging)
	})
}

func TestPrepareRawRejectsSourceChangedDuringStreaming(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 256<<10)
	root, game := rawFixture(t, "game.sfc", body)
	sourcePath := filepath.Join(root.Path, "game.sfc")
	originalInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	preparer.openFile = func(root *os.Root, name string) (sourceFile, error) {
		file, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		return &mutatingSourceFile{File: file, path: sourcePath, mtime: originalInfo.ModTime().Add(time.Second)}, nil
	}

	_, err = preparer.Prepare(context.Background(), root, game)
	assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
	assertStagingEmpty(t, staging)
}

func TestPrepareZIPStreamsOnlyRecordedMember(t *testing.T) {
	rom := []byte("selected-synthetic-rom")
	archive := zipBytes(t, []zipEntry{{name: "README.txt", body: []byte("not-the-rom-body")}, {name: "Games/HERO.SFC", body: rom}})
	readmeOffset := bytes.Index(archive, []byte("not-the-rom-body"))
	if readmeOffset < 0 {
		t.Fatal("README body not found in stored ZIP")
	}
	archive[readmeOffset] ^= 0xff // CRC fails only if the unselected member is opened.
	root, game := zipFixture(t, "collection.zip", archive, "Games/HERO.SFC")
	staging := t.TempDir()

	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	t.Cleanup(func() { _ = prepared.Remove() })
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(rom))
	want := protocol.ContentIdentity{SHA256: wantDigest, Size: int64(len(rom)), Extension: "sfc"}
	if prepared.Content != want {
		t.Fatalf("content = %+v, want %+v", prepared.Content, want)
	}
}

func TestPrepareZIPRejectsChecksumFailureAndCleansStaging(t *testing.T) {
	archive := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("selected-body")}})
	bodyOffset := bytes.Index(archive, []byte("selected-body"))
	if bodyOffset < 0 {
		t.Fatal("member body not found in stored ZIP")
	}
	archive[bodyOffset] ^= 0xff
	root, game := zipFixture(t, "game.zip", archive, "game.sfc")
	staging := t.TempDir()

	_, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	assertPrepareError(t, err, game.ID, protocol.CodeInvalidArchive, root.Path)
	if !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("error = %v, want zip.ErrChecksum", err)
	}
	assertStagingEmpty(t, staging)
}

func TestPrepareZIPRejectsDeclaredAndStreamedContentOverLimit(t *testing.T) {
	t.Run("declared size before member open", func(t *testing.T) {
		archive := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("tiny")}})
		patchCentralUncompressedSize(t, archive, uint32(protocol.MaxContentBytes+1))
		root, game := zipFixture(t, "game.zip", archive, "game.sfc")
		staging := t.TempDir()
		opened := false
		preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
		preparer.openZIPMember = func(member *zip.File) (io.ReadCloser, error) {
			opened = true
			return member.Open()
		}

		_, err := preparer.Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeInvalidArchive, root.Path)
		if opened {
			t.Fatal("oversized declared member was opened")
		}
		assertStagingEmpty(t, staging)
	})

	t.Run("stream exceeds cap", func(t *testing.T) {
		archive := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("four")}})
		root, game := zipFixture(t, "game.zip", archive, "game.sfc")
		staging := t.TempDir()
		preparer := Preparer{StagingRoot: staging, MaxBytes: 4}
		preparer.openZIPMember = func(*zip.File) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("five!")), nil
		}

		_, err := preparer.Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeInvalidArchive, root.Path)
		assertStagingEmpty(t, staging)
	})
}

func TestPrepareZIPRevalidatesCompleteCatalogFingerprint(t *testing.T) {
	t.Run("changed central directory", func(t *testing.T) {
		original := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("original")}})
		root, game := zipFixture(t, "game.zip", original, "game.sfc")
		source := filepath.Join(root.Path, "game.zip")
		mtime := time.Unix(1_700_000_000, 123_456_789)
		changed := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("mutated!")}})
		if len(changed) != len(original) {
			t.Fatalf("changed ZIP length = %d, want %d", len(changed), len(original))
		}
		if err := os.WriteFile(source, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(source, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		game.Fingerprint.SourceSize = int64(len(changed))
		game.Fingerprint.ModifiedNS = mtime.UnixNano()
		staging := t.TempDir()

		_, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
		assertStagingEmpty(t, staging)
	})

	t.Run("selected member disappeared", func(t *testing.T) {
		original := zipBytes(t, []zipEntry{{name: "game.sfc", body: []byte("content")}})
		root, game := zipFixture(t, "game.zip", original, "game.sfc")
		source := filepath.Join(root.Path, "game.zip")
		mtime := time.Unix(1_700_000_001, 987_654_321)
		changed := zipBytes(t, []zipEntry{{name: "gone.sfc", body: []byte("content")}})
		if len(changed) != len(original) {
			t.Fatalf("changed ZIP length = %d, want %d", len(changed), len(original))
		}
		if err := os.WriteFile(source, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(source, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		game.Fingerprint.SourceSize = int64(len(changed))
		game.Fingerprint.ModifiedNS = mtime.UnixNano()
		staging := t.TempDir()

		_, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
		assertPrepareError(t, err, game.ID, protocol.CodeSourceUnavailable, root.Path)
		assertStagingEmpty(t, staging)
	})
}

func TestPrepareZIPTreatsTraversalMemberAsArchiveNameAndRemoveCleansUp(t *testing.T) {
	body := []byte("traversal-name-rom")
	archive := zipBytes(t, []zipEntry{{name: "../../escape.SFC", body: body}})
	root, game := zipFixture(t, "game.zip", archive, "../../escape.SFC")
	stagingParent := t.TempDir()
	staging := filepath.Join(stagingParent, "staging")

	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared.Content.Extension != "sfc" {
		t.Fatalf("extension = %q, want sfc", prepared.Content.Extension)
	}
	if _, err := os.Stat(filepath.Join(stagingParent, "escape.SFC")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("archive member was treated as filesystem path: %v", err)
	}
	if err := prepared.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	assertStagingEmpty(t, staging)
	if err := prepared.Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

func TestPreparedRemoveUsesHeldStagingDirectoryAfterPathReplacement(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("content"))
	parent := t.TempDir()
	staging := filepath.Join(parent, "staging")
	prepared, err := (Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}).Prepare(context.Background(), root, game)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	base := filepath.Base(prepared.Path)
	moved := filepath.Join(parent, "moved-staging")
	if err := os.Rename(staging, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	decoy := filepath.Join(staging, base)
	if err := os.WriteFile(decoy, []byte("do-not-remove"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := prepared.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, base)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("held staging file remains after Remove: %v", err)
	}
	data, err := os.ReadFile(decoy)
	if err != nil || string(data) != "do-not-remove" {
		t.Fatalf("replacement staging file = %q, err=%v", data, err)
	}
}

func TestPrepareCleansStagingWhenInitialFileSetupFails(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*Preparer, *string)
	}{
		{
			name: "initial stat",
			inject: func(preparer *Preparer, createdPath *string) {
				preparer.statStagedFile = func(file *os.File) (fs.FileInfo, error) {
					*createdPath = file.Name()
					return nil, errors.New("injected staging stat failure")
				}
			},
		},
		{
			name: "chmod",
			inject: func(preparer *Preparer, createdPath *string) {
				preparer.chmodStagedFile = func(file *os.File, _ fs.FileMode) error {
					*createdPath = file.Name()
					return errors.New("injected staging chmod failure")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, game := rawFixture(t, "game.sfc", []byte("content"))
			staging := t.TempDir()
			preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
			createdPath := ""
			test.inject(&preparer, &createdPath)

			prepared, err := preparer.Prepare(context.Background(), root, game)
			if prepared != nil {
				t.Cleanup(func() { _ = prepared.Remove() })
			}
			assertPrepareError(t, err, game.ID, protocol.CodeTransferFailed, staging)
			if createdPath == "" {
				t.Fatal("fault hook did not observe the created staging file")
			}
			if _, err := os.Lstat(createdPath); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("created staging file remains after setup failure: %v", err)
			}
			assertStagingEmpty(t, staging)
		})
	}
}

func TestPrepareCreatesPartialStagingFileOnlyInValidatedHeldRoot(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("content"))
	parent := t.TempDir()
	staging := filepath.Join(parent, "staging")
	moved := filepath.Join(parent, "validated-staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	mutationAttempted := false
	mutationCompleted := false
	var mutationErr error
	createdBase := ""
	preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	preparer.beforeStagingCreate = func(rootPath string) error {
		mutationAttempted = true
		if err := os.Rename(rootPath, moved); err != nil {
			mutationErr = err
			return nil
		}
		if err := os.Mkdir(rootPath, 0o700); err != nil {
			mutationErr = err
			return nil
		}
		mutationCompleted = true
		return nil
	}
	preparer.statStagedFile = func(file *os.File) (fs.FileInfo, error) {
		createdBase = filepath.Base(file.Name())
		return nil, errors.New("injected initial staging stat failure")
	}

	_, err := preparer.Prepare(context.Background(), root, game)
	if !mutationAttempted || !mutationCompleted || mutationErr != nil {
		t.Fatalf("staging root mutation attempted=%v completed=%v err=%v", mutationAttempted, mutationCompleted, mutationErr)
	}
	if createdBase == "" {
		t.Fatal("staging creation was not observed")
	}
	assertPrepareError(t, err, game.ID, protocol.CodeTransferFailed, staging)
	if _, err := os.Lstat(filepath.Join(moved, createdBase)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("partial artifact remains in validated root: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(staging, createdBase)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("partial artifact escaped into replacement root: %v", err)
	}
	assertStagingEmpty(t, moved)
	assertStagingEmpty(t, staging)
}

func TestPrepareInitialStatFailureRemovesCreatedIdentityNotSameNameReplacement(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("content"))
	staging := t.TempDir()
	mutationAttempted := false
	mutationCompleted := false
	var mutationErr error
	partialPath := ""
	decoyPath := ""
	preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	preparer.statStagedFile = func(file *os.File) (fs.FileInfo, error) {
		mutationAttempted = true
		info, err := file.Stat()
		if err != nil {
			mutationErr = err
			return nil, errors.New("injected initial staging stat failure")
		}
		decoyPath = file.Name()
		partialPath = decoyPath + ".partial"
		if err := os.Rename(decoyPath, partialPath); err != nil {
			mutationErr = err
			return nil, errors.New("injected initial staging stat failure")
		}
		if err := os.WriteFile(decoyPath, []byte("same-name-decoy"), 0o600); err != nil {
			mutationErr = err
			return nil, errors.New("injected initial staging stat failure")
		}
		mutationCompleted = true
		return info, errors.New("injected initial staging stat failure")
	}

	_, err := preparer.Prepare(context.Background(), root, game)
	if !mutationAttempted || !mutationCompleted || mutationErr != nil {
		t.Fatalf("staging entry mutation attempted=%v completed=%v err=%v", mutationAttempted, mutationCompleted, mutationErr)
	}
	assertPrepareError(t, err, game.ID, protocol.CodeTransferFailed, staging)
	if _, err := os.Lstat(partialPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("actual partial artifact remains after failure: %v", err)
	}
	decoy, err := os.ReadFile(decoyPath)
	if err != nil || string(decoy) != "same-name-decoy" {
		t.Fatalf("same-name replacement = %q, err=%v", decoy, err)
	}
}

func TestPrepareRejectsStagingModeChangesBeforeFinalVerification(t *testing.T) {
	tests := []struct {
		name     string
		wantMode fs.FileMode
		mutate   func(string, string) error
	}{
		{
			name:     "root becomes group accessible",
			wantMode: 0o750,
			mutate: func(rootPath, _ string) error {
				return os.Chmod(rootPath, 0o750)
			},
		},
		{
			name:     "file becomes group readable",
			wantMode: 0o640,
			mutate: func(_, filePath string) error {
				return os.Chmod(filePath, 0o640)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, game := rawFixture(t, "game.sfc", bytes.Repeat([]byte("content"), 1024))
			staging := t.TempDir()
			preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
			mutationAttempted := false
			mutationCompleted := false
			var mutationErr error
			var observedMode fs.FileMode
			preparer.beforeStagingFinalVerify = func(rootPath, filePath string) error {
				mutationAttempted = true
				mutationErr = test.mutate(rootPath, filePath)
				if mutationErr != nil {
					return nil
				}
				mutatedPath := filePath
				if test.wantMode == 0o750 {
					mutatedPath = rootPath
				}
				info, err := os.Lstat(mutatedPath)
				if err != nil {
					mutationErr = err
					return nil
				}
				observedMode = info.Mode().Perm()
				mutationCompleted = true
				return nil
			}

			prepared, err := preparer.Prepare(context.Background(), root, game)
			if prepared != nil {
				t.Cleanup(func() { _ = prepared.Remove() })
			}
			if !mutationAttempted || !mutationCompleted || mutationErr != nil {
				t.Fatalf("mode mutation attempted=%v completed=%v err=%v", mutationAttempted, mutationCompleted, mutationErr)
			}
			if observedMode != test.wantMode {
				t.Fatalf("mutated mode = %#o, want %#o", observedMode, test.wantMode)
			}
			assertPrepareError(t, err, game.ID, protocol.CodeTransferFailed, staging)
			assertStagingEmpty(t, staging)
			if test.wantMode == 0o750 {
				info, statErr := os.Stat(staging)
				if statErr != nil || info.Mode().Perm() != 0o750 {
					t.Fatalf("final staging root mode = %v, err=%v, want 0750", info, statErr)
				}
			}
		})
	}
}

func TestPrepareChmodsHeldStagingRootWithoutFollowingReplacementSymlink(t *testing.T) {
	root, game := rawFixture(t, "game.sfc", []byte("content"))
	parent := t.TempDir()
	staging := filepath.Join(parent, "staging")
	moved := filepath.Join(parent, "moved-staging")
	external := filepath.Join(parent, "external")
	if err := os.Mkdir(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(external, 0o755); err != nil {
		t.Fatal(err)
	}
	preparer := Preparer{StagingRoot: staging, MaxBytes: protocol.MaxContentBytes}
	mutationAttempted := false
	mutationCompleted := false
	var mutationErr error
	preparer.beforeStagingRootChmod = func(rootPath string) error {
		mutationAttempted = true
		if err := os.Rename(rootPath, moved); err != nil {
			mutationErr = err
			return nil
		}
		if err := os.Symlink(external, rootPath); err != nil {
			mutationErr = err
			return nil
		}
		mutationCompleted = true
		return nil
	}

	prepared, err := preparer.Prepare(context.Background(), root, game)
	if prepared != nil {
		t.Cleanup(func() { _ = prepared.Remove() })
	}
	if !mutationAttempted || !mutationCompleted || mutationErr != nil {
		t.Fatalf("root replacement attempted=%v completed=%v err=%v", mutationAttempted, mutationCompleted, mutationErr)
	}
	assertPrepareError(t, err, game.ID, protocol.CodeTransferFailed, staging)
	stagingInfo, err := os.Lstat(staging)
	if err != nil || stagingInfo.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("configured staging path mode = %v, err=%v, want symlink replacement", stagingInfo, err)
	}
	externalInfo, err := os.Stat(external)
	if err != nil {
		t.Fatal(err)
	}
	if got := externalInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("replacement symlink target mode = %#o, want unchanged 0755", got)
	}
	assertStagingEmpty(t, moved)
	assertStagingEmpty(t, external)
}

type mutatingSourceFile struct {
	*os.File
	path  string
	mtime time.Time
	once  sync.Once
}

func (f *mutatingSourceFile) Read(buffer []byte) (int, error) {
	n, err := f.File.Read(buffer)
	f.once.Do(func() {
		data := bytes.Repeat([]byte("b"), 256<<10)
		if writeErr := os.WriteFile(f.path, data, 0o600); writeErr == nil {
			_ = os.Chtimes(f.path, f.mtime, f.mtime)
		}
	})
	return n, err
}

type zipEntry struct {
	name string
	body []byte
}

func zipBytes(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := member.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func patchCentralUncompressedSize(t *testing.T, archive []byte, size uint32) {
	t.Helper()
	offset := bytes.Index(archive, []byte{'P', 'K', 1, 2})
	if offset < 0 {
		t.Fatal("central directory header not found")
	}
	binary.LittleEndian.PutUint32(archive[offset+24:offset+28], size)
}

func rawFixture(t *testing.T, relativePath string, body []byte) (catalog.Root, catalog.Game) {
	t.Helper()
	library := t.TempDir()
	source := filepath.Join(library, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return catalogFixture(t, library, relativePath, catalog.SourceKindRaw, "", 0, 0, 0)
}

func zipFixture(t *testing.T, relativePath string, archive []byte, memberName string) (catalog.Root, catalog.Game) {
	t.Helper()
	library := t.TempDir()
	source := filepath.Join(library, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range reader.File {
		if member.Name == memberName {
			return catalogFixture(t, library, relativePath, catalog.SourceKindZIP, memberName, int64(member.UncompressedSize64), member.CRC32, len(reader.File))
		}
	}
	t.Fatalf("ZIP member %q not found", memberName)
	return catalog.Root{}, catalog.Game{}
}

func catalogFixture(t *testing.T, library, relativePath string, kind catalog.SourceKind, member string, memberSize int64, memberCRC uint32, entryCount ...int) (catalog.Root, catalog.Game) {
	t.Helper()
	info, err := os.Stat(filepath.Join(library, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	root := catalog.Root{ID: "snes-library", System: protocol.SystemSNES, Path: library}
	count := 0
	if len(entryCount) > 0 {
		count = entryCount[0]
	}
	game := catalog.Game{
		ID: "snes-synthetic-game", Title: "Synthetic Game", LibraryID: root.ID,
		RelativePath: relativePath, System: root.System, Kind: kind,
		State: catalog.SourceStateAvailable, RootOnline: true,
		Fingerprint: catalog.Fingerprint{
			SourceSize: info.Size(), ModifiedNS: info.ModTime().UnixNano(), ZIPMember: member,
			ZIPSize: memberSize, ZIPCRC32: memberCRC, ZIPEntryCount: count,
		},
	}
	return root, game
}

func assertSecureStagingPath(t *testing.T, stagingRoot, path string) {
	t.Helper()
	relative, err := filepath.Rel(stagingRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("staging path %q is not a child of configured root %q", path, stagingRoot)
	}
	rootInfo, err := os.Stat(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("staging root mode = %v, want directory 0700", rootInfo.Mode())
	}
	fileInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("staging file mode = %v, want regular 0600", fileInfo.Mode())
	}
}

func assertPrepareError(t *testing.T, err error, gameID string, code protocol.ErrorCode, privatePath string) {
	t.Helper()
	if err == nil {
		t.Fatal("Prepare succeeded, want error")
	}
	var prepareErr *Error
	if !errors.As(err, &prepareErr) {
		t.Fatalf("error type = %T, want *romsource.Error", err)
	}
	if prepareErr.GameID != gameID || prepareErr.Code != code {
		t.Fatalf("error = %+v, want game=%q code=%q", prepareErr, gameID, code)
	}
	message := err.Error()
	if !strings.Contains(message, gameID) || !strings.Contains(message, string(code)) {
		t.Fatalf("error message %q does not expose game ID and typed code", message)
	}
	if privatePath != "" && strings.Contains(message, privatePath) {
		t.Fatalf("error message exposes private source path: %q", message)
	}
}

func assertStagingEmpty(t *testing.T, stagingRoot string) {
	t.Helper()
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging root contains %d entries after failure: %v", len(entries), entries)
	}
}
