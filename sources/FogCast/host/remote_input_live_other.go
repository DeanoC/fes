//go:build !unix

package host

import "net"

// peerStreamLive is the non-unix fallback. A present connection counts as live;
// a later write error still becomes ErrRemoteInputNoStream.
func peerStreamLive(conn net.Conn) bool {
	return conn != nil
}
