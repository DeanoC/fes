package targetcache_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

func TestPutRejectsInvalidIdentityBeforeReadingOrCreatingPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		system   protocol.System
		identity protocol.ContentIdentity
		code     protocol.ErrorCode
	}{
		{name: "zero length", system: protocol.SystemSNES, identity: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 0, Extension: "sfc"}, code: protocol.CodeBadRequest},
		{name: "oversized length", system: protocol.SystemSNES, identity: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: protocol.MaxContentBytes + 1, Extension: "sfc"}, code: protocol.CodeBadRequest},
		{name: "unsupported extension", system: protocol.SystemSNES, identity: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 1, Extension: "md"}, code: protocol.CodeUnsupportedSystem},
		{name: "unsupported system", system: "unknown", identity: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 1, Extension: "bin"}, code: protocol.CodeUnsupportedSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			manager := openUploadManager(t, uploadManagerConfig(root, 64<<20))
			reader := &countingReader{reader: strings.NewReader("private-rom-byte")}

			response, apiErr := manager.Put(context.Background(), tt.system, tt.identity, reader)

			assertSafeAPIError(t, apiErr, tt.code, root, "private-rom-byte")
			if response != (protocol.CacheUploadResponse{}) {
				t.Fatalf("Put response = %#v, want zero response", response)
			}
			if reader.reads.Load() != 0 {
				t.Fatalf("invalid upload read body %d times", reader.reads.Load())
			}
			assertNoUploadParts(t, root)
		})
	}
}

func TestPutRejectsBodyFailuresDigestMismatchAndCleansOwnPart(t *testing.T) {
	t.Parallel()

	content := []byte("synthetic upload body")
	identity := contentIdentity(content, "sfc")
	tests := []struct {
		name   string
		body   io.Reader
		mutate func(protocol.ContentIdentity) protocol.ContentIdentity
		code   protocol.ErrorCode
	}{
		{name: "short body", body: bytes.NewReader(content[:len(content)-1]), code: protocol.CodeTransferFailed},
		{name: "one excess byte", body: bytes.NewReader(append(append([]byte(nil), content...), '!')), code: protocol.CodeTransferFailed},
		{name: "reader failure", body: &failingReader{data: content[:5], err: errors.New("private source read failure")}, code: protocol.CodeTransferFailed},
		{name: "digest mismatch", body: bytes.NewReader([]byte("same-length-wrong-body!")), mutate: func(got protocol.ContentIdentity) protocol.ContentIdentity {
			got.Size = int64(len("same-length-wrong-body!"))
			return got
		}, code: protocol.CodeDigestMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			manager := openUploadManager(t, uploadManagerConfig(root, 64<<20))
			declared := identity
			if tt.mutate != nil {
				declared = tt.mutate(declared)
			}

			response, apiErr := manager.Put(context.Background(), protocol.SystemSNES, declared, tt.body)

			assertSafeAPIError(t, apiErr, tt.code, root, string(content), "private source read failure")
			if response != (protocol.CacheUploadResponse{}) {
				t.Fatalf("Put response = %#v, want zero response", response)
			}
			assertNoUploadParts(t, root)
			assertDestinationAbsent(t, root, protocol.SystemSNES, declared)
			if manager.Usage() != 0 {
				t.Fatalf("Usage after failed upload = %d, want 0", manager.Usage())
			}
		})
	}
}

