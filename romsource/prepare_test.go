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
