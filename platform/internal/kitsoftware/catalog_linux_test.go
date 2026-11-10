package kitsoftware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The helper's real SQLite/WAL/fault behavior is covered in FogCast's
// catalogcheckpoint tests. This fixture exercises the real platform journal,
// generation checks and file identities using a deterministic stopped image.
type catalogFixture struct {
	t                *testing.T
	repo             *FileRepository
	manager          *CatalogTransitions
	schema           int
	restores         int
	failCheckpoint   bool
	failAfterRestore bool
	retained         map[string][]byte
	retainedSchemas  map[string]int
	helperCalls      []string
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	base := t.TempDir()
	repo, err := OpenRepository(filepath.Join(base, "control"), testFactoryID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	manager, err := OpenCatalogTransitions(repo, filepath.Join(base, "fogcast-host"), testCandidateID, func(context.Context, string) (string, error) { return "", errors.New("fixture helper not injected") })
	if err != nil {
		t.Fatal(err)
	}
	f := &catalogFixture{t: t, repo: repo, manager: manager, retained: map[string][]byte{}, retainedSchemas: map[string]int{}}
	manager.command = f.command
	return f
}

func imageFor(data []byte, schema int) catalogImage {
	if data == nil {
		return catalogImage{}
	}
	hash := sha256.Sum256(data)
	return catalogImage{Present: true, Schema: schema, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
}

func (f *catalogFixture) set(schema int, contents string) {
	f.t.Helper()
	f.schema = schema
	if err := os.WriteFile(f.manager.database, []byte(contents), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *catalogFixture) live() []byte {
	f.t.Helper()
	data, err := os.ReadFile(f.manager.database)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return data
}

func (f *catalogFixture) command(_ context.Context, helper string, args []string) (catalogImage, error) {
	f.helperCalls = append(f.helperCalls, helper+":"+args[0])
	flags := map[string]string{}
	for i := 1; i < len(args); i += 2 {
		flags[args[i]] = args[i+1]
	}
	data := f.live()
	current := imageFor(data, f.schema)
	switch args[0] {
	case "schema":
		return catalogImage{Schema: 22}, nil
	case "inspect":
		return current, nil
	case "checkpoint":
		if f.failCheckpoint {
			return catalogImage{}, errors.New("injected checkpoint failure")
		}
		root := flags["--snapshot"]
		if err := os.Mkdir(root, 0700); err != nil {
			return catalogImage{}, err
		}
		if current.Present {
			if err := os.WriteFile(filepath.Join(root, "catalog.sqlite3"), data, 0600); err != nil {
				return catalogImage{}, err
			}
		}
		encoded, _ := json.Marshal(current)
		if err := os.WriteFile(filepath.Join(root, "metadata.json"), encoded, 0600); err != nil {
			return catalogImage{}, err
		}
		return current, nil
	case "restore":
		root := flags["--snapshot"]
		encoded, err := os.ReadFile(filepath.Join(root, "metadata.json"))
		if err != nil {
			return catalogImage{}, err
		}
		var target catalogImage
		if err = json.Unmarshal(encoded, &target); err != nil {
			return catalogImage{}, err
		}
		var targetData []byte
		if target.Present {
			targetData, err = os.ReadFile(filepath.Join(root, "catalog.sqlite3"))
			if err != nil {
				return catalogImage{}, err
			}
		}
		expected, _ := strconv.Atoi(flags["--expected-current-schema"])
		retain := flags["--retain"]
		if old, ok := f.retained[retain]; ok {
			if expected != f.retainedSchemas[retain] {
				return catalogImage{}, errors.New("retained source schema mismatch")
			}
			if current != imageFor(old, expected) && current != target {
				return catalogImage{}, errors.New("foreign live catalog")
			}
		} else {
			if expected != f.schema {
				return catalogImage{}, errors.New("unexpected current schema")
			}
			f.retained[retain] = append([]byte(nil), data...)
			f.retainedSchemas[retain] = f.schema
		}
		f.restores++
		if target.Present {
			err = os.WriteFile(f.manager.database, targetData, 0600)
		} else {
			err = os.Remove(f.manager.database)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			return catalogImage{}, err
		}
		f.schema = target.Schema
		if f.failAfterRestore {
			f.failAfterRestore = false
			return catalogImage{}, errors.New("interrupted after atomic restore")
		}
		return target, nil
	default:
		return catalogImage{}, fmt.Errorf("unexpected helper command %s", args[0])
	}
}

func (f *catalogFixture) operation(id, candidate string) Operation {
	f.t.Helper()
	s, err := f.repo.Read()
	if err != nil {
		f.t.Fatal(err)
	}
	req := Request{ID: strings.Repeat(id, 64), Candidate: candidate, Requester: "catalog-test", Generation: s.Generation}
	if err = Queue(f.repo, req); err != nil {
		f.t.Fatal(err)
	}
	s, err = f.repo.Read()
	if err != nil {
		f.t.Fatal(err)
	}
	op := *s.Pending
	if err = transition(f.repo, op, "queued", "applying", ""); err != nil {
		f.t.Fatal(err)
	}
	return op
}

func (f *catalogFixture) accepted(op Operation) {
	f.t.Helper()
	if err := complete(f.repo, op, "applying", "accepted", ""); err != nil {
		f.t.Fatal(err)
	}
	if err := f.repo.Update(func(s *State) error { s.Unreleased = nil; return nil }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *catalogFixture) recovering(op Operation) {
	f.t.Helper()
	if err := transition(f.repo, op, "applying", "recovering", "candidate failed"); err != nil {
		f.t.Fatal(err)
	}
}

func TestCatalogManagerRollbackAndFailedRollbackRecovery(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "original entries firmware settings")
	upgrade := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), upgrade, upgrade.Previous); err != nil {
		t.Fatal(err)
	}
	f.set(22, "new catalog with remote entries")
	f.accepted(upgrade)
	rollback := f.operation("2", testFactoryID)
	if err := f.manager.Apply(context.Background(), rollback, rollback.Previous); err != nil {
		t.Fatal(err)
	}
	if string(f.live()) != "original entries firmware settings" || f.schema != 21 {
		t.Fatalf("baseline catalog was not restored: %q schema %d", f.live(), f.schema)
	}
	retained := filepath.Join(f.manager.opRoot(rollback.ID), "rollback-retain")
	if string(f.retained[retained]) != "new catalog with remote entries" {
		t.Fatal("newer catalog was not retained")
	}
	// This failure must recover the rollback operation's B22 checkpoint,
	// rather than restoring the original upgrade's A21 checkpoint again.
	f.recovering(rollback)
	if err := f.manager.Recover(context.Background(), rollback, rollback.Previous); err != nil {
		t.Fatal(err)
	}
	if string(f.live()) != "new catalog with remote entries" || f.schema != 22 {
		t.Fatalf("wrong failed-rollback recovery: %q schema %d", f.live(), f.schema)
	}
	if err := f.manager.Recover(context.Background(), rollback, rollback.Previous); err != nil {
		t.Fatalf("completed restore replay failed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(f.manager.snapshot(upgrade.ID), "catalog.sqlite3")); err != nil || string(data) != "original entries firmware settings" {
		t.Fatalf("prior checkpoint changed: %q %v", data, err)
	}
}

func TestCatalogManagerSameFormatPreservesEdits(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "old selection")
	up := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), up, up.Previous); err != nil {
		t.Fatal(err)
	}
	f.set(21, "new user edits on same schema")
	f.accepted(up)
	down := f.operation("2", testFactoryID)
	if err := f.manager.Apply(context.Background(), down, down.Previous); err != nil {
		t.Fatal(err)
	}
	if f.restores != 0 || string(f.live()) != "new user edits on same schema" {
		t.Fatal("same-format return restored stale data")
	}
}

