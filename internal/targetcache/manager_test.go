package targetcache_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestInventoryCleansOnlyDirectRegularPartsAndAccountsConservatively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	snes := filepath.Join(root, string(protocol.SystemSNES))
	mega := filepath.Join(root, string(protocol.SystemMegaDrive))
	for _, directory := range []string{snes, mega} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	validContent := []byte("valid-snes")
	valid := contentIdentity(validContent, "sfc")
	validPath := writeCacheFile(t, root, protocol.SystemSNES, valid, validContent)
	mismatchName := contentIdentity([]byte("expected-body"), "sfc")
	mismatchPath := writeCacheFile(t, root, protocol.SystemSNES, mismatchName, []byte("different-body"))
	wrongExtension := writeNamedFile(t, snes, strings.Repeat("1", 64)+".md", []byte("wrong-extension"))
	uppercaseDigest := writeNamedFile(t, snes, strings.Repeat("A", 64)+".sfc", []byte("uppercase"))
	shortDigest := writeNamedFile(t, snes, strings.Repeat("2", 63)+".sfc", []byte("short"))
	uppercaseExtension := writeNamedFile(t, snes, strings.Repeat("3", 64)+".SFC", []byte("upper-extension"))
	unfamiliar := writeNamedFile(t, snes, "README.txt", []byte("leave me"))
	zero := writeNamedFile(t, snes, strings.Repeat("4", 64)+".sfc", nil)
	oversized := filepath.Join(snes, strings.Repeat("5", 64)+".sfc")
	oversizedFile, err := os.OpenFile(oversized, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := oversizedFile.Truncate(protocol.MaxContentBytes + 1); err != nil {
		_ = oversizedFile.Close()
		t.Fatal(err)
	}
	if err := oversizedFile.Close(); err != nil {
		t.Fatal(err)
	}

	stalePart := writeNamedFile(t, snes, ".fogcast-stale.part", []byte("partial"))
	emptyTokenPart := writeNamedFile(t, snes, ".fogcast-.part", []byte("partial without token"))
	nested := filepath.Join(snes, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	nestedPart := writeNamedFile(t, nested, ".fogcast-nested.part", []byte("nested partial"))
	validDirectory := filepath.Join(snes, strings.Repeat("6", 64)+".sfc")
	if err := os.Mkdir(validDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	nestedSentinel := writeNamedFile(t, validDirectory, ".fogcast-sentinel.part", []byte("do not traverse"))

	outside := filepath.Join(t.TempDir(), "outside-rom")
	if err := os.WriteFile(outside, []byte("outside bytes must not be followed"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkedValid := filepath.Join(snes, strings.Repeat("7", 64)+".sfc")
	if err := os.Symlink(outside, linkedValid); err != nil {
		t.Fatal(err)
	}
	linkedPart := filepath.Join(snes, ".fogcast-linked.part")
	if err := os.Symlink(outside, linkedPart); err != nil {
		t.Fatal(err)
	}

	fifo := filepath.Join(snes, strings.Repeat("8", 64)+".sfc")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create FIFO: %v", err)
	}

	rootPart := writeNamedFile(t, root, ".fogcast-root.part", []byte("not an upload location"))
	unknownSystem := filepath.Join(root, "nes")
	if err := os.Mkdir(unknownSystem, 0o700); err != nil {
		t.Fatal(err)
	}
	unknownNestedPart := writeNamedFile(t, unknownSystem, ".fogcast-unknown.part", []byte("unknown system"))
	rootUnfamiliar := writeNamedFile(t, root, "operator-note", []byte("operator data"))

	var openCount int
	manager := openTestManager(t, root, targetcache.WithOpenFile(func(path string) (*os.File, error) {
		openCount++
		return os.Open(path)
	}))
	if openCount != 0 {
		t.Fatalf("Open hashed cache content %d times", openCount)
	}
	for _, path := range []string{stalePart, emptyTokenPart} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("recognized stale part %q still exists: %v", filepath.Base(path), err)
		}
	}
	for _, path := range []string{
		validPath, mismatchPath, wrongExtension, uppercaseDigest, shortDigest, uppercaseExtension,
		unfamiliar, zero, oversized, nested, nestedPart, validDirectory, nestedSentinel,
		linkedValid, linkedPart, fifo, rootPart, unknownSystem, unknownNestedPart, rootUnfamiliar,
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("inventory changed %q: %v", filepath.Base(path), err)
		}
	}

	accounted := []string{
		validPath, mismatchPath, wrongExtension, uppercaseDigest, shortDigest, uppercaseExtension,
		unfamiliar, zero, oversized, nested, validDirectory, linkedValid, linkedPart, fifo,
		rootPart, unknownSystem, rootUnfamiliar,
	}
	var wantUsage int64
	for _, path := range accounted {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 0 {
			wantUsage += info.Size()
		}
	}
	if got := manager.Usage(); got != wantUsage {
		t.Fatalf("Usage() = %d, want direct known sizes %d", got, wantUsage)
	}

	// A fresh manager deterministically reconstructs the same accounting from disk.
	reopened := openTestManager(t, root)
	if got := reopened.Usage(); got != wantUsage {
		t.Fatalf("Usage() after reopen = %d, want %d", got, wantUsage)
	}
}

