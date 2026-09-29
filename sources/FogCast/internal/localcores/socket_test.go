package localcores

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

var sockSeq atomic.Uint32

func shortSock(t *testing.T) string {
	t.Helper()
	n := sockSeq.Add(1)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("fc%d.sock", n))
	if len(path) >= 104 {
		path = fmt.Sprintf("/tmp/fc%d.sock", n)
	}
	if len(path) >= 104 {
		if runtime.GOOS == "darwin" {
			t.Skipf("unix socket path is %d bytes", len(path))
		}
		t.Fatalf("unix socket path is %d bytes", len(path))
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func TestLocalControlSocketMode(t *testing.T) {
	path := shortSock(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, path, Handler(New(nil, nil, Roots{})))
	}()
	deadline := time.Now().Add(2 * time.Second)
	var info os.FileInfo
	var err error
	for {
		info, err = os.Stat(path)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		},
	}}
	response, err := client.Get("http://local-control/v1/local/cores")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	var cores []Core
	if err := json.NewDecoder(response.Body).Decode(&cores); err != nil {
		t.Fatal(err)
	}
	if len(cores) != 0 {
		t.Fatalf("cores %+v", cores)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("socket serve did not stop")
	}
}
