//go:build linux && fpgadev

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionInstallPrerequisitesMissingConfigFailsBeforeMutation(t *testing.T) {
	_, _, err := productionInstallPrerequisites(t.TempDir() + "/agent.toml")
	if err == nil || !strings.Contains(err.Error(), "development profile configuration unavailable") {
		t.Fatalf("prerequisite error = %v", err)
	}
}

func TestProductionInstallPrerequisitesRejectsMalformedConfig(t *testing.T) {
	for _, content := range []string{"not = [toml"} {
		path := filepath.Join(t.TempDir(), "agent.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := productionInstallPrerequisites(path); err == nil || !strings.Contains(err.Error(), "development profile configuration unavailable") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestProductionInstallPrerequisitesRejectsExactPlaceholderInOtherwiseValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := strings.Replace(validProductionDevelopmentConfig, `token = "operator-token"`, `token = "REPLACE_WITH_A_TARGET_LOCAL_TOKEN"`, 1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := productionInstallPrerequisites(path); err == nil || err.Error() != "development profile configuration invalid: placeholder token" {
		t.Fatalf("placeholder prerequisite error = %v", err)
	}
}

func TestProductionInstallPrerequisitesAcceptsValidProtectedDevelopmentConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := os.WriteFile(path, []byte(validProductionDevelopmentConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, hash, err := productionInstallPrerequisites(path); err != nil || hash == "" {
		t.Fatalf("valid prerequisite = hash %q err %v", hash, err)
	}
}

const validProductionDevelopmentConfig = `listen_address = "0.0.0.0:8182"
token = "operator-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
build_profile = "development"
development_profile = true
hardware_owner_path = "/var/lib/fogcast/hardware-owner-v1.json"
hardware_owner_lock = "/run/fogcast/hardware-owner-v1.lock"
designation_path = "/run/fogcast/designation"
target_identity_path = "/run/fogcast/target-identity"
fpgadev_boot_dispatcher = "/media/fat/linux/user-startup.sh"
fpgadev_start_sources = ["/etc/init.d/S99fogcast-agent", "/media/fat/linux/user-startup.sh"]
`
