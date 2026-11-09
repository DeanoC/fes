// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	gnet "github.com/usbarmory/go-net"
)

type peer struct {
	in  <-chan []byte
	out chan<- []byte
}

func (p peer) Receive(buf []byte) (int, error) {
	select {
	case frame := <-p.in:
		return copy(buf, frame), nil
	default:
		runtime.Gosched()
		return 0, nil
	}
}
func (p peer) Transmit(frame []byte) error {
	select {
	case p.out <- bytes.Clone(frame):
		return nil
	default:
		return fmt.Errorf("model peer queue full")
	}
}

// Actual Ethernet/ARP/IPv4/TCP/HTTP traffic crosses the driver's descriptors.
// Neither endpoint uses a host TCP socket or a fake HTTP RoundTripper.
func TestHTTPThroughDMAAndIPStack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outbound, inbound := make(chan []byte, 256), make(chan []byte, 256)
	h := newHardware()
	h.wireOut = outbound
	h.wireIn = inbound
	device := open(t, h)
	clientStack := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())
	serverStack := gnet.NewGVisorStack(1)
	clientIface, err := network.NewLink(clientStack, device, "192.0.2.2/24", device.MAC(), "")
	if err != nil {
		t.Fatal(err)
	}
	serverIface := &gnet.Interface{Stack: serverStack, NetworkDevice: peer{in: outbound, out: inbound}}
	if err := serverIface.Init("192.0.2.1/24", "02:aa:bb:cc:dd:01", ""); err != nil {
		t.Fatal(err)
	}
	loops := make(chan error, 2)
	go func() { loops <- clientIface.Start(ctx) }()
	go func() { loops <- serverIface.Start(ctx) }()
	defer func() {
		cancel()
		for i := 0; i < 2; i++ {
			select {
			case <-loops:
			case <-time.After(time.Second):
				t.Error("packet loop did not stop")
			}
		}
	}()
	socket, err := serverStack.Socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8081}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, ok := socket.(net.Listener)
	if !ok {
		t.Fatalf("listener: %T", socket)
	}
	payload := bytes.Repeat([]byte("native-xgmac-http\n"), 1024)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog" {
			http.NotFound(w, r)
			return
		}
		w.Write(payload)
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Close()
		select {
		case <-served:
		case <-time.After(time.Second):
			t.Error("HTTP server did not stop")
		}
	}()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		raw, err := clientStack.Socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, nil, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 8081})
		if err != nil {
			return nil, err
		}
		conn, ok := raw.(net.Conn)
		if !ok {
			return nil, fmt.Errorf("connection: %T", raw)
		}
		return conn, nil
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://192.0.2.1:8081/catalog", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || !bytes.Equal(body, payload) {
		t.Fatalf("HTTP payload: bytes=%d status=%d err=%v", len(body), response.StatusCode, err)
	}
	stats := device.Statistics()
	if stats.Received <= ringSize || stats.Transmitted < 2 || device.Err() != nil {
		t.Fatalf("packet path: %+v err=%v", stats, device.Err())
	}
	// Model-only platform stop/restart. The software gate must finish every
	// driver call before these hooks touch the synchronous register model.
	// This supplier cannot qualify physical DMA or establish firmware authority.
	stops, starts := 0, 0
	control, err := network.NewController(clientIface, network.DMAControl{
		Stop: func(context.Context) error {
			stops++
			for _, reg := range []uint32{txConfig, rxConfig, txControl, rxControl} {
				h.Write32(reg, h.Read32(reg)&^1)
			}
			return nil
		},
		Start: func(context.Context) error {
			starts++
			for _, reg := range []uint32{txControl, rxControl, txConfig, rxConfig} {
				h.Write32(reg, h.Read32(reg)|1)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if err = control.PauseNetwork(ctx); err != nil {
			t.Fatal(err)
		}
		accesses, writes := h.accesses, h.mem.writes
		if err = control.PauseNetwork(ctx); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
		if h.accesses != accesses || h.mem.writes != writes || !errors.Is(clientIface.Available(), network.ErrPaused) {
			t.Fatal("traffic touched stopped DMA")
		}
		if err = control.ResumeNetwork(ctx); err != nil {
			t.Fatal(err)
		}
		request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "http://192.0.2.1:8081/catalog", nil)
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err = io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil || response.StatusCode != 200 || !bytes.Equal(body, payload) {
			t.Fatal("HTTP did not survive pause/resume", err, closeErr)
		}
	}
	if stops != 3 || starts != 3 {
		t.Fatal(stops, starts)
	}
}
