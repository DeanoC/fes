// Package localsocket creates kit-local Unix sockets without overlapping
// process-wide umask changes from the agent's concurrent startup listeners.
package localsocket

import (
	"net"
	"sync"
	"syscall"
)

var umaskMu sync.Mutex

func withUmask(mask int, operation func() error) error {
	umaskMu.Lock()
	defer umaskMu.Unlock()
	previous := syscall.Umask(mask)
	defer syscall.Umask(previous)
	return operation()
}

// Listen applies mask while binding the Unix socket, then restores the prior
// process umask. All socket listeners that change the umask must use this path.
func Listen(path string, mask int) (net.Listener, error) {
	var listener net.Listener
	err := withUmask(mask, func() error {
		var err error
		listener, err = net.Listen("unix", path)
		return err
	})
	return listener, err
}
