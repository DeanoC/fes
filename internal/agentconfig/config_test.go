package agentconfig_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/agentconfig"
)

const validAgentConfig = `listen_address = "0.0.0.0:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
`

func TestLoadAgentConfig(t *testing.T) {
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
}

func TestLoadAgentConfigRejectsUnknownAndUnsafeValues(t *testing.T) {
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
