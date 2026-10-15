package kitsoftware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEstablishedRepositoryNeverRecreatesMissingState(t *testing.T) {
	r, _, _, _ := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "state.json")); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenRepository(r.root, testFactoryID); err == nil {
		reopened.Close()
		t.Fatal("missing established state reset to factory")
	}
	if _, err := os.Stat(filepath.Join(r.root, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("failed open mutated missing state: %v", err)
	}
}

func TestInterruptedInitialCreation(t *testing.T) {
	for _, stage := range []string{"empty", "temporary", "published"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			if stage == "temporary" {
				if err := os.WriteFile(filepath.Join(root, "state.tmp"), []byte(`{"version":`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "published" {
				initial := State{Version: 1, Selected: testCandidateID, Receipts: map[string]Receipt{}}
				b, _ := json.Marshal(initial)
				if err := os.WriteFile(filepath.Join(root, "state.json"), b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			r, err := OpenRepository(root, testFactoryID)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			s, err := r.Read()
			want := testFactoryID
			if stage == "published" {
				want = testCandidateID
			}
			if err != nil || s.Selected != want {
				t.Fatalf("initial selection lost: %+v %v", s, err)
			}
			if initialized, err := r.initialized(); !initialized || err != nil {
				t.Fatalf("initialization not marked: %v %v", initialized, err)
			}
		})
	}
}

func TestMissingMarkerWithEstablishedArtifactsFailsClosed(t *testing.T) {
	for _, artifact := range []string{"admission.key", "worker.lock", ".grant-old"} {
		t.Run(artifact, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, artifact), []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			if r, err := OpenRepository(root, testFactoryID); err == nil {
				r.Close()
				t.Fatal("established artifact ignored")
			}
		})
	}
	r, _, _, _ := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, initializedMarker)); err != nil {
		t.Fatal(err)
	}
	if reopened, err := OpenRepository(r.root, testFactoryID); err == nil {
		reopened.Close()
		t.Fatal("pending state accepted without initialization marker")
	}
}

func TestCorruptStateAndMarkerFailClosed(t *testing.T) {
	for _, corruption := range []string{"json", "fifo", "marker"} {
		t.Run(corruption, func(t *testing.T) {
			r, _, _, _ := setup(t)
			name := filepath.Join(r.root, "state.json")
			var err error
			switch corruption {
			case "json":
				err = os.WriteFile(name, []byte("broken"), 0600)
			case "fifo":
				if err = os.Remove(name); err == nil {
					err = unix.Mkfifo(name, 0600)
				}
			case "marker":
				err = os.WriteFile(filepath.Join(r.root, initializedMarker), []byte("broken"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if reopened, err := OpenRepository(r.root, testFactoryID); err == nil {
				reopened.Close()
				t.Fatal("corruption accepted")
			}
		})
	}
}

func TestInvalidBundleIdentitiesAndOversizedWritesPreserveState(t *testing.T) {
	r, _, _, _ := setup(t)
	for _, mutate := range []func(*State){
		func(s *State) { s.Selected = "factory" },
		func(s *State) { s.Previous = "old" },
		func(s *State) {
			p := req()
			p.Candidate = "candidate"
			s.Pending = &Operation{Request: p, Previous: s.Selected, Phase: "queued"}
		},
		func(s *State) {
			p := req()
			s.Pending = &Operation{Request: p, Previous: s.Selected, Phase: "queued", Reason: strings.Repeat("x", maxStateSize)}
		},
	} {
		if err := r.Update(func(s *State) error { mutate(s); return nil }); err == nil {
			t.Fatal("invalid or oversized state persisted")
		}
		if s, err := r.Read(); err != nil || s.Selected != testFactoryID || s.Pending != nil {
			t.Fatalf("failed write damaged state: %+v %v", s, err)
		}
	}
	bad := req()
	bad.Candidate = "candidate"
	if err := Queue(r, bad); err == nil {
		t.Fatal("noncanonical candidate queued")
	}
}