func TestPutBodyFailureNeverEvictsExistingVerifiedContent(t *testing.T) {
	t.Parallel()

	uploadBytes := []byte("incoming-content")
	uploadIdentity := contentIdentity(uploadBytes, "sfc")
	tests := []struct {
		name       string
		body       func(context.CancelFunc) io.Reader
		identity   protocol.ContentIdentity
		wantCode   protocol.ErrorCode
		useContext bool
	}{
		{name: "short body", body: func(context.CancelFunc) io.Reader { return bytes.NewReader(uploadBytes[:len(uploadBytes)-1]) }, identity: uploadIdentity, wantCode: protocol.CodeTransferFailed},
		{name: "reader error", body: func(context.CancelFunc) io.Reader {
			return &failingReader{data: uploadBytes[:4], err: errors.New("private body failure")}
		}, identity: uploadIdentity, wantCode: protocol.CodeTransferFailed},
		{name: "digest mismatch", body: func(context.CancelFunc) io.Reader { return bytes.NewReader([]byte("wrong-body-bytes")) }, identity: uploadIdentity, wantCode: protocol.CodeDigestMismatch},
		{name: "cancellation", body: func(cancel context.CancelFunc) io.Reader {
			return &cancelAfterReadReader{reader: bytes.NewReader(uploadBytes), cancel: cancel}
		}, identity: uploadIdentity, wantCode: protocol.CodeTransferFailed, useContext: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			victimBytes := []byte("verified-victim")
			victim := contentIdentity(victimBytes, "sfc")
			victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
			maxBytes := int64(len(victimBytes)) + tt.identity.Size - 1
			manager := openUploadManager(t, uploadManagerConfig(root, maxBytes), targetcache.WithSpaceProbe(unlimitedSpace))
			probe, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, victim.Key())
			if apiErr != nil || !probe.Present {
				t.Fatalf("prime verified victim: response=%#v error=%v", probe, apiErr)
			}
			ctx := context.Background()
			cancel := func() {}
			if tt.useContext {
				var cancelContext context.CancelFunc
				ctx, cancelContext = context.WithCancel(ctx)
				cancel = cancelContext
				defer cancelContext()
			}

			_, apiErr = manager.Put(ctx, protocol.SystemSNES, tt.identity, tt.body(cancel))

			assertSafeAPIError(t, apiErr, tt.wantCode, root, victimPath, string(victimBytes), "private body failure")
			got, err := os.ReadFile(victimPath)
			if err != nil || !bytes.Equal(got, victimBytes) {
				t.Fatalf("failed upload changed verified victim: bytes=%q error=%v", got, err)
			}
			if gotUsage := manager.Usage(); gotUsage != int64(len(victimBytes)) {
				t.Fatalf("Usage after failed upload = %d, want retained victim size %d", gotUsage, len(victimBytes))
			}
			assertNoUploadParts(t, root)
		})
	}
}

func TestPutPublishesPrivateVerifiedContentAndIsIdempotentAcrossRestart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("created cache content")
	identity := contentIdentity(content, "sfc")
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20))

	created, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, bytes.NewReader(content))
	if apiErr != nil {
		t.Fatalf("Put created: %v", apiErr)
	}
	assertUploadResponse(t, created, protocol.CacheUploadCreated, protocol.SystemSNES, identity)
	path := cacheDestination(root, protocol.SystemSNES, identity)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("published bytes = %q, want %q", got, content)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("published mode = %v, want regular 0600", info.Mode())
	}
	assertNoUploadParts(t, root)
	if manager.Usage() != identity.Size {
		t.Fatalf("Usage = %d, want %d", manager.Usage(), identity.Size)
	}

	manager = openUploadManager(t, uploadManagerConfig(root, 64<<20))
	reader := &countingReader{reader: strings.NewReader("must not be read")}
	present, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, reader)
	if apiErr != nil {
		t.Fatalf("Put present: %v", apiErr)
	}
	assertUploadResponse(t, present, protocol.CacheUploadPresent, protocol.SystemSNES, identity)
	if reader.reads.Load() != 0 {
		t.Fatalf("idempotent Put read body %d times", reader.reads.Load())
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("idempotent Put changed content: bytes=%q error=%v", got, err)
	}
}

func TestPutAcceptsFinalBytesReturnedWithEOF(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("final bytes with EOF")
	identity := contentIdentity(content, "sfc")
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(unlimitedSpace))

	response, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, &finalEOFReader{data: content})

	if apiErr != nil {
		t.Fatalf("Put final bytes with EOF: %v", apiErr)
	}
	assertUploadResponse(t, response, protocol.CacheUploadCreated, protocol.SystemSNES, identity)
}

