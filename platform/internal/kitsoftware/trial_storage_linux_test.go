package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
)

// These are the actual held-back e412 selection and f607 factory manifests.
// Their metadata exercises real compatibility; file descriptors below model
// storage captured by processes at startup, not an emulated kit or SQLite API.
func trialStorageManifests(t *testing.T) (map[string]software.Manifest, software.Compatibility, string, string) {
	t.Helper()
	manifests := map[string]software.Manifest{}
	ids := []string{}
	for _, name := range []string{"old", "factory"} {
		f, err := os.Open("testdata/trial-storage-" + name + "-manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		m, err := software.DecodeManifest(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		id, err := m.ID()
		if err != nil {
			t.Fatal(err)
		}
		manifests[id] = m
		ids = append(ids, id)
	}
	if ids[0] != "a0e9e3c208a925240d5fdd558b413477d1ea2bf9005ebfddaa7692eb111980f0" || ids[1] != "ea5b37ccd1d0e252dc77f7429e29beb6ca56869a3b1e7418a0be5803aba50c01" {
		t.Fatal("actual manifest identities changed", ids)
	}
	raw, err := os.ReadFile("testdata/trial-storage-base.json")
	if err != nil {
		t.Fatal(err)
	}
	var base software.Compatibility
	if err = json.Unmarshal(raw, &base); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(manifests[ids[0]].Compatible(base), software.ErrIncompatible) {
		t.Fatal("old selection must be held back")
	}
	if err = manifests[ids[1]].Compatible(base); err != nil {
		t.Fatal(err)
	}
	return manifests, base, ids[0], ids[1]
}

type trialStorageControl struct {
	pc            *ProcessController
	manifests     map[string]software.Manifest
	base          software.Compatibility
	directory     string
	admission     *fakeAdmission
	starts, stops int
}

func (c *trialStorageControl) Verify(_ context.Context, id string) error {
	m, ok := c.manifests[id]
	if !ok {
		return errors.New("unknown manifest")
	}
	return m.Compatible(c.base)
}
func (c *trialStorageControl) Active() string { return c.pc.Active() }
func (c *trialStorageControl) Stop(ctx context.Context) error {
	if c.admission.acquires == 0 || c.admission.busy {
		return errors.New("stop before admission")
	}
	c.stops++
	return c.pc.Stop(ctx)
}
func (c *trialStorageControl) Start(ctx context.Context, id string, _ Request) error {
	c.starts++
	c.pc.active = id
	script := `case "$2" in
host) root="$1/temporary"; if [ -f "$1/persistent" ]; then root="$1/durable"; fi; exec 3<>"$root/library.sqlite3" ;;
agent) if [ -f "$1/persistent" ]; then exec 3<"$1/durable/packages"; fi ;;
esac
exec sleep 60`
	for _, role := range []string{roleRuntime, roleAgent, roleLauncher, roleHost} {
		p, err := c.pc.spawn("/bin/sh", []string{"-c", script, "storage-child", c.directory, role}, os.Environ(), role)
		if err != nil {
			return err
		}
		if role == roleHost {
			c.pc.host = p
		} else {
			c.pc.children = append(c.pc.children, p)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, hostErr := os.Readlink(fmt.Sprintf("/proc/%d/fd/3", c.pc.host.cmd.Process.Pid))
		_, persistentErr := os.Stat(filepath.Join(c.directory, "persistent"))
		_, agentErr := os.Readlink(fmt.Sprintf("/proc/%d/fd/3", c.pc.children[1].cmd.Process.Pid))
		if hostErr == nil && (errors.Is(persistentErr, os.ErrNotExist) || agentErr == nil) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("storage child not ready")
		}
		time.Sleep(time.Millisecond)
	}
}
func (c *trialStorageControl) Healthy(_ context.Context, _ string) error {
	for _, p := range append(append([]*child{}, c.pc.children...), c.pc.host) {
		select {
		case <-p.done:
			return errors.New("storage process exited")
		default:
		}
	}
	return nil
}
func processStoragePath(t *testing.T, p *child) string {
	t.Helper()
	path, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/3", p.cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrialStorageRefreshReopensHeldBackFactory(t *testing.T) {
	manifests, base, selected, factory := trialStorageManifests(t)
	ctx := context.Background()
	directory := t.TempDir()
	for _, name := range []string{"temporary", "durable"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"durable/library.sqlite3": "existing durable catalog", "durable/packages": "existing package library"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := OpenRepository(filepath.Join(directory, "control"), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = repo.Update(func(s *State) error { s.Selected = selected; s.Generation = 39; return nil }); err != nil {
		t.Fatal(err)
	}
	admission := &fakeAdmission{}
	pc := &ProcessController{RunDir: filepath.Join(directory, "run"), Output: io.Discard}
	defer func() {
		if err := pc.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	control := &trialStorageControl{pc: pc, manifests: manifests, base: base, directory: directory, admission: admission}
	if err = control.Start(ctx, factory, Request{}); err != nil {
		t.Fatal(err)
	}
	old := append(append([]*child{}, pc.children...), pc.host)
	if got := processStoragePath(t, pc.host); got != filepath.Join(directory, "temporary/library.sqlite3") {
		t.Fatal(got)
	}
	if _, err = os.Readlink(fmt.Sprintf("/proc/%d/fd/3", pc.children[1].cmd.Process.Pid)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("bootstrap agent unexpectedly opened durable packages", err)
	}
	// Confirmation opens persistent storage before the existing atomic handoff.
	if err = os.WriteFile(filepath.Join(directory, "persistent"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	hold := SuccessorHold{}
	lease := &trialLease{held: true}
	if err = HandoffTrial(ctx, repo, restore, &hold, func(context.Context, Request) error { return lease.Install() }, nil, nil, func(context.Context) error { return lease.Release() }); err != nil {
		t.Fatal(err)
	}
	if !hold.Ready || !lease.queued || lease.held {
		t.Fatal("successor was not installed before release")
	}
	now := time.Unix(100, 0)
	engine := &Engine{Repository: repo, Controller: control, Admission: admission, Factory: factory, TrialStorageRefreshRequest: hold.Request.ID, Stable: time.Millisecond, Poll: time.Millisecond, Deadline: time.Second, Now: func() time.Time { return now }, Wait: func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }}
	if err = engine.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if control.starts != 2 || control.stops != 1 || engine.TrialStorageRefreshRequest != "" {
		t.Fatalf("starts=%d stops=%d refresh=%s", control.starts, control.stops, engine.TrialStorageRefreshRequest)
	}
	for _, p := range old {
		assertProcessGone(t, p.cmd.Process.Pid)
	}
	if got := processStoragePath(t, pc.host); got != filepath.Join(directory, "durable/library.sqlite3") {
		t.Fatal("host kept temporary catalog", got)
	}
	if got := processStoragePath(t, pc.children[1]); got != filepath.Join(directory, "durable/packages") {
		t.Fatal("agent did not reopen package library", got)
	}
	if raw, err := os.ReadFile(filepath.Join(directory, "durable/library.sqlite3")); err != nil || string(raw) != "existing durable catalog" {
		t.Fatal("durable catalog changed", err)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Selected != selected || state.Generation != 39 || state.Pending != nil || state.Unreleased != nil || engine.HeldBack != selected || state.Receipts[restore.ID].Result != "recovered" {
		t.Fatalf("state=%+v held=%s", state, engine.HeldBack)
	}
	if err = engine.Run(ctx); err != nil || control.starts != 2 || control.stops != 1 {
		t.Fatal("completed handoff restarted again", err)
	}
}

func TestTrialStorageRefreshUsesOnlyAdmittedQueuedSuccessor(t *testing.T) {
	for _, mode := range []string{"ordinary", "compatible", "busy", "gate", "cancelled", "changed", "unreleased", "blocked", "recovering", "stop-failure", "readiness-failure"} {
		t.Run(mode, func(t *testing.T) {
			repo, e, c, a := setup(t)
			e.Factory = testFactoryID
			c.active = testFactoryID
			c.incompatible = map[string]bool{testSelectedID: true}
			if err := repo.Update(func(s *State) error { s.Selected = testSelectedID; s.Generation = 39; return nil }); err != nil {
				t.Fatal(err)
			}
			request := Request{ID: strings.Repeat("d", 64), Candidate: testSelectedID, Requester: "kit-software-supervisor", Generation: 39}
			if mode == "compatible" {
				request.Candidate = testFactoryID
			}
			if err := Queue(repo, request); err != nil {
				t.Fatal(err)
			}
			e.TrialStorageRefreshRequest = request.ID
			switch mode {
			case "ordinary":
				e.TrialStorageRefreshRequest = ""
			case "busy":
				a.busy = true
			case "gate":
				e.Gate = func(context.Context) error { return errors.New("trial gate closed") }
			case "cancelled":
				if err := Cancel(repo, request.ID, 39); err != nil {
					t.Fatal(err)
				}
			case "changed":
				e.TrialStorageRefreshRequest = strings.Repeat("e", 64)
			case "unreleased":
				if err := complete(repo, *mustStatePending(t, repo), "queued", "recovered", "held back"); err != nil {
					t.Fatal(err)
				}
			case "blocked":
				if err := transition(repo, *mustStatePending(t, repo), "queued", "blocked", "spent"); err != nil {
					t.Fatal(err)
				}
			case "recovering":
				if err := transition(repo, *mustStatePending(t, repo), "queued", "applying", ""); err != nil {
					t.Fatal(err)
				}
				if err := transition(repo, *mustStatePending(t, repo), "applying", "recovering", "spent"); err != nil {
					t.Fatal(err)
				}
			case "stop-failure":
				c.stopErr = true
			case "readiness-failure":
				c.fail[testFactoryID] = true
			}
			err := e.Run(context.Background())
			wantStarts, wantStops := 0, 0
			if mode == "compatible" {
				wantStarts, wantStops = 1, 1
				if err != nil || e.TrialStorageRefreshRequest != "" {
					t.Fatal(err, e.TrialStorageRefreshRequest)
				}
			}
			if mode == "stop-failure" || mode == "readiness-failure" {
				wantStops = 1
				if mode == "readiness-failure" {
					wantStarts = 1
				}
				if err == nil {
					t.Fatal("stop error swallowed")
				}
				c.stopErr = false
				if again := e.Run(context.Background()); again == nil {
					t.Fatal("blocked stop failure retried")
				}
			}
			state, stateErr := repo.Read()
			if stateErr != nil {
				t.Fatal(stateErr)
			}
			wantGeneration := uint64(39)
			if mode == "compatible" {
				wantGeneration = 40
			}
			if state.Generation != wantGeneration {
				t.Fatal("unexpected selection change", state)
			}
			if c.starts != wantStarts || c.stops != wantStops {
				t.Fatalf("starts=%d stops=%d error=%v", c.starts, c.stops, err)
			}
			if mode == "busy" || mode == "gate" || mode == "blocked" || mode == "recovering" {
				if err == nil {
					t.Fatal("failure swallowed")
				}
			}
		})
	}
}
