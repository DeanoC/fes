package agentconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/agentconfig"
)

const validAgentConfig = `listen_address = "0.0.0.0:8182"
token = "test-token"
`

func TestLoadAgentConfigDefaultsCacheMaximum(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(validAgentConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agentconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenAddress != "0.0.0.0:8182" || got.Token != "test-token" {
		t.Fatalf("config = %#v", got)
	}
	if got.CacheMaxBytes != 2<<30 {
		t.Fatalf("default cache maximum = %d, want %d", got.CacheMaxBytes, int64(2<<30))
	}
}

func TestLoadAgentConfigAcceptsExplicitPositiveCacheMaximum(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := validAgentConfig + "cache_max_bytes = 33554432\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agentconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CacheMaxBytes != 33554432 {
		t.Fatalf("cache maximum = %d, want 33554432", got.CacheMaxBytes)
	}
}

func TestLoadAgentConfigAcceptsCanonicalTargetID(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := validAgentConfig + "target_id = \"01234567-89ab-cdef-0123-456789abcdef\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agentconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetID != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("target ID = %q", got.TargetID)
	}
}

func TestLoadAgentConfigRejectsUnknownUnsafeAndInvalidCacheValues(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unknown":          validAgentConfig + "extra = true\n",
		"empty token":      strings.Replace(validAgentConfig, `token = "test-token"`, `token = ""`, 1),
		"malformed listen": strings.Replace(validAgentConfig, `0.0.0.0:8182`, `0.0.0.0`, 1),
		"zero cache":       validAgentConfig + "cache_max_bytes = 0\n",
		"negative cache":   validAgentConfig + "cache_max_bytes = -1\n",
		"overflow cache":   validAgentConfig + "cache_max_bytes = 9223372036854775808\n",
		"cache root":       validAgentConfig + "cache_root = \"/private/cache\"\n",
		"malformed target": validAgentConfig + "target_id = \"NOT-A-UUID\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := agentconfig.Load(path); err == nil {
				t.Fatal("invalid config loaded")
			}
		})
	}
}

func TestLoadAgentConfigAcceptsConfiguredHTTPPort(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := strings.Replace(validAgentConfig, `0.0.0.0:8182`, `0.0.0.0:9191`, 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := agentconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenAddress != "0.0.0.0:9191" {
		t.Fatalf("listen address = %q", got.ListenAddress)
	}
}

func TestNativeConfigHasNoMainSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	base := "listen_address = \"127.0.0.1:18182\"\ntoken = \"test-token\"\n"
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentconfig.Load(path); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mister_process_comm", "command_pipe", "core_name_file", "menu_rbf", "mgl_directory"} {
		if err := os.WriteFile(path, []byte(base+key+" = \"/retired\"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := agentconfig.Load(path); err == nil {
			t.Fatalf("%s must be rejected clearly", key)
		} else if message, safe := agentconfig.RetiredSettingsMessage(err); !safe || !strings.Contains(message, key) || strings.Contains(message, "/retired") || strings.Contains(message, "test-token") {
			t.Fatalf("unsafe or unhelpful retired-setting error: %q", message)
		}
	}
}
