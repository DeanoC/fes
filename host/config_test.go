package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clawzai2-tech/mister-remote/host"
)

const validConnectionConfig = `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
manifest_path = "games.toml"
`

func TestLoadConnectionResolvesRelativeManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(validConnectionConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := host.LoadConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ManifestPath != filepath.Join(dir, "games.toml") || cfg.RequestTimeout != 12*time.Second {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.BaseURL.String() != "http://192.0.2.10:8182" || cfg.Token != "test-token" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestLoadConnectionRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing scheme":   strings.Replace(validConnectionConfig, "http://192.0.2.10:8182", "192.0.2.10:8182", 1),
		"userinfo":         strings.Replace(validConnectionConfig, "http://192.0.2.10:8182", "http://user@192.0.2.10:8182", 1),
		"https":            strings.Replace(validConnectionConfig, "http://", "https://", 1),
		"path":             strings.Replace(validConnectionConfig, ":8182\"", ":8182/base\"", 1),
		"query":            strings.Replace(validConnectionConfig, ":8182\"", ":8182?x=1\"", 1),
		"fragment":         strings.Replace(validConnectionConfig, ":8182\"", ":8182#fragment\"", 1),
		"empty token":      strings.Replace(validConnectionConfig, `token = "test-token"`, `token = ""`, 1),
		"zero timeout":     strings.Replace(validConnectionConfig, "request_timeout_seconds = 12", "request_timeout_seconds = 0", 1),
		"timeout overflow": strings.Replace(validConnectionConfig, "request_timeout_seconds = 12", "request_timeout_seconds = 9223372036854775807", 1),
		"empty manifest":   strings.Replace(validConnectionConfig, `manifest_path = "games.toml"`, `manifest_path = ""`, 1),
		"unknown":          validConnectionConfig + "discovery = true\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := host.LoadConnection(path); err == nil {
				t.Fatal("invalid connection loaded")
			}
		})
	}
}

func TestLoadConnectionAllowsExplicitDevelopmentPort(t *testing.T) {
	t.Parallel()
	content := strings.Replace(validConnectionConfig, "http://192.0.2.10:8182", "http://127.0.0.1:49152", 1)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	connection, err := host.LoadConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	if connection.BaseURL.String() != "http://127.0.0.1:49152" {
		t.Fatalf("base URL = %q", connection.BaseURL)
	}
}
