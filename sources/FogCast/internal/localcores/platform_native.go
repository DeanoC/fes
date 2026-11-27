//go:build !tamago

package localcores

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"github.com/DeanoC/FogCast/internal/localsocket"
)

func openInventoryReadOnly(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

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
	listener, err := localsocket.Listen(path, 0o077)
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