func TestCatalogManagerHeldBackFactoryDoesNotPoisonBaselineCheckpoint(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "baseline A21 entries")
	upgrade := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), upgrade, testFactoryID); err != nil {
		t.Fatal(err)
	}
	f.set(22, "candidate catalog")
	f.accepted(upgrade)
	// The durable selection is A, but this image cannot run A and is running
	// factory F instead. Its catalog must never be remembered as A's catalog.
	if err := f.repo.Update(func(s *State) error { s.Selected = testFactoryID; return nil }); err != nil {
		t.Fatal(err)
	}
	factory := strings.Repeat("e", 64)
	next := strings.Repeat("d", 64)
	f.set(22, "factory F22 entries")
	fromFactory := f.operation("2", next)
	if err := f.manager.Apply(context.Background(), fromFactory, factory); err != nil {
		t.Fatal(err)
	}
	record, err := f.manager.read(fromFactory.ID)
	if err != nil || record.Previous != testFactoryID || record.Source != factory {
		t.Fatalf("actual source was confused with selection: %+v %v", record, err)
	}
	f.accepted(fromFactory)
	f.set(22, "next D22 entries")
	baseline := f.operation("3", testFactoryID)
	if err := f.manager.Apply(context.Background(), baseline, next); err != nil {
		t.Fatal(err)
	}
	if f.schema != 21 || string(f.live()) != "baseline A21 entries" {
		t.Fatalf("factory catalog poisoned baseline checkpoint: schema=%d live=%q", f.schema, f.live())
	}
}