func TestPutRejectsOccupiedDestinationWithoutChangingIt(t *testing.T) {
	t.Parallel()

	content := []byte("wanted bytes")
	identity := contentIdentity(content, "sfc")
	tests := []struct {
		name  string
		make  func(*testing.T, string)
		check func(*testing.T, string)
	}{
		{
			name: "wrong regular content",
			make: func(t *testing.T, path string) {
				writeNamedFile(t, filepath.Dir(path), filepath.Base(path), []byte("wrong bytes!"))
			},
			check: func(t *testing.T, path string) {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != "wrong bytes!" {
					t.Fatalf("conflict changed: bytes=%q error=%v", got, err)
				}
			},
		},
		{
			name: "symlink",
			make: func(t *testing.T, path string) {
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("conflicting symlink changed: info=%v error=%v", info, err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			path := cacheDestination(root, protocol.SystemSNES, identity)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			tt.make(t, path)
			manager := openUploadManager(t, uploadManagerConfig(root, 64<<20))

			_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, bytes.NewReader(content))

			assertSafeAPIError(t, apiErr, protocol.CodeTransferFailed, root, path, string(content))
			tt.check(t, path)
			assertNoUploadParts(t, root)
		})
	}
}

func TestPutReservesAbsoluteCeilingAndFreeSpaceBeforeReadingOrPartCreation(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		maxBytes  int64
		available int64
	}{
		{name: "absolute ceiling", maxBytes: 12, available: 1 << 30},
		{name: "actual free space", maxBytes: 1 << 30, available: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, string(protocol.SystemSNES)), 0o700); err != nil {
				t.Fatal(err)
			}
			stale := writeNamedFile(t, filepath.Join(root, string(protocol.SystemSNES)), ".fogcast-retained.part", []byte("counted"))
			content := []byte("new content")
			identity := contentIdentity(content, "sfc")
			reader := &countingReader{reader: bytes.NewReader(content)}
			manager := openUploadManager(t, uploadManagerConfig(root, tt.maxBytes), targetcache.WithSpaceProbe(func(string) (int64, error) {
				return tt.available, nil
			}))

			_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, reader)

			assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, stale, string(content))
			if reader.reads.Load() != 0 {
				t.Fatalf("capacity failure read body %d times", reader.reads.Load())
			}
			got, err := os.ReadFile(stale)
			if err != nil || string(got) != "counted" {
				t.Fatalf("capacity check mutated policy-A stale part: bytes=%q error=%v", got, err)
			}
			assertNoUploadParts(t, root)
		})
	}
}

func TestPutRefreshesAbsoluteAccountingForEntriesAddedAfterOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("new content")
	identity := contentIdentity(content, "sfc")
	manager := openUploadManager(t, uploadManagerConfig(root, int64(len(content))), targetcache.WithSpaceProbe(unlimitedSpace))
	late := writeNamedFile(t, filepath.Join(root, string(protocol.SystemSNES)), "late-operator-file", []byte("late"))
	reader := &countingReader{reader: bytes.NewReader(content)}

	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, reader)

	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, late)
	if reader.reads.Load() != 0 {
		t.Fatalf("absolute accounting failure read body %d times", reader.reads.Load())
	}
	if got := manager.Usage(); got != int64(len("late")) {
		t.Fatalf("refreshed Usage = %d, want %d", got, len("late"))
	}
}

