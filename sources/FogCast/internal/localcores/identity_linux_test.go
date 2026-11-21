//go:build linux

package localcores

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestIdentityUsesTokenFreeAuthenticatedUnixPeer(t *testing.T) {
	path := shortSock(t)
	const id = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	authorization := make(chan string, 1)
	serverCtx, stopServer := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(serverCtx, path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization <- r.Header.Get("Authorization")
			HandlerWithIdentity(New(Roots{}), id).ServeHTTP(w, r)
		}))
	}()
	defer func() { stopServer(); <-done }()
	for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := NewClient(path).Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetID != id || got.PeerPID != os.Getpid() || got.PeerStart == "" {
		t.Fatalf("unexpected identity %+v", got)
	}
	if got := <-authorization; got != "" {
		t.Fatal("identity request exposed credentials")
	}
}

func TestIdentityRejectsMalformedLocalReply(t *testing.T) {
	path := shortSock(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"target_id":"not-a-uuid"}`)) })}
	go server.Serve(listener)
	defer server.Close()
	if _, err := NewClient(path).Identity(context.Background()); err == nil {
		t.Fatal("malformed identity accepted")
	}
}

func TestIdentityEndpointRejectsUnverifiedPeerAndUnexpectedPaths(t *testing.T) {
	handler := HandlerWithIdentity(New(Roots{}), "73dc9f5f-1a12-4a95-a820-a9b4e600769a")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/identity", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("unverified peer status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/identity/extra", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unexpected path status = %d", response.Code)
	}
}
