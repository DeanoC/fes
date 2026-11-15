package kitsoftware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
)

type pausedUpload struct {
	source  io.Reader
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (p *pausedUpload) Read(b []byte) (int, error) {
	p.once.Do(func() { close(p.entered); <-p.resume })
	return p.source.Read(b)
}

// A listing or activation queued behind an upload must see the complete
// published release, never its in-progress staging directory.
func TestInventoryAndActivationWaitForConcurrentInstall(t *testing.T) {
	ctx := context.Background()
	base, payloadRoot, archive := softwareFixture(t, "base")
	compat := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "base", Interfaces: map[string]string{"runtime": "1"}}
	store, err := software.New(filepath.Join(t.TempDir(), "store"), compat)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(ctx, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "state"), base.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	controller := &ProcessController{active: base.ID, Resolve: store.Path}
	admission, err := NewUnixAdmission(filepath.Join(t.TempDir(), "absent.sock"), filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Repository: repo, Store: store, Controller: controller, Admission: admission}

	candidate := base.Manifest
	candidate.Version = "test-next"
	candidateID, err := candidate.ID()
	if err != nil {
		t.Fatal(err)
	}
	var candidateArchive bytes.Buffer
	if err := software.WriteBundle(&candidateArchive, candidate, os.DirFS(payloadRoot)); err != nil {
		t.Fatal(err)
	}
	upload := &pausedUpload{source: bytes.NewReader(candidateArchive.Bytes()), entered: make(chan struct{}), resume: make(chan struct{})}
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(upload.resume) }) }
	defer resume()

	postInstall := httptest.NewRequest(http.MethodPost, "/v1/kit/software/releases", upload)
	installed := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); server.ServeHTTP(w, postInstall); installed <- w }()
	<-upload.entered

	request := Request{ID: strings.Repeat("a", 64), Candidate: candidateID, Requester: "host", Generation: 0}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	listed := make(chan *httptest.ResponseRecorder, 1)
	activated := make(chan *httptest.ResponseRecorder, 1)
	started := make(chan struct{}, 2)
	go func() {
		started <- struct{}{}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/kit/software/releases", nil))
		listed <- w
	}()
	go func() {
		started <- struct{}{}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/activate", bytes.NewReader(body)))
		activated <- w
	}()
	<-started
	<-started
	select {
	case w := <-listed:
		t.Fatalf("inventory completed during incomplete upload: %d %s", w.Code, w.Body)
	case w := <-activated:
		t.Fatalf("activation completed during incomplete upload: %d %s", w.Code, w.Body)
	case <-time.After(100 * time.Millisecond):
	}
	resume()
	if w := <-installed; w.Code != http.StatusCreated {
		t.Fatalf("install: %d %s", w.Code, w.Body)
	}
	w := <-listed
	if w.Code != http.StatusOK {
		t.Fatalf("inventory: %d %s", w.Code, w.Body)
	}
	var releases []software.Release
	if err := json.Unmarshal(w.Body.Bytes(), &releases); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, release := range releases {
		ids[release.ID] = true
	}
	if len(releases) != 2 || !ids[base.ID] || !ids[candidateID] {
		t.Fatalf("inventory did not contain both fully published releases: %v", ids)
	}
	w = <-activated
	if w.Code != http.StatusOK {
		t.Fatalf("activation: %d %s", w.Code, w.Body)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending == nil || state.Pending.Candidate != candidateID {
		t.Fatalf("candidate not queued: %+v", state.Pending)
	}
}