func TestPutEvictsVerifiedOldestThenFilenameAndRetainsUnverifiedInvalidEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldA := contentIdentity([]byte("old-a"), "sfc")
	oldB := contentIdentity([]byte("old-b"), "sfc")
	newer := contentIdentity([]byte("newer"), "sfc")
	paths := map[protocol.ContentIdentity]string{
		oldA:  writeCacheFile(t, root, protocol.SystemSNES, oldA, []byte("old-a")),
		oldB:  writeCacheFile(t, root, protocol.SystemSNES, oldB, []byte("old-b")),
		newer: writeCacheFile(t, root, protocol.SystemSNES, newer, []byte("newer")),
	}
	tie := time.Unix(100, 0)
	for _, identity := range []protocol.ContentIdentity{oldA, oldB} {
		if err := os.Chtimes(paths[identity], tie, tie); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(paths[newer], tie.Add(time.Hour), tie.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	unfamiliar := writeNamedFile(t, filepath.Join(root, string(protocol.SystemSNES)), "operator-note", []byte("invalid-retained"))
	manager := openUploadManager(t, uploadManagerConfig(root, int64(len("old-a")+len("old-b")+len("newer")+len("invalid-retained")+len("uploaded")-1)), targetcache.WithSpaceProbe(unlimitedSpace))
	for identity := range paths {
		response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
		if apiErr != nil || !response.Present {
			t.Fatalf("prime verified victim %#v: response=%#v error=%v", identity, response, apiErr)
		}
	}
	uploaded := contentIdentity([]byte("uploaded"), "sfc")

	response, apiErr := manager.Put(context.Background(), protocol.SystemSNES, uploaded, strings.NewReader("uploaded"))
	if apiErr != nil {
		t.Fatalf("Put with eviction: %v", apiErr)
	}
	assertUploadResponse(t, response, protocol.CacheUploadCreated, protocol.SystemSNES, uploaded)

	wantEvicted := oldA
	if filepath.Base(paths[oldB]) < filepath.Base(paths[oldA]) {
		wantEvicted = oldB
	}
	for identity, path := range paths {
		_, err := os.Lstat(path)
		if identity == wantEvicted {
			if !os.IsNotExist(err) {
				t.Fatalf("filename-tie oldest victim still exists: %v", err)
			}
		} else if err != nil {
			t.Fatalf("non-victim %q removed: %v", filepath.Base(path), err)
		}
	}
	got, err := os.ReadFile(unfamiliar)
	if err != nil || string(got) != "invalid-retained" {
		t.Fatalf("unfamiliar entry changed: bytes=%q error=%v", got, err)
	}
}

func TestPutReturnsCacheFullWhenOnlyUnsafeVictimsRemain(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wrongBody := []byte("wrong-body")
	wouldBe := contentIdentity([]byte("right-body"), "sfc")
	wrongPath := writeCacheFile(t, root, protocol.SystemSNES, wouldBe, wrongBody)
	unfamiliar := writeNamedFile(t, filepath.Join(root, string(protocol.SystemSNES)), "keep.dat", []byte("keep"))
	manager := openUploadManager(t, uploadManagerConfig(root, int64(len(wrongBody)+len("keep")+2)), targetcache.WithSpaceProbe(unlimitedSpace))
	probe, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, wouldBe.Key())
	if apiErr != nil || probe.Present {
		t.Fatalf("digest mismatch probe = %#v, %v", probe, apiErr)
	}
	newContent := []byte("new")
	newIdentity := contentIdentity(newContent, "sfc")

	_, apiErr = manager.Put(context.Background(), protocol.SystemSNES, newIdentity, bytes.NewReader(newContent))

	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, wrongPath, unfamiliar)
	for _, path := range []string{wrongPath, unfamiliar} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("unsafe victim was removed: %v", err)
		}
	}
}

func TestPutPersistentLowSpaceNeverPartiallyDrainsVerifiedVictims(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	victimBytes := []byte("small-victim")
	victim := contentIdentity(victimBytes, "sfc")
	victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(func(string) (int64, error) {
		return 0, nil
	}))
	probe, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, victim.Key())
	if apiErr != nil || !probe.Present {
		t.Fatalf("prime low-space victim: response=%#v error=%v", probe, apiErr)
	}
	uploadBytes := []byte("incoming-content-is-larger")
	upload := contentIdentity(uploadBytes, "sfc")
	body := &countingReader{reader: bytes.NewReader(uploadBytes)}

	_, apiErr = manager.Put(context.Background(), protocol.SystemSNES, upload, body)

	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, victimPath, string(victimBytes))
	got, err := os.ReadFile(victimPath)
	if err != nil || !bytes.Equal(got, victimBytes) {
		t.Fatalf("inevitable CACHE_FULL changed victim: bytes=%q error=%v", got, err)
	}
	if body.reads.Load() != 0 {
		t.Fatalf("inevitable CACHE_FULL read body %d times", body.reads.Load())
	}
}

