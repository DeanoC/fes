package localcores

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DefaultSocket is the kit-local control feed. It is a root-only unix
// socket, mode 0600, owned by the agent process (root on the kit), and it
// is not reachable from the network. There is no bearer token. Any root
// process on the kit can call it; that is the same authority that owns
// the hardware.
const DefaultSocket = "/run/fogcast/local-control.sock"

func listen(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("local control socket path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Umask is process-wide. Restore it as soon as Listen returns so the
	// socket is created 0600 and later files keep the caller's mask.
	oldMask := syscall.Umask(0o077)
	listener, err := net.Listen("unix", path)
	syscall.Umask(oldMask)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return listener, nil
}

// Serve accepts HTTP on a root-only unix socket until ctx is cancelled.
// An empty path does not listen. The socket is removed when Serve returns.
func Serve(ctx context.Context, path string, handler http.Handler) error {
	if ctx == nil {
		return errors.New("local control context is required")
	}
	listener, err := listen(path)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	_ = os.Remove(path)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