func TestInventoryExcludesInvalidEntryKindsWithoutOpeningThem(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	snes := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(snes, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	zeroKey := protocol.ContentKey{SHA256: strings.Repeat("a", 64), Extension: "sfc"}
	zeroPath := filepath.Join(snes, zeroKey.SHA256+".sfc")
	if err := os.WriteFile(zeroPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	oversizedKey := protocol.ContentKey{SHA256: strings.Repeat("b", 64), Extension: "sfc"}
	oversizedPath := filepath.Join(snes, oversizedKey.SHA256+".sfc")
	f, err := os.OpenFile(oversizedPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(protocol.MaxContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	symlinkKey := protocol.ContentKey{SHA256: strings.Repeat("c", 64), Extension: "sfc"}
	if err := os.Symlink(outside, filepath.Join(snes, symlinkKey.SHA256+".sfc")); err != nil {
		t.Fatal(err)
	}
	directoryKey := protocol.ContentKey{SHA256: strings.Repeat("d", 64), Extension: "sfc"}
	if err := os.Mkdir(filepath.Join(snes, directoryKey.SHA256+".sfc"), 0o700); err != nil {
		t.Fatal(err)
	}
	fifoKey := protocol.ContentKey{SHA256: strings.Repeat("e", 64), Extension: "sfc"}
	if err := syscall.Mkfifo(filepath.Join(snes, fifoKey.SHA256+".sfc"), 0o600); err != nil {
		t.Fatal(err)
	}

	var openCount int
	manager := openTestManager(t, root, targetcache.WithOpenFile(func(path string) (*os.File, error) {
		openCount++
		return os.Open(path)
	}))
	for _, key := range []protocol.ContentKey{zeroKey, oversizedKey, symlinkKey, directoryKey, fifoKey} {
		response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, key)
		if apiErr != nil {
			t.Fatalf("Probe(%s): %v", key.SHA256, apiErr)
		}
		if response.Present {
			t.Fatalf("Probe(%s) returned invalid entry as present", key.SHA256)
		}
	}
	if openCount != 0 {
		t.Fatalf("invalid entries were opened %d times", openCount)
	}
}

func TestInventoryLogsSanitizedInvalidEntryDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	snes := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(snes, 0o700); err != nil {
		t.Fatal(err)
	}
	privateName := "private-operator-note"
	privateContent := "private synthetic bytes"
	writeNamedFile(t, snes, privateName, []byte(privateContent))
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	openTestManager(t, root, targetcache.WithLogger(logger))
	got := output.String()
	if !strings.Contains(got, `"category":"unfamiliar"`) || !strings.Contains(got, `"system":"snes"`) {
		t.Fatalf("sanitized invalid-entry diagnostic missing category/system: %s", got)
	}
	for _, private := range []string{root, privateName, privateContent} {
		if strings.Contains(got, private) {
			t.Fatalf("inventory diagnostic exposes private value %q: %s", private, got)
		}
	}
}

func TestInventoryAbortsOnParentDirectoryReplacement(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemDirectory := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(systemDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeNamedFile(t, systemDirectory, "!.trigger", []byte("trigger sanitized diagnostic"))
	partName := ".fogcast-victim.part"
	insidePart := writeNamedFile(t, systemDirectory, partName, []byte("inside partial"))
	movedDirectory := systemDirectory + "-moved"
	outsideDirectory := t.TempDir()
	outsidePart := writeNamedFile(t, outsideDirectory, partName, []byte("outside partial"))

	var mutationErr error
	writer := &callbackWriter{callback: func() {
		if err := os.Rename(systemDirectory, movedDirectory); err != nil {
			mutationErr = err
			return
		}
		mutationErr = os.Symlink(outsideDirectory, systemDirectory)
	}}
	logger := slog.New(slog.NewJSONHandler(writer, nil))
	manager, err := targetcache.Open(testManagerConfig(root), core.DefaultRegistry(), targetcache.WithLogger(logger))
	if mutationErr != nil {
		t.Fatalf("replace system directory: %v", mutationErr)
	}
	if writer.calls() == 0 {
		t.Fatal("inventory replacement callback did not run")
	}
	if manager != nil || err == nil {
		t.Fatalf("Open after system replacement returned manager=%t error=%t; want manager=false error=true", manager != nil, err != nil)
	}
	for _, private := range []string{root, systemDirectory, movedDirectory, outsideDirectory, outsidePart, partName} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("Open error %q exposes private value %q", err, private)
		}
	}
	if _, err := os.Lstat(outsidePart); err != nil {
		t.Fatalf("inventory followed replacement and changed outside part: %v", err)
	}
	movedInsidePart := filepath.Join(movedDirectory, filepath.Base(insidePart))
	if info, statErr := os.Lstat(movedInsidePart); statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("inventory changed stale part after system replacement: info=%v error=%v", info, statErr)
	}
}

