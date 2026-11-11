package fogcast_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
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
	if paths.LibrarySettings != filepath.Join(home, ".local", "share", "fogcast", "library-settings.json") {
		t.Fatalf("LibrarySettings = %q", paths.LibrarySettings)
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
root = "`+snesRoot+`"

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

func TestLoadConfigDefaultsAgentBaseURLWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "SNES")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(validConfig(root, filepath.Join(dir, "other")), `base_url = "http://192.0.2.10:8182"`+"\n", "", 1)
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != fogcast.DefaultAgentBaseURL {
		t.Fatalf("base URL = %q, want %q", config.BaseURL, fogcast.DefaultAgentBaseURL)
	}
}

func TestLoadConfigMigratesLegacyAgentToDevTarget(t *testing.T) {
	dir := t.TempDir()
	config, err := fogcast.LoadConfig(writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))))
	if err != nil {
		t.Fatal(err)
	}
	if config.SelectedTarget != "dev" || len(config.Targets) != 1 {
		t.Fatalf("targets = %#v, selected = %q", config.Targets, config.SelectedTarget)
	}
	if got := config.Targets[0]; got.Name != "dev" || !got.Enabled || got.Address != "http://192.0.2.10:8182" || got.Agent != "test-token" || got.AgentSet {
		t.Fatalf("dev target = %#v", got)
	}
}

func TestLoadConfigLoadsNamedTargetsIncludingDisabledTarget(t *testing.T) {
	dir := t.TempDir()
	content := strings.Replace(validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")),
		`base_url = "http://192.0.2.10:8182"`+"\n"+`token = "test-token"`+"\n",
		`selected_target = "dev"`+"\n", 1) +
		"\n[[targets]]\nname = \"dev\"\nenabled = true\naddress = \"http://192.0.2.10:8182\"\nagent = \"test-token\"\n" +
		"\n[[targets]]\nname = \"spare\"\nenabled = false\naddress = \"\"\nagent = \"\"\n"
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if config.SelectedTarget != "dev" || len(config.Targets) != 2 {
		t.Fatalf("targets = %#v, selected = %q", config.Targets, config.SelectedTarget)
	}
	if got := config.Targets[1]; got.Name != "spare" || got.Enabled || got.Address != "" || got.Agent != "" || got.AgentSet {
		t.Fatalf("disabled target = %#v", got)
	}
	if config.BaseURL != "http://192.0.2.10:8182" || config.Token != "test-token" {
		t.Fatalf("selected compatibility mirrors = %q, token-set=%t", config.BaseURL, config.Token != "")
	}
}

func TestLoadConfigRejectsDisabledSelectedTargetForStartupBoundFeatures(t *testing.T) {
	dir := t.TempDir()
	base := strings.Replace(validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")),
		`base_url = "http://192.0.2.10:8182"`+"\n"+`token = "test-token"`+"\n",
		`selected_target = "spare"`+"\n", 1) + `
[[targets]]
name = "dev"
enabled = true
address = "http://192.0.2.10:8182"
agent = "test-token"

[[targets]]
name = "spare"
enabled = false
address = ""
agent = ""
`
	for name, feature := range map[string]string{
		"remote input": "[remote_input]\nenabled = true\n",
		"media":        "[media]\nenabled = true\nsession = \"fixture-session\"\nssrc = 1\nrtp_listen = \"127.0.0.1:5000\"\nrtp_destination = \"127.0.0.1:5001\"\ncapture_device = \"fixture-device\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, base+"\n"+feature)); err == nil {
				t.Fatal("disabled selected target accepted for startup-bound feature")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidNamedTargetsWithoutLeakingAgent(t *testing.T) {
	dir := t.TempDir()
	base := strings.Replace(validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")),
		`base_url = "http://192.0.2.10:8182"`+"\n"+`token = "test-token"`+"\n", "", 1)
	const secretFixture = "test-token-must-not-appear"
	tests := map[string]string{
		"invalid name":          `selected_target = "Bad Name"\n[[targets]]\nname = "Bad Name"\nenabled = true\naddress = "http://192.0.2.10:8182"\nagent = "` + secretFixture + `"\n`,
		"invalid address":       `selected_target = "dev"\n[[targets]]\nname = "dev"\nenabled = true\naddress = "https://192.0.2.10:8182"\nagent = "` + secretFixture + `"\n`,
		"duplicate name":        `selected_target = "dev"\n[[targets]]\nname = "dev"\nenabled = true\naddress = "http://192.0.2.10:8182"\nagent = "` + secretFixture + `"\n[[targets]]\nname = "dev"\n`,
		"address without agent": `selected_target = "dev"\n[[targets]]\nname = "dev"\nenabled = true\naddress = "http://192.0.2.10:8182"\nagent = ""\n`,
		"agent without address": `selected_target = "dev"\n[[targets]]\nname = "dev"\nenabled = true\naddress = ""\nagent = "` + secretFixture + `"\n`,
		"unknown selected":      `selected_target = "missing"\n[[targets]]\nname = "dev"\nenabled = true\naddress = "http://192.0.2.10:8182"\nagent = "` + secretFixture + `"\n`,
	}
	for name, targetConfig := range tests {
		t.Run(name, func(t *testing.T) {
			targetConfig = strings.ReplaceAll(targetConfig, `\n`, "\n")
			selectedLine, targetTables, ok := strings.Cut(targetConfig, "\n")
			if !ok {
				t.Fatal("test target fixture must contain a selected target and target tables")
			}
			content := strings.Replace(base, "request_timeout_seconds = 12", selectedLine+"\nrequest_timeout_seconds = 12", 1) + targetTables
			_, err := fogcast.LoadConfig(writeConfig(t, content))
			if err == nil {
				t.Fatal("invalid target configuration accepted")
			}
			if strings.Contains(err.Error(), secretFixture) {
				t.Fatalf("error leaked agent fixture: %v", err)
			}
		})
	}
}

func TestLoadConfigDefaultsMediaDisabled(t *testing.T) {
	dir := t.TempDir()
	config, err := fogcast.LoadConfig(writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))))
	if err != nil {
		t.Fatal(err)
	}
	if config.Media.Enabled {
		t.Fatalf("media unexpectedly enabled: %#v", config.Media)
	}
}