func TestPutCancellationDuringColdDestinationVerificationIsTransferFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("cold destination cancellation")
	identity := contentIdentity(content, "sfc")
	destination := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	ctx := &switchCancelContext{Context: context.Background()}
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		ctx.canceled.Store(true)
		return os.Open(candidate)
	}))
	body := &countingReader{reader: bytes.NewReader(content)}

	_, apiErr := manager.Put(ctx, protocol.SystemSNES, identity, body)

	assertSafeAPIError(t, apiErr, protocol.CodeTransferFailed, root, destination, string(content))
	if body.reads.Load() != 0 {
		t.Fatalf("canceled destination verification read upload body %d times", body.reads.Load())
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("canceled destination verification changed content: bytes=%q error=%v", got, err)
	}
}

func TestPutCancellationDuringColdVictimVerificationIsTransferFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	victimBytes := []byte("cold eviction victim")
	victim := contentIdentity(victimBytes, "sfc")
	victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
	ctx := &switchCancelContext{Context: context.Background()}
	manager := openUploadManager(t, uploadManagerConfig(root, int64(len(victimBytes))), targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		ctx.canceled.Store(true)
		return os.Open(candidate)
	}))
	newBytes := []byte("new")
	newIdentity := contentIdentity(newBytes, "sfc")
	body := &countingReader{reader: bytes.NewReader(newBytes)}

	_, apiErr := manager.Put(ctx, protocol.SystemSNES, newIdentity, body)

	assertSafeAPIError(t, apiErr, protocol.CodeTransferFailed, root, victimPath, string(victimBytes))
	if body.reads.Load() != 0 {
		t.Fatalf("canceled victim verification read upload body %d times", body.reads.Load())
	}
	got, err := os.ReadFile(victimPath)
	if err != nil || !bytes.Equal(got, victimBytes) {
		t.Fatalf("canceled victim verification changed content: bytes=%q error=%v", got, err)
	}
}

func TestPutEvictsForActualFreeSpaceEvenWhenCeilingHasRoom(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	victimBytes := []byte("free-space-victim")
	victim := contentIdentity(victimBytes, "sfc")
	victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
	var probes atomic.Int64
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(func(string) (int64, error) {
		probes.Add(1)
		return 0, nil
	}))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, victim.Key())
	if apiErr != nil || !response.Present {
		t.Fatalf("prime free-space victim: response=%#v error=%v", response, apiErr)
	}
	content := []byte("new")
	identity := contentIdentity(content, "sfc")

	if _, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, bytes.NewReader(content)); apiErr != nil {
		t.Fatalf("Put after free-space eviction: %v", apiErr)
	}
	if _, err := os.Lstat(victimPath); !os.IsNotExist(err) {
		t.Fatalf("free-space victim still exists: %v", err)
	}
	if probes.Load() < 2 {
		t.Fatalf("space probe calls = %d, want recheck after eviction", probes.Load())
	}
}