func TestInventoryAbortsBeforeCleaningDetachedSystemDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemDirectory := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(systemDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	writeNamedFile(t, systemDirectory, "!.trigger", []byte("trigger sanitized diagnostic"))
	partName := ".fogcast-detached.part"
	writeNamedFile(t, systemDirectory, partName, []byte("detached partial"))
	detachedDirectory := filepath.Join(t.TempDir(), "detached-snes")
	detachedPart := filepath.Join(detachedDirectory, partName)

	var mutationErr error
	writer := &callbackWriter{callback: func() {
		if err := os.Rename(systemDirectory, detachedDirectory); err != nil {
			mutationErr = err
			return
		}
		mutationErr = os.Mkdir(systemDirectory, 0o700)
	}}
	logger := slog.New(slog.NewJSONHandler(writer, nil))
	manager, err := targetcache.Open(testManagerConfig(root), core.DefaultRegistry(), targetcache.WithLogger(logger))
	if mutationErr != nil {
		t.Fatalf("detach system directory: %v", mutationErr)
	}
	if manager != nil || err == nil {
		t.Fatalf("Open after system detachment returned manager=%t error=%t; want manager=false error=true", manager != nil, err != nil)
	}
	for _, private := range []string{root, systemDirectory, detachedDirectory, detachedPart, partName, "detached partial"} {
		if strings.Contains(err.Error(), private) {
			t.Fatalf("Open error %q exposes private value %q", err, private)
		}
	}
	if info, statErr := os.Lstat(detachedPart); statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("detached stale part was removed or changed: info=%v error=%v", info, statErr)
	}
}