func TestLoadConfigAllowsLocalMJPEGPreviewWithoutTransportIdentity(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))+`
[media]
enabled = true
decoder = "mjpeg"
capture_device = "fixture-device"
`)
	config, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Media; !got.Enabled || got.Decoder != "mjpeg" || got.CaptureDevice != "fixture-device" {
		t.Fatalf("media config = %#v", got)
	}
}

func TestLoadConfigRejectsInvalidEnabledMediaWithoutLeakingValues(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	for name, media := range map[string]string{
		"missing capture":   `enabled = true`,
		"bad decoder":       `enabled = true\ncapture_device = "device"\ndecoder = "secret-decoder"`,
		"streaming decoder": `enabled = true\ncapture_device = "device"\ndecoder = "none"`,
		"partial fps":       `enabled = true\ncapture_device = "device"\nfps_numerator = 30`,
		"retired transport": `enabled = true\ncapture_device = "device"\nrtp_destination = "127.0.0.1:5001"`,
		"retired audio":     `enabled = true\ncapture_device = "device"\n[media.audio]\nenabled = true`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fogcast.LoadConfig(writeConfig(t, base+"\n[media]\n"+strings.ReplaceAll(media, `\n`, "\n")+"\n"))
			if err == nil || strings.Contains(err.Error(), "secret-decoder") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadConfigRejectsRetiredHostEmulator(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	_, err := fogcast.LoadConfig(writeConfig(t, base+"\n[host_emulator]\nbinary = \"/retroarch\"\n"))
	if err == nil {
		t.Fatal("retired host emulator accepted")
	}
}

func TestLoadConfigLoadsRemoteInputEnablement(t *testing.T) {
	dir := t.TempDir()
	firstRoot := filepath.Join(dir, "SNES")
	secondRoot := filepath.Join(dir, "Genesis")
	if err := os.MkdirAll(firstRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, validConfig(firstRoot, secondRoot)+`
[remote_input]
enabled = true
`)
	config, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.RemoteInput.Enabled {
		t.Fatalf("remote input config = %#v", config.RemoteInput)
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
		"unknown system":       strings.Replace(valid, `system = "snes"`, `system = "mystery"`, 1),
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

func TestLoadConfigWatchRootComesFromConfigNotCandidateDefault(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "games-a")
	second := filepath.Join(dir, "games-b")
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	firstCfg, err := fogcast.LoadConfig(writeConfig(t, validConfig(first, filepath.Join(dir, "other-a"))+"\n[library]\nwatch_root = \""+first+"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	secondCfg, err := fogcast.LoadConfig(writeConfig(t, validConfig(second, filepath.Join(dir, "other-b"))+"\n[library]\nwatch_root = \""+second+"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if firstCfg.Library.WatchRoot != first || secondCfg.Library.WatchRoot != second {
		t.Fatalf("watch roots = %q and %q", firstCfg.Library.WatchRoot, secondCfg.Library.WatchRoot)
	}
	if firstCfg.Library.WatchRoot == fogcast.DefaultFolderWatchRoot || secondCfg.Library.WatchRoot == fogcast.DefaultFolderWatchRoot {
		t.Fatal("explicit watch_root ignored in favor of the candidate UNC")
	}
}

func TestLoadConfigWatchRootDefaultsToShareRoot(t *testing.T) {
	dir := t.TempDir()
	snesRoot := filepath.Join(dir, "SNES")
	if err := os.MkdirAll(snesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	withLibrary, err := fogcast.LoadConfig(writeConfig(t, validConfig(snesRoot, filepath.Join(dir, "Genesis"))))
	if err != nil {
		t.Fatal(err)
	}
	if withLibrary.Library.WatchRoot != fogcast.DefaultFolderWatchRoot {
		t.Fatalf("watch root with local libraries = %q", withLibrary.Library.WatchRoot)
	}

	withoutSNES := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
`
	bare, err := fogcast.LoadConfig(writeConfig(t, withoutSNES))
	if err != nil {
		t.Fatal(err)
	}
	if bare.Library.WatchRoot != fogcast.DefaultFolderWatchRoot {
		t.Fatalf("default watch_root = %q, want %q", bare.Library.WatchRoot, fogcast.DefaultFolderWatchRoot)
	}
}

func TestLoadConfigPreservesUNCWatchRootCandidate(t *testing.T) {
	content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "//deano-clawz/Games/Games"
`
	cfg, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.WatchRoot != fogcast.DefaultFolderWatchRoot {
		t.Fatalf("UNC watch_root = %q", cfg.Library.WatchRoot)
	}
}

func TestLoadConfigNormalizesLegacyMappedFolderWatchRootToShare(t *testing.T) {
	content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "//deano-clawz/Games/Games/SNES"
`
	cfg, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.WatchRoot != fogcast.DefaultFolderWatchRoot {
		t.Fatalf("legacy watch_root normalized to %q", cfg.Library.WatchRoot)
	}
}

func TestLoadConfigMigratesLegacyLocalMappedFolderToLibrary(t *testing.T) {
	share := t.TempDir()
	legacy := filepath.Join(share, "SNES")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "` + legacy + `"
`
	cfg, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.WatchRoot != share {
		t.Fatalf("legacy local share root = %q, want %q", cfg.Library.WatchRoot, share)
	}
	if len(cfg.Libraries) != 1 || cfg.Libraries[0].System != protocol.SystemSNES || cfg.Libraries[0].Path != legacy || !strings.HasPrefix(cfg.Libraries[0].ID, "folder-watch-") {
		t.Fatalf("legacy local library = %#v", cfg.Libraries)
	}
}

func TestLoadConfigMigratesRootLevelLegacyMappedFolderToLibrary(t *testing.T) {
	tests := map[string]protocol.System{
		"Genesis": protocol.SystemMegaDrive,
		"SNES":    protocol.SystemSNES,
	}
	for folder, system := range tests {
		t.Run(folder, func(t *testing.T) {
			legacy := "/" + folder
			content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "` + legacy + `"
`
			cfg, err := fogcast.LoadConfig(writeConfig(t, content))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Library.WatchRoot != "/" {
				t.Fatalf("legacy local share root = %q, want /", cfg.Library.WatchRoot)
			}
			if len(cfg.Libraries) != 1 || cfg.Libraries[0].System != system || cfg.Libraries[0].Path != legacy || !strings.HasPrefix(cfg.Libraries[0].ID, "folder-watch-") {
				t.Fatalf("legacy local library = %#v", cfg.Libraries)
			}
		})
	}
}

func TestLoadConfigRejectsAmbiguousLocalShareWithoutLibraries(t *testing.T) {
	content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
watch_root = "` + t.TempDir() + `"
`
	if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil || !strings.Contains(err.Error(), "requires an explicit [[libraries]] mapping") {
		t.Fatalf("ambiguous local share error = %v", err)
	}
}

func TestLoadConfigRejectsLegacyLocalMappedFolderConflicts(t *testing.T) {
	share := t.TempDir()
	legacy := filepath.Join(share, "SNES")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(legacy))
	legacyID := "folder-watch-" + hex.EncodeToString(digest[:6])
	tests := map[string]string{
		"path": `[[libraries]]
id = "nes-conflict"
system = "nes"
root = "` + legacy + `"
`,
		"id": `[[libraries]]
id = "` + legacyID + `"
system = "nes"
root = "` + filepath.Join(share, "NES") + `"
`,
	}
	for name, library := range tests {
		t.Run(name, func(t *testing.T) {
			content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

` + library + `
[library]
watch_root = "` + legacy + `"
`
			if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil || !strings.Contains(err.Error(), "conflicts with [[libraries]]") {
				t.Fatalf("legacy conflict error = %v", err)
			}
		})
	}
}

func TestLoadConfigAllowsMultipleLocalRootsForOneMappedSystem(t *testing.T) {
	dir := t.TempDir()
	firstRoot := filepath.Join(dir, "SNES-a")
	secondRoot := filepath.Join(dir, "SNES-b")
	content := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "snes-a"
system = "snes"
root = "` + firstRoot + `"

[[libraries]]
id = "snes-b"
system = "snes"
root = "` + secondRoot + `"

[library]
watch_root = "//server/share"
`
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Libraries) != 2 {
		t.Fatalf("libraries = %#v", config.Libraries)
	}
}

func TestLoadConfigRejectsInvalidWatchRoot(t *testing.T) {
	base := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[library]
`
	tests := map[string]string{
		"relative":  base + "watch_root = \"relative/games\"\n",
		"empty UNC": base + "watch_root = \"//server\"\n",
		"dot UNC":   base + "watch_root = \"//server/share/../other\"\n",
		"unclean":   base + "watch_root = \"/tmp/../etc\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
				t.Fatal("invalid watch_root accepted")
			}
		})
	}
}

func TestNormalizeLibraryConfigClampsOverflowingAttractIdle(t *testing.T) {
	normalized, err := fogcast.NormalizeLibraryConfig(fogcast.LibraryConfig{
		AttractIdleSeconds: fogcast.MaxAttractIdleSeconds + 1000,
		PreferredRegions:   []string{"usa"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.AttractIdleSeconds != fogcast.MaxAttractIdleSeconds {
		t.Fatalf("idle = %d want %d", normalized.AttractIdleSeconds, fogcast.MaxAttractIdleSeconds)
	}
}

func TestNormalizeLibraryConfigNormalizesLibrariesAndTargets(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "SNES")
	normalized, err := fogcast.NormalizeLibraryConfig(fogcast.LibraryConfig{
		Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: root}},
		Targets:        []fogcast.TargetConfig{{Name: "dev", Enabled: true, Address: "http://192.0.2.10:8182/", Agent: "test-token", AgentSet: true}},
		SelectedTarget: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Libraries) != 1 || normalized.Libraries[0].Path != root {
		t.Fatalf("libraries = %#v", normalized.Libraries)
	}
	if len(normalized.Targets) != 1 || normalized.Targets[0].Address != "http://192.0.2.10:8182" || normalized.Targets[0].AgentSet {
		t.Fatalf("targets = %#v", normalized.Targets)
	}
	if normalized.SelectedTarget != "dev" {
		t.Fatalf("selected target = %q", normalized.SelectedTarget)
	}
}

func TestLoadConfigReadsZX81MachineROM(t *testing.T) {
	path := writeConfig(t, `base_url = "http://192.0.2.10:8182"
token = "secret"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[zx81_machine_rom]
script = "/opt/fes/link_static_rbf.py"
image = "/opt/fes/zx8x.hex"
mistral_cv = "/opt/fes/mistral-cv"
`)
	cfg, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ZX81MachineROM.Script != "/opt/fes/link_static_rbf.py" || cfg.ZX81MachineROM.Image != "/opt/fes/zx8x.hex" || cfg.ZX81MachineROM.MistralCV != "/opt/fes/mistral-cv" {
		t.Fatalf("machine ROM = %#v", cfg.ZX81MachineROM)
	}
	partial := writeConfig(t, `base_url = "http://192.0.2.10:8182"
token = "secret"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[zx81_machine_rom]
script = "link_static_rbf.py"
`)
	if _, err = fogcast.LoadConfig(partial); err == nil {
		t.Fatal("relative machine ROM path was accepted")
	}
}

func TestLoadConfigReadsBundledFirmwareDirectory(t *testing.T) {
	dir := t.TempDir()
	config, err := fogcast.LoadConfig(writeConfig(t, validConfig(filepath.Join(dir, "games-a"), filepath.Join(dir, "games-b"))+`
[firmware]
bundled_dir = "/usr/share/fogcast/firmware"
`))
	if err != nil {
		t.Fatal(err)
	}
	if config.Firmware.BundledDir != "/usr/share/fogcast/firmware" {
		t.Fatalf("firmware config = %#v", config.Firmware)
	}
	if _, err := fogcast.LoadConfig(writeConfig(t, validConfig(filepath.Join(dir, "games-a"), filepath.Join(dir, "games-b"))+`
[firmware]
bundled_dir = "firmware"
`)); err == nil {
		t.Fatal("relative firmware directory was accepted")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigMeshEnsureDefaultsOff(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "SNES")
	second := filepath.Join(dir, "Genesis")
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := fogcast.LoadConfig(writeConfig(t, validConfig(first, second)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MeshEnsure {
		t.Fatal("missing [mesh] defaulted ensure on")
	}
	bare := writeConfig(t, validConfig(first, second)+"\n[mesh]\n")
	cfg, err = fogcast.LoadConfig(bare)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MeshEnsure {
		t.Fatal("bare [mesh] turned ensure on")
	}
	off := writeConfig(t, validConfig(first, second)+"\n[mesh]\nensure = false\n")
	cfg, err = fogcast.LoadConfig(off)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MeshEnsure {
		t.Fatal("ensure = false turned ensure on")
	}
	on := writeConfig(t, validConfig(first, second)+"\n[mesh]\nensure = true\n")
	cfg, err = fogcast.LoadConfig(on)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MeshEnsure {
		t.Fatal("ensure = true stayed off")
	}
}

func TestLoadConfigRejectsRetiredRawCoreMap(t *testing.T) {
	path := writeConfig(t, `token = "test"
[fpga_rom_paths]
old_game = "/media/fat/games/old.rom"
`)
	if _, err := fogcast.LoadConfig(path); err == nil {
		t.Fatal("retired raw-core map accepted")
	}
}

func TestCoreCatalogConfigRequiresLibraryIdentity(t *testing.T) {
	base := `base_url = "http://127.0.0.1:8182"
token = "synthetic"
request_timeout_seconds = 15
upload_timeout_seconds = 120
[core_catalog]
path = "/private/catalog.json"
`
	p := writeConfig(t, base)
	if _, err := fogcast.LoadConfig(p); err == nil {
		t.Fatal("accepted catalog without serving library identity")
	}
	p = writeConfig(t, base+`library_source_id = "library-one"`)
	cfg, err := fogcast.LoadConfig(p)
	if err != nil || cfg.CoreLibrarySourceID != "library-one" {
		t.Fatal(cfg, err)
	}
}

func TestLoadConfigMeshPlacement(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "SNES")
	second := filepath.Join(dir, "Genesis")
	for _, root := range []string{first, second} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	preference := "11111111-1111-4111-8111-111111111111"
	override := "22222222-2222-4222-8222-222222222222"
	for _, tc := range []struct {
		name string
		mesh string
		want fogcast.MeshPlacementConfig
	}{
		{name: "no mesh table"},
		{name: "bare mesh table", mesh: "\n[mesh]\n"},
		{name: "placement false", mesh: "\n[mesh]\nplacement = false\n"},
		{name: "ensure alone", mesh: "\n[mesh]\nensure = true\n"},
		{name: "placement true", mesh: "\n[mesh]\nplacement = true\n", want: fogcast.MeshPlacementConfig{Enabled: true}},
		{
			name: "household ids",
			mesh: "\n[mesh]\nplacement = true\ndisplay_preference = \" " + preference + " \"\nplacement_override = \"" + override + "\"\n",
			want: fogcast.MeshPlacementConfig{Enabled: true, DisplayPreference: preference, Override: override},
		},
		{
			name: "ids without placement",
			mesh: "\n[mesh]\ndisplay_preference = \"" + preference + "\"\n",
			want: fogcast.MeshPlacementConfig{DisplayPreference: preference},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := fogcast.LoadConfig(writeConfig(t, validConfig(first, second)+tc.mesh))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MeshPlacement != tc.want {
				t.Fatalf("placement %+v, want %+v", cfg.MeshPlacement, tc.want)
			}
		})
	}
	for _, bad := range []string{
		"\n[mesh]\ndisplay_preference = \"living-room\"\n",
		"\n[mesh]\nplacement_override = \"kit-b\"\n",
		"\n[mesh]\nplacement = \"yes\"\n",
	} {
		if _, err := fogcast.LoadConfig(writeConfig(t, validConfig(first, second)+bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