func TestConcurrentPutSerializesBodiesWhileProbeRemainsAvailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	existingContent := []byte("probe-while-upload")
	existing := contentIdentity(existingContent, "sfc")
	writeCacheFile(t, root, protocol.SystemSNES, existing, existingContent)
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(unlimitedSpace))
	probe, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, existing.Key())
	if apiErr != nil || !probe.Present {
		t.Fatalf("prime Probe = %#v, %v", probe, apiErr)
	}

	firstBytes := []byte("first-upload")
	firstIdentity := contentIdentity(firstBytes, "sfc")
	firstReader := newBlockingReader(firstBytes)
	secondBytes := []byte("second-upload")
	secondIdentity := contentIdentity(secondBytes, "sfc")
	secondReader := &notifyingReader{reader: bytes.NewReader(secondBytes), entered: make(chan struct{})}
	firstDone := make(chan *protocol.APIError, 1)
	go func() {
		_, putErr := manager.Put(context.Background(), protocol.SystemSNES, firstIdentity, firstReader)
		firstDone <- putErr
	}()
	<-firstReader.entered
	secondDone := make(chan *protocol.APIError, 1)
	go func() {
		_, putErr := manager.Put(context.Background(), protocol.SystemSNES, secondIdentity, secondReader)
		secondDone <- putErr
	}()

	select {
	case <-secondReader.entered:
		t.Fatal("second upload body was read before first upload finished")
	case <-time.After(50 * time.Millisecond):
	}
	probeDone := make(chan *protocol.APIError, 1)
	go func() {
		response, probeErr := manager.Probe(context.Background(), protocol.SystemSNES, existing.Key())
		if probeErr == nil && !response.Present {
			probeErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "probe unexpectedly absent"}
		}
		probeDone <- probeErr
	}()
	select {
	case probeErr := <-probeDone:
		if probeErr != nil {
			t.Fatalf("Probe during upload: %v", probeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Probe blocked behind upload body")
	}

	close(firstReader.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Put: %v", err)
	}
	select {
	case <-secondReader.entered:
	case <-time.After(time.Second):
		t.Fatal("second upload did not begin after first completed")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Put: %v", err)
	}
}

func TestPutCancellationWhileQueuedDoesNotReadBody(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(unlimitedSpace))
	firstBytes := []byte("blocking-upload")
	firstReader := newBlockingReader(firstBytes)
	firstDone := make(chan *protocol.APIError, 1)
	go func() {
		_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(firstBytes, "sfc"), firstReader)
		firstDone <- apiErr
	}()
	<-firstReader.entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	secondReader := &countingReader{reader: strings.NewReader("queued")}
	_, apiErr := manager.Put(ctx, protocol.SystemSNES, contentIdentity([]byte("queued"), "sfc"), secondReader)
	assertSafeAPIError(t, apiErr, protocol.CodeTransferFailed, root)
	if secondReader.reads.Load() != 0 {
		t.Fatalf("canceled queued upload read body %d times", secondReader.reads.Load())
	}
	close(firstReader.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Put: %v", err)
	}
}

func TestPutCancellationAfterPartCreationCleansWithoutPublicationOrEviction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	victimBytes := []byte("verified victim retained after part cancellation")
	victim := contentIdentity(victimBytes, "sfc")
	victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
	content := []byte("cancel after upload part exists")
	identity := contentIdentity(content, "sfc")
	maximum := int64(len(victimBytes) + len(content) - 1)
	manager := openUploadManager(t, uploadManagerConfig(root, maximum), targetcache.WithSpaceProbe(unlimitedSpace))
	ctx := &cancelWhenUploadPartExistsContext{
		Context:   context.Background(),
		directory: filepath.Join(root, string(protocol.SystemSNES)),
	}

	_, apiErr := manager.Put(ctx, protocol.SystemSNES, identity, bytes.NewReader(content))

	assertSafeAPIError(t, apiErr, protocol.CodeTransferFailed, root, string(content))
	if !ctx.observed.Load() {
		t.Fatal("cancellation hook did not observe an upload part")
	}
	assertNoUploadParts(t, root)
	assertDestinationAbsent(t, root, protocol.SystemSNES, identity)
	got, err := os.ReadFile(victimPath)
	if err != nil || !bytes.Equal(got, victimBytes) {
		t.Fatalf("post-part cancellation changed victim: bytes=%q error=%v", got, err)
	}
}

func TestPutConcurrentVerifiedDestinationWinsWithoutOverwrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("concurrent publication")
	identity := contentIdentity(content, "sfc")
	path := cacheDestination(root, protocol.SystemSNES, identity)
	reader := &publishOnEOFReader{reader: bytes.NewReader(content), publish: func() error {
		return os.WriteFile(path, content, 0o600)
	}}
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(unlimitedSpace))

	response, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, reader)

	if apiErr != nil {
		t.Fatalf("Put concurrent present: %v", apiErr)
	}
	assertUploadResponse(t, response, protocol.CacheUploadPresent, protocol.SystemSNES, identity)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("concurrent destination changed: bytes=%q error=%v", got, err)
	}
	assertNoUploadParts(t, root)
}