func TestCatalogManagerUnknownActualSourceFailsBeforeCheckpoint(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "protected existing catalog")
	op := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), op, ""); err == nil {
		t.Fatal("missing actual source was inferred from selection")
	}
	if len(f.helperCalls) != 0 || string(f.live()) != "protected existing catalog" {
		t.Fatal("unknown actual source authorized catalog work")
	}
}

func TestCatalogManagerFactoryTmpfsCheckpointDoesNotRevertSupportedLiveSchema(t *testing.T) {
	f := newCatalogFixture(t)
	// B22 is the repaired factory/helper. During trial its running host uses
	// tmpfs, while the durable database still contains the selected A21 data.
	f.set(21, "durable A21 before factory reconciliation")
	op := f.operation("1", strings.Repeat("d", 64))
	if err := f.manager.Apply(context.Background(), op, testCandidateID); err != nil {
		t.Fatal(err)
	}
	f.accepted(op)
	// A later compatible host has migrated the durable database to22 and the
	// user has edited it. Returning to B22 must preserve those current edits.
	f.set(22, "current edited catalog on schema22")
	baseline := f.operation("2", testCandidateID)
	if err := f.manager.Apply(context.Background(), baseline, baseline.Previous); err != nil {
		t.Fatal(err)
	}
	if f.restores != 0 || f.schema != 22 || string(f.live()) != "current edited catalog on schema22" {
		t.Fatalf("factory's older durable checkpoint replaced supported live data: restores=%d schema=%d live=%q", f.restores, f.schema, f.live())
	}
	if !slices.Contains(f.helperCalls, testCandidateID+":schema") {
		t.Fatal("repaired factory schema was not authenticated/probed")
	}
}

func TestCatalogManagerRecoveryRefusesUnreadableSelectionButAllowsFactory(t *testing.T) {
	f := newCatalogFixture(t)
	// A is still the durable selection, while actual factory B owns schema22.
	// Returning to compatible-by-base-contract A can fail its host startup.
	f.set(22, "factory B22 catalog")
	op := f.operation("1", testFactoryID)
	if err := f.manager.Apply(context.Background(), op, testCandidateID); err != nil {
		t.Fatal(err)
	}
	f.set(21, "failed A attempt current data")
	f.recovering(op)
	if err := f.manager.Recover(context.Background(), op, testFactoryID); err == nil {
		t.Fatal("unproven legacy A was allowed to start against the B22 checkpoint")
	}
	if f.restores != 0 || string(f.live()) != "failed A attempt current data" {
		t.Fatal("refused recovery replaced current catalog")
	}
	// If this image held A back and resolved recovery to B, that actual target
	// can read its own schema22 checkpoint and safely recover it.
	if err := f.manager.Recover(context.Background(), op, testCandidateID); err != nil {
		t.Fatal(err)
	}
	if f.schema != 22 || string(f.live()) != "factory B22 catalog" {
		t.Fatal("resolved factory recovery restored the wrong catalog")
	}
}

func TestCatalogManagerRecoveryAcceptsKnownFactoryAndObservedLegacySchema(t *testing.T) {
	for _, target := range []string{"known-factory", "observed-legacy"} {
		t.Run(target, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.set(21, "A21 catalog")
			rememberA := f.operation("1", strings.Repeat("d", 64))
			if err := f.manager.Apply(context.Background(), rememberA, testFactoryID); err != nil {
				t.Fatal(err)
			}
			f.accepted(rememberA)
			if err := f.repo.Update(func(s *State) error { s.Selected = testFactoryID; return nil }); err != nil {
				t.Fatal(err)
			}
			f.set(21, "durable A21 while factory uses tmpfs")
			op := f.operation("2", testFactoryID)
			if err := f.manager.Apply(context.Background(), op, strings.Repeat("e", 64)); err != nil {
				t.Fatal(err)
			}
			f.set(21, "failed candidate edits")
			f.recovering(op)
			recovery := testFactoryID
			if target == "known-factory" {
				recovery = testCandidateID
			}
			if err := f.manager.Recover(context.Background(), op, recovery); err != nil {
				t.Fatal(err)
			}
			if f.schema != 21 || string(f.live()) != "durable A21 while factory uses tmpfs" {
				t.Fatal("readable recovery did not restore its own snapshot")
			}
		})
	}
}

