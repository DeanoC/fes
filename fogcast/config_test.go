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

[host_emulator]
binary = "/Applications/RetroArch"
core = "/cores/snes.dylib"
systems = ["snes"]

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
	if got := cfg.HostEmulator; got.Binary != "/Applications/RetroArch" || got.Core != "/cores/snes.dylib" || len(got.Systems) != 1 || got.Systems[0] != protocol.SystemSNES {
		t.Fatalf("host emulator = %#v", got)
	}
	if len(cfg.FPGAROMPaths) != 1 || cfg.FPGAROMPaths[fogcast.DefaultFPGAROMGameID] != fogcast.DefaultActRaiserROMPath {
		t.Fatalf("seeded FPGA ROM map = %#v", cfg.FPGAROMPaths)
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

func TestLoadConfigLoadsFPGAROMPathAllowlist(t *testing.T) {
	dir := t.TempDir()
	content := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")) + `
[fpga_rom_paths]
snes-actraiser-live = "/media/fat/games/SNES/ActRaiser.smc"
`
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if got := config.FPGAROMPaths["snes-actraiser-live"]; got != "/media/fat/games/SNES/ActRaiser.smc" {
		t.Fatalf("FPGA ROM map = %#v", config.FPGAROMPaths)
	}
	if _, seeded := config.FPGAROMPaths[fogcast.DefaultFPGAROMGameID]; seeded {
		t.Fatalf("explicit map unexpectedly kept seed: %#v", config.FPGAROMPaths)
	}
}

func TestLoadConfigRejectsInvalidFPGAROMPaths(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	tests := map[string]string{
		"relative path":   base + "\n[fpga_rom_paths]\nactraiser = \"games/SNES/ActRaiser.smc\"\n",
		"escaped path":    base + "\n[fpga_rom_paths]\nactraiser = \"/media/fat/../etc/passwd\"\n",
		"outside fat":     base + "\n[fpga_rom_paths]\nactraiser = \"/tmp/ActRaiser.smc\"\n",
		"invalid game id": base + "\n[fpga_rom_paths]\n\"ActRaiser\" = \"/media/fat/games/SNES/ActRaiser.smc\"\n",
		"empty path":      base + "\n[fpga_rom_paths]\nactraiser = \"\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
				t.Fatal("invalid FPGA ROM map accepted")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidHostEmulatorSystems(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "games")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	base := validConfig(root, filepath.Join(dir, "other"))
	for name, systems := range map[string]string{"unknown": `["not-a-platform"]`, "duplicate": `["snes", "snes"]`} {
		t.Run(name, func(t *testing.T) {
			content := base + "\n[host_emulator]\nbinary = \"/Applications/RetroArch\"\ncore = \"/cores/snes.dylib\"\nsystems = " + systems + "\n"
			if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
				t.Fatal("invalid systems accepted")
			}
		})
	}
}

func TestLoadConfigAcceptsHostEmulatorCatalogPlatforms(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "games")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := validConfig(root, filepath.Join(dir, "other")) + `
[host_emulator]
binary = "/Applications/RetroArch"
core = "/cores/nes.dylib"
systems = ["nes"]
`
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.HostEmulator.Systems) != 1 || config.HostEmulator.Systems[0] != "nes" {
		t.Fatalf("host emulator systems = %#v", config.HostEmulator.Systems)
	}
}

func TestLoadConfigAcceptsPerPlatformHostEmulatorCores(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "games")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := validConfig(root, filepath.Join(dir, "other")) + `
[host_emulator]
binary = "/Applications/RetroArch"

[[host_emulator.cores]]
platform = "nes"
core = "/cores/nestopia_libretro.dylib"

[[host_emulator.cores]]
platform = "gba"
core = "/cores/mgba_libretro.dylib"
`
	config, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if config.HostEmulator.CoreFor("nes") != "/cores/nestopia_libretro.dylib" {
		t.Fatalf("nes core = %#v", config.HostEmulator)
	}
	if config.HostEmulator.CoreFor("gba") != "/cores/mgba_libretro.dylib" {
		t.Fatalf("gba core = %#v", config.HostEmulator)
	}
	if got := config.HostEmulator.LaunchPlatforms(); len(got) != 2 || got[0] != "nes" || got[1] != "gba" {
		t.Fatalf("launch platforms = %#v", got)
	}
}

func TestLoadConfigRejectsMixingLegacyCoreWithPerPlatformCores(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "games")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := validConfig(root, filepath.Join(dir, "other")) + `
[host_emulator]
binary = "/Applications/RetroArch"
core = "/cores/snes.dylib"
systems = ["snes"]

[[host_emulator.cores]]
platform = "nes"
core = "/cores/nestopia_libretro.dylib"
`
	if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
		t.Fatal("mixed host emulator config accepted")
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

func TestLoadConfigLoadsAndValidatesMediaConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))+`
[media]
enabled = true
session = "session-1"
generation = 7
ssrc = 42
rtp_listen = "127.0.0.1:5000"
rtp_destination = "127.0.0.1:5001"
control_address = "127.0.0.1:5002"
decoder = "none"
capture_device = "device-1"
width = 1280
height = 720
fps_numerator = 30
fps_denominator = 1
bitrate = 4000000
gop = 60
mtu = 1200
`)
	config, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Media; !got.Enabled || got.Session != "session-1" || got.Generation != 7 || got.SSRC != 42 || got.RTPListen != "127.0.0.1:5000" || got.RTPDestination != "127.0.0.1:5001" || got.ControlAddress != "127.0.0.1:5002" || got.CaptureDevice != "device-1" || got.Width != 1280 || got.Height != 720 || got.FPSNumerator != 30 || got.FPSDenominator != 1 || got.Bitrate != 4000000 || got.GOP != 60 || got.MTU != 1200 {
		t.Fatalf("media config = %#v", got)
	}
}

func TestLoadConfigLoadsNestedAudioConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))+`
[media]
enabled = true
session = "session-1"
ssrc = 42
rtp_listen = "127.0.0.1:5000"
rtp_destination = "127.0.0.1:5001"
capture_device = "screen"

[media.audio]
enabled = true
source = "shadowcast_uac"
device = "ShadowCast 3"
device_uid = "uid-redacted-in-test"
device_hash = "sha256:be0109cefaf140745eb4824851b296d5fd2ba5443e8e775d8c9fa5b7763f039a"
rtp_destination = "127.0.0.1:5101"
control_address = "127.0.0.1:5102"
ssrc = 43
sample_rate = 48000
channels = 2
frame_samples = 240
mtu = 1200
`)
	config, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := config.Media.Audio
	if !got.Enabled || got.Source.Kind != "shadowcast_uac" || got.Source.EndpointName != "ShadowCast 3" || got.Source.EndpointUID != "uid-redacted-in-test" || got.Source.EndpointDigest == "" || got.Transport.SSRC != 43 || got.Transport.RTPDestination != "127.0.0.1:5101" || got.Transport.ControlAddress != "127.0.0.1:5102" {
		t.Fatalf("audio config = %#v", got)
	}
}

func TestLoadConfigRejectsInvalidAudioConfiguration(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis")) + `
[media]
enabled = true
session = "session-1"
ssrc = 42
rtp_listen = "127.0.0.1:5000"
rtp_destination = "127.0.0.1:5001"
capture_device = "screen"

[media.audio]
enabled = true
source = "shadowcast_uac"
device = "ShadowCast 3"
device_uid = "uid-redacted-in-test"
device_hash = "sha256:be0109cefaf140745eb4824851b296d5fd2ba5443e8e775d8c9fa5b7763f039a"
rtp_destination = "127.0.0.1:5101"
control_address = "127.0.0.1:5102"
ssrc = 43
sample_rate = 48000
channels = 2
frame_samples = 240
mtu = 1200
`
	for name, mutate := range map[string]func(string) string{
		"unknown field":   func(value string) string { return value + "unexpected = true\n" },
		"duplicate SSRC":  func(value string) string { return strings.Replace(value, "ssrc = 43", "ssrc = 42", 1) },
		"zero SSRC":       func(value string) string { return strings.Replace(value, "ssrc = 43", "ssrc = 0", 1) },
		"invalid address": func(value string) string { return strings.Replace(value, "127.0.0.1:5101", "not-an-address", 1) },
		"invalid port":    func(value string) string { return strings.Replace(value, "127.0.0.1:5101", "127.0.0.1:not-a-port", 1) },
		"invalid source":  func(value string) string { return strings.Replace(value, "shadowcast_uac", "unknown", 1) },
		"missing endpoint identity": func(value string) string {
			return strings.Replace(value, "device_uid = \"uid-redacted-in-test\"\n", "", 1)
		},
		"mismatched endpoint digest": func(value string) string {
			return strings.Replace(value, "be0109cefaf140745eb4824851b296d5fd2ba5443e8e775d8c9fa5b7763f039a", "0000000000000000000000000000000000000000000000000000000000000000", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fogcast.LoadConfig(writeConfig(t, mutate(base))); err == nil {
				t.Fatal("invalid audio configuration accepted")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidEnabledMediaWithoutLeakingValues(t *testing.T) {
	dir := t.TempDir()
	base := validConfig(filepath.Join(dir, "SNES"), filepath.Join(dir, "Genesis"))
	for name, media := range map[string]string{
		"missing capture": `enabled = true\nsession = "secret-session"\nssrc = 1\nrtp_listen = "127.0.0.1:5000"\nrtp_destination = "127.0.0.1:5001"`,
		"bad decoder":     `enabled = true\nsession = "secret-session"\nssrc = 1\nrtp_listen = "127.0.0.1:5000"\nrtp_destination = "127.0.0.1:5001"\ncapture_device = "device"\ndecoder = "secret-decoder"`,
		"bad address":     `enabled = true\nsession = "secret-session"\nssrc = 1\nrtp_listen = "not-an-address"\nrtp_destination = "127.0.0.1:5001"\ncapture_device = "device"`,
		"partial fps":     `enabled = true\nsession = "secret-session"\nssrc = 1\nrtp_listen = "127.0.0.1:5000"\nrtp_destination = "127.0.0.1:5001"\ncapture_device = "device"\nfps_numerator = 30`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fogcast.LoadConfig(writeConfig(t, base+"\n[media]\n"+strings.ReplaceAll(media, `\n`, "\n")+"\n"))
			if err == nil || strings.Contains(err.Error(), "secret-session") || strings.Contains(err.Error(), "secret-decoder") {
				t.Fatalf("error = %v", err)
			}
		})
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

func TestLoadConfigWatchRootDefaultsToSNESLibraryThenCandidateUNC(t *testing.T) {
	dir := t.TempDir()
	snesRoot := filepath.Join(dir, "SNES")
	if err := os.MkdirAll(snesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	withLibrary, err := fogcast.LoadConfig(writeConfig(t, validConfig(snesRoot, filepath.Join(dir, "Genesis"))))
	if err != nil {
		t.Fatal(err)
	}
	if withLibrary.Library.WatchRoot != filepath.Clean(snesRoot) {
		t.Fatalf("watch root from SNES library = %q", withLibrary.Library.WatchRoot)
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
watch_root = "//deano-clawz/Games/Games/SNES"
`
	cfg, err := fogcast.LoadConfig(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Library.WatchRoot != fogcast.DefaultFolderWatchRoot {
		t.Fatalf("UNC watch_root = %q", cfg.Library.WatchRoot)
	}
}

func TestLoadConfigRejectsAmbiguousUNCWatchRoot(t *testing.T) {
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
watch_root = "//server/share/SNES"
`
	if _, err := fogcast.LoadConfig(writeConfig(t, content)); err == nil {
		t.Fatal("ambiguous UNC watch_root accepted")
	} else if !strings.Contains(err.Error(), "more than one local absolute SNES root") {
		t.Fatalf("unexpected error = %v", err)
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

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
