package agentconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
)

const validAgentConfig = `listen_address = "0.0.0.0:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
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
	if got.ListenAddress != "0.0.0.0:8182" || got.Token != "test-token" || got.CommandPipe != "/dev/MiSTer_cmd" {
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

func TestLoadAgentConfigRejectsUnknownUnsafeAndInvalidCacheValues(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unknown":          validAgentConfig + "extra = true\n",
		"empty token":      strings.Replace(validAgentConfig, `token = "test-token"`, `token = ""`, 1),
		"empty process":    strings.Replace(validAgentConfig, `mister_process_comm = "MiSTer"`, `mister_process_comm = ""`, 1),
		"wrong port":       strings.Replace(validAgentConfig, `0.0.0.0:8182`, `0.0.0.0:8183`, 1),
		"malformed listen": strings.Replace(validAgentConfig, `0.0.0.0:8182`, `0.0.0.0`, 1),
		"relative pipe":    strings.Replace(validAgentConfig, `command_pipe = "/dev/MiSTer_cmd"`, `command_pipe = "MiSTer_cmd"`, 1),
		"relative core":    strings.Replace(validAgentConfig, `core_name_file = "/tmp/CORENAME"`, `core_name_file = "CORENAME"`, 1),
		"relative menu":    strings.Replace(validAgentConfig, `menu_rbf = "/media/fat/menu.rbf"`, `menu_rbf = "menu.rbf"`, 1),
		"relative MGL":     strings.Replace(validAgentConfig, `mgl_directory = "/tmp/mister-remote"`, `mgl_directory = "mister-remote"`, 1),
		"zero cache":       validAgentConfig + "cache_max_bytes = 0\n",
		"negative cache":   validAgentConfig + "cache_max_bytes = -1\n",
		"overflow cache":   validAgentConfig + "cache_max_bytes = 9223372036854775808\n",
		"cache root":       validAgentConfig + "cache_root = \"/private/cache\"\n",
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
