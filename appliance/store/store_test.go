package appliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	release "github.com/DeanoC/FogCast/appliance"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func manifest(b []byte) release.Manifest {
	return release.Manifest{Format: 1, Board: release.Board, BootABI: release.BootABI, Version: "v1", KernelSHA256: strings.Repeat("a", 64), ImageSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), ImageSize: int64(len(b)), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
}
func fixture(t *testing.T) (*Store, release.Manifest, []byte) {
	t.Helper()
	b := make([]byte, 4096)
	b[1080] = 0x53
	b[1081] = 0xef
	b[1120] = 0x40
	m := manifest(b)
	s, e := New(t.TempDir(), m)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	return s, m, b
}
func candidate(t *testing.T, s *Store, b []byte) release.Manifest {
	t.Helper()
	b = bytes.Clone(b)
	b[0]++
	m := manifest(b)
	if e := s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestTrialConsumptionAndConfirmation(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if e := s.Activate(m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	sel, e := s.BeginBoot("boot-a")
	if e != nil || !sel.Trial || sel.Manifest.ImageSHA256 != m.ImageSHA256 {
		t.Fatalf("trial %+v %v", sel, e)
	}
	if _, e = s.BeginBoot("boot-a"); e == nil {
		t.Fatal("trial replay accepted")
	}
	if e = s.Confirm("stale", m.ImageSHA256); e == nil {
		t.Fatal("stale confirmation accepted")
	}
	if e = s.Confirm("boot-a", f.ImageSHA256); e == nil {
		t.Fatal("wrong image confirmed")
	}
	if e = s.Confirm("boot-a", m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if e = s.Confirm("boot-a", m.ImageSHA256); e != nil {
		t.Fatal("confirmation retry", e)
	}
	st, e := s.Status()
	if e != nil || st.Good != m.ImageSHA256 || st.Previous != f.ImageSHA256 || st.TrialImage != "" {
		t.Fatalf("status %+v %v", st, e)
	}
	if e = s.Rollback(); e != nil {
		t.Fatal(e)
	}
	sel, e = s.BeginBoot("boot-b")
	if e != nil || !sel.Trial || sel.Manifest.ImageSHA256 != f.ImageSHA256 {
		t.Fatalf("rollback %+v %v", sel, e)
	}
	sel, e = s.BeginBoot("boot-c")
	if e != nil || sel.Trial || sel.Manifest.ImageSHA256 != m.ImageSHA256 {
		t.Fatalf("fallback %+v %v", sel, e)
	}
	if e = s.Confirm("boot-b", f.ImageSHA256); e == nil {
		t.Fatal("old boot confirmation accepted")
	}
}

func TestVisibleConfirmationRequiresSuccessfulPersistenceBeforeGuardAccepts(t *testing.T) {
	s, _, b := fixture(t)
	m := candidate(t, s, b)
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("trial-boot"); err != nil {
		t.Fatal(err)
	}
	// A directory-fsync failure happens AFTER rename made confirmed state
	// visible. The reader must establish durability itself before disarming.
	s.syncDirectory = func(string) error { return unix.EIO }
	if err := s.Confirm("trial-boot", m.ImageSHA256); !errors.Is(err, unix.EIO) {
		t.Fatalf("missing injected fsync failure: %v", err)
	}
	st, err := s.Status()
	if err != nil || st.ConfirmedImage != m.ImageSHA256 {
		t.Fatalf("fault did not reach post-rename state: %+v %v", st, err)
	}
	if ok, err := s.ConfirmedContext(context.Background(), "trial-boot", m.ImageSHA256); ok || !errors.Is(err, unix.EIO) {
		t.Fatalf("guard accepted undurable confirmation: %v %v", ok, err)
	}
	if err := s.Confirm("trial-boot", m.ImageSHA256); !errors.Is(err, unix.EIO) {
		t.Fatalf("retry skipped durability: %v", err)
	}
	s.syncDirectory = syncDir
	if ok, err := s.ConfirmedContext(context.Background(), "trial-boot", m.ImageSHA256); !ok || err != nil {
		t.Fatalf("guard could not establish durability: %v %v", ok, err)
	}
	if err := s.Confirm("trial-boot", m.ImageSHA256); err != nil {
		t.Fatalf("durable retry failed: %v", err)
	}
}

func TestConfirmedContextRequiresExactIdentityAndStateFileSync(t *testing.T) {
	s, _, b := fixture(t)
	m := candidate(t, s, b)
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("trial-boot"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ConfirmedContext(context.Background(), "trial-boot", m.ImageSHA256); ok || err != nil {
		t.Fatalf("unconfirmed trial accepted: %v %v", ok, err)
	}
	if err := s.Confirm("trial-boot", m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][2]string{{"stale", m.ImageSHA256}, {"trial-boot", strings.Repeat("0", 64)}} {
		if ok, err := s.ConfirmedContext(context.Background(), ids[0], ids[1]); ok || err != nil {
			t.Fatalf("wrong identity accepted: %v %v", ok, err)
		}
	}
	s.syncFile = func(*os.File) error { return unix.EIO }
	if ok, err := s.ConfirmedContext(context.Background(), "trial-boot", m.ImageSHA256); ok || !errors.Is(err, unix.EIO) {
		t.Fatalf("state file sync failure accepted: %v %v", ok, err)
	}
}

func TestRejectedTrialDoesNotBlockNewUpdateOrReplayInSameBoot(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("trial-boot"); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][2]string{{"stale", m.ImageSHA256}, {"trial-boot", f.ImageSHA256}} {
		if err := s.RejectTrialContext(context.Background(), ids[0], ids[1]); err == nil {
			t.Fatal("wrong attempt rejected")
		}
	}
	if err := s.RejectTrialContext(context.Background(), "trial-boot", m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil || st.Good != f.ImageSHA256 || st.TrialImage != "" || st.Pending != "" || st.ConfirmedImage != "" {
		t.Fatalf("fallback state incorrect: %+v %v", st, err)
	}
	if _, err := s.BeginBoot("trial-boot"); err == nil {
		t.Fatal("rejected trial replayed in same boot")
	}
	if err := s.Confirm("trial-boot", m.ImageSHA256); err == nil {
		t.Fatal("rejected trial later confirmed")
	}
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatalf("fallback left updates blocked: %v", err)
	}
}

func TestRejectionSyncFailureMustBeReportedToBootstrap(t *testing.T) {
	s, _, b := fixture(t)
	m := candidate(t, s, b)
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("trial-boot"); err != nil {
		t.Fatal(err)
	}
	s.syncDirectory = func(string) error { return unix.EIO }
	if err := s.RejectTrialContext(context.Background(), "trial-boot", m.ImageSHA256); !errors.Is(err, unix.EIO) {
		t.Fatalf("bootstrap was told uncertain rejection succeeded: %v", err)
	}
}

func TestRecordedFactoryFallbackBecomesNextUpdatesActualPrevious(t *testing.T) {
	s, f, b := fixture(t)
	bad := candidate(t, s, b)
	if err := s.Activate(bad.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("first"); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm("first", bad.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	path, _ := s.ImagePath(bad.ImageSHA256)
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	sel, err := s.BeginBoot("fallback")
	if err != nil || sel.Trial || sel.Manifest.ImageSHA256 != f.ImageSHA256 {
		t.Fatalf("factory not selected: %+v %v", sel, err)
	}
	if err := s.RecordFallbackContext(context.Background(), "fallback", f.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status()
	if err != nil || st.Good != f.ImageSHA256 || st.Previous != "" {
		t.Fatalf("invalid known-good retained: %+v %v", st, err)
	}
	b = bytes.Clone(b)
	b[0] = 10
	next := candidate(t, s, b)
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("next"); err != nil {
		t.Fatal(err)
	}
	if err := s.Confirm("next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	st, err = s.Status()
	if err != nil || st.Previous != f.ImageSHA256 {
		t.Fatalf("rollback does not select actual preceding factory: %+v %v", st, err)
	}
}

func TestFallbackRecordRejectsWrongBootUnselectedImageAndActiveTrial(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if _, err := s.BeginBoot("good"); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][2]string{{"wrong", f.ImageSHA256}, {"good", m.ImageSHA256}} {
		if err := s.RecordFallbackContext(context.Background(), ids[0], ids[1]); err == nil {
			t.Fatal("unprepared or wrong-boot fallback accepted")
		}
	}
	if err := s.RecordFallbackContext(context.Background(), "good", f.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("trial"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFallbackContext(context.Background(), "trial", f.ImageSHA256); err == nil {
		t.Fatal("fallback silently erased consumed trial")
	}
	if err := s.RejectTrialContext(context.Background(), "trial", m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFallbackContext(context.Background(), "trial", f.ImageSHA256); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackPreservesCorruptEvidenceAndReportsFailedSync(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if _, err := s.BeginBoot("good"); err != nil {
		t.Fatal(err)
	}
	s.syncDirectory = func(string) error { return unix.EIO }
	if err := s.RecordFallbackContext(context.Background(), "good", f.ImageSHA256); !errors.Is(err, unix.EIO) {
		t.Fatalf("fallback durability failure hidden: %v", err)
	}
	path := filepath.Join(s.root, "state.json")
	corrupt := []byte("torn-state-evidence")
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFallbackContext(context.Background(), "recovery", f.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFallbackContext(context.Background(), "recovery", m.ImageSHA256); err == nil {
		t.Fatal("nonfactory accepted with corrupt state")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, corrupt) {
		t.Fatalf("corrupt evidence overwritten: %q %v", got, err)
	}
}

func TestLeaseCancellationCannotSelectOrConfirmAfterWaitingForStore(t *testing.T) {
	s, factory, b := fixture(t)
	m := candidate(t, s, b)
	unlock, err := s.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err = s.ActivateContext(ctx, m.ImageSHA256); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("activation: %v", err)
	}
	unlock()
	st, _ := s.Status()
	if st.Pending != "" {
		t.Fatal("canceled owner selected a pending image")
	}
	if err = s.Activate(m.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginBoot("new-boot"); err != nil {
		t.Fatal(err)
	}
	if err = s.ConfirmContext(ctx, "new-boot", m.ImageSHA256); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("confirmation: %v", err)
	}
	st, _ = s.Status()
	if st.Good != factory.ImageSHA256 || st.TrialImage != m.ImageSHA256 {
		t.Fatal("canceled owner confirmed trial")
	}
}
func TestRejectedUploadsLeaveSelectionAndImages(t *testing.T) {
	s, f, b := fixture(t)
	before, e := s.Status()
	if e != nil {
		t.Fatal(e)
	}
	bad := bytes.Clone(b)
	bad[0]++
	m := manifest(bad)
	for _, payload := range [][]byte{bad[:100], append(bytes.Clone(bad), 0), b} {
		if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(payload)); e == nil {
			t.Fatal("bad upload accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = s.Stage(ctx, m, m.ImageSize, bytes.NewReader(bad)); e == nil {
		t.Fatal("canceled accepted")
	}
	bad[1080] = 0
	m = manifest(bad)
	if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(bad)); e == nil {
		t.Fatal("non ext4 accepted")
	}
	after, e := s.Status()
	if e != nil || before != after {
		t.Fatalf("selection changed %+v -> %+v %v", before, after, e)
	}
	if _, e = s.Verify(f.ImageSHA256); e != nil {
		t.Fatal("factory changed", e)
	}
	files, e := os.ReadDir(filepath.Join(s.root, "images"))
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 1 {
		t.Fatalf("unexpected files %v", files)
	}
}
func TestCorruptStateFallsBackToFactory(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if e := s.Activate(m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(s.root, "state.json"), []byte(`{"payload":`), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := s.Status()
	if e != nil || !st.Corrupt || st.Good != f.ImageSHA256 {
		t.Fatalf("corrupt status %+v %v", st, e)
	}
	sel, e := s.BeginBoot("new-boot")
	if e != nil || sel.Trial || sel.Manifest.ImageSHA256 != f.ImageSHA256 {
		t.Fatalf("fallback %+v %v", sel, e)
	}
	if e = s.Activate(m.ImageSHA256); e == nil {
		t.Fatal("corrupt state mutated")
	}
}

func TestPublicationFailureCanResumeWithoutImageOverwrite(t *testing.T) {
	s, _, b := fixture(t)
	b = bytes.Clone(b)
	b[0] = 7
	m := manifest(b)
	dest := filepath.Join(s.root, "manifests", m.ImageSHA256+".json")
	if e := os.Mkdir(dest, 0700); e != nil {
		t.Fatal(e)
	}
	if e := s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e == nil {
		t.Fatal("manifest publication failure ignored")
	}
	path, _ := s.ImagePath(m.ImageSHA256)
	before, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(dest); e != nil {
		t.Fatal(e)
	}
	if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal("could not repair interrupted publication:", e)
	}
	after, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	if !os.SameFile(before, after) {
		t.Fatal("immutable image replaced")
	}
}
func TestExt2IsNotAcceptedAsExt4(t *testing.T) {
	s, _, b := fixture(t)
	b = bytes.Clone(b)
	b[1120] = 0
	m := manifest(b)
	if e := s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e == nil {
		t.Fatal("ext2 filesystem accepted")
	}
}
func TestStateWriteFailureDoesNotSelectCandidate(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if e := os.Mkdir(filepath.Join(s.root, "state.json"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := s.Activate(m.ImageSHA256); e == nil {
		t.Fatal("state storage error ignored")
	}
	if e := os.Remove(filepath.Join(s.root, "state.json")); e != nil {
		t.Fatal(e)
	}
	st, e := s.Status()
	if e != nil || st.Good != f.ImageSHA256 || st.Pending != "" {
		t.Fatalf("state changed %+v %v", st, e)
	}
}
func TestConsumedStateSurvivesReopenAndInvalidGoodUsesFactory(t *testing.T) {
	s, f, b := fixture(t)
	m := candidate(t, s, b)
	if e := s.Activate(m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginBoot("first"); e != nil {
		t.Fatal(e)
	}
	reopened, e := New(s.root, f)
	if e != nil {
		t.Fatal(e)
	}
	sel, e := reopened.BeginBoot("second")
	if e != nil || sel.Trial || sel.Manifest.ImageSHA256 != f.ImageSHA256 {
		t.Fatalf("restart fallback %+v %v", sel, e)
	}
	if e = s.Activate(m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e = s.BeginBoot("third"); e != nil {
		t.Fatal(e)
	}
	if e = s.Confirm("third", m.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	st, e := s.Status()
	if e != nil || st.ConfirmedBootID != "third" || st.ConfirmedImage != m.ImageSHA256 {
		t.Fatalf("confirmation evidence %+v %v", st, e)
	}
	p, _ := s.ImagePath(m.ImageSHA256)
	if e = os.WriteFile(p, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	sel, e = s.BeginBoot("fourth")
	if e != nil || sel.Trial || sel.Manifest.ImageSHA256 != f.ImageSHA256 {
		t.Fatalf("corrupt good fallback %+v %v", sel, e)
	}
}
func TestContextStatusCancelsWhileAnotherHandleOwnsLock(t *testing.T) {
	s, _, _ := fixture(t)
	unlock, e := s.lock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.StatusContext(ctx); e != context.Canceled {
		t.Fatalf("status ignored cancellation: %v", e)
	}
}

func TestChecksummedStateStillRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, extra := range []string{`,"unknown":1}`, `,"good":"` + strings.Repeat("a", 64) + `"}`} {
		t.Run(extra, func(t *testing.T) {
			s, f, b := fixture(t)
			m := candidate(t, s, b)
			if e := s.Activate(m.ImageSHA256); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(s.root, "state.json")
			raw, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			var env envelope
			if e = json.Unmarshal(raw, &env); e != nil {
				t.Fatal(e)
			}
			env.Payload = append(bytes.TrimSuffix(env.Payload, []byte("}")), []byte(extra)...)
			env.SHA256 = fmt.Sprintf("%x", sha256.Sum256(env.Payload))
			raw, e = json.Marshal(env)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			st, e := s.Status()
			if e != nil || !st.Corrupt || st.Good != f.ImageSHA256 {
				t.Fatalf("semantically malformed state accepted: %+v %v", st, e)
			}
		})
	}
}
func TestPartialFilesystemWriteFailureLeavesSelection(t *testing.T) {
	if os.Getenv("FOGCAST_STORE_WRITE_FAILURE_CHILD") == "1" {
		s, f, b := fixture(t)
		b = bytes.Clone(b)
		b[0] = 9
		m := manifest(b)
		var before unix.Rlimit
		if e := unix.Getrlimit(unix.RLIMIT_FSIZE, &before); e != nil {
			t.Fatal(e)
		}
		limited := before
		limited.Cur = 2048
		if e := unix.Setrlimit(unix.RLIMIT_FSIZE, &limited); e != nil {
			t.Fatal(e)
		}
		defer unix.Setrlimit(unix.RLIMIT_FSIZE, &before)
		if e := s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e == nil {
			t.Fatal("partial filesystem write accepted")
		}
		st, e := s.Status()
		if e != nil || st.Good != f.ImageSHA256 || st.Pending != "" {
			t.Fatalf("failed write changed state: %+v %v", st, e)
		}
		files, e := os.ReadDir(filepath.Join(s.root, "images"))
		if e != nil || len(files) != 1 {
			t.Fatalf("failed staging residue %v %v", files, e)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestPartialFilesystemWriteFailureLeavesSelection$")
	cmd.Env = append(os.Environ(), "FOGCAST_STORE_WRITE_FAILURE_CHILD=1")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("storage failure child: %v\n%s", e, b)
	}
}
