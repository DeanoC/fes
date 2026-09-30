package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	clientPongID   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	clientColecoID = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	clientInUseID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	clientMissing  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestClientListLaunchStopOnUnixSocket(t *testing.T) {
	path := shortSock(t)
	var mu sync.Mutex
	var launches []string
	var stops int
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/local/cores":
			writeJSON(w, http.StatusOK, []Core{
				{CoreID: "fes.pong", PackageID: clientPongID, Name: "FES Pong", ABI: "fes.computer", Needs: "none", Launchable: true},
				{CoreID: "fes.coleco", PackageID: clientColecoID, Name: "ColecoVision", ABI: "fes.computer", Needs: "media", Launchable: false, Block: "Needs a cartridge"},
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/local/cores/") && strings.HasSuffix(r.URL.Path, "/launch"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/local/cores/"), "/launch")
			mu.Lock()
			launches = append(launches, id)
			mu.Unlock()
			switch id {
			case clientPongID:
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "package_id": id, "core_id": "fes.pong"})
			case clientColecoID:
				writeJSON(w, http.StatusConflict, map[string]string{"error": "blocked"})
			case clientInUseID:
				writeJSON(w, http.StatusConflict, map[string]string{"error": "in_use"})
			default:
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v1/local/stop":
			mu.Lock()
			stops++
			mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		}
	})

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

	client := NewClient(path)
	cores, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cores) != 2 || cores[0].Name != "FES Pong" || cores[0].PackageID != clientPongID || !cores[0].Launchable {
		t.Fatalf("list %+v", cores)
	}
	if cores[1].Name != "ColecoVision" || cores[1].Block != "Needs a cartridge" || cores[1].Needs != "media" || cores[1].Launchable {
		t.Fatalf("blocked row %+v", cores[1])
	}
	if err := client.Launch(context.Background(), clientPongID); err != nil {
		t.Fatal(err)
	}
	if err := client.Launch(context.Background(), clientColecoID); !errors.Is(err, ErrBlocked) {
		t.Fatalf("blocked err %v", err)
	}
	if err := client.Launch(context.Background(), clientInUseID); !errors.Is(err, ErrInUse) {
		t.Fatalf("in_use err %v", err)
	}
	if err := client.Launch(context.Background(), clientMissing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err %v", err)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if stops != 1 || len(launches) != 4 || launches[0] != clientPongID {
		t.Fatalf("stops %d launches %v", stops, launches)
	}
}

func TestClientRejectsUnsafePackageIDWithoutDial(t *testing.T) {
	var called atomic.Bool
	ln, err := net.Listen("unix", shortSock(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called.Store(true)
	}))
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	client := NewClient(ln.Addr().String())
	for _, id := range []string{"", "pong", "../" + clientPongID, strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if err := client.Launch(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("id %q err %v", id, err)
		}
	}
	if called.Load() {
		t.Fatal("unsafe package id was sent")
	}
}

func TestClientNilAndEmptyAreUnavailable(t *testing.T) {
	var client *Client
	if _, err := client.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil list %v", err)
	}
	if err := client.Launch(context.Background(), clientPongID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil launch %v", err)
	}
	if err := client.Stop(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil stop %v", err)
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

func TestClientMissingAndRefusedSocketsAreUnavailable(t *testing.T) {
	missing := NewClient(shortSock(t))
	if _, err := missing.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing list %v", err)
	}
	if err := missing.Launch(context.Background(), clientPongID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing launch %v", err)
	}
	if err := missing.Stop(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing stop %v", err)
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

func TestLaunchTimeoutThenRunningIsNotASecondLaunch(t *testing.T) {
	path := shortSock(t)
	var posts atomic.Int32
	running := atomic.Bool{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/local/status":
			phase := phaseLaunching
			up := running.Load()
			if up {
				phase = phaseRunning
			}
			writeJSON(w, http.StatusOK, RunStatus{Phase: phase, PackageID: clientPongID, Running: up})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/launch"):
			if posts.Add(1) != 1 {
				t.Errorf("launch posted %d times", posts.Load())
			}
			running.Store(true)
			<-r.Context().Done()
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		}
	})
	serveUnix(t, path, handler)
	client := NewClient(path)
	client.launchPost = 150 * time.Millisecond
	client.reconcileFor = time.Second
	client.reconcileEvery = 20 * time.Millisecond
	if err := client.Launch(context.Background(), clientPongID); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts %d", posts.Load())
	}
}

func TestLaunchTimeoutThenIdleStaysFailed(t *testing.T) {
	path := shortSock(t)
	var posts atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/local/status":
			writeJSON(w, http.StatusOK, RunStatus{Phase: phaseIdle, Running: false})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/launch"):
			posts.Add(1)
			<-r.Context().Done()
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		}
	})
	serveUnix(t, path, handler)
	client := NewClient(path)
	client.launchPost = 80 * time.Millisecond
	client.reconcileFor = time.Second
	if err := client.Launch(context.Background(), clientPongID); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("idle after timeout %v", err)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts %d", posts.Load())
	}
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
