package fogcast_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestDefaultPathsUsesHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := fogcast.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths.Config != filepath.Join(home, ".config", "fogcast", "config.toml") {
		t.Fatalf("Config = %q", paths.Config)
	}
	if paths.Index != filepath.Join(home, ".local", "share", "fogcast", "library.sqlite3") {
		t.Fatalf("Index = %q", paths.Index)
	}
	if paths.Staging != filepath.Join(home, ".cache", "fogcast", "staging") {
		t.Fatalf("Staging = %q", paths.Staging)
	}
}

func TestLoadConfigLoadsApprovedTOML(t *testing.T) {
	dir := t.TempDir()
	snesRoot := filepath.Join(dir, "SNES")
	megaRoot := filepath.Join(dir, "Genesis")
	if err := os.MkdirAll(snesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(megaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, `base_url = "http://192.0.2.10:8182"
token = "  exact token  "
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "snes-main"
system = "snes"
root = "`+snesRoot+`/."

[[libraries]]
id = "genesis-main"
system = "megadrive"
root = "`+megaRoot+`"
`)

	cfg, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://192.0.2.10:8182" || cfg.Token != "  exact token  " {
		t.Fatalf("connection = %#v", cfg)
	}
	if cfg.RequestTimeout != 12*time.Second || cfg.UploadTimeout != 60*time.Second {
		t.Fatalf("timeouts = request %s, upload %s", cfg.RequestTimeout, cfg.UploadTimeout)
	}
	if len(cfg.Libraries) != 2 {
		t.Fatalf("Libraries = %#v", cfg.Libraries)
	}
	if got := cfg.Libraries[0]; got.ID != "snes-main" || got.System != protocol.SystemSNES || got.Path != filepath.Clean(snesRoot) {
		t.Fatalf("SNES library = %#v", got)
	}
	if got := cfg.Libraries[1]; got.ID != "genesis-main" || got.System != protocol.SystemMegaDrive || got.Path != filepath.Clean(megaRoot) {
		t.Fatalf("Mega Drive library = %#v", got)
	}
}

func TestLoadConfigRejectsSymlinkRootConsistentlyOnlineAndOffline(t *testing.T) {
	dir := t.TempDir()
	actual := filepath.Join(dir, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	offline := filepath.Join(dir, "missing", "..", "offline")
	path := writeConfig(t, validConfig(link, offline))

	if _, err := fogcast.LoadConfig(path); err == nil {
		t.Fatal("online symlink root was accepted")
	}
	moved := filepath.Join(dir, "actual-offline")
	if err := os.Rename(actual, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := fogcast.LoadConfig(path); err == nil {
		t.Fatal("offline symlink root was accepted")
	}
}

func TestLoadConfigKeepsIntermediateSymlinkBindingStableAcrossOfflineReopen(t *testing.T) {
	dir := t.TempDir()
	actualParent := filepath.Join(dir, "actual-parent")
	actualRoot := filepath.Join(actualParent, "library")
	if err := os.MkdirAll(actualRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(dir, "linked-parent")
	if err := os.Symlink(actualParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	configuredRoot := filepath.Join(linkedParent, "library")
	path := writeConfig(t, validConfig(configuredRoot, filepath.Join(dir, "offline")))

	online, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(online): %v", err)
	}
	if err := os.Rename(actualRoot, actualRoot+"-offline"); err != nil {
		t.Fatal(err)
	}
	offline, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(offline): %v", err)
	}
	if got, want := online.Libraries[0].Path, filepath.Clean(configuredRoot); got != want {
		t.Fatalf("online binding = %q, want configured identity %q", got, want)
	}
	if online.Libraries[0].Path != offline.Libraries[0].Path {
		t.Fatalf("binding changed across offline reopen: online=%q offline=%q", online.Libraries[0].Path, offline.Libraries[0].Path)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	firstRoot := filepath.Join(dir, "first")
	secondRoot := filepath.Join(dir, "second")
	if err := os.MkdirAll(firstRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	valid := validConfig(firstRoot, secondRoot)

	tests := map[string]string{
		"unknown field":        valid + "discovery = true\n",
		"duplicate library id": strings.Replace(valid, `id = "genesis-main"`, `id = "snes-main"`, 1),
		"invalid library id":   strings.Replace(valid, `id = "snes-main"`, `id = "SNES Main"`, 1),
		"duplicate root":       strings.Replace(valid, secondRoot, firstRoot, 1),
		"unknown system":       strings.Replace(valid, `system = "snes"`, `system = "nes"`, 1),
		"empty root":           strings.Replace(valid, firstRoot, "", 1),
		"relative root":        strings.Replace(valid, firstRoot, "relative/games", 1),
		"empty token":          strings.Replace(valid, `token = "test-token"`, `token = " \t "`, 1),
		"zero request timeout": strings.Replace(valid, "request_timeout_seconds = 12", "request_timeout_seconds = 0", 1),
		"zero upload timeout":  strings.Replace(valid, "upload_timeout_seconds = 60", "upload_timeout_seconds = 0", 1),
		"request overflow":     strings.Replace(valid, "request_timeout_seconds = 12", "request_timeout_seconds = 9223372037", 1),
		"upload overflow":      strings.Replace(valid, "upload_timeout_seconds = 60", "upload_timeout_seconds = 9223372037", 1),
		"credentials in url":   strings.Replace(valid, "http://192.0.2.10:8182", "http://token@192.0.2.10:8182", 1),
		"https url":            strings.Replace(valid, "http://", "https://", 1),
		"url path":             strings.Replace(valid, ":8182\"", ":8182/base\"", 1),
		"url query":            strings.Replace(valid, ":8182\"", ":8182?x=1\"", 1),
		"empty url query":      strings.Replace(valid, ":8182\"", ":8182?\"", 1),
		"url fragment":         strings.Replace(valid, ":8182\"", ":8182#fragment\"", 1),
		"empty url fragment":   strings.Replace(valid, ":8182\"", ":8182#\"", 1),
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
				t.Fatal("invalid configuration loaded")
			}
		})
	}
}

func validConfig(firstRoot, secondRoot string) string {
	return `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "snes-main"
system = "snes"
root = "` + firstRoot + `"

[[libraries]]
id = "genesis-main"
system = "megadrive"
root = "` + secondRoot + `"
`
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
