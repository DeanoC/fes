package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
)

const testLauncherJSON = `{"listen":"0.0.0.0:8789","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`

func TestLauncherConfigPrivateAndStrict(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"valid", testLauncherJSON, 0600, true},
		{"read only", testLauncherJSON, 0400, true},
		{"public", testLauncherJSON, 0644, false},
		{"unknown", strings.Replace(testLauncherJSON, `"listen":`, `"extra":1,"listen":`, 1), 0600, false},
		{"short token", strings.Replace(testLauncherJSON, "12345678901234567890123456789012", "short", 1), 0600, false},
		{"invalid id", strings.Replace(testLauncherJSON, "73dc9f5f-1a12-4a95-a820-a9b4e600769a", "invalid", 1), 0600, false},
		{"invalid address", strings.Replace(testLauncherJSON, "0.0.0.0:8789", "example.org:8789", 1), 0600, false},
		{"extra document", testLauncherJSON + "{}", 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launcher.json")
			if err := os.WriteFile(path, []byte(tc.body), tc.mode); err != nil {
				t.Fatal(err)
			}
			_, err := loadLauncherConfig(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
func TestLauncherFlagEnablesRemoteInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "launcher.json")
	if err := os.WriteFile(path, []byte(testLauncherJSON), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`base_url = "http://127.0.0.1:8182"
token = "token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
[[libraries]]
id = "test"
system = "snes"
root = "`+dir+`"
`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	composed := false
	code := runWithComposer(context.Background(), []string{"--config", configPath, "--launcher-config", path}, &output, &output,
		func(context.Context, fogcast.Paths) (service, error) { return &hostOnlyCompositionService{}, nil },
		func(_ service, config fogcast.Config, _ bridgeStarterFactory) (http.Handler, func() error, error) {
			composed = true
			if !config.RemoteInput.Enabled {
				t.Error("launcher input not enabled")
			}
			return nil, nil, errors.New("stop before listener")
		})
	if !composed || code != 1 {
		t.Fatalf("composed=%v code=%d output=%s", composed, code, output.String())
	}
}

func TestRunHeadlessDisablesMediaCapture(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`base_url = "http://127.0.0.1:8182"
token = "token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
[[libraries]]
id = "test"
system = "snes"
root = "`+dir+`"
[media]
enabled = true
decoder = "mjpeg"
capture_device = "/dev/video0"
`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var enabled *bool
	code := runWithComposer(context.Background(), []string{"--config", configPath, "--headless"}, &output, &output,
		func(context.Context, fogcast.Paths) (service, error) { return &hostOnlyCompositionService{}, nil },
		func(_ service, config fogcast.Config, _ bridgeStarterFactory) (http.Handler, func() error, error) {
			value := config.Media.Enabled
			enabled = &value
			return nil, nil, errors.New("stop before listener")
		})
	if enabled == nil || *enabled || code != 1 {
		t.Fatalf("media enabled=%v code=%d output=%s", enabled, code, output.String())
	}
}

func TestLauncherPairingsFile(t *testing.T) {
	var entries []string
	for i := 0; i < 20; i++ {
		token := strings.Repeat("b", 180) + fmt.Sprintf("%020d", i)
		id := fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1)
		entries = append(entries, fmt.Sprintf(`{"token":"%s","target_id":"%s"}`, token, id))
	}
	body := `{"listen":"0.0.0.0:8789","pairings":[` + strings.Join(entries, ",") + `]}`
	if len(body) <= 4096 || len(body) > 16384 {
		t.Fatalf("fixture size %d", len(body))
	}
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadLauncherConfig(path)
	if err != nil || len(loaded.Pairings) != 20 || loaded.Pairings[1].TargetID != "00000000-0000-4000-8000-000000000002" {
		t.Fatalf("loaded=%d err=%v", len(loaded.Pairings), err)
	}
	unknown := strings.TrimSuffix(body, "}") + `,"extra":1}`
	unknownPath := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(unknownPath, []byte(unknown), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLauncherConfig(unknownPath); err == nil {
		t.Fatal("unknown field accepted")
	}
	nested := `{"listen":"0.0.0.0:8789","pairings":[{"token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","extra":1}]}`
	nestedPath := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(nestedPath, []byte(nested), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLauncherConfig(nestedPath); err == nil {
		t.Fatal("nested unknown field accepted")
	}
}

func TestLauncherRejectsHostTokenInAnyPairing(t *testing.T) {
	const hostToken = "12345678901234567890123456789012"
	dir := t.TempDir()
	path := filepath.Join(dir, "launcher.json")
	body := `{"listen":"0.0.0.0:8789","token":"abcdefghijklmnopqrstuvwxyz012345","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","pairings":[{"token":"` + hostToken + `","target_id":"67c5f4e2-d288-49bb-9049-39ecf39cf6f6"}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`base_url = "http://127.0.0.1:8182"
token = "`+hostToken+`"
request_timeout_seconds = 12
upload_timeout_seconds = 60
[[libraries]]
id = "test"
system = "snes"
root = "`+dir+`"
`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	composed := false
	code := runWithComposer(context.Background(), []string{"--config", configPath, "--launcher-config", path}, &output, &output,
		func(context.Context, fogcast.Paths) (service, error) { return &hostOnlyCompositionService{}, nil },
		func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error) {
			composed = true
			return nil, nil, errors.New("stop before listener")
		})
	if composed || code != 2 || !strings.Contains(output.String(), "launcher requires a separate credential") {
		t.Fatalf("composed=%v code=%d output=%s", composed, code, output.String())
	}
}
