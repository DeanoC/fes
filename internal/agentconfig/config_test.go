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

const validDevelopmentConfig = validAgentConfig + `build_profile = "development"
development_profile = true
hardware_owner_path = "/var/lib/fogcast/hardware-owner-v1.json"
hardware_owner_lock = "/run/fogcast/hardware-owner-v1.lock"
designation_path = "/run/fogcast/designation"
target_identity_path = "/run/fogcast/target-identity"
fpgadev_boot_dispatcher = "/media/fat/linux/user-startup.sh"
fpgadev_start_sources = ["/etc/init.d/S99fogcast-agent", "/media/fat/linux/user-startup.sh"]
`

func TestLoadAgentConfigCapturesExplicitFPGABootAuthority(t *testing.T) {
	t.Parallel()
	cfg, err := agentconfig.Parse([]byte(validDevelopmentConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FPGADevBootDispatcher != "/media/fat/linux/user-startup.sh" {
		t.Fatalf("dispatcher = %q", cfg.FPGADevBootDispatcher)
	}
	if len(cfg.FPGADevStartSources) != 2 || cfg.FPGADevStartSources[1] != "/media/fat/linux/user-startup.sh" {
		t.Fatalf("start sources = %#v", cfg.FPGADevStartSources)
	}
	if err := cfg.ValidateDevelopmentInventory(); err != nil {
		t.Fatalf("explicit boot authority rejected: %v", err)
	}
}

func TestLoadAgentConfigRejectsInvalidFPGABootAuthority(t *testing.T) {
	t.Parallel()
	base, err := agentconfig.Parse([]byte(validDevelopmentConfig))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]agentconfig.Config{}
	missingDispatcher := base
	missingDispatcher.FPGADevBootDispatcher = ""
	tests["missing dispatcher"] = missingDispatcher
	missingSources := base
	missingSources.FPGADevStartSources = nil
	tests["missing sources"] = missingSources
	unsorted := base
	unsorted.FPGADevStartSources = []string{"/media/fat/linux/user-startup.sh", "/etc/init.d/S99fogcast-agent"}
	tests["unsorted sources"] = unsorted
	duplicate := base
	duplicate.FPGADevStartSources = []string{"/etc/init.d/S99fogcast-agent", "/etc/init.d/S99fogcast-agent"}
	tests["duplicate sources"] = duplicate
	notMember := base
	notMember.FPGADevBootDispatcher = "/media/fat/linux/other-startup.sh"
	tests["dispatcher not inventoried"] = notMember
	tooMany := base
	tooMany.FPGADevStartSources = make([]string, 17)
	for index := range tooMany.FPGADevStartSources {
		tooMany.FPGADevStartSources[index] = "/etc/init.d/S" + strings.Repeat("0", 2) + string(rune('A'+index))
	}
	tests["too many sources"] = tooMany
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			if err := cfg.ValidateFPGABootAuthority(); err == nil {
				t.Fatal("invalid FPGA development boot authority accepted")
			}
		})
	}
	for name, content := range map[string]string{
		"missing dispatcher field": strings.Replace(validDevelopmentConfig, "fpgadev_boot_dispatcher = \"/media/fat/linux/user-startup.sh\"\n", "", 1),
		"missing sources field":    strings.Replace(validDevelopmentConfig, "fpgadev_start_sources = [\"/etc/init.d/S99fogcast-agent\", \"/media/fat/linux/user-startup.sh\"]\n", "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := agentconfig.Parse([]byte(content)); err == nil {
				t.Fatal("development config without explicit FPGA authority parsed")
			}
		})
	}
}

func TestLoadAgentConfigValidatesRestrictedDevelopmentProfile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(validDevelopmentConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := agentconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BuildProfile != "development" || !cfg.DevelopmentProfile || cfg.HardwareOwnerPath == "" || cfg.HardwareOwnerLock == "" || cfg.DesignationPath == "" || cfg.TargetIdentityPath == "" {
		t.Fatalf("development config = %#v", cfg)
	}
	if err := cfg.ValidateProfile(true); err != nil {
		t.Fatalf("development profile with capability rejected: %v", err)
	}
	if err := cfg.ValidateProfile(false); err == nil {
		t.Fatal("development profile accepted without compile-time capability")
	}
}

func TestLoadAgentConfigRejectsDevelopmentFieldsInProductionProfile(t *testing.T) {
	t.Parallel()
	for _, field := range []string{
		`development_profile = false`,
		`hardware_owner_path = "/var/lib/fogcast/owner.json"`,
		`hardware_owner_lock = "/run/fogcast/owner.lock"`,
		`designation_path = "/run/fogcast/designation"`,
		`target_identity_path = "/run/fogcast/identity"`,
	} {
		t.Run(field, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.toml")
			if err := os.WriteFile(path, []byte(validAgentConfig+field+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := agentconfig.Load(path); err == nil {
				t.Fatal("development-only field accepted by production config")
			}
		})
	}
}

func TestLoadAgentConfigRequiresCompleteDevelopmentPathsAndExplicitOptIn(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing opt in":       strings.Replace(validDevelopmentConfig, "development_profile = true\n", "", 1),
		"missing owner path":   strings.Replace(validDevelopmentConfig, "hardware_owner_path = \"/var/lib/fogcast/hardware-owner-v1.json\"\n", "", 1),
		"relative lock":        strings.Replace(validDevelopmentConfig, "hardware_owner_lock = \"/run/fogcast/hardware-owner-v1.lock\"", "hardware_owner_lock = \"owner.lock\"", 1),
		"relative designation": strings.Replace(validDevelopmentConfig, "designation_path = \"/run/fogcast/designation\"", "designation_path = \"designation\"", 1),
		"relative identity":    strings.Replace(validDevelopmentConfig, "target_identity_path = \"/run/fogcast/target-identity\"", "target_identity_path = \"identity\"", 1),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := agentconfig.Load(path)
			if err != nil {
				return
			}
			if err := cfg.ValidateProfile(true); err == nil {
				t.Fatal("incomplete development profile accepted")
			}
		})
	}
}
