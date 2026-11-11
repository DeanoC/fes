package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestClientNilAndEmptyAreUnavailable(t *testing.T) {
	var client *Client
	if _, err := client.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil list %v", err)
	}
	empty := NewClient("")
	if _, err := empty.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("empty list %v", err)
	}
}

func TestClientEmptyListIsNotNil(t *testing.T) {
	path := shortSock(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	}))
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	cores, err := NewClient(path).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cores == nil || len(cores) != 0 {
		t.Fatalf("cores %#v", cores)
	}
	raw, err := json.Marshal(cores)
	if err != nil || string(raw) != "[]" {
		t.Fatalf("marshal %s %v", raw, err)
	}
}

func TestClientListWarmingReturnsWithoutTheClientTimeout(t *testing.T) {
	path := shortSock(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "warming"})
	}))
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	started := time.Now()
	cores, err := NewClient(path).List(context.Background())
	if !errors.Is(err, ErrWarming) || cores != nil {
		t.Fatalf("list %+v %v", cores, err)
	}
	if time.Since(started) >= time.Second {
		t.Fatalf("warming list waited %s", time.Since(started))
	}
}

func TestClientMissingAndRefusedSocketsAreUnavailable(t *testing.T) {
	missing := NewClient(shortSock(t))
	if _, err := missing.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing list %v", err)
	}

	refused := NewClient(refuseSock(t))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := refused.List(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("refused list %v", err)
	}
}

func refuseSock(t *testing.T) string {
	t.Helper()
	path := shortSock(t)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		t.Fatal(err)
	}
	return path
}

func serveUnix(t *testing.T, path string, handler http.Handler) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
}