func TestPutConcurrentVerifiedDestinationWinsBeforeTightCapacityPlanning(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		victimBytes []byte
	}{
		{name: "no victim"},
		{name: "verified victim remains", victimBytes: []byte("unrelated verified victim with enough reclaimable bytes")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			content := []byte("tight concurrent publication")
			identity := contentIdentity(content, "sfc")
			var victimPath string
			if len(tt.victimBytes) > 0 {
				victim := contentIdentity(tt.victimBytes, "sfc")
				victimPath = writeCacheFile(t, root, protocol.SystemSNES, victim, tt.victimBytes)
			}
			maximum := int64(len(content) + len(tt.victimBytes))
			manager := openUploadManager(t, uploadManagerConfig(root, maximum), targetcache.WithSpaceProbe(unlimitedSpace))
			destination := cacheDestination(root, protocol.SystemSNES, identity)
			reader := &publishOnEOFReader{reader: bytes.NewReader(content), publish: func() error {
				return os.WriteFile(destination, content, 0o600)
			}}

			response, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, reader)

			if apiErr != nil {
				t.Fatalf("Put concurrent destination at tight ceiling: %v", apiErr)
			}
			assertUploadResponse(t, response, protocol.CacheUploadPresent, protocol.SystemSNES, identity)
			got, err := os.ReadFile(destination)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("concurrent destination changed: bytes=%q error=%v", got, err)
			}
			if victimPath != "" {
				got, err := os.ReadFile(victimPath)
				if err != nil || !bytes.Equal(got, tt.victimBytes) {
					t.Fatalf("concurrent destination displaced victim: bytes=%q error=%v", got, err)
				}
			}
			assertNoUploadParts(t, root)
		})
	}
}

func TestPutNeverMutatesRetainedStaleParts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := writeNamedFile(t, directory, ".fogcast-prior-process.part", []byte("policy-a-retained"))
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20), targetcache.WithSpaceProbe(unlimitedSpace))
	content := []byte("clean retry")
	identity := contentIdentity(content, "sfc")

	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, identity, bytes.NewReader(content))
	if apiErr != nil {
		t.Fatalf("Put: %v", apiErr)
	}
	got, err := os.ReadFile(stale)
	if err != nil || string(got) != "policy-a-retained" {
		t.Fatalf("Put mutated stale part: bytes=%q error=%v", got, err)
	}
}

func TestPutNeverEvictsThroughDetachedOrLinkedSystemDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	victimBytes := []byte("verified-victim")
	victim := contentIdentity(victimBytes, "sfc")
	victimPath := writeCacheFile(t, root, protocol.SystemSNES, victim, victimBytes)
	manager := openUploadManager(t, uploadManagerConfig(root, int64(len(victimBytes))), targetcache.WithSpaceProbe(unlimitedSpace))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, victim.Key())
	if apiErr != nil || !response.Present {
		t.Fatalf("prime victim = %#v, %v", response, apiErr)
	}
	systemDirectory := filepath.Dir(victimPath)
	movedDirectory := systemDirectory + "-detached"
	if err := os.Rename(systemDirectory, movedDirectory); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := writeNamedFile(t, outside, filepath.Base(victimPath), []byte("outside-sentinel"))
	if err := os.Symlink(outside, systemDirectory); err != nil {
		t.Fatal(err)
	}
	newBytes := []byte("new")
	newIdentity := contentIdentity(newBytes, "sfc")

	_, apiErr = manager.Put(context.Background(), protocol.SystemSNES, newIdentity, bytes.NewReader(newBytes))

	assertSafeAPIError(t, apiErr, protocol.CodeInternal, root, movedDirectory, outside, sentinel)
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "outside-sentinel" {
		t.Fatalf("outside entry changed: bytes=%q error=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(movedDirectory, filepath.Base(victimPath))); err != nil || !bytes.Equal(got, victimBytes) {
		t.Fatalf("detached victim changed: bytes=%q error=%v", got, err)
	}
}

