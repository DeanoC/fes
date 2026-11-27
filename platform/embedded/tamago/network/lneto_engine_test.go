// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestSocketDialPreservesLocalTCPPort(t *testing.T) {
	linkA, linkB, _, cleanup := setupLnetoPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := linkB.ListenTCP(ctx, 9093)
	if err != nil {
		t.Fatalf("linkB.ListenTCP: %v", err)
	}
	stopAccept := startAcceptDrain(listener)
	defer stopAccept()

	const localPort = 10443
	raw, err := linkA.engine.Socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM,
		&net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: localPort},
		&net.TCPAddr{IP: net.ParseIP("192.0.2.20"), Port: 9093},
	)
	if err != nil {
		t.Fatalf("Socket dial: %v", err)
	}
	conn, ok := raw.(net.Conn)
	if !ok {
		t.Fatalf("Socket returned %T, want net.Conn", raw)
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("LocalAddr returned %T, want *net.TCPAddr", conn.LocalAddr())
	}
	if localAddr.Port != localPort {
		t.Fatalf("LocalAddr port = %d, want %d", localAddr.Port, localPort)
	}
}
