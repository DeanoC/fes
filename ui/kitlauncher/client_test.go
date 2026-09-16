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

func TestClientSessionRejectsOversizeTrailingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"idle"}` + strings.Repeat("x", 1<<20)))
	}))
	defer server.Close()

	if _, err := NewClient(Config{API: server.URL, Token: strings.Repeat("x", 32), TargetID: "id"}).Session(context.Background()); err == nil {
		t.Fatal("accepted oversized session response")
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

func TestLoadConfigAcceptsOptionalInputProfile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","input_profile":"swap-ab"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.InputProfile != "swap-ab" {
		t.Fatalf("profile %q", c.InputProfile)
	}
}

func TestLoadConfigAcceptsOptionalShelf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","shelf":"megadrive"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Shelf != "megadrive" {
		t.Fatalf("shelf %q", c.Shelf)
	}
}

func TestSaveConfigRoundTripShelf(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	c.Shelf = "snes"
	if err := SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shelf != "snes" || got.API != c.API || got.Token != c.Token || got.TargetID != c.TargetID {
		t.Fatalf("round trip %+v", got)
	}
}

func TestLoadConfigAcceptsOptionalTheme(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","theme":"arcade"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme != "arcade" {
		t.Fatalf("theme %q", c.Theme)
	}
}

func TestLoadConfigAcceptsOptionalAudioChrome(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","audio_chrome":true}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.AudioChrome {
		t.Fatal("audio_chrome")
	}
	c.Theme = "neon"
	if err := SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AudioChrome || got.Theme != "neon" {
		t.Fatalf("round trip %+v", got)
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
