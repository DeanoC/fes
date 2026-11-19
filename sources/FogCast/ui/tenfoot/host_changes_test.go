package tenfoot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testwait"
)

// A kit that is its own host can be busy for half a minute with a library
// change, and a room's status read sent meanwhile fails. The room keeps the
// last snapshot it read instead of showing the computer as unavailable. With
// no change of ours in flight, a failed read still reports its error.
func TestRoomHardwareKeepsLastStatusDuringOwnLibraryChange(t *testing.T) {
	var busy atomic.Bool
	release := make(chan struct{})
	arrived := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/hardware":
			if busy.Load() {
				http.Error(w, `{"error":{"code":"UNAVAILABLE","message":"busy"}}`, http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"machines":[],"computers":[{"core_id":"fes.atari-st","ready":true}],"expansion_selection":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/library/core-entries":
			arrived <- struct{}{}
			<-release
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"fpga-new"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	// Cleanups run last-first: release a blocked change before closing.
	t.Cleanup(srv.Close)
	var releaseOnce sync.Once
	releaseChange := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseChange)
	client := NewClient(srv.URL, srv.Client())
	svc := roomServices{client: client}
	ctx := context.Background()

	first, err := svc.Hardware(ctx)
	if err != nil || len(first.Computers) != 1 {
		t.Fatalf("first read = %+v, %v", first, err)
	}

	busy.Store(true)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/library/core-entries", http.NoBody)
		resp, err := client.HTTPClient().Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-arrived:
	case <-time.After(testwait.Timeout):
		t.Fatal("library change never reached the host")
	}

	during, err := svc.Hardware(ctx)
	if err != nil {
		t.Fatalf("read during our library change failed: %v", err)
	}
	if len(during.Computers) != 1 || during.Computers[0].CoreID != "fes.atari-st" {
		t.Fatalf("read during our library change = %+v, want the last snapshot", during)
	}

	releaseChange()
	<-done
	if _, err := svc.Hardware(ctx); err == nil {
		t.Fatal("a failed read with no change in flight hid its error")
	}
}

// Session control and UI events are not library changes.
func TestIsLibraryChange(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/v1/library/core-entries", true},
		{http.MethodDelete, "/api/v1/library/core-entries/fpga-x", true},
		{http.MethodPost, "/api/v1/core-media", true},
		{http.MethodGet, "/api/v1/library/hardware", false},
		{http.MethodPost, "/api/v1/session/stop", false},
		{http.MethodPost, "/api/v1/launcher/ui-events", false},
	} {
		req := httptest.NewRequest(c.method, c.path, http.NoBody)
		if got := isLibraryChange(req); got != c.want {
			t.Errorf("%s %s = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
