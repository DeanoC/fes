package host

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
