package fogcast

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/pelletier/go-toml/v2"
)

func TestWriteCanonicalConfigUpdatesManagedSettingsAndPreservesOtherConfig(t *testing.T) {
	dir := t.TempDir()
	oldRoot := filepath.Join(dir, "old-SNES")
	newRoot := filepath.Join(dir, "new-SNES")
	path := filepath.Join(dir, "config.toml")
	content := `base_url = "http://192.0.2.20:8182"
token = "test-token"
request_timeout_seconds = 17
upload_timeout_seconds = 71

[remote_input]
enabled = true

[[libraries]]
id = "old-snes"
system = "snes"
root = "` + oldRoot + `"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := writeCanonicalConfig(path,
		[]catalog.Root{{ID: "new-snes", System: protocol.SystemSNES, Path: newRoot}},
		[]TargetConfig{
			{Name: "dev", Enabled: true, Address: "http://192.0.2.30:8182", Agent: "test-token", AgentSet: true},
			{Name: "spare"},
		},
		"dev",
	)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RequestTimeout.Seconds() != 17 || loaded.UploadTimeout.Seconds() != 71 || !loaded.RemoteInput.Enabled {
		t.Fatalf("unrelated config was not preserved: %#v", loaded)
	}
	if len(loaded.Libraries) != 1 || loaded.Libraries[0].ID != "new-snes" || loaded.Libraries[0].Path != newRoot {
		t.Fatalf("libraries = %#v", loaded.Libraries)
	}
	if loaded.SelectedTarget != "dev" || len(loaded.Targets) != 2 || loaded.Targets[1].Name != "spare" {
		t.Fatalf("targets = %#v, selected = %q", loaded.Targets, loaded.SelectedTarget)
	}
	if loaded.BaseURL != "http://192.0.2.30:8182" || loaded.Token != "test-token" {
		t.Fatalf("compatibility mirrors = %q, token-set=%t", loaded.BaseURL, loaded.Token != "")
	}
	written, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer written.Close()
	var persisted fileConfig
	if err := toml.NewDecoder(written).Decode(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.BaseURL != "" || persisted.Token != "" {
		t.Fatal("legacy singleton credentials remained after named-target persistence")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("atomic temporary files remain: %#v", entries)
	}
}

func TestWriteCanonicalConfigRejectsInvalidManagedSettingsWithoutChangingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const original = `base_url = "http://192.0.2.20:8182"
token = "test-token"
request_timeout_seconds = 17
upload_timeout_seconds = 71
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeCanonicalConfig(path, nil, []TargetConfig{{Name: "dev", Enabled: true, Address: "not-an-origin", Agent: "test-token"}}, "dev")
	if err == nil {
		t.Fatal("invalid targets accepted")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatal("config changed after rejected write")
	}
}

func TestWriteCanonicalConfigClearsLegacyMappedFolderWatchRoot(t *testing.T) {
	dir := t.TempDir()
	legacyRoot := filepath.Join(dir, "SNES")
	newRoot := filepath.Join(dir, "NES")
	for _, root := range []string{legacyRoot, newRoot} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	content := `base_url = "http://192.0.2.20:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "` + legacyRoot + `"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Libraries) != 1 {
		t.Fatalf("legacy libraries = %#v", loaded.Libraries)
	}
	legacyID := loaded.Libraries[0].ID
	if err := writeCanonicalConfig(path,
		[]catalog.Root{{ID: legacyID, System: protocol.SystemNES, Path: newRoot}},
		[]TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.20:8182", Agent: "test-token"}},
		"dev",
	); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Libraries) != 1 || reloaded.Libraries[0].Path != newRoot || reloaded.Libraries[0].System != protocol.SystemNES {
		t.Fatalf("reloaded libraries = %#v", reloaded.Libraries)
	}
	written, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer written.Close()
	var persisted fileConfig
	if err := toml.NewDecoder(written).Decode(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Library == nil || persisted.Library.WatchRoot != "" {
		t.Fatalf("legacy watch_root remained: %#v", persisted.Library)
	}
}
