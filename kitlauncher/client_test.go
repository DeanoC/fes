package kitlauncher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientAuthenticatesAndRefusesRedirect(t *testing.T) {
	leaked := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
	defer sink.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-FogCast-Target-ID") != "id" {
			t.Error("missing identity")
		}
		http.Redirect(w, r, sink.URL, 302)
	}))
	defer origin.Close()
	c := NewClient(Config{API: origin.URL, Token: "secret", TargetID: "id"})
	if _, err := c.Session(context.Background()); err == nil {
		t.Fatal("redirect accepted")
	}
	if leaked {
		t.Fatal("credentials redirected")
	}
}
func TestLoadConfigRejectsInvalidEndpoint(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	for _, api := range []string{"file:///etc/passwd", "http://user:pass@localhost", "http://localhost/path"} {
		_ = os.WriteFile(p, []byte(`{"api":"`+api+`","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`), 0600)
		if _, err := LoadConfig(p); err == nil {
			t.Fatalf("accepted %s", api)
		}
	}
}

func TestLoadConfigRejectsInvalidLauncherToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	base := `{"api":"http://127.0.0.1:8789","token":"%s","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`
	for _, token := range []string{strings.Repeat("x", 31), strings.Repeat("x", 257), strings.Repeat("x", 31) + "\n"} {
		if err := os.WriteFile(p, []byte(fmt.Sprintf(base, token)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(p); err == nil {
			t.Fatalf("accepted token length %d", len(token))
		}
	}
}

func TestLaunchAllowsHardwareOperationLongerThanPollTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5200 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"active","game_id":"pong","execution":"fpga_native"}`))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	c := NewClient(Config{API: server.URL, Token: "secret", TargetID: "id"})
	result, err := c.Library.Launch(context.Background(), "pong")
	if err != nil || result.State != "active" {
		t.Fatalf("legitimate hardware launch interrupted: %+v %v", result, err)
	}
}
