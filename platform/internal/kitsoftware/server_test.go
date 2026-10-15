package kitsoftware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
)

func softwareFixture(t *testing.T, base string) (software.Release, string, []byte) {
	t.Helper()
	root := t.TempDir()
	m := software.Manifest{Format: 1, Board: "de10-nano", Architecture: "armv7", BaseContract: base, Version: "test", Interfaces: map[string]string{"runtime": "1"}, Provenance: map[string]string{"source": "test"}}
	data := []byte("executable-" + base)
	sum := sha256.Sum256(data)
	for _, name := range software.RequiredExecutables {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0555); err != nil {
			t.Fatal(err)
		}
		m.Files = append(m.Files, software.File{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), Mode: 0555})
	}
	b, err := m.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), b, 0444); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := software.WriteBundle(&archive, m, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	id, err := m.ID()
	if err != nil {
		t.Fatal(err)
	}
	return software.Release{ID: id, Manifest: m}, root, archive.Bytes()
}
func TestServerRejectsIncompatibleHistoryAndQueuesFactory(t *testing.T) {
	ctx := context.Background()
	incompatible, _, oldArchive := softwareFixture(t, "old-base")
	factory, factoryRoot, factoryArchive := softwareFixture(t, "new-base")
	root := filepath.Join(t.TempDir(), "store")
	base := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "old-base", Interfaces: map[string]string{"runtime": "1"}}
	oldStore, err := software.New(root, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := oldStore.Install(ctx, bytes.NewReader(oldArchive)); err != nil {
		t.Fatal(err)
	}
	base.BaseContract = "new-base"
	store, err := software.New(root, base)
	if err != nil {
		t.Fatal(err)
	}
	// Historical discovery remains valid across a base contract change.
	if _, err := store.Read(ctx, incompatible.ID); err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "state"), incompatible.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	controller := &ProcessController{active: incompatible.ID, Resolve: func(ctx context.Context, id string) (string, error) {
		if id == factory.ID {
			release, err := software.VerifyDirectory(ctx, factoryRoot)
			if err != nil {
				return "", err
			}
			return factoryRoot, release.Manifest.Compatible(base)
		}
		return store.Path(ctx, id)
	}}
	admission, err := NewUnixAdmission(filepath.Join(t.TempDir(), "absent.sock"), filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Repository: repo, Store: store, Factory: &factory, Controller: controller, Admission: admission}
	post := func(request Request) *httptest.ResponseRecorder {
		body, _ := json.Marshal(request)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/activate", bytes.NewReader(body)))
		return w
	}
	request := Request{ID: strings.Repeat("a", 64), Candidate: incompatible.ID, Requester: "host", Generation: 0}
	if got := post(request); got.Code != 400 {
		t.Fatal(got.Code, got.Body.String())
	}
	state, _ := repo.Read()
	if state.Pending != nil || len(state.Receipts) != 0 {
		t.Fatal("incompatible historical release altered durable state")
	}
	request.Candidate = factory.ID
	request.Generation = 1
	if got := post(request); got.Code != 409 {
		t.Fatal("stale factory selection accepted", got.Code)
	}
	request.Generation = 0
	if got := post(request); got.Code != 200 {
		t.Fatal("factory could not be selected", got.Code, got.Body.String())
	}
	state, _ = repo.Read()
	if state.Pending == nil || state.Pending.Candidate != factory.ID {
		t.Fatal(state)
	}
	// Factory is listed even before durable installation, and never duplicated.
	for _, install := range []bool{false, true} {
		if install {
			if _, _, err := store.Install(ctx, bytes.NewReader(factoryArchive)); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/kit/software/releases", nil))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var releases []software.Release
		if err := json.Unmarshal(w.Body.Bytes(), &releases); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, release := range releases {
			if release.ID == factory.ID {
				count++
			}
		}
		if count != 1 {
			t.Fatal("factory count", count)
		}
	}
	if !errors.Is(Queue(repo, Request{ID: strings.Repeat("b", 64), Candidate: factory.ID, Requester: "host"}), ErrConflict) {
		t.Fatal("factory displaced pending request")
	}
}

func TestFactorySelectionSurvivesBaseImageReplacement(t *testing.T) {
	ctx := context.Background()
	old, oldRoot, _ := softwareFixture(t, "base-contract")
	base := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "base-contract", Interfaces: map[string]string{"runtime": "1"}}
	store, err := software.New(filepath.Join(t.TempDir(), "store"), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := RetainFactory(ctx, store, old, oldRoot); err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "control"), old.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	// The new OS has a different factory manifest but the same explicit base ABI.
	replacement := old.Manifest
	replacement.Version = "replacement"
	replacementID, err := replacement.ID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := replacement.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(oldRoot, "manifest.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldRoot, "manifest.json"), b, 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(oldRoot, "manifest.json"), 0444); err != nil {
		t.Fatal(err)
	}
	next := software.Release{ID: replacementID, Manifest: replacement}
	if err := RetainFactory(ctx, store, next, oldRoot); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Selected != old.ID {
		t.Fatal("new OS silently changed durable selection")
	}
	path, err := store.Path(ctx, state.Selected)
	if err != nil {
		t.Fatal("prior factory was not retained", err)
	}
	retained, err := software.VerifyDirectory(ctx, path)
	if err != nil || retained.ID != old.ID {
		t.Fatal(retained, err)
	}
	if _, err := store.Path(ctx, next.ID); err != nil {
		t.Fatal("new factory not retained", err)
	}
}
