// SPDX-License-Identifier: GPL-3.0-or-later
package network

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/soypat/lneto"
	"github.com/soypat/lneto/ethernet"
	"github.com/soypat/lneto/ipv4"
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

func TestListenerRepliesToOnLinkPeer(t *testing.T) {
	for _, tc := range []struct {
		name    string
		gateway [6]byte
	}{
		{name: "without gateway"},
		{name: "with distinct router", gateway: [6]byte{2, 0x11, 0x22, 0x33, 0x44, 0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server, _, cleanup := setupLnetoPair(t)
			defer cleanup()
			if tc.gateway != ([6]byte{}) {
				server.engine.stack.SetGatewayHardwareAddr(tc.gateway)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			listener, err := server.ListenTCP(ctx, 9093)
			if err != nil {
				t.Fatal(err)
			}
			defer startAcceptDrain(listener)()
			dest := make(chan [6]byte, 1)
			remove := client.AddTap(func(frame []byte) {
				if eth, _, ok := tcpIPv4Frame(frame); ok {
					select {
					case dest <- *eth.DestinationHardwareAddr():
					default:
					}
				}
			})
			defer remove()
			con, err := client.DialTCP(ctx, netip.AddrPortFrom(server.Prefix().Addr(), 9093))
			if err != nil {
				t.Fatalf("on-link client could not connect: %v", err)
			}
			defer con.Close()
			want := [6]byte{2, 0x11, 0x22, 0x33, 0x44, 1}
			select {
			case mac := <-dest:
				if mac != want {
					t.Fatalf("SYN-ACK MAC %x, want on-link peer %x", mac, want)
				}
			case <-ctx.Done():
				t.Fatal("client never received the SYN-ACK")
			}
		})
	}
}

func TestTCPPeerTurnover(t *testing.T) {
	e := lnetoEngine{subnet: netip.MustParsePrefix("192.0.2.1/24"), tcpPeers: make([]tcpPeer, 2)}
	one := tcpPeer{ip: [4]byte{192, 0, 2, 2}, mac: [6]byte{2, 0, 0, 0, 0, 2}}
	two := tcpPeer{ip: [4]byte{192, 0, 2, 3}, mac: [6]byte{2, 0, 0, 0, 0, 3}}
	three := tcpPeer{ip: [4]byte{192, 0, 2, 4}, mac: [6]byte{2, 0, 0, 0, 0, 4}}
	replaced := tcpPeer{ip: one.ip, mac: [6]byte{2, 0, 0, 0, 0, 10}}
	remote := tcpPeer{ip: [4]byte{198, 51, 100, 1}, mac: three.mac}
	frame := make([]byte, 54)
	eth, _ := ethernet.NewFrame(frame)
	eth.SetEtherType(ethernet.TypeIPv4)
	ip, _ := ipv4.NewFrame(eth.Payload())
	ip.SetProtocol(lneto.IPProtoTCP)
	*ip.DestinationAddr() = e.subnet.Addr().As4()
	for _, peer := range []tcpPeer{one, two, replaced, three, remote} {
		*eth.SourceHardwareAddr() = peer.mac
		*ip.SourceAddr() = peer.ip
		e.learnTCPPeer(frame)
	}
	if !slices.Equal(e.tcpPeers, []tcpPeer{three, replaced}) {
		t.Fatalf("recent on-link clients = %+v", e.tcpPeers)
	}
}
