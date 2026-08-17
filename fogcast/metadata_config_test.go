package fogcast_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/fogcast"
)

func TestDefaultPathsIncludesPrivateMetadataRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if paths.MetadataRoot != filepath.Join(home, ".cache", "fogcast", "metadata") {
		t.Fatalf("MetadataRoot = %q", paths.MetadataRoot)
	}
}

func TestLoadConfigDistinguishesAbsentDisabledAndEnabledMetadata(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	cases := map[string]string{
		"absent": base,
		"disabled": base + `
[metadata]
provider = "igdb"
enabled = false
`,
		"enabled": base + `
[metadata]
provider = "igdb"
enabled = true
client_id = "client-id"
client_secret = "client-secret"
`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			config, err := fogcast.LoadConfig(writeConfig(t, content))
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "absent":
				if config.Metadata.Configured || config.Metadata.Enabled {
					t.Fatalf("metadata = %#v", config.Metadata)
				}
			case "disabled":
				if !config.Metadata.Configured || config.Metadata.Enabled || config.Metadata.Provider != "igdb" {
					t.Fatalf("metadata = %#v", config.Metadata)
				}
			case "enabled":
				if !config.Metadata.Configured || !config.Metadata.Enabled || config.Metadata.Provider != "igdb" || config.Metadata.ClientID != "client-id" || config.Metadata.ClientSecret != "client-secret" {
					t.Fatalf("metadata = %#v", config.Metadata)
				}
			}
		})
	}
}

func TestLoadConfigRejectsUnsafeEnabledMetadataConfiguration(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	for name, section := range map[string]string{
		"missing client id": `enabled = true
provider = "igdb"
client_secret = "secret"`,
		"missing client secret": `enabled = true
provider = "igdb"
client_id = "id"`,
		"wrong provider": `enabled = true
provider = "steam"
client_id = "id"
client_secret = "secret"`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, base+"\n[metadata]\n"+section+"\n")); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
}

func TestLoadConfigAcceptsLaunchBoxArchiveWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "Metadata.zip")
	if err := os.WriteFile(archive, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")) + `
[metadata]
provider = "launchbox"
enabled = true
archive = "` + archive + `"
`
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if !config.Metadata.Enabled || config.Metadata.Provider != "launchbox" || config.Metadata.Archive != archive || config.Metadata.ClientSecret != "" {
		t.Fatalf("metadata = %#v", config.Metadata)
	}
}

func TestLoadConfigRequiresExplicitIGDBProviderWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	content := base + `
[metadata]
enabled = true
client_id = "id"
client_secret = "secret"
`
	if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
		t.Fatal("enabled metadata without explicit provider accepted")
	}
}

func TestLoadConfigRejectsNonCleanLibraryRoot(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	content := strings.Replace(base, filepath.Join(dir, "SNES"), dir+"/SNES/../SNES", 1)
	if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
		t.Fatal("non-clean library root accepted")
	}
}

func TestLoadConfigRequiresPrivateConfigFileWhenMetadataEnabled(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))+`[metadata]
enabled = true
provider = "igdb"
client_id = "id"
client_secret = "secret"
`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fogcast.LoadConfig(path); err == nil {
		t.Fatal("world-readable metadata config accepted")
	}
}

func TestREADMEDocumentsDisabledMetadataContract(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(readme)
	for _, required := range []string{
		"## Host metadata (disabled by default)",
		"[metadata]",
		"enabled = false",
		"provider = \"igdb\"",
		"enabled = true",
		"client_id = \"REPLACE_IN_PRIVATE_CONFIG\"",
		"client_secret = \"REPLACE_IN_PRIVATE_CONFIG\"",
		"mode `0600`",
		"~/.cache/fogcast/metadata",
		"cache.sqlite3*",
		"memory only",
		"`0700` directories",
		"no `player_count` request",
		"Data from IGDB.com",
		"purges",
		"same-origin",
		"terms",
		"synthetic provider responses",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("README is missing metadata contract text %q", required)
		}
	}
}
