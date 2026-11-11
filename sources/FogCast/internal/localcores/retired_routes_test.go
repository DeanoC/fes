package localcores

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestControlSocketOnlyListsCores(t *testing.T) {
	service := New(Roots{})
	defer service.Close()
	handler := Handler(service)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/v1/local/status"},
		{http.MethodPost, "/v1/local/stop"},
		{http.MethodPost, "/v1/local/cores/" + strings.Repeat("a", 64) + "/launch"},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d", route.method, route.path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("inventory = %d", w.Code)
	}
}

func TestCloseCancelsInventoryWarm(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	useFactoryListGate(t, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	service := New(Roots{})
	service.Warm()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inventory warm did not start")
	}
	service.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("inventory warm did not stop after close")
	}
}
