package softwareagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestDrainCancelsAndWaitsForOutstandingMutation(t *testing.T) {
	f := NewFence(false)
	ctx, done, err := f.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- f.Drain(context.Background()) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("mutation not cancelled")
	}
	if _, _, err := f.Begin(context.Background()); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
	select {
	case <-drained:
		t.Fatal("drained before handler left")
	default:
	}
	done()
	done()
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain stuck")
	}
	f.Open()
	_, done, err = f.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done()
}
func TestDrainTimeoutKeepsFenceClosed(t *testing.T) {
	f := NewFence(false)
	_, done, _ := f.Begin(context.Background())
	defer done()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := f.Begin(context.Background()); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
}
func TestFenceAllowsStatusButBlocksCoreAndImageWrites(t *testing.T) {
	f := NewFence(true)
	h := f.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/v1/health", 204}, {"POST", "/v1/core-packages", 503}, {"PUT", "/v1/core-selections/fes.pong", 503}, {"POST", "/v1/update/activate", 503}, {"POST", "/v1/update/rollback", 204}, {"GET", "/v1/kit/software", 204}, {"POST", "/v1/kit/software/cancel", 204}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		if tc.status == 503 {
			var envelope protocol.ErrorEnvelope
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != protocol.CodeBusy {
				t.Fatalf("%s returned undecodable or non-busy rejection: %q (%v)", tc.path, w.Body.String(), err)
			}
		}
	}
}

func TestOpenFenceTracksRollbackBeforeSoftwareMaintenance(t *testing.T) {
	f := NewFence(false)
	started, cancelled, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := f.Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		w.WriteHeader(http.StatusNoContent)
		close(finished)
	}))
	w := httptest.NewRecorder()
	go h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/update/rollback", nil))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("rollback did not enter the software fence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.Drain(ctx); err != nil {
		t.Fatal("software maintenance did not drain rollback", err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("rollback context was not cancelled by software maintenance")
	}
	select {
	case <-finished:
	default:
		t.Fatal("software fence released before rollback left")
	}
	if w.Code != http.StatusNoContent {
		t.Fatal("rollback handler did not finish", w.Code)
	}
}
