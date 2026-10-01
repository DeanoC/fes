package kitlauncher

import (
	"context"
	"errors"
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

func TestPairedKitLeaseUsesAuthenticatedTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/launcher/kit-lease" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-FogCast-Target-ID") != "73dc9f5f-1a12-4a95-a820-a9b4e600769a" {
			t.Fatalf("lease request %s headers=%v", r.URL.Path, r.Header)
		}
		_, _ = w.Write([]byte(`{"state":"held","owner":"kit-hostless","purpose":"kit-local-core","generation":"gen-9","expires_in_ms":9000}`))
	}))
	defer server.Close()
	c := NewClient(Config{API: server.URL, Token: "secret", TargetID: "73dc9f5f-1a12-4a95-a820-a9b4e600769a"})
	got, err := c.PairedKitLease(context.Background())
	if err != nil || got.State != "held" || got.Owner != "kit-hostless" || got.Purpose != "kit-local-core" || got.Generation != "gen-9" || got.ExpiresInMS != 9000 {
		t.Fatalf("lease=%+v err=%v", got, err)
	}
}

func TestPairedKitLease404IsUnknown(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	c := NewClient(Config{API: server.URL, Token: "secret", TargetID: "paired-a"})
	status, err := c.PairedKitLease(context.Background())
	if !errors.Is(err, errPairedKitLeaseUnsupported) || status.State != "" || status.Owner != "" {
		t.Fatalf("404 lease=%+v err=%v", status, err)
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

func TestLoadConfigAcceptsHPSFramebuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","hps_framebuffer":true}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HPSFramebuffer {
		t.Fatal("hps_framebuffer was not loaded")
	}
	if err := SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HPSFramebuffer {
		t.Fatal("hps_framebuffer did not round-trip")
	}
}

func TestMenuDisplayConfigSurvivesThemeSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","menu_display":true}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil || !c.MenuDisplay {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	c.Theme = "neon"
	if err := SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil || !got.MenuDisplay {
		t.Fatalf("saved config=%+v err=%v", got, err)
	}
}

func TestKitUISurvivesThemeSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","menu_display":true,"kit_ui":"tenfoot"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil || c.KitUI != "tenfoot" || !c.MenuDisplay {
		t.Fatalf("config=%+v err=%v", c, err)
	}
	c.Theme = "neon"
	if err := SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil || got.KitUI != "tenfoot" || !got.MenuDisplay || got.Theme != "neon" {
		t.Fatalf("saved config=%+v err=%v", got, err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"kit_ui": "tenfoot"`) {
		t.Fatalf("launcher.json dropped kit_ui: %s", raw)
	}
}

func TestTemporaryMenuDisplaySelectionDoesNotChangeSavedConfig(t *testing.T) {
	c := NewClient(Config{MenuDisplay: false})
	c.SetMenuDisplay(true)
	if !c.MenuDisplayEnabled() || c.config.MenuDisplay {
		t.Fatal("temporary selection changed saved config")
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

// On the kit's /media/fat exFAT, launcher.json.tmp / launcher.json.bak open
// launcher.json itself (#317). SaveConfig must not write any name that has
// launcher.json as a prefix; a directory squatting on the old path+".tmp"
// name proves it, and the directory must end up exactly as before plus the
// updated file.
func TestSaveConfigNeverWritesTargetPrefixedTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "launcher.json")
	body := `{"api":"http://127.0.0.1:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".bak", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	c.Theme = "neon"
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig used a launcher.json-prefixed temp: %v", err)
	}
	got, err := LoadConfig(p)
	if err != nil || got.Theme != "neon" {
		t.Fatalf("saved config=%+v err=%v", got, err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("launcher.json mode: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(p + ".bak"); string(b) != "keep" {
		t.Fatalf("launcher.json.bak touched: %q", b)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "launcher.json,launcher.json.bak,launcher.json.tmp" {
		t.Fatalf("unexpected directory contents %v", names)
	}
}
