package kitsoftware

import (
	"context"
	"net"

	"golang.org/x/sys/unix"
)

type localPeerCredentials struct {
	uid   uint32
	valid bool
}

type localPeerCredentialsKey struct{}

// UnixPeerContext attaches the kernel-authenticated peer UID to a request.
// It is intended for ConnContext on listeners bound to Unix sockets.
func UnixPeerContext(ctx context.Context, connection net.Conn) context.Context {
	peer := localPeerCredentials{}
	if unixConnection, ok := connection.(*net.UnixConn); ok {
		if raw, err := unixConnection.SyscallConn(); err == nil {
			var credential *unix.Ucred
			var credentialErr error
			if err := raw.Control(func(fd uintptr) {
				credential, credentialErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
			}); err == nil && credentialErr == nil && credential != nil {
				peer = localPeerCredentials{uid: credential.Uid, valid: true}
			}
		}
	}
	return context.WithValue(ctx, localPeerCredentialsKey{}, peer)
}

func unixPeerUID(ctx context.Context) (uint32, bool) {
	peer, ok := ctx.Value(localPeerCredentialsKey{}).(localPeerCredentials)
	return peer.uid, ok && peer.valid
}
