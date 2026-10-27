package kitsoftware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
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

func TestServerRefusesReleaseLackingRequiredInterfaceAndReportsHoldBack(t *testing.T) {
	ctx := context.Background()
	release, _, archive := softwareFixture(t, "base")
	root := filepath.Join(t.TempDir(), "store")
	base := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "base", Interfaces: map[string]string{"runtime": "1"}}
	older, err := software.New(root, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := older.Install(ctx, bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	// The next image's menu needs an interface this release's runtime does not accept.
	base.Requires = map[string]string{"menu.fes.video.menu-blitter": "1.0"}
	store, err := software.New(root, base)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "state"), release.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	admission, err := NewUnixAdmission(filepath.Join(t.TempDir(), "absent.sock"), filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	controller := &ProcessController{active: release.ID, Resolve: func(ctx context.Context, id string) (string, error) { return store.Path(ctx, id) }}
	server := &Server{Repository: repo, Store: store, Controller: controller, Admission: admission}
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/releases", bytes.NewReader(archive)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "menu.fes.video.menu-blitter 1.0") {
		t.Fatal("install admitted a release this image cannot run", w.Code, w.Body.String())
	}
	body, _ := json.Marshal(Request{ID: strings.Repeat("a", 64), Candidate: release.ID, Requester: "host"})
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/activate", bytes.NewReader(body)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "menu.fes.video.menu-blitter 1.0") {
		t.Fatal("activation queued a release this image cannot run", w.Code, w.Body.String())
	}
	if state, _ := repo.Read(); state.Pending != nil {
		t.Fatal("refused activation was queued", state.Pending)
	}
	server.HeldBack, server.HeldBackReason = release.ID, "incompatible software: release does not accept required interface menu.fes.video.menu-blitter 1.0"
	server.PublishStatus()
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/kit/software", nil))
	var status softwarecontrol.Status
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.HeldBack != release.ID || status.HeldBackReason != server.HeldBackReason || status.Selected != release.ID {
		t.Fatal("status does not report the held-back selection", status)
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

type failUpdateRepo struct {
	Repository
	fail atomic.Bool
}

func (f *failUpdateRepo) Update(fn func(*State) error) error {
	if f.fail.Load() {
		return errors.New("injected repository write failure")
	}
	return f.Repository.Update(fn)
}

type admissionStub struct {
	mu           sync.Mutex
	queued       map[string]bool
	fenced       bool
	failQueue    bool
	dropFence    bool
	rejectFence  bool
	dropCancel   bool
	rejectCancel bool
	order        []string
	entered      chan struct{}
	blockQueue   chan struct{}
}

func (s *admissionStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	s.mu.Lock()
	s.order = append(s.order, r.URL.Path)
	switch r.URL.Path {
	case "/fence":
		if s.rejectFence {
			s.mu.Unlock()
			http.Error(w, "fence rejected", http.StatusInternalServerError)
			return
		}
		s.fenced = true
		drop := s.dropFence
		s.mu.Unlock()
		if drop {
			discardAdmissionResponse(w)
			return
		}
		w.Write([]byte("{}"))
	case "/clear-fence":
		s.fenced = false
		s.mu.Unlock()
		w.Write([]byte("{}"))
	case "/cancel":
		if s.rejectCancel {
			s.mu.Unlock()
			http.Error(w, "cancel rejected", http.StatusInternalServerError)
			return
		}
		id, _ := body["request_id"].(string)
		delete(s.queued, id)
		drop := s.dropCancel
		s.mu.Unlock()
		if drop {
			discardAdmissionResponse(w)
			return
		}
		w.Write([]byte("{}"))
	case "/queue":
		id, _ := body["request_id"].(string)
		fail := s.failQueue
		entered := s.entered
		block := s.blockQueue
		s.mu.Unlock()
		if entered != nil {
			entered <- struct{}{}
			<-block
		}
		if fail {
			http.Error(w, "queue failed", http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.queued[id] = true
		s.mu.Unlock()
		w.Write([]byte("{}"))
	case "/test/claim":
		busy := s.fenced || len(s.queued) > 0
		s.mu.Unlock()
		if busy {
			http.Error(w, "busy", http.StatusConflict)
			return
		}
		w.Write([]byte("{}"))
	default:
		s.mu.Unlock()
		http.NotFound(w, r)
	}
}

func discardAdmissionResponse(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	conn.Close()
}

func listenAdmission(t *testing.T, stub *admissionStub) *UnixAdmission {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "admission.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: stub}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	admission, err := NewUnixAdmission(socket, filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func claimCode(stub *admissionStub) int {
	w := httptest.NewRecorder()
	stub.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/test/claim", nil))
	return w.Code
}

func cancelFixture(t *testing.T, stub *admissionStub) (*Server, *failUpdateRepo, Request) {
	t.Helper()
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	request := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, request); err != nil {
		t.Fatal(err)
	}
	base := software.Compatibility{Board: "de10-nano", Architecture: "armv7", BaseContract: "base", Interfaces: map[string]string{"runtime": "1"}}
	store, err := software.New(filepath.Join(t.TempDir(), "store"), base)
	if err != nil {
		t.Fatal(err)
	}
	failing := &failUpdateRepo{Repository: repo}
	stub.queued = map[string]bool{request.ID: true}
	return &Server{Repository: failing, Store: store, Admission: listenAdmission(t, stub)}, failing, request
}

func TestCancelWriteFailureRestoresQueueBeforeUnlock(t *testing.T) {
	stub := &admissionStub{}
	server, failing, request := cancelFixture(t, stub)
	failing.fail.Store(true)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if server.ExecutionFenced {
		t.Fatal("restored queue left execution fenced")
	}
	if !stub.queued[request.ID] {
		t.Fatal("queue was not restored")
	}
	state, err := failing.Read()
	if err != nil || state.Pending == nil || state.Pending.ID != request.ID {
		t.Fatal("durable request lost", state, err)
	}
	want := []string{"/fence", "/cancel", "/queue", "/clear-fence"}
	if strings.Join(stub.order, ",") != strings.Join(want, ",") {
		t.Fatal(stub.order)
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted", code)
	}
}

func TestCancelWriteFailureFencesClaimWhenRestoreFails(t *testing.T) {
	stub := &admissionStub{failQueue: true, entered: make(chan struct{}), blockQueue: make(chan struct{})}
	server, failing, request := cancelFixture(t, stub)
	failing.fail.Store(true)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
		close(done)
	}()
	select {
	case <-stub.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("restore did not start before the handler returned")
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted while cancellation was failing", code)
	}
	close(stub.blockQueue)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel handler did not finish")
	}
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !server.ExecutionFenced || !stub.fenced {
		t.Fatal("execution fence opened after restore failed", server.ExecutionFenced, stub.fenced)
	}
	state, err := failing.Read()
	if err != nil || state.Pending == nil || state.Pending.ID != request.ID {
		t.Fatal("durable request lost", state, err)
	}
	stub.mu.Lock()
	stub.failQueue = false
	stub.entered = nil
	stub.blockQueue = nil
	stub.mu.Unlock()
	if err := server.ReconcileExecutionFence(context.Background()); err != nil {
		t.Fatal(err)
	}
	if server.ExecutionFenced {
		t.Fatal("fence stayed closed after reconciliation")
	}
	if !stub.queued[request.ID] {
		t.Fatal("reconciliation did not restore the queue")
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted after reconciliation", code)
	}
}

