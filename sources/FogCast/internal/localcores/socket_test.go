package localcores

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testsock"
)

func shortSock(t *testing.T) string {
	t.Helper()
	return testsock.Path(t, "fc.sock")
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

func TestListenRestoresUmask(t *testing.T) {
	before := syscall.Umask(0)
	syscall.Umask(before)
	t.Cleanup(func() { syscall.Umask(before) })
	path := shortSock(t)
	listener, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	after := syscall.Umask(0)
	syscall.Umask(after)
	if after != before {
		t.Fatalf("umask = %#o, want %#o", after, before)
	}
}
