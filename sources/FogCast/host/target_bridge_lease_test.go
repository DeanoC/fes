package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type leaseProbeController struct{}

func (leaseProbeController) Health(string) protocol.Health { return protocol.Health{} }
func (leaseProbeController) Status() protocol.Status       { return protocol.Status{} }
func (leaseProbeController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{}, nil
}

type leaseProbeInput struct{}

func (leaseProbeInput) Attach(context.Context, input.Spec) error { return nil }
func (leaseProbeInput) Detach(context.Context, uint64) error     { return nil }
func (leaseProbeInput) OpenStream(context.Context, uint64) (net.Conn, error) {
	client, peer := net.Pipe()
	peer.Close()
	return client, nil
}
func (leaseProbeInput) Close() error { return nil }

func TestRevokedGrantConnectStopsInputReconnect(t *testing.T) {
	var streams atomic.Int32
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(func() { manager.Close() })
	// New reconciles asynchronously; wait before requesting the first grant.
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := manager.Status()
		if status.State == "free" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lease manager did not finish startup reconciliation: %+v", status)
		}
		time.Sleep(time.Millisecond)
	}
	api := httpapi.New(leaseProbeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager), httpapi.WithInput(leaseProbeInput{}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/input/stream" {
			streams.Add(1)
		}
		api.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(base, "bearer", server.Client(), "host", "game")
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	claim, err := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Authorize(claim, true); err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{
		BaseURL: base, Token: "bearer", KitLease: lease, DialTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := starter.Start(context.Background(), BridgeSpec{Session: 1, Token: []byte("0123456789abcdef"), Core: "SNES"})
	if err != nil {
		t.Fatal(err)
	}
	token := handle.(*httpBridgeHandle).kitToken
	if token == "" || token != lease.CurrentToken() {
		t.Fatalf("attach token %q current %q", token, lease.CurrentToken())
	}
	if _, err := manager.Release(token); err != nil {
		t.Fatal(err)
	}
	if lease.CurrentToken() != token {
		t.Fatal("host dropped its grant before the kit refusal")
	}
	input := &RemoteInput{bridge: handle, state: RemoteInputReconnecting, now: time.Now, grace: time.Hour, dial: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = input.reconnectLocked(ctx)
	elapsed := time.Since(started)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeKitLeaseDenied || apiErr.Phase != "recovery" || apiErr.Message != "session does not own the kit lease" || strings.Contains(err.Error(), "KIT_LEASE_REQUIRED") || errors.Is(err, ErrRemoteInputNoStream) {
		t.Fatalf("error=%v", err)
	}
	if streams.Load() != 1 {
		t.Fatalf("CONNECT attempts=%d, want 1", streams.Load())
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("reconnect held the input lock for %s", elapsed)
	}
}

func TestHTTPBridgeDialDecodesBoundedConnectOwnership(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   protocol.ErrorCode
	}{
		{name: "required", status: http.StatusForbidden, body: `{"error":{"code":"KIT_LEASE_REQUIRED","message":"private grant path"}}`, want: protocol.CodeKitLeaseDenied},
		{name: "denied", status: http.StatusForbidden, body: `{"error":{"code":"KIT_LEASE_DENIED","message":"private grant path"}}`, want: protocol.CodeKitLeaseDenied},
		{name: "busy", status: http.StatusConflict, body: `{"error":{"code":"KIT_LEASE_BUSY","message":"private grant path"}}`, want: protocol.CodeKitLeaseBusy},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			address := serveConnect(t, test.status, test.body, false)
			err := dialConnect(t, address)
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.want || apiErr.Message != "session does not own the kit lease" || strings.Contains(err.Error(), "private") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestHTTPBridgeDialBoundsNonOwnershipConnectBody(t *testing.T) {
	other := serveConnect(t, http.StatusForbidden, `{"error":{"code":"OTHER","message":"no"}}`, false)
	if err := dialConnect(t, other); !errors.Is(err, ErrRemoteInputNoStream) {
		t.Fatalf("other refusal: %v", err)
	}
	oversized := `{"error":{"code":"KIT_LEASE_REQUIRED","message":"` + strings.Repeat("x", maxBridgeRequestBytes) + `"}}`
	address := serveConnect(t, http.StatusForbidden, oversized, true)
	started := time.Now()
	err := dialConnect(t, address)
	if !errors.Is(err, ErrRemoteInputNoStream) || errors.As(err, new(*protocol.APIError)) {
		t.Fatalf("oversized body: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("oversized CONNECT blocked for %s", elapsed)
	}
}

func dialConnect(t *testing.T, address string) error {
	t.Helper()
	base, err := url.Parse("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: base, Token: "bearer", DialTimeout: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	handle := &httpBridgeHandle{starter: starter, session: 1, ready: make(chan struct{})}
	close(handle.ready)
	_, err = handle.Dial(context.Background())
	return err
}

func serveConnect(t *testing.T, status int, body string, stall bool) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		for {
			line, err := reader.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}
		if stall {
			_ = conn.SetDeadline(time.Time{})
			fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\n\r\n", status, http.StatusText(status))
			_, _ = io.WriteString(conn, body)
			time.Sleep(3 * time.Second)
			return
		}
		fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, http.StatusText(status), len(body), body)
	}()
	return listener.Addr().String()
}
