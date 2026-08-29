package fpgadev

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testPayloadHex = "4f53532046504741204f4b0a"

func validResult() Result {
	payload, _ := hex.DecodeString(testPayloadHex)
	digest := sha256.Sum256(payload)
	return Result{
		Schema:          1,
		RunID:           testRunID,
		Generation:      42,
		Session:         "fedcba9876543210fedcba9876543210",
		Mode:            "updating",
		Experiment:      "020_linux_mailbox",
		BuildLane:       "oss",
		ArtifactSHA256:  testArtifactHash,
		SourceCommit:    testSourceCommit,
		Phase:           "done_observed",
		PrimaryCode:     "ok",
		PrimaryDetail:   "",
		PayloadHex:      testPayloadHex,
		PayloadLength:   uint64(len(payload)),
		PayloadSHA256:   hex.EncodeToString(digest[:]),
		TerminalWord:    "d3130c00",
		RecoveryRequest: "pending",
		ElapsedMS:       125,
	}
}

func validFailureResult() Result {
	result := validResult()
	result.Phase = "message_partial"
	result.PrimaryCode = "protocol_violation"
	result.PrimaryDetail = "invalid mailbox word"
	result.PayloadHex = ""
	result.PayloadLength = 0
	result.PayloadSHA256 = emptySHA256
	result.TerminalWord = "00000000"
	return result
}

func canonicalResultBytes(t *testing.T, result Result) []byte {
	t.Helper()
	raw, err := result.MarshalCanonical()
	if err != nil {
		t.Fatalf("marshal canonical result: %v", err)
	}
	return raw
}

func TestResultValidateAcceptsSuccessAndFailure(t *testing.T) {
	for name, result := range map[string]Result{
		"success": validResult(),
		"failure": validFailureResult(),
	} {
		name, result := name, result
		t.Run(name, func(t *testing.T) {
			if err := result.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestResultValidateRequiresDONEWordForEveryDoneObservedResult(t *testing.T) {
	failure := validFailureResult()
	failure.Phase = ResultPhaseDoneObserved
	failure.TerminalWord = "00000000"
	if err := failure.Validate(); err == nil {
		t.Fatal("done_observed failure without DONE word was accepted")
	}
	failure.TerminalWord = "d3130c00"
	if err := failure.Validate(); err != nil {
		t.Fatalf("done_observed failure with DONE word rejected: %v", err)
	}

	for _, phase := range []string{
		ResultPhaseIntentCommitted,
		ResultPhaseLoadAttempted,
		ResultPhaseMainAbsent,
		ResultPhaseLeaseActive,
		ResultPhaseHelloObserved,
		ResultPhaseMessagePartial,
		ResultPhaseEndAckWritten,
	} {
		early := validFailureResult()
		early.Phase = phase
		early.TerminalWord = "d3130c00"
		if err := early.Validate(); err == nil {
			t.Fatalf("early phase %q accepted nonzero terminal word", phase)
		}
	}
}

func TestResultRejectsHostileMatrix(t *testing.T) {
	base := validResult()
	tests := map[string]func(*Result){
		"schema":            func(r *Result) { r.Schema = 2 },
		"run id":            func(r *Result) { r.RunID = strings.ToUpper(r.RunID) },
		"generation":        func(r *Result) { r.Generation = 0 },
		"session":           func(r *Result) { r.Session = "bad" },
		"mode":              func(r *Result) { r.Mode = "fpga_native" },
		"experiment":        func(r *Result) { r.Experiment = "other" },
		"lane":              func(r *Result) { r.BuildLane = "quartus" },
		"artifact hash":     func(r *Result) { r.ArtifactSHA256 = strings.Repeat("B", 64) },
		"source commit":     func(r *Result) { r.SourceCommit = strings.Repeat("a", 39) },
		"phase":             func(r *Result) { r.Phase = "unknown" },
		"primary":           func(r *Result) { r.PrimaryCode = "unknown" },
		"success phase":     func(r *Result) { r.Phase = "hello_observed" },
		"success payload":   func(r *Result) { r.PayloadHex = "00" },
		"payload length":    func(r *Result) { r.PayloadLength++ },
		"payload hash":      func(r *Result) { r.PayloadSHA256 = strings.Repeat("b", 64) },
		"terminal":          func(r *Result) { r.TerminalWord = "D3130C00" },
		"recovery":          func(r *Result) { r.RecoveryRequest = "verified" },
		"failure detail":    func(r *Result) { r.PrimaryCode = "mmio_failed"; r.PrimaryDetail = "" },
		"detail newline":    func(r *Result) { r.PrimaryDetail = "bad\nline" },
		"detail tab":        func(r *Result) { r.PrimaryDetail = "bad\tline" },
		"detail escape":     func(r *Result) { r.PrimaryDetail = "bad\x1bline" },
		"payload odd hex":   func(r *Result) { r.PayloadHex = "0" },
		"payload uppercase": func(r *Result) { r.PayloadHex = "4F" },
		"payload too large": func(r *Result) {
			r.PayloadHex = strings.Repeat("00", 257)
			r.PayloadLength = 257
			r.PayloadSHA256 = strings.Repeat("b", 64)
			r.Phase = "message_partial"
			r.PrimaryCode = "protocol_violation"
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			result := base
			mutate(&result)
			if err := result.Validate(); err == nil {
				t.Fatal("hostile result was accepted")
			}
		})
	}
}

func TestResultValidateRejectsControlDiagnostics(t *testing.T) {
	for _, detail := range []string{"bad\tline", "bad\x1bline"} {
		result := validFailureResult()
		result.PrimaryDetail = detail
		if err := result.Validate(); err == nil {
			t.Fatalf("control diagnostic %q was accepted", detail)
		}
	}
}

func TestResultParserRequiresCanonicalSchemaAndRejectsDuplicates(t *testing.T) {
	base := canonicalResultBytes(t, validResult())
	mutations := map[string][]byte{
		"unknown":    bytes.Replace(base, []byte(`{"schema":1,`), []byte(`{"extra":true,"schema":1,`), 1),
		"duplicate":  bytes.Replace(base, []byte(`"run_id":"`+testRunID+`",`), []byte(`"run_id":"`+testRunID+`","run_id":"`+testRunID+`",`), 1),
		"trailing":   append(append([]byte(nil), base...), []byte("null")...),
		"missing":    bytes.Replace(base, []byte(`,"elapsed_ms":125`), nil, 1),
		"order":      bytes.Replace(base, []byte(`{"schema":1,"run_id":"`+testRunID+`"`), []byte(`{"run_id":"`+testRunID+`","schema":1`), 1),
		"no newline": bytes.TrimSuffix(base, []byte("\n")),
	}
	for name, raw := range mutations {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if _, err := ParseResult(raw); err == nil {
				t.Fatal("noncanonical result was accepted")
			}
		})
	}
	parsed, err := ParseResult(base)
	if err != nil || parsed != validResult() {
		t.Fatalf("canonical result round trip = %#v, %v", parsed, err)
	}
}

func newResultStoreFixture(t *testing.T) (*ResultStore, Result) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "results")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	result := validResult()
	store := NewResultStore(directory, uint32(os.Getuid()))
	return store, result
}