func TestCancelLostResponseRestoresQueueBeforeOpen(t *testing.T) {
	stub := &admissionStub{dropCancel: true}
	server, _, request := cancelFixture(t, stub)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if server.ExecutionFenced {
		t.Fatal("restored queue left execution fenced")
	}
	if !stub.queued[request.ID] || stub.fenced {
		t.Fatal("queue or fence left open", stub.queued, stub.fenced)
	}
	state, err := server.Repository.Read()
	if err != nil || state.Pending == nil || state.Pending.ID != request.ID {
		t.Fatal("durable request lost", state, err)
	}
	want := []string{"/fence", "/cancel", "/queue", "/clear-fence"}
	if strings.Join(stub.order, ",") != strings.Join(want, ",") {
		t.Fatal(stub.order)
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted", code)
	}
}

func TestCancelLostResponseKeepsFenceWhenRestoreFails(t *testing.T) {
	stub := &admissionStub{dropCancel: true, failQueue: true}
	server, _, request := cancelFixture(t, stub)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !server.ExecutionFenced || !stub.fenced || stub.queued[request.ID] {
		t.Fatal("fence opened after a lost cancel", server.ExecutionFenced, stub.fenced, stub.queued)
	}
	stub.mu.Lock()
	stub.failQueue = false
	stub.mu.Unlock()
	if err := server.ReconcileExecutionFence(context.Background()); err != nil {
		t.Fatal(err)
	}
	if server.ExecutionFenced || !stub.queued[request.ID] || stub.fenced {
		t.Fatal("reconciliation did not restore priority before opening", server.ExecutionFenced, stub.queued, stub.fenced)
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted after reconciliation", code)
	}
}

