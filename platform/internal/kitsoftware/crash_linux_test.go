package kitsoftware

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SIGKILL bypasses defers and releases flock through actual process death.
// This covers process crashes, not loss of the filesystem's volatile cache.
func crashPhase(s State) string {
	if s.Pending != nil {
		return s.Pending.Phase
	}
	if s.Unreleased != nil {
		return s.Receipts[s.Unreleased.ID].Result
	}
	if len(s.Receipts) != 0 {
		return "released"
	}
	return "initial"
}
func crashAt(t *testing.T, event, target string) {
	t.Helper()
	if event != target {
		return
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {}
}

type crashControl struct {
	*fakeControl
	t            *testing.T
	root, target string
}

func (c *crashControl) Stop(ctx context.Context) error {
	stage := "candidate"
	if c.stops > 0 {
		stage = "previous"
	}
	crashAt(c.t, "before-stop:"+stage, c.target)
	err := c.fakeControl.Stop(ctx)
	crashAt(c.t, "after-stop:"+stage, c.target)
	return err
}

type crashAdmission struct {
	fakeAdmission
	t      *testing.T
	target string
}

func (a *crashAdmission) Release(ctx context.Context, r Request) error {
	crashAt(a.t, "before-release", a.target)
	err := a.fakeAdmission.Release(ctx, r)
	crashAt(a.t, "after-release", a.target)
	return err
}

func (c *crashControl) Start(ctx context.Context, id string, r Request) error {
	role := "candidate"
	if id == testFactoryID {
		role = "previous"
	}
	crashAt(c.t, "before-start:"+role, c.target)
	f, err := os.OpenFile(filepath.Join(c.root, "starts"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err = f.WriteString(role + "\n"); err != nil {
		c.t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		c.t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		c.t.Fatal(err)
	}
	crashAt(c.t, "after-start:"+role, c.target)
	return c.fakeControl.Start(ctx, id, r)
}
func crashEngine(t *testing.T, root, target string, fail bool) (*FileRepository, *Engine) {
	t.Helper()
	r, err := OpenRepository(root, testFactoryID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	r.writeCheckpoint = func(stage string, s State) { crashAt(t, crashPhase(s)+":"+stage, target) }
	c := &crashControl{&fakeControl{fail: map[string]bool{testCandidateID: fail}}, t, root, target}
	now := time.Unix(100, 0)
	return r, &Engine{Repository: r, Controller: c, Admission: &crashAdmission{t: t, target: target}, Deadline: time.Second, Stable: 200 * time.Millisecond, Poll: 100 * time.Millisecond, Now: func() time.Time { return now }, Wait: func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }}
}
func TestCrashJournalChild(t *testing.T) {
	root := os.Getenv("FES_CRASH_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	r, e := crashEngine(t, root, os.Getenv("FES_CRASH_TEST_POINT"), os.Getenv("FES_CRASH_TEST_FAIL") == "1")
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash point not reached")
}
func TestProcessCrashAtJournalAndStartBoundaries(t *testing.T) {
	stages := []string{"before-create", "after-create", "after-write", "after-file-sync", "after-close", "before-rename", "after-rename", "after-directory-sync"}
	for _, mode := range []string{"accept", "recover"} {
		phases := []string{"queued", "applying", "accepted", "released"}
		if mode == "recover" {
			phases = []string{"queued", "applying", "recovering", "recovered", "released"}
		}
		var points []string
		for _, phase := range phases {
			for _, stage := range stages {
				points = append(points, phase+":"+stage)
			}
		}
		points = append(points, "before-start:candidate", "after-start:candidate", "before-stop:candidate", "after-stop:candidate", "before-release", "after-release")
		if mode == "recover" {
			points = append(points, "before-start:previous", "after-start:previous", "before-stop:previous", "after-stop:previous")
		}
		for _, point := range points {
			t.Run(mode+"/"+point, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "state")
				r, err := OpenRepository(root, testFactoryID)
				if err != nil {
					t.Fatal(err)
				}
				r.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCrashJournalChild$")
				fail := "0"
				if mode == "recover" {
					fail = "1"
				}
				cmd.Env = append(os.Environ(), "FES_CRASH_TEST_ROOT="+root, "FES_CRASH_TEST_POINT="+point, "FES_CRASH_TEST_FAIL="+fail)
				output, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("checkpoint timed out instead of firing: %v %s", ctx.Err(), output)
				}
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("not killed: %v %s", err, output)
				}
				status, ok := exit.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatalf("not SIGKILL: %v %s", err, output)
				}
				r, e := crashEngine(t, root, "", mode == "recover")
				before, err := r.Read()
				if err != nil {
					t.Fatal(err)
				}
				startsBefore, _ := os.ReadFile(filepath.Join(root, "starts"))
				var runErr error
				if before.Pending != nil && (before.Pending.Phase == "applying" || before.Pending.Phase == "recovering") {
					runErr = e.Boot(context.Background(), Request{})
				} else {
					runErr = e.Run(context.Background())
				}
				after, err := r.Read()
				if err != nil {
					t.Fatal(err)
				}
				startsAfter, _ := os.ReadFile(filepath.Join(root, "starts"))
				if before.Pending != nil && before.Pending.Phase != "queued" && strings.Count(string(startsAfter), "candidate\n") != strings.Count(string(startsBefore), "candidate\n") {
					t.Fatal("candidate replayed")
				}
				if strings.Count(string(startsAfter), "previous\n") > 1 {
					t.Fatal("multiple recovery attempts")
				}
				if before.Pending != nil && before.Pending.Phase == "recovering" {
					if runErr == nil || after.Pending == nil || after.Pending.Phase != "blocked" {
						t.Fatalf("spent recovery reopened: %+v %v", after, runErr)
					}
					if strings.Count(string(startsAfter), "previous\n") != strings.Count(string(startsBefore), "previous\n") {
						t.Fatal("spent recovery restarted")
					}
					return
				}
				if runErr != nil {
					t.Fatal(runErr)
				}
				if before.Selected == testCandidateID && after.Selected != testCandidateID {
					t.Fatal("accepted selection undone")
				}
				if after.Pending != nil || after.Unreleased != nil {
					t.Fatalf("not reconciled: %+v", after)
				}
				wantResult := ""
				wantSelected, wantGeneration := before.Selected, before.Generation
				if before.Pending != nil {
					wantResult = "recovered"
					if before.Pending.Phase == "queued" && mode == "accept" {
						wantResult, wantSelected, wantGeneration = "accepted", testCandidateID, uint64(1)
					}
				} else if receipt, ok := before.Receipts[req().ID]; ok {
					wantResult = receipt.Result
				}
				if after.Selected != wantSelected || after.Generation != wantGeneration || after.Receipts[req().ID].Result != wantResult {
					t.Fatalf("lost queue or wrong completion: before=%+v after=%+v want=%s/%s/%d", before, after, wantResult, wantSelected, wantGeneration)
				}
				old := Operation{Request: req(), Previous: testFactoryID, Phase: "applying"}
				newer := req()
				newer.ID = strings.Repeat("b", 64)
				newer.Candidate = strings.Repeat("d", 64)
				newer.Generation = after.Generation
				if err := Queue(r, newer); err != nil {
					t.Fatal(err)
				}
				if err := e.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				current, _ := r.Read()
				for _, phase := range []string{"applying", "recovering"} {
					if !errors.Is(complete(r, old, phase, "accepted", "stale"), ErrConflict) {
						t.Fatal("stale completion accepted")
					}
				}
				final, _ := r.Read()
				if final.Selected != newer.Candidate || final.Generation != current.Generation || final.Previous != current.Previous {
					t.Fatal("newer selection changed")
				}
				if err := e.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