func TestResultStoreCreateIsExclusiveAndCanonical(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := store.PathFor(result.RunID)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 || info.Size() == 0 {
		t.Fatalf("result metadata = %#v", info)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, canonicalResultBytes(t, result)) {
		t.Fatal("stored result is not canonical")
	}
	if err := store.Create(result); err == nil {
		t.Fatal("result collision was accepted")
	}
	second, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(second, first) {
		t.Fatalf("collision changed old bytes: err=%v", err)
	}
	loaded, err := store.Load(result.RunID)
	if err != nil || loaded != result {
		t.Fatalf("Load = %#v, %v", loaded, err)
	}
}

func TestResultStoreRequiresPreexistingProtectedDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "missing", "results")
	store := NewResultStore(directory, uint32(os.Getuid()))
	if err := store.Create(validResult()); err == nil {
		t.Fatal("result store created a missing directory")
	}
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing result directory was mutated: %v", err)
	}
}

func TestResultStoreCreateSerializesMarkBetweenLinkAndValidation(t *testing.T) {
	store, result := newResultStoreFixture(t)
	second := NewResultStore(store.Dir, uint32(os.Getuid()))
	markStarted := make(chan struct{})
	markDone := make(chan error, 1)
	store.AfterCreateLink = func() {
		close(markStarted)
		go func() {
			markDone <- second.MarkRecoveryFailed(result)
		}()
		select {
		case err := <-markDone:
			t.Fatalf("Mark ran before Create validation completed: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := store.Create(result); err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case err := <-markDone:
		if err != nil {
			t.Fatalf("serialized Mark: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serialized Mark did not complete")
	}
	loaded, err := store.Load(result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RecoveryRequest != "failed" {
		t.Fatalf("serialized publication result = %#v", loaded)
	}
}

func TestResultStoreCreateRollbackDoesNotUnlinkConcurrentReplacement(t *testing.T) {
	store, result := newResultStoreFixture(t)
	replacement := result
	replacement.PrimaryCode = string(CodeProtocolViolation)
	replacement.PrimaryDetail = "replacement during create validation"
	replacement.Phase = ResultPhaseMessagePartial
	replacement.PayloadHex = ""
	replacement.PayloadLength = 0
	replacement.PayloadSHA256 = emptySHA256
	replacement.TerminalWord = "00000000"
	if err := replacement.Validate(); err != nil {
		t.Fatal(err)
	}
	replacementBytes := canonicalResultBytes(t, replacement)
	path := store.PathFor(result.RunID)
	store.AfterCreateLink = func() {
		if err := os.Rename(path, path+".create-old"); err != nil {
			t.Fatalf("move create publication: %v", err)
		}
		if err := os.WriteFile(path, replacementBytes, 0o600); err != nil {
			t.Fatalf("publish create replacement: %v", err)
		}
	}
	if err := store.Create(result); err == nil {
		t.Fatal("create accepted a replacement during validation")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, replacementBytes) {
		t.Fatalf("create rollback clobbered replacement: %v", err)
	}
}

func TestResultStoreRejectsAncestorSymlinkInsertedAtTrustedRootBoundary(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	directory := filepath.Join(parent, "results")
	hostile := filepath.Join(root, "hostile")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(hostile, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewResultStore(directory, uint32(os.Getuid()))
	store.BeforeDirectoryOpen = func() {
		if err := os.Rename(parent, parent+".retained"); err != nil {
			t.Fatalf("move trusted-root parent: %v", err)
		}
		if err := os.Symlink(hostile, parent); err != nil {
			t.Fatalf("insert ancestor symlink: %v", err)
		}
	}
	if err := store.Create(validResult()); err == nil {
		t.Fatal("ancestor symlink inserted at result open boundary was accepted")
	}
	if _, err := os.Lstat(filepath.Join(hostile, filepath.Base(store.PathFor(validResult().RunID)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile result directory was touched: %v", err)
	}
}

func TestResultStoreCreatePreservesOldBytesOnInjectedFsyncFailures(t *testing.T) {
	for _, field := range []string{"file", "parent"} {
		t.Run(field, func(t *testing.T) {
			store, result := newResultStoreFixture(t)
			if field == "file" {
				store.SyncFile = func(*os.File) error { return errors.New("injected file fsync") }
			} else {
				store.SyncParent = func(*os.File) error { return errors.New("injected parent fsync") }
			}
			if err := store.Create(result); err == nil {
				t.Fatal("injected failure was ignored")
			}
			if _, err := os.Lstat(store.PathFor(result.RunID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed create left a result: %v", err)
			}
		})
	}
}

func TestResultStoreCreateReportsTemporaryCleanupFsyncFailure(t *testing.T) {
	store, result := newResultStoreFixture(t)
	store.SyncFile = func(*os.File) error { return errors.New("injected temporary fsync") }
	store.SyncParent = func(*os.File) error { return errors.New("injected cleanup fsync") }
	err := store.Create(result)
	if err == nil || !strings.Contains(err.Error(), "injected cleanup fsync") {
		t.Fatalf("temporary cleanup error = %v, want cleanup fsync evidence", err)
	}
	if _, statErr := os.Lstat(store.PathFor(result.RunID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed create left a result: %v", statErr)
	}
}

func TestResultStoreMarkRecoveryFailedRequiresExactPendingExpected(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(store.PathFor(result.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRecoveryFailed(result); err != nil {
		t.Fatalf("MarkRecoveryFailed: %v", err)
	}
	updated, err := os.ReadFile(store.PathFor(result.RunID))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(updated, old) || !bytes.Contains(updated, []byte(`"recovery_request":"failed"`)) {
		t.Fatal("pending result was not conditionally updated")
	}
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("repeated pending update was accepted")
	}
	repeated, _ := os.ReadFile(store.PathFor(result.RunID))
	if !bytes.Equal(repeated, updated) {
		t.Fatal("repeated update changed result bytes")
	}
}

func TestResultStoreMarkRecoveryFailedRejectsEveryBindingMismatchByteForByte(t *testing.T) {
	mutations := map[string]func(*Result){
		"run":        func(r *Result) { r.RunID = "11111111111111111111111111111111" },
		"session":    func(r *Result) { r.Session = "11111111111111111111111111111111" },
		"generation": func(r *Result) { r.Generation++ },
		"mode":       func(r *Result) { r.Mode = "none" },
		"artifact":   func(r *Result) { r.ArtifactSHA256 = strings.Repeat("b", 64) },
		"source":     func(r *Result) { r.SourceCommit = strings.Repeat("b", 40) },
		"primary":    func(r *Result) { r.PrimaryCode = "mmio_failed"; r.PrimaryDetail = "failed"; r.Phase = "mmio_failed" },
		"payload": func(r *Result) {
			r.PayloadHex = ""
			r.PayloadLength = 0
			r.PayloadSHA256 = emptySHA256
			r.Phase = "message_partial"
			r.PrimaryCode = "protocol_violation"
			r.PrimaryDetail = "bad"
			r.TerminalWord = "00000000"
		},
		"elapsed": func(r *Result) { r.ElapsedMS++ },
	}
	for name, mutate := range mutations {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			store, result := newResultStoreFixture(t)
			if err := store.Create(result); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(store.PathFor(result.RunID))
			if err != nil {
				t.Fatal(err)
			}
			mutated := result
			mutate(&mutated)
			if err := store.MarkRecoveryFailed(mutated); err == nil {
				t.Fatal("mismatching expected result was accepted")
			}
			after, err := os.ReadFile(store.PathFor(result.RunID))
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("mismatch changed old bytes: %v", err)
			}
		})
	}

	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	failed := result
	failed.RecoveryRequest = "failed"
	before, _ := os.ReadFile(store.PathFor(result.RunID))
	if err := store.MarkRecoveryFailed(failed); err == nil {
		t.Fatal("non-pending existing result was accepted")
	}
	after, _ := os.ReadFile(store.PathFor(result.RunID))
	if !bytes.Equal(after, before) {
		t.Fatal("non-pending update changed old bytes")
	}
}

func TestResultStoreMarkRecoveryFailedRejectsConcurrentReplacement(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	replacement := result
	replacement.PrimaryCode = "mmio_failed"
	replacement.PrimaryDetail = "replacement"
	replacement.Phase = "message_partial"
	replacement.PayloadHex = ""
	replacement.PayloadLength = 0
	replacement.PayloadSHA256 = emptySHA256
	replacement.TerminalWord = "00000000"
	if err := replacement.Validate(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	store.BeforeConditionalReplace = func() {
		once.Do(func() {
			if err := os.Rename(store.PathFor(result.RunID), store.PathFor(result.RunID)+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.PathFor(result.RunID), canonicalResultBytes(t, replacement), 0o600); err != nil {
				t.Fatal(err)
			}
		})
	}
	before, _ := os.ReadFile(store.PathFor(result.RunID))
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("concurrent replacement race was accepted")
	}
	after, err := os.ReadFile(store.PathFor(result.RunID))
	if err != nil || !bytes.Equal(after, canonicalResultBytes(t, replacement)) {
		t.Fatalf("replacement bytes were not preserved: %v", err)
	}
	if bytes.Equal(after, before) {
		t.Fatal("replacement did not occur")
	}
}

func TestResultStoreMarkRecoveryFailedRejectsSecondStoreReplacementAtFinalBoundary(t *testing.T) {
	store, result := newResultStoreFixture(t)
	second := NewResultStore(store.Dir, uint32(os.Getuid()))
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	store.BeforeMarkLock = func() {
		if err := second.MarkRecoveryFailed(result); err != nil {
			t.Fatalf("second store replacement: %v", err)
		}
	}
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("first store clobbered a second-store replacement")
	}
	got, err := os.ReadFile(store.PathFor(result.RunID))
	if err != nil {
		t.Fatal(err)
	}
	want := result
	want.RecoveryRequest = "failed"
	if !bytes.Equal(got, canonicalResultBytes(t, want)) {
		t.Fatalf("second-store bytes were not preserved: %q", got)
	}
}

func TestResultStoreMarkRecoveryFailedRejectsReplacementAtAtomicExchangeBoundary(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	replacement := result
	replacement.PrimaryCode = string(CodeProtocolViolation)
	replacement.PrimaryDetail = "replacement at exchange boundary"
	replacement.Phase = ResultPhaseMessagePartial
	replacement.PayloadHex = ""
	replacement.PayloadLength = 0
	replacement.PayloadSHA256 = emptySHA256
	replacement.TerminalWord = "00000000"
	if err := replacement.Validate(); err != nil {
		t.Fatal(err)
	}
	replacementBytes := canonicalResultBytes(t, replacement)
	path := store.PathFor(result.RunID)
	store.BeforeAtomicExchange = func() {
		if err := os.Rename(path, path+".boundary-old"); err != nil {
			t.Fatalf("move pending result: %v", err)
		}
		if err := os.WriteFile(path, replacementBytes, 0o600); err != nil {
			t.Fatalf("publish boundary replacement: %v", err)
		}
	}
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("atomic-boundary replacement was accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, replacementBytes) {
		t.Fatalf("atomic-boundary replacement was clobbered: %v", err)
	}
}

func TestResultStoreMarkRecoveryFailedRestoresEveryDisplacedEntryType(t *testing.T) {
	tests := map[string]struct {
		install func(t *testing.T, path string)
		assert  func(t *testing.T, path string)
	}{
		"zero-byte regular": {
			install: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Size() != 0 || info.Mode().Perm() != 0o600 {
					t.Fatalf("zero-byte entry = %#v, %v", info, err)
				}
			},
		},
		"symlink": {
			install: func(t *testing.T, path string) {
				target := filepath.Join(filepath.Dir(path), "symlink-target")
				if err := os.WriteFile(target, []byte("target-bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Base(target), path); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				target, err := os.Readlink(path)
				if err != nil || target != "symlink-target" {
					t.Fatalf("symlink target = %q, %v", target, err)
				}
			},
		},
		"FIFO": {
			install: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeNamedPipe == 0 || info.Mode().Perm() != 0o600 {
					t.Fatalf("FIFO entry = %#v, %v", info, err)
				}
			},
		},
		"wrong mode": {
			install: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("wrong-mode"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
					t.Fatalf("wrong-mode entry = %#v, %v", info, err)
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != "wrong-mode" {
					t.Fatalf("wrong-mode bytes = %q, %v", got, readErr)
				}
			},
		},
		"wrong owner": {
			install: func(t *testing.T, path string) {
				if os.Getuid() != 0 {
					t.Skip("wrong-owner entry case requires root test privileges")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("wrong-owner"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chown(path, 1, 1); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("wrong-owner entry = %#v, %v", info, err)
				}
				stat, ok := info.Sys().(*syscall.Stat_t)
				if !ok || stat.Uid != 1 {
					t.Fatalf("wrong-owner uid = %#v", info.Sys())
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != "wrong-owner" {
					t.Fatalf("wrong-owner bytes = %q, %v", got, readErr)
				}
			},
		},
		"directory type": {
			install: func(t *testing.T, path string) {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			assert: func(t *testing.T, path string) {
				info, err := os.Lstat(path)
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Fatalf("directory entry = %#v, %v", info, err)
				}
			},
		},
	}
	for name, test := range tests {
		name, test := name, test
		t.Run(name, func(t *testing.T) {
			store, result := newResultStoreFixture(t)
			if err := store.Create(result); err != nil {
				t.Fatal(err)
			}
			path := store.PathFor(result.RunID)
			store.BeforeAtomicExchange = func() { test.install(t, path) }
			if err := store.MarkRecoveryFailed(result); err == nil {
				t.Fatal("displaced entry was accepted")
			}
			test.assert(t, path)
			entries, err := os.ReadDir(store.Dir)
			if err != nil {
				t.Fatal(err)
			}
			foundFinal := false
			for _, entry := range entries {
				if entry.Name() == filepath.Base(path) {
					foundFinal = true
				}
				if strings.HasPrefix(entry.Name(), ".update-") || strings.HasPrefix(entry.Name(), ".rollback-") {
					t.Fatalf("stale candidate entry = %v", entries)
				}
			}
			if !foundFinal {
				t.Fatalf("restored result entry is absent: %v", entries)
			}
		})
	}
}

func TestResultStoreMarkRecoveryFailedRejectsABASameBytesWithNewInode(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	path := store.PathFor(result.RunID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	store.BeforeFinalReplace = func() {
		abaPath := path + ".aba"
		if err := os.Rename(path, abaPath); err != nil {
			t.Fatalf("move original result: %v", err)
		}
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatalf("publish ABA result: %v", err)
		}
	}
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("ABA replacement was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("ABA replacement bytes were not preserved")
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(originalInfo, afterInfo) {
		t.Fatal("ABA replacement inode was clobbered")
	}
}

func TestResultStoreMarkRecoveryFailedUsesDirectoryDescriptorAcrossSwap(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	originalDirectory := store.Dir
	retainedDirectory := originalDirectory + ".retained"
	hostileDirectory := filepath.Join(t.TempDir(), "hostile")
	if err := os.Mkdir(hostileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	store.BeforeFinalReplace = func() {
		if err := os.Rename(originalDirectory, retainedDirectory); err != nil {
			t.Fatalf("move result directory: %v", err)
		}
		if err := os.Symlink(hostileDirectory, originalDirectory); err != nil {
			t.Fatalf("replace result directory with symlink: %v", err)
		}
	}
	if err := store.MarkRecoveryFailed(result); err != nil {
		t.Fatalf("descriptor-bound update failed after directory swap: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(retainedDirectory, filepath.Base(store.PathFor(result.RunID))))
	if err != nil {
		t.Fatal(err)
	}
	want := result
	want.RecoveryRequest = "failed"
	if !bytes.Equal(updated, canonicalResultBytes(t, want)) {
		t.Fatal("descriptor-bound update did not reach retained directory")
	}
	if _, err := os.Lstat(filepath.Join(hostileDirectory, filepath.Base(store.PathFor(result.RunID)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile directory was touched: %v", err)
	}
}

func TestResultStoreCreateUsesDirectoryDescriptorAcrossSwap(t *testing.T) {
	store, result := newResultStoreFixture(t)
	originalDirectory := store.Dir
	retainedDirectory := originalDirectory + ".retained"
	hostileDirectory := filepath.Join(t.TempDir(), "hostile")
	if err := os.Mkdir(hostileDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	store.BeforeFinalCreate = func() {
		if err := os.Rename(originalDirectory, retainedDirectory); err != nil {
			t.Fatalf("move result directory: %v", err)
		}
		if err := os.Symlink(hostileDirectory, originalDirectory); err != nil {
			t.Fatalf("replace result directory with symlink: %v", err)
		}
	}
	if err := store.Create(result); err != nil {
		t.Fatalf("descriptor-bound create failed after directory swap: %v", err)
	}
	created, err := os.ReadFile(filepath.Join(retainedDirectory, filepath.Base(store.PathFor(result.RunID))))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(created, canonicalResultBytes(t, result)) {
		t.Fatal("descriptor-bound create did not reach retained directory")
	}
	if _, err := os.Lstat(filepath.Join(hostileDirectory, filepath.Base(store.PathFor(result.RunID)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hostile directory was touched: %v", err)
	}
}

func TestResultStorePersistenceFaultMatrixPreservesEvidence(t *testing.T) {
	t.Run("create link failure cleans temporary", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		store.Link = func(string, string) error { return errors.New("injected link failure") }
		if err := store.Create(result); err == nil {
			t.Fatal("link failure was ignored")
		}
		if _, err := os.Lstat(store.PathFor(result.RunID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("link failure published a result: %v", err)
		}
		entries, err := os.ReadDir(store.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("link failure left temporary entries: %v", entries)
		}
	})

	t.Run("create parent fsync failure removes publication", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		store.SyncParent = func(*os.File) error { return errors.New("injected parent fsync") }
		if err := store.Create(result); err == nil {
			t.Fatal("parent fsync failure was ignored")
		}
		if _, err := os.Lstat(store.PathFor(result.RunID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("parent fsync failure left a result: %v", err)
		}
	})

	t.Run("exchange failure preserves pending bytes", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		store.Exchange = func(string, string) error { return errors.New("injected exchange failure") }
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("exchange failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("exchange failure changed pending bytes: %v", err)
		}
	})

	t.Run("rename seam failure preserves pending bytes", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		store.Rename = func(string, string) error { return errors.New("injected rename failure") }
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("rename failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("rename failure changed pending bytes: %v", err)
		}
	})

	t.Run("cleanup fsync failure preserves pending bytes", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		store.Exchange = func(string, string) error { return errors.New("injected exchange failure") }
		store.SyncParent = func(*os.File) error { return errors.New("injected cleanup fsync") }
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("cleanup fsync failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("cleanup fsync failure changed pending bytes: %v", err)
		}
		entries, err := os.ReadDir(store.Dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != filepath.Base(store.PathFor(result.RunID)) {
			t.Fatalf("cleanup fsync failure left temporary entries: %v", entries)
		}
	})

	t.Run("cleanup failure restores old bytes", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		store.Unlink = func(string) error {
			calls++
			if calls == 1 {
				return errors.New("injected cleanup failure")
			}
			return nil
		}
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("cleanup failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("cleanup failure changed pending bytes: %v", err)
		}
	})

	t.Run("parent fsync failure rolls back durably", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		store.SyncParent = func(*os.File) error {
			calls++
			if calls == 1 {
				return errors.New("injected publication fsync")
			}
			return nil
		}
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("publication fsync failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("publication fsync failure did not restore old bytes: %v", err)
		}
	})

	t.Run("post-publication file fsync failure rolls back", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		store.SyncFile = func(*os.File) error {
			calls++
			if calls == 2 {
				return errors.New("injected post-publication file fsync")
			}
			return nil
		}
		if err := store.MarkRecoveryFailed(result); err == nil {
			t.Fatal("post-publication file fsync failure was ignored")
		}
		after, err := os.ReadFile(store.PathFor(result.RunID))
		if err != nil || !bytes.Equal(after, before) {
			t.Fatalf("post-publication file fsync failure changed old bytes: %v", err)
		}
	})

	t.Run("post-publication replacement is not clobbered during rollback", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		replacement := result
		replacement.PrimaryCode = string(CodeMMIOFailed)
		replacement.PrimaryDetail = "concurrent replacement"
		replacement.Phase = ResultPhaseMessagePartial
		replacement.PayloadHex = ""
		replacement.PayloadLength = 0
		replacement.PayloadSHA256 = emptySHA256
		replacement.TerminalWord = "00000000"
		if err := replacement.Validate(); err != nil {
			t.Fatal(err)
		}
		replacementBytes := canonicalResultBytes(t, replacement)
		calls := 0
		store.SyncFile = func(*os.File) error {
			calls++
			if calls == 2 {
				path := store.PathFor(result.RunID)
				temporaryPath := path + ".external"
				if err := os.WriteFile(temporaryPath, replacementBytes, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(temporaryPath, path); err != nil {
					t.Fatal(err)
				}
				return errors.New("injected post-publication file fsync")
			}
			return nil
		}
		err := store.MarkRecoveryFailed(result)
		if err == nil {
			t.Fatal("post-publication replacement failure was ignored")
		}
		got, readErr := os.ReadFile(store.PathFor(result.RunID))
		if readErr != nil || !bytes.Equal(got, replacementBytes) {
			t.Fatalf("concurrent replacement was clobbered: err=%v bytes=%q", readErr, got)
		}
	})

	t.Run("rollback file fsync failure is explicit", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		fileCalls := 0
		store.SyncFile = func(*os.File) error {
			fileCalls++
			if fileCalls == 3 {
				return errors.New("injected rollback file fsync")
			}
			return nil
		}
		parentCalls := 0
		store.SyncParent = func(*os.File) error {
			parentCalls++
			if parentCalls == 1 {
				return errors.New("injected publication fsync")
			}
			return nil
		}
		err := store.MarkRecoveryFailed(result)
		if err == nil || !strings.Contains(err.Error(), "restoration failure") {
			t.Fatalf("rollback fsync failure = %v, want explicit restoration failure", err)
		}
	})

	t.Run("rollback parent fsync failure is explicit", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := store.Create(result); err != nil {
			t.Fatal(err)
		}
		store.SyncParent = func(*os.File) error { return errors.New("injected publication and rollback fsync") }
		err := store.MarkRecoveryFailed(result)
		if err == nil || !strings.Contains(err.Error(), "restoration failure") {
			t.Fatalf("rollback parent fsync failure = %v, want explicit restoration failure", err)
		}
	})
}

func TestResultStoreRejectsSymlinkedResultAncestor(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	hostile := filepath.Join(root, "hostile")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(hostile, 0o700); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(parent, "results")
	if err := os.Mkdir(results, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hostile, parent); err != nil {
		t.Fatal(err)
	}
	store := NewResultStore(results, uint32(os.Getuid()))
	if err := store.Create(validResult()); err == nil {
		t.Fatal("symlinked result ancestor was accepted")
	}
}

func TestResultStoreRejectsResultSymlinkAndUnsafeRunIDPath(t *testing.T) {
	store, result := newResultStoreFixture(t)
	target := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.PathFor(result.RunID)); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(result); err == nil {
		t.Fatal("result symlink was accepted")
	}
	sentinel, _ := os.ReadFile(target)
	if string(sentinel) != "sentinel" {
		t.Fatal("result symlink target changed")
	}
	if _, err := store.Load("../escape"); err == nil {
		t.Fatal("unsafe run id path was accepted")
	}
}

func TestResultStoreRejectsHostileResultFileMetadata(t *testing.T) {
	t.Run("wrong mode", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		path := store.PathFor(result.RunID)
		if err := os.WriteFile(path, canonicalResultBytes(t, result), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(result.RunID); err == nil {
			t.Fatal("world-readable result file was accepted")
		}
	})

	t.Run("hard link", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		path := store.PathFor(result.RunID)
		if err := os.WriteFile(path, canonicalResultBytes(t, result), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, path+".alias"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(result.RunID); err == nil {
			t.Fatal("multi-link result file was accepted")
		}
	})

	t.Run("FIFO", func(t *testing.T) {
		store, result := newResultStoreFixture(t)
		if err := unix.Mkfifo(store.PathFor(result.RunID), 0o600); err != nil {
			t.Fatal(err)
		}
		loaded := make(chan error, 1)
		go func() {
			_, err := store.Load(result.RunID)
			loaded <- err
		}()
		select {
		case err := <-loaded:
			if err == nil {
				t.Fatal("FIFO result file was accepted")
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("FIFO result file open blocked")
		}
	})

	if os.Getuid() == 0 {
		t.Run("wrong owner", func(t *testing.T) {
			store, result := newResultStoreFixture(t)
			path := store.PathFor(result.RunID)
			if err := os.WriteFile(path, canonicalResultBytes(t, result), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 1, 1); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(result.RunID); err == nil {
				t.Fatal("non-root-owned result file was accepted")
			}
		})
	}
}

func TestResultStoreRejectsHostileResultDirectoryMetadata(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := os.Chmod(store.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(result); err == nil {
		t.Fatal("result directory with mode 0755 was accepted")
	}
	if os.Getuid() == 0 {
		store, result = newResultStoreFixture(t)
		if err := os.Chown(store.Dir, 1, 1); err != nil {
			t.Fatal(err)
		}
		if err := store.Create(result); err == nil {
			t.Fatal("non-root-owned result directory was accepted")
		}
	}
}

func TestFramingIsExactAndPathFree(t *testing.T) {
	if got := PreflightLine(CodeOK); got != "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n" {
		t.Fatalf("preflight success framing = %q", got)
	}
	if got := PreflightLine(CodeManifestRejected); got != "FOGCAST_FPGA_DEV_PREFLIGHT code=manifest_rejected\n" {
		t.Fatalf("preflight failure framing = %q", got)
	}
	result := validResult()
	if got := ResultLine(result); got != "FOGCAST_FPGA_DEV_RESULT run_id="+result.RunID+" primary=ok\n" {
		t.Fatalf("result framing = %q", got)
	}
	failed := validFailureResult()
	if got := ResultLine(failed); got != "FOGCAST_FPGA_DEV_RESULT run_id="+failed.RunID+" primary=protocol_violation\n" {
		t.Fatalf("failure framing = %q", got)
	}
	if strings.ContainsAny(strings.TrimSuffix(ResultLine(result), "\n"), "/\\\r\n") {
		t.Fatal("result framing contains unsafe path/control data")
	}
}

func TestFramingEnforcesStageSpecificCodeSetsAndInvalidResultFailsClosed(t *testing.T) {
	preflightCodes := []Code{
		CodeOK,
		CodeDesignationFailed,
		CodeProfileDisabled,
		CodePrivilegeRequired,
		CodeManifestRejected,
		CodeResultConflict,
		CodeOwnershipConflict,
	}
	for _, code := range preflightCodes {
		want := "FOGCAST_FPGA_DEV_PREFLIGHT code=" + string(code) + "\n"
		if got := PreflightLine(code); got != want {
			t.Fatalf("preflight %q = %q, want %q", code, got, want)
		}
	}
	if got := PreflightLine(CodeLoadDispatchFailed); got == "FOGCAST_FPGA_DEV_PREFLIGHT code=load_dispatch_failed\n" {
		t.Fatal("result-only code was accepted by preflight framing")
	}
	if got := PreflightLine(Code("unknown")); got != "" {
		t.Fatalf("unknown preflight code = %q, want fail-closed empty output", got)
	}

	runCodes := append(append([]Code(nil), preflightCodes[1:]...), CodeGenerationFailed, CodeStateStoreFailed)
	for _, code := range runCodes {
		want := "FOGCAST_FPGA_DEV_RUN code=" + string(code) + "\n"
		if got := RunLine(code); got != want {
			t.Fatalf("run %q = %q, want %q", code, got, want)
		}
	}
	if got := RunLine(CodeOK); got != "" {
		t.Fatalf("run success code = %q, want failure-only empty output", got)
	}
	if got := RunLine(CodeLoadDispatchFailed); got == "FOGCAST_FPGA_DEV_RUN code=load_dispatch_failed\n" {
		t.Fatal("post-intent code was unexpectedly accepted by pre-intent run framing")
	}
	if got := RunLine(Code("unknown")); got != "" {
		t.Fatalf("unknown run code = %q, want fail-closed empty output", got)
	}

	invalid := validResult()
	invalid.RunID = "../private"
	if got := ResultLine(invalid); got != "" {
		t.Fatalf("invalid result framing = %q, want empty", got)
	}
	if got := SuccessOutput(invalid); got != "" {
		t.Fatalf("invalid success output = %q, want empty", got)
	}
	invalid = validResult()
	invalid.PrimaryCode = "mmio_failed"
	invalid.PrimaryDetail = "failure"
	if got := SuccessOutput(invalid); got != "" {
		t.Fatalf("failure success output = %q, want empty", got)
	}
}

func TestFramingByteExactStageFixtures(t *testing.T) {
	failed := validFailureResult()
	fixtures := []struct {
		name   string
		stdout string
		stderr string
	}{
		{
			name:   "preflight success",
			stdout: "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n",
		},
		{
			name:   "preflight failure",
			stderr: "FOGCAST_FPGA_DEV_PREFLIGHT code=manifest_rejected\n",
		},
		{
			name:   "run pre-intent failure",
			stderr: "FOGCAST_FPGA_DEV_RUN code=generation_failed\n",
		},
		{
			name:   "post-intent success",
			stdout: "FPGA> OSS FPGA OK\nFOGCAST_FPGA_DEV_RESULT run_id=" + validResult().RunID + " primary=ok\n",
		},
		{
			name:   "post-intent failure",
			stdout: "FOGCAST_FPGA_DEV_RESULT run_id=" + failed.RunID + " primary=protocol_violation\n",
			stderr: "invalid mailbox word\n",
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			var stdout, stderr string
			switch fixture.name {
			case "preflight success":
				stdout = PreflightLine(CodeOK)
			case "preflight failure":
				stderr = PreflightLine(CodeManifestRejected)
			case "run pre-intent failure":
				stderr = RunLine(CodeGenerationFailed)
			case "post-intent success":
				stdout = SuccessOutput(validResult())
			case "post-intent failure":
				stdout = ResultLine(failed)
				stderr = failed.PrimaryDetail + "\n"
			}
			if stdout != fixture.stdout || stderr != fixture.stderr {
				t.Fatalf("stdout=%q stderr=%q, want stdout=%q stderr=%q", stdout, stderr, fixture.stdout, fixture.stderr)
			}
			if strings.ContainsAny(stdout+stderr, "\\/\r") {
				t.Fatalf("fixture contains path/control separator: stdout=%q stderr=%q", stdout, stderr)
			}
			for _, stream := range []string{stdout, stderr} {
				if stream != "" && !strings.HasSuffix(stream, "\n") {
					t.Fatalf("stream lacks exactly terminal newline: %q", stream)
				}
			}
		})
	}
}

func TestResultStoreFaultPreservesPendingBytes(t *testing.T) {
	store, result := newResultStoreFixture(t)
	if err := store.Create(result); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.PathFor(result.RunID))
	store.SyncParent = func(*os.File) error { return errors.New("injected parent fsync") }
	if err := store.MarkRecoveryFailed(result); err == nil {
		t.Fatal("injected conditional-update failure was ignored")
	}
	after, _ := os.ReadFile(store.PathFor(result.RunID))
	if !bytes.Equal(after, before) {
		t.Fatal("conditional-update fault changed pending bytes")
	}
}
