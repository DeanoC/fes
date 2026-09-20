package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/targetclient"
)

func TestHTTPBridgeStarterUsesAuthenticatedTargetLease(t *testing.T) {
	var gotPath string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		data, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		switch r.URL.Path {
		case "/v1/input/attach":
			var body struct {
				Session uint64 `json:"session"`
				Token   string `json:"token"`
				Core    string `json:"core"`
			}
			if err := json.Unmarshal(data, &body); err != nil || body.Session != 9 || body.Token == "" || body.Core != "SNES" {
				t.Fatalf("lease body = %#v, err=%v", body, err)
			}
		case "/v1/input/detach":
			var body struct {
				Session uint64 `json:"session"`
			}
			if err := json.Unmarshal(data, &body); err != nil || body.Session != 9 {
				t.Fatalf("detach body = %#v, err=%v", body, err)
			}
		default:
			t.Fatalf("unexpected target path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: baseURL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := starter.Start(context.Background(), BridgeSpec{Session: 9, Token: []byte("0123456789abcdef"), Core: "SNES"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/input/attach" || gotAuth != "Bearer private-token" {
		t.Fatalf("target lease request path=%q auth=%q", gotPath, gotAuth)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/input/detach" {
		t.Fatalf("target release path=%q", gotPath)
	}
}

func TestHTTPBridgeStarterSetOriginChangesAttachURL(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("first origin should not receive attach")
	}))
	defer first.Close()
	var gotHost string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer second.Close()
	firstURL, err := url.Parse(first.URL)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: firstURL, Token: "first-token"})
	if err != nil {
		t.Fatal(err)
	}
	secondURL, err := url.Parse(second.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := starter.SetOrigin(secondURL, "second-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := starter.Start(context.Background(), BridgeSpec{Session: 9, Token: []byte("0123456789abcdef"), Core: "SNES"}); err != nil {
		t.Fatal(err)
	}
	if gotHost != secondURL.Host {
		t.Fatalf("attach host = %q, want %q", gotHost, secondURL.Host)
	}
}

func TestHTTPBridgeStarterUsesKitLeaseSourceOnStart(t *testing.T) {
	fallback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("fallback origin should not receive attach")
	}))
	defer fallback.Close()
	var gotHost string
	current := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		if r.URL.Path == "/v1/kit/claim" {
			fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_at":%q,"expires_in_ms":60000},"token":"shared"}`, time.Now().Add(time.Minute).Format(time.RFC3339))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer current.Close()
	fallbackURL, err := url.Parse(fallback.URL)
	if err != nil {
		t.Fatal(err)
	}
	currentURL, err := url.Parse(current.URL)
	if err != nil {
		t.Fatal(err)
	}
	starter, err := NewHTTPBridgeStarter(HTTPBridgeStarterConfig{BaseURL: fallbackURL, Token: "fallback-token"})
	if err != nil {
		t.Fatal(err)
	}
	lease := targetclient.NewKitLease(currentURL, "current-token", current.Client(), "host", "test")
	starter.WithKitLeaseSource(func() *targetclient.KitLease { return lease })
	if _, err := starter.Start(context.Background(), BridgeSpec{Session: 9, Token: []byte("0123456789abcdef"), Core: "SNES"}); err != nil {
		t.Fatal(err)
	}
	if gotHost != currentURL.Host {
		t.Fatalf("attach host = %q, want %q", gotHost, currentURL.Host)
	}
}