func TestInventoryRetainsStaleCandidateReplacedBeforeRemoval(t *testing.T) {
	tests := []struct {
		name    string
		replace func(*testing.T, string) string
		check   func(os.FileInfo) bool
	}{
		{
			name: "link",
			replace: func(t *testing.T, path string) string {
				t.Helper()
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
				return outside
			},
			check: func(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 },
		},
		{
			name: "directory",
			replace: func(t *testing.T, path string) string {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				return ""
			},
			check: func(info os.FileInfo) bool { return info.IsDir() },
		},
		{
			name: "special",
			replace: func(t *testing.T, path string) string {
				t.Helper()
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
				return ""
			},
			check: func(info os.FileInfo) bool { return info.Mode()&os.ModeNamedPipe != 0 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			systemDirectory := filepath.Join(root, string(protocol.SystemSNES))
			if err := os.MkdirAll(systemDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			partName := ".fogcast-replaced.part"
			part := writeNamedFile(t, systemDirectory, partName, []byte("partial to replace"))
			var mutationErr error
			var outside string
			writer := &callbackWriter{callback: func() {
				if err := os.Remove(part); err != nil {
					mutationErr = err
					return
				}
				outside = tt.replace(t, part)
			}}
			logger := slog.New(slog.NewJSONHandler(writer, nil))
			manager, err := targetcache.Open(testManagerConfig(root), core.DefaultRegistry(), targetcache.WithLogger(logger))
			if mutationErr != nil {
				t.Fatalf("replace stale candidate: %v", mutationErr)
			}
			if manager != nil || err == nil {
				t.Fatalf("Open after stale candidate replacement returned manager=%t error=%t; want manager=false error=true", manager != nil, err != nil)
			}
			for _, private := range []string{root, part, outside, partName, "partial to replace"} {
				if private != "" && strings.Contains(err.Error(), private) {
					t.Fatalf("Open error %q exposes private value %q", err, private)
				}
			}
			info, statErr := os.Lstat(part)
			if statErr != nil || !tt.check(info) {
				t.Fatalf("replacement was removed or changed: info=%v error=%v", info, statErr)
			}
		})
	}
}

func TestProbeHashesOncePerMetadataVersionAndResolveUsesMemo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("memoized synthetic content")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	counter := &openCounter{}
	manager := openTestManager(t, root, targetcache.WithOpenFile(counter.open))

	first, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil {
		t.Fatalf("first Probe: %v", apiErr)
	}
	assertPresentIdentity(t, first, protocol.SystemSNES, identity)
	second, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil {
		t.Fatalf("second Probe: %v", apiErr)
	}
	assertPresentIdentity(t, second, protocol.SystemSNES, identity)
	if got := counter.count(); got != 1 {
		t.Fatalf("opens after two probes = %d, want 1", got)
	}

	resolved, apiErr := manager.Resolve(context.Background(), protocol.SystemSNES, identity)
	if apiErr != nil || resolved.Path != path {
		t.Fatalf("Resolve = %#v, %#v", resolved, apiErr)
	}
	wrongSize := identity
	wrongSize.Size++
	_, apiErr = manager.Resolve(context.Background(), protocol.SystemSNES, wrongSize)
	assertSafeAPIError(t, apiErr, protocol.CodeContentNotCached, root, path)
	if got := counter.count(); got != 1 {
		t.Fatalf("Resolve rehashed memoized content: opens = %d", got)
	}

	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	third, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil {
		t.Fatalf("Probe after metadata change: %v", apiErr)
	}
	assertPresentIdentity(t, third, protocol.SystemSNES, identity)
	if got := counter.count(); got != 2 {
		t.Fatalf("metadata change opens = %d, want 2", got)
	}
}

