package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contract "github.com/DeanoC/FogCast/kitlease"
)

func stopGeneration(h http.Handler, generation string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"expected_generation": generation})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/local/stop", strings.NewReader(string(body))))
	return w
}

func TestHTTPStopRejectsOldSamePackageGeneration(t *testing.T) {
	runtime := &fakeRuntime{}
	s, _, _, _ := testService(t, runtime)
	id := pongID(t, s)
	a, err := s.Launch(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(s)
	if w := stopGeneration(h, a.Generation); w.Code != http.StatusOK {
		t.Fatalf("stop A: %d %s", w.Code, w.Body.String())
	}
	b, err := s.Launch(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Generation == b.Generation || !sha256Hex(b.Generation) {
		t.Fatal("claim generations not unique")
	}
	_, before := runtime.calls()
	for _, gen := range []string{a.Generation, "", "not-a-generation"} {
		if w := stopGeneration(h, gen); w.Code != http.StatusConflict {
			t.Fatalf("stale/missing stop: %d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/local/stop", nil))
	if w.Code != http.StatusConflict {
		t.Fatal("empty body accepted")
	}
	_, after := runtime.calls()
	if after != before || s.RunStatus().Generation != b.Generation || !s.RunStatus().Running {
		t.Fatal("stale Stop affected B")
	}
	s.clearLostSession(a.Generation) // opaque old token cannot clear a newer claim either
	if s.RunStatus().Generation != b.Generation {
		t.Fatal("stale lost-session callback erased B")
	}
	if err := s.Stop(t.Context(), b.Generation); err != nil {
		t.Fatal(err)
	}
}

type delayedReleaseGate struct {
	LeaseGate
	released chan struct{}
	resume   chan struct{}
	token    string
}

func (g *delayedReleaseGate) Release(token string) (contract.Status, error) {
	st, err := g.LeaseGate.Release(token)
	if token == g.token {
		if _, e := g.LeaseGate.Settled(context.Background()); e != nil {
			return st, e
		}
		close(g.released)
		<-g.resume
	}
	return st, err
}

func TestOldStopCompletionCannotCancelReplacementRenewal(t *testing.T) {
	runtime := &fakeRuntime{}
	s, m, _, _ := testService(t, runtime)
	a, err := s.Launch(t.Context(), pongID(t, s))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	oldToken := s.token
	s.mu.Unlock()
	gate := &delayedReleaseGate{LeaseGate: m, released: make(chan struct{}), resume: make(chan struct{}), token: oldToken}
	s.leases = gate
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop(context.Background(), a.Generation) }()
	select {
	case <-gate.released:
	case <-time.After(3 * time.Second):
		t.Fatal("A did not release")
	}
	b, err := s.Launch(t.Context(), a.PackageID)
	if err != nil {
		close(gate.resume)
		t.Fatal(err)
	}
	s.mu.Lock()
	newDone := s.renewDone
	newToken := s.token
	s.mu.Unlock()
	close(gate.resume)
	select {
	case err := <-stopped:
		if !errors.Is(err, errInUse) {
			t.Fatalf("A completion: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("A Stop blocked")
	}
	s.clearLostSession(oldToken)
	st := s.RunStatus()
	if !st.Running || st.Generation != b.Generation || st.PackageID != b.PackageID {
		t.Fatalf("B overwritten: %+v", st)
	}
	select {
	case <-newDone:
		t.Fatal("B renewal was cancelled by old Stop")
	default:
	}
	s.mu.Lock()
	same := s.token == newToken && s.renewDone == newDone
	s.mu.Unlock()
	if !same {
		t.Fatal("B renewal replaced")
	}
	if err := s.Stop(t.Context(), b.Generation); err != nil {
		t.Fatal(err)
	}
}

type failedStopGate struct {
	LeaseGate
	failBegin   bool
	failRelease bool
	staleStatus bool
	token       string
}

func (g *failedStopGate) Begin(token string) (context.Context, func(), error) {
	if g.failBegin {
		_, _ = g.LeaseGate.Release(token)
		_, _ = g.LeaseGate.Settled(context.Background())
		return nil, nil, errors.New("revoked")
	}
	return g.LeaseGate.Begin(token)
}
func (g *failedStopGate) Release(token string) (contract.Status, error) {
	st, err := g.LeaseGate.Release(token)
	if g.failRelease {
		return st, errors.New("release reply lost")
	}
	return st, err
}
func (g *failedStopGate) Status() contract.Status {
	st := g.LeaseGate.Status()
	if g.staleStatus {
		g.staleStatus = false
		_, _ = g.LeaseGate.Release(g.token)
		_, _ = g.LeaseGate.Settled(context.Background())
	}
	return st
}
func TestFailedStopDoesNotReviveRevokedGeneration(t *testing.T) {
	for _, mode := range []string{"begin", "runtime", "release", "status-snapshot"} {
		t.Run(mode, func(t *testing.T) {
			runtime := &fakeRuntime{}
			s, m, _, _ := testService(t, runtime)
			a, err := s.Launch(t.Context(), pongID(t, s))
			if err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			token := s.token
			s.mu.Unlock()
			gate := &failedStopGate{LeaseGate: m, token: token, failBegin: mode == "begin", failRelease: mode == "release", staleStatus: mode == "status-snapshot"}
			s.leases = gate
			if mode == "runtime" || mode == "status-snapshot" {
				runtime.stopErr = errors.New("stop failed")
				if mode == "runtime" {
					runtime.onStop = func() { _, _ = m.Release(token) }
				}
			}
			if err := s.Stop(t.Context(), a.Generation); err == nil {
				t.Fatal("fault was hidden")
			}
			st := s.RunStatus()
			if st.Phase != phaseIdle || st.Running || st.Generation != "" {
				t.Fatalf("revoked claim survived error: %+v", st)
			}
		})
	}
}

func TestLaunchPublishesAndReturnsItsExactGeneration(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	runtime := &fakeRuntime{onLoad: func() { close(entered); <-finish }}
	s, m, _, _ := testService(t, runtime)
	result := make(chan LaunchResult, 1)
	failure := make(chan error, 1)
	go func() { r, e := s.Launch(context.Background(), pongID(t, s)); result <- r; failure <- e }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no load")
	}
	launching := s.RunStatus()
	if launching.Phase != phaseLaunching || launching.Running || launching.Generation != m.Status().Generation || !sha256Hex(launching.Generation) {
		t.Fatalf("launch status %+v", launching)
	}
	if err := s.Stop(t.Context(), launching.Generation); !errors.Is(err, errInUse) {
		t.Fatalf("Stop interrupted programming: %v", err)
	}
	close(finish)
	r := <-result
	if err := <-failure; err != nil {
		t.Fatal(err)
	}
	if r.Generation != launching.Generation || s.RunStatus().Generation != r.Generation {
		t.Fatal("launch returned a different claim")
	}
	if err := s.Stop(t.Context(), r.Generation); err != nil {
		t.Fatal(err)
	}
	if st := s.RunStatus(); st.Generation != "" || st.Phase != phaseIdle {
		t.Fatalf("idle retains generation %+v", st)
	}
}