func TestCancelRejectedLeavesQueueAndOpensFence(t *testing.T) {
	stub := &admissionStub{rejectCancel: true}
	server, _, request := cancelFixture(t, stub)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if server.ExecutionFenced || stub.fenced || !stub.queued[request.ID] {
		t.Fatal("known cancel rejection changed the queue or left the fence closed", server.ExecutionFenced, stub.fenced, stub.queued)
	}
	want := []string{"/fence", "/cancel", "/clear-fence"}
	if strings.Join(stub.order, ",") != strings.Join(want, ",") {
		t.Fatal(stub.order)
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted", code)
	}
}

func TestFenceLostResponseReconcilesBeforeAdmittingClaim(t *testing.T) {
	stub := &admissionStub{dropFence: true}
	server, _, request := cancelFixture(t, stub)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	stub.mu.Lock()
	fenced, queued := stub.fenced, stub.queued[request.ID]
	stub.mu.Unlock()
	if !server.ExecutionFenced || !fenced || !queued {
		t.Fatal("lost fence reply was treated as open", server.ExecutionFenced, fenced, queued)
	}
	stub.mu.Lock()
	delete(stub.queued, request.ID)
	stub.mu.Unlock()
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted while the lost fence was closed", code)
	}
	if err := server.ReconcileExecutionFence(context.Background()); err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	fenced, queued = stub.fenced, stub.queued[request.ID]
	order := append([]string(nil), stub.order...)
	stub.mu.Unlock()
	if server.ExecutionFenced || fenced || !queued {
		t.Fatal("reconciliation cleared priority or left the fence closed", server.ExecutionFenced, fenced, queued)
	}
	var calls []string
	for _, path := range order {
		if path != "/test/claim" {
			calls = append(calls, path)
		}
	}
	want := []string{"/fence", "/queue", "/clear-fence"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatal(calls)
	}
	if code := claimCode(stub); code != 409 {
		t.Fatal("claim admitted over the restored queue", code)
	}
	stub.mu.Lock()
	delete(stub.queued, request.ID)
	stub.mu.Unlock()
	if code := claimCode(stub); code != 200 {
		t.Fatal("fence stayed closed after reconciliation", code)
	}
}

func TestFenceRejectedStaysOpen(t *testing.T) {
	stub := &admissionStub{rejectFence: true}
	server, _, request := cancelFixture(t, stub)
	body := []byte(`{"id":"` + request.ID + `","expected_generation":0}`)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/kit/software/cancel", bytes.NewReader(body)))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
	if server.ExecutionFenced || stub.fenced || !stub.queued[request.ID] {
		t.Fatal("rejected fence was recorded as closed", server.ExecutionFenced, stub.fenced, stub.queued)
	}
	if strings.Join(stub.order, ",") != "/fence" {
		t.Fatal(stub.order)
	}
	stub.mu.Lock()
	delete(stub.queued, request.ID)
	stub.mu.Unlock()
	if code := claimCode(stub); code != 200 {
		t.Fatal("known-open fence blocked a claim", code)
	}
}