func TestProbeRechecksCancellationAfterWaitingForManagerMutex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("queued cancellation")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	openEntered := make(chan struct{})
	releaseOpen := make(chan struct{})
	var blockFirst sync.Once
	manager := openTestManager(t, root, targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		blockFirst.Do(func() {
			close(openEntered)
			<-releaseOpen
		})
		return os.Open(candidate)
	}))

	firstDone := make(chan probeResult, 1)
	go func() {
		response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
		firstDone <- probeResult{response: response, apiErr: apiErr}
	}()
	<-openEntered

	base, cancel := context.WithCancel(context.Background())
	observed := &observedContext{Context: base, checked: make(chan struct{})}
	secondDone := make(chan probeResult, 1)
	go func() {
		response, apiErr := manager.Probe(observed, protocol.SystemSNES, identity.Key())
		secondDone <- probeResult{response: response, apiErr: apiErr}
	}()
	<-observed.checked
	cancel()
	close(releaseOpen)

	first := <-firstDone
	if first.apiErr != nil {
		t.Fatalf("first Probe: %v", first.apiErr)
	}
	assertPresentIdentity(t, first.response, protocol.SystemSNES, identity)
	second := <-secondDone
	assertSafeAPIError(t, second.apiErr, protocol.CodeInternal, root, path)
	if second.response.Present {
		t.Fatalf("queued canceled Probe returned a memoized hit: %#v", second.response)
	}
}

func TestResolveRejectsColdDeclaredSizeMismatchWithoutOpening(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("cold size mismatch")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	counter := &openCounter{}
	manager := openTestManager(t, root, targetcache.WithOpenFile(counter.open))

	wrongSize := identity
	wrongSize.Size++
	_, apiErr := manager.Resolve(context.Background(), protocol.SystemSNES, wrongSize)
	assertSafeAPIError(t, apiErr, protocol.CodeContentNotCached, root, path)
	if got := counter.count(); got != 0 {
		t.Fatalf("cold size mismatch opened content %d times, want 0", got)
	}

	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil {
		t.Fatalf("Probe after size rejection: %v", apiErr)
	}
	assertPresentIdentity(t, response, protocol.SystemSNES, identity)
}

func TestProbeQuarantinesDigestMismatchUntilMetadataChangesAndAcrossReopen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	expected := []byte("expected")
	wrong := []byte("WRONG!!!")
	identity := contentIdentity(expected, "bin")
	path := writeCacheFile(t, root, protocol.SystemMegaDrive, identity, wrong)
	counter := &openCounter{}
	manager := openTestManager(t, root, targetcache.WithOpenFile(counter.open))

	for i := 0; i < 2; i++ {
		response, apiErr := manager.Probe(context.Background(), protocol.SystemMegaDrive, identity.Key())
		if apiErr != nil {
			t.Fatalf("Probe mismatch %d: %v", i, apiErr)
		}
		if response.Present || response.System != nil || response.Content != nil {
			t.Fatalf("mismatched response = %#v", response)
		}
	}
	if got := counter.count(); got != 1 {
		t.Fatalf("mismatch opens = %d, want one quarantined verification", got)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("digest mismatch was changed on disk: %v", err)
	}
	if got, want := manager.Usage(), int64(len(wrong)); got != want {
		t.Fatalf("Usage() after mismatch = %d, want %d", got, want)
	}

	// Verification state is intentionally in-memory: restart rehashes persisted bytes.
	manager = openTestManager(t, root, targetcache.WithOpenFile(counter.open))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemMegaDrive, identity.Key())
	if apiErr != nil || response.Present {
		t.Fatalf("Probe mismatch after reopen = %#v, %#v", response, apiErr)
	}
	if got := counter.count(); got != 2 {
		t.Fatalf("reopen opens = %d, want 2", got)
	}

	if err := os.WriteFile(path, expected, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(4 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	response, apiErr = manager.Probe(context.Background(), protocol.SystemMegaDrive, identity.Key())
	if apiErr != nil {
		t.Fatalf("Probe corrected content: %v", apiErr)
	}
	assertPresentIdentity(t, response, protocol.SystemMegaDrive, identity)
	if got := counter.count(); got != 3 {
		t.Fatalf("corrected content opens = %d, want 3", got)
	}
}

func TestProbeInvalidatesPositiveMemoWhenFileChangesOrBecomesLink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("original")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	counter := &openCounter{}
	manager := openTestManager(t, root, targetcache.WithOpenFile(counter.open))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil || !response.Present {
		t.Fatalf("initial Probe = %#v, %#v", response, apiErr)
	}

	if err := os.WriteFile(path, []byte("mutated!"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(6 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	response, apiErr = manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil || response.Present {
		t.Fatalf("Probe changed content = %#v, %#v", response, apiErr)
	}
	if got := counter.count(); got != 2 {
		t.Fatalf("changed content opens = %d, want 2", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	response, apiErr = manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil || response.Present {
		t.Fatalf("Probe symlink replacement = %#v, %#v", response, apiErr)
	}
	if got := counter.count(); got != 2 {
		t.Fatalf("symlink replacement was opened: opens = %d", got)
	}
}

func TestProbeRejectsIdentitySwapDuringOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("matching external bytes")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	original := path + ".replaced"
	manager := openTestManager(t, root, targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		if err := os.Rename(candidate, original); err != nil {
			return nil, err
		}
		if err := os.Symlink(outside, candidate); err != nil {
			return nil, err
		}
		return os.Open(candidate)
	}))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	if apiErr != nil && apiErr.Code != protocol.CodeInternal {
		t.Fatalf("Probe swap error = %#v", apiErr)
	}
	if response.Present {
		t.Fatalf("Probe followed a replacement link: %#v", response)
	}
}

func TestProbeRejectsParentDirectoryReplacementDuringOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("same inode cannot justify an escaped parent")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	systemDirectory := filepath.Dir(path)
	movedDirectory := systemDirectory + "-moved"
	outsideDirectory := t.TempDir()
	outsidePath := filepath.Join(outsideDirectory, filepath.Base(path))
	if err := os.Link(path, outsidePath); err != nil {
		t.Fatalf("create outside hard link: %v", err)
	}

	manager := openTestManager(t, root, targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		if err := os.Rename(systemDirectory, movedDirectory); err != nil {
			return nil, err
		}
		if err := os.Symlink(outsideDirectory, systemDirectory); err != nil {
			return nil, err
		}
		return os.Open(candidate)
	}))
	response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	assertSafeAPIError(t, apiErr, protocol.CodeInternal, root, outsideDirectory, outsidePath)
	if response.Present {
		t.Fatalf("Probe accepted content through a replaced parent: %#v", response)
	}
	resolvedCandidate, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOutsidePath, err := filepath.EvalSymlinks(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedCandidate != resolvedOutsidePath {
		t.Fatalf("replacement did not escape fixture root: resolved=%q want=%q", resolvedCandidate, resolvedOutsidePath)
	}
}

