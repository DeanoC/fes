//go:build tamago

package localcores

import (
	"errors"
	"net"
	"os"
)

// The embedded base owns package storage and hardware services. It has no
// native Unix control socket or equivalent no-follow file admission yet.
// Do not downgrade either boundary to make the UI package compile.
func openInventoryReadOnly(string) (*os.File, error) {
	return nil, errors.New("native package inventory is unavailable on the embedded platform")
}

func listen(string) (net.Listener, error) {
	return nil, errors.New("native Unix control socket is unavailable on the embedded platform")
}