func uploadManagerConfig(root string, maxBytes int64) targetcache.Config {
	return targetcache.Config{
		Root:         root,
		ActiveRecord: filepath.Join(filepath.Dir(root), "run", filepath.Base(root)+"-active.json"),
		MaxBytes:     maxBytes,
	}
}

func openUploadManager(t *testing.T, config targetcache.Config, options ...targetcache.Option) *targetcache.Manager {
	t.Helper()
	options = append([]targetcache.Option{
		targetcache.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}, options...)
	manager, err := targetcache.Open(config, options...)
	if err != nil {
		t.Fatalf("Open target cache: %v", err)
	}
	return manager
}

func unlimitedSpace(string) (int64, error) { return 1 << 40, nil }

func cacheDestination(root string, system protocol.System, identity protocol.ContentIdentity) string {
	return filepath.Join(root, string(system), identity.SHA256+"."+identity.Extension)
}

func assertUploadResponse(t *testing.T, response protocol.CacheUploadResponse, result protocol.CacheUploadResult, system protocol.System, identity protocol.ContentIdentity) {
	t.Helper()
	if response.Result != result || response.System != system || response.Content != identity {
		t.Fatalf("upload response = %#v, want result=%q system=%q content=%#v", response, result, system, identity)
	}
}

func assertNoUploadParts(t *testing.T, root string) {
	t.Helper()
	for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
		entries, err := os.ReadDir(filepath.Join(root, string(system)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".fogcast-upload-") && strings.HasSuffix(entry.Name(), ".part") {
				t.Fatalf("upload part remains after Put: %s", entry.Name())
			}
		}
	}
}

func assertDestinationAbsent(t *testing.T, root string, system protocol.System, identity protocol.ContentIdentity) {
	t.Helper()
	if _, err := os.Lstat(cacheDestination(root, system, identity)); !os.IsNotExist(err) {
		t.Fatalf("failed upload published destination: %v", err)
	}
}

type countingReader struct {
	reader io.Reader
	reads  atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads.Add(1)
	return r.reader.Read(p)
}

type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

type blockingReader struct {
	data    []byte
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingReader(data []byte) *blockingReader {
	return &blockingReader{data: data, entered: make(chan struct{}), release: make(chan struct{})}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		close(r.entered)
		<-r.release
	})
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

type notifyingReader struct {
	reader  io.Reader
	entered chan struct{}
	once    sync.Once
}

func (r *notifyingReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	return r.reader.Read(p)
}

type publishOnEOFReader struct {
	reader  io.Reader
	publish func() error
	done    bool
}

type finalEOFReader struct {
	data []byte
}

type cancelAfterReadReader struct {
	reader io.Reader
	cancel context.CancelFunc
	once   sync.Once
}

type switchCancelContext struct {
	context.Context
	canceled atomic.Bool
}

type cancelWhenUploadPartExistsContext struct {
	context.Context
	directory string
	observed  atomic.Bool
}

func (c *cancelWhenUploadPartExistsContext) Err() error {
	entries, err := os.ReadDir(c.directory)
	if err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".fogcast-upload-") && strings.HasSuffix(entry.Name(), ".part") {
				c.observed.Store(true)
				return context.Canceled
			}
		}
	}
	return c.Context.Err()
}

func (c *switchCancelContext) Err() error {
	if c.canceled.Load() {
		return context.Canceled
	}
	return c.Context.Err()
}

func (r *cancelAfterReadReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.once.Do(r.cancel)
	return n, err
}

func (r *finalEOFReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}

func (r *publishOnEOFReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if errors.Is(err, io.EOF) && !r.done {
		r.done = true
		if publishErr := r.publish(); publishErr != nil {
			return 0, publishErr
		}
	}
	return n, err
}