func TestProbeReturnsTypedSanitizedOpenError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := []byte("unopenable")
	identity := contentIdentity(content, "sfc")
	path := writeCacheFile(t, root, protocol.SystemSNES, identity, content)
	manager := openTestManager(t, root, targetcache.WithOpenFile(func(candidate string) (*os.File, error) {
		return nil, errors.New("private open failure at " + candidate)
	}))
	_, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key())
	assertSafeAPIError(t, apiErr, protocol.CodeInternal, root, path, "private open failure")
}

type openCounter struct {
	mu    sync.Mutex
	opens int
}

type probeResult struct {
	response protocol.CacheProbeResponse
	apiErr   *protocol.APIError
}

type observedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *observedContext) Err() error {
	c.once.Do(func() { close(c.checked) })
	return c.Context.Err()
}

type callbackWriter struct {
	mu       sync.Mutex
	writes   int
	once     sync.Once
	callback func()
}

func (w *callbackWriter) Write(content []byte) (int, error) {
	w.once.Do(w.callback)
	w.mu.Lock()
	w.writes++
	w.mu.Unlock()
	return len(content), nil
}

func (w *callbackWriter) calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func (c *openCounter) open(path string) (*os.File, error) {
	c.mu.Lock()
	c.opens++
	c.mu.Unlock()
	return os.Open(path)
}

func (c *openCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opens
}

func writeNamedFile(t *testing.T, directory, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertPresentIdentity(t *testing.T, response protocol.CacheProbeResponse, system protocol.System, content protocol.ContentIdentity) {
	t.Helper()
	if !response.Present || response.System == nil || *response.System != system || response.Content == nil || *response.Content != content {
		t.Fatalf("probe response = %#v, want system=%q content=%#v", response, system, content)
	}
}