func TestCatalogManagerRecoveryReplaysAfterAtomicRestore(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "original")
	op := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), op, op.Previous); err != nil {
		t.Fatal(err)
	}
	f.set(22, "candidate data")
	f.recovering(op)
	f.failAfterRestore = true
	if err := f.manager.Recover(context.Background(), op, op.Previous); err == nil {
		t.Fatal("injected interruption ignored")
	}
	if err := f.manager.Recover(context.Background(), op, op.Previous); err != nil {
		t.Fatalf("restore replay failed: %v", err)
	}
	if f.schema != 21 || string(f.live()) != "original" {
		t.Fatal("original not restored")
	}
	retained := filepath.Join(f.manager.opRoot(op.ID), "recovery-retain")
	if string(f.retained[retained]) != "candidate data" {
		t.Fatal("replay replaced newer retained catalog")
	}
}

func TestCatalogManagerRefusesForeignLiveReplay(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "original")
	op := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), op, op.Previous); err != nil {
		t.Fatal(err)
	}
	f.set(22, "candidate data")
	f.recovering(op)
	f.failAfterRestore = true
	if err := f.manager.Recover(context.Background(), op, op.Previous); err == nil {
		t.Fatal("interruption ignored")
	}
	f.set(21, "unrelated newer operation")
	if err := f.manager.Recover(context.Background(), op, op.Previous); err == nil {
		t.Fatal("foreign catalog overwritten")
	}
	if string(f.live()) != "unrelated newer operation" {
		t.Fatal("foreign live bytes changed")
	}
}

func TestCatalogManagerMissingAndCorruptSnapshotsFailClosed(t *testing.T) {
	for _, failure := range []string{"missing-record", "missing-snapshot", "corrupt-snapshot", "corrupt-metadata", "foreign-operation", "replaced-state", "replaced-journal"} {
		t.Run(failure, func(t *testing.T) {
			f := newCatalogFixture(t)
			f.set(21, "original")
			op := f.operation("1", testCandidateID)
			if failure != "missing-record" {
				if err := f.manager.Apply(context.Background(), op, op.Previous); err != nil {
					t.Fatal(err)
				}
			}
			f.set(22, "candidate protected data")
			f.recovering(op)
			switch failure {
			case "missing-snapshot":
				if err := os.RemoveAll(f.manager.snapshot(op.ID)); err != nil {
					t.Fatal(err)
				}
			case "corrupt-snapshot":
				if err := os.WriteFile(filepath.Join(f.manager.snapshot(op.ID), "catalog.sqlite3"), []byte("corrupt!"), 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt-metadata":
				if err := os.WriteFile(filepath.Join(f.manager.snapshot(op.ID), "metadata.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "foreign-operation":
				op.Requester = "foreign-owner"
			case "replaced-state":
				state := filepath.Dir(f.manager.database)
				if err := os.Rename(state, state+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(state, 0700); err != nil {
					t.Fatal(err)
				}
				f.set(22, "foreign replacement catalog")
			case "replaced-journal":
				if err := os.Rename(f.manager.root, f.manager.root+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.manager.root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := string(f.live())
			if err := f.manager.Recover(context.Background(), op, op.Previous); err == nil {
				t.Fatalf("%s recovery accepted", failure)
			}
			if string(f.live()) != before || f.restores != 0 {
				t.Fatal("rejected recovery changed live catalog")
			}
		})
	}
}

func TestCatalogManagerFailedPreparationNeverReplacesCatalog(t *testing.T) {
	f := newCatalogFixture(t)
	f.set(21, "original")
	op := f.operation("1", testCandidateID)
	f.failCheckpoint = true
	if err := f.manager.Apply(context.Background(), op, op.Previous); err == nil {
		t.Fatal("checkpoint failure ignored")
	}
	f.recovering(op)
	if err := f.manager.Recover(context.Background(), op, op.Previous); err != nil {
		t.Fatal(err)
	}
	if f.restores != 0 || string(f.live()) != "original" {
		t.Fatal("incomplete preparation replaced catalog")
	}
}

func TestCatalogManagerAbsentOriginalAndHelperIdentity(t *testing.T) {
	f := newCatalogFixture(t)
	op := f.operation("1", testCandidateID)
	if err := f.manager.Apply(context.Background(), op, op.Previous); err != nil {
		t.Fatal(err)
	}
	f.set(22, "new candidate catalog")
	f.recovering(op)
	// A later configured factory must not silently substitute its helper for
	// the authenticated helper recorded by this operation.
	f.manager.helper = strings.Repeat("d", 64)
	if err := f.manager.Recover(context.Background(), op, op.Previous); err != nil {
		t.Fatal(err)
	}
	if f.live() != nil {
		t.Fatal("absent original was not restored")
	}
	for _, call := range f.helperCalls {
		if !strings.HasPrefix(call, testCandidateID+":") {
			t.Fatalf("changed recorded helper: %s", call)
		}
	}
}
