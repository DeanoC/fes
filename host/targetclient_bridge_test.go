package host

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/targetclient"
)

func TestKitLeaseSharedWithInputConnectAndDetach(t *testing.T) {
	paths := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		switch r.URL.Path {
		case "/v1/kit/claim":
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":%q,"expires_in_ms":60000},"token":"shared"}`, time.Now().Add(time.Minute).Format(time.RFC3339))
		case "/v1/kit/release":
			fmt.Fprint(w, `{"state":"free"}`)
		default:
			if r.Header.Get(targetclient.KitLeaseHeader) != "shared" {
				t.Error("input mutation omitted shared lease")
			}
			if r.Method == http.MethodConnect {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				fmt.Fprint(conn, "HTTP/1.1 200 OK\r\n\r\n")
				conn.Close()
				return
			}
			fmt.Fprint(w, `{"ready":true}`)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	l := targetclient.NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer l.Close(context.Background())
	r, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
	if err := l.Authorize(r, true); err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: u, Token: "bearer", KitLease: l})
	if err != nil {
		t.Fatal(err)
	}
	h, err := starter.Start(context.Background(), BridgeSpec{Session: 1, Token: []byte("0123456789abcdef"), Core: "SNES"})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := h.(BridgeDialer).Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := h.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/v1/kit/claim", "/v1/input/attach", "/v1/input/stream", "/v1/input/detach"} {
		if got := <-paths; got != want {
			t.Fatalf("path %q want %q", got, want)
		}
	}
}

func TestDelayedInputAttachRetainsDispatchedKitToken(t *testing.T) {
	attached, respond := make(chan struct{}), make(chan struct{})
	claims := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/kit/claim":
			claims++
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"g-%d","expires_in_ms":90000},"token":"token-%d"}`, claims, claims)
		case "/v1/kit/release":
			fmt.Fprint(w, `{"state":"free"}`)
		case "/v1/input/attach":
			close(attached)
			<-respond
			fmt.Fprint(w, `{"ready":true}`)
		default:
			t.Error("stale input handle dispatched a mutation")
			fmt.Fprint(w, `{"ready":true}`)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	l := targetclient.NewKitLease(u, "bearer", server.Client(), "test", "game")
	defer l.Close(context.Background())
	starter, _ := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: u, Token: "bearer", KitLease: l})
	type outcome struct {
		handle BridgeHandle
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		h, err := starter.Start(context.Background(), BridgeSpec{Session: 1, Token: []byte("0123456789abcdef"), Core: "SNES"})
		done <- outcome{h, err}
	}()
	<-attached
	if err := l.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/launch", nil)
	if err := l.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	close(respond)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got := result.handle.(*httpBridgeHandle).kitToken; got != "token-1" {
		t.Fatalf("old attach captured replacement token %q", got)
	}
	if err := result.handle.Stop(context.Background()); err == nil {
		t.Fatal("stale handle stopped replacement input")
	}
}

func TestRemoteInputInvalidationDoesNotStopPreviousBridge(t *testing.T) {
	stopped := false
	bridge := &invalidationBridge{stopped: &stopped}
	conn, peer := net.Pipe()
	defer peer.Close()
	remote := &RemoteInput{bridge: bridge, conn: conn, state: RemoteInputAttached, ready: true, session: 7}
	remote.Invalidate()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("old input stream not closed: %v", err)
	}
	if stopped || remote.bridge != nil || remote.Status().State != RemoteInputDetached {
		t.Fatal("invalidation sent cleanup or retained stale input")
	}
}

type invalidationBridge struct{ stopped *bool }

func (b *invalidationBridge) Ready() <-chan struct{}     { ch := make(chan struct{}); close(ch); return ch }
func (b *invalidationBridge) Endpoint() string           { return "127.0.0.1:1" }
func (b *invalidationBridge) Stop(context.Context) error { *b.stopped = true; return nil }
