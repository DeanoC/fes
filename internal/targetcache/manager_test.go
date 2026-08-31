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

	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

func TestInventoryRetainsDirectStalePartsAndAccountsConservatively(t *testing.T) {
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
	unknownSystem := filepath.Join(root, "unknown")
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
	for path, want := range map[string][]byte{
		stalePart:      []byte("partial"),
		emptyTokenPart: []byte("partial without token"),
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("recognized stale part %q was removed: %v", filepath.Base(path), err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("recognized stale part %q changed type: %s", filepath.Base(path), info.Mode())
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read retained stale part %q: %v", filepath.Base(path), err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("retained stale part %q changed content: got %q want %q", filepath.Base(path), got, want)
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
		rootPart, unknownSystem, rootUnfamiliar, stalePart, emptyTokenPart,
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

func TestInventoryLogsSanitizedRetainedStalePartDiagnostics(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	snes := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(snes, 0o700); err != nil {
		t.Fatal(err)
	}
	privateName := ".fogcast-sensitive.part"
	privateContent := "private stale bytes"
	privatePath := writeNamedFile(t, snes, privateName, []byte(privateContent))
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	openTestManager(t, root, targetcache.WithLogger(logger))
	got := output.String()
	if !strings.Contains(got, `"category":"stale-part-retained"`) ||
		!strings.Contains(got, `"system":"snes"`) || !strings.Contains(got, `"size":`) {
		t.Fatalf("retained stale-part diagnostic missing category/system/size: %s", got)
	}
	for _, private := range []string{root, privatePath, privateName, privateContent} {
		if strings.Contains(got, private) {
			t.Fatalf("retained stale-part diagnostic exposes private value %q: %s", private, got)
		}
	}
	if _, err := os.Lstat(privatePath); err != nil {
		t.Fatalf("retained stale part changed during diagnostic test: %v", err)
	}
}

func TestInventoryDoesNotFollowOutsideSystemDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outsideDirectory := t.TempDir()
	outsidePart := writeNamedFile(t, outsideDirectory, ".fogcast-outside.part", []byte("outside stale bytes"))
	systemDirectory := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.Symlink(outsideDirectory, systemDirectory); err != nil {
		t.Fatal(err)
	}

	openTestManager(t, root)
	info, err := os.Lstat(outsidePart)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("outside stale entry was changed: info=%v error=%v", info, err)
	}
	content, err := os.ReadFile(outsidePart)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "outside stale bytes" {
		t.Fatalf("outside stale entry content changed: %q", content)
	}
	linkInfo, err := os.Lstat(systemDirectory)
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("outside system link was changed: info=%v error=%v", linkInfo, err)
	}
}

func TestInventoryRetainsStaleNonRegularEntriesUnchanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	snes := filepath.Join(root, string(protocol.SystemSNES))
	if err := os.MkdirAll(snes, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside target"), 0o600); err != nil {
		t.Fatal(err)
	}
	staleLink := filepath.Join(snes, ".fogcast-link.part")
	if err := os.Symlink(outside, staleLink); err != nil {
		t.Fatal(err)
	}
	staleDirectory := filepath.Join(snes, ".fogcast-directory.part")
	if err := os.Mkdir(staleDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := writeNamedFile(t, staleDirectory, "sentinel", []byte("do not traverse"))
	staleFIFO := filepath.Join(snes, ".fogcast-fifo.part")
	if err := syscall.Mkfifo(staleFIFO, 0o600); err != nil {
		t.Fatal(err)
	}

	openTestManager(t, root)
	linkTarget, err := os.Readlink(staleLink)
	if err != nil || linkTarget != outside {
		t.Fatalf("stale symlink changed: target=%q error=%v", linkTarget, err)
	}
	if info, err := os.Lstat(staleLink); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("stale symlink type changed: info=%v error=%v", info, err)
	}
	if info, err := os.Lstat(staleDirectory); err != nil || !info.IsDir() {
		t.Fatalf("stale directory changed: info=%v error=%v", info, err)
	}
	if content, err := os.ReadFile(sentinel); err != nil || string(content) != "do not traverse" {
		t.Fatalf("stale directory contents changed: content=%q error=%v", content, err)
	}
	if info, err := os.Lstat(staleFIFO); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("stale FIFO changed: info=%v error=%v", info, err)
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
