package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestParseArgsDefaultsToLoopbackHostAPI(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != hostclient.DefaultAPIBase || opts.Width != 1280 || opts.Height != 720 || opts.Fullscreen || opts.Smoke {
		t.Fatalf("opts = %#v", opts)
	}
	if opts.SafeAreaSet || opts.NoAttract {
		t.Fatalf("living-room defaults = %#v", opts)
	}
}

func TestParseArgsKitConfigPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(path, []byte(`{"api":"http://kit.test:8789","token":"launcher-token-012345678901234567890123456789","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"FOGCAST_API": "http://env.test:8789", "FOGCAST_TOKEN": "environment-token", "FOGCAST_TARGET_ID": "environment-target"}
	getenv := func(key string) string { return env[key] }
	opts, err := parseArgsWithEnv([]string{"-config", path}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://env.test:8789" || opts.APIToken != "environment-token" || opts.TargetID != "environment-target" {
		t.Fatalf("env precedence: %#v", opts)
	}
	opts, err = parseArgs([]string{"-config", path, "-api", "http://flag.test:8789", "-token", "flag-token", "-target-id", "flag-target"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://flag.test:8789" || opts.APIToken != "flag-token" || opts.TargetID != "flag-target" {
		t.Fatalf("flag precedence: %#v", opts)
	}
	env = map[string]string{}
	opts, err = parseArgsWithEnv([]string{"-config", path}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://kit.test:8789" || opts.APIToken != "launcher-token-012345678901234567890123456789" || opts.TargetID != "73dc9f5f-1a12-4a95-a820-a9b4e600769a" {
		t.Fatalf("config precedence: %#v", opts)
	}
}

func TestParseArgsSmokeAndAPI(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-api", "http://127.0.0.1:8787", "-smoke", "-smoke-timeout", "12s", "-max-games", "50"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://127.0.0.1:8787" || !opts.Smoke || opts.SmokeTimeout != 12*time.Second || opts.MaxGames != 50 {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsAPIHost(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-api", "http://host.docker.internal:8787", "-api-host", "127.0.0.1:8787"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.APIBase != "http://host.docker.internal:8787" || opts.APIHost != "127.0.0.1:8787" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsSafeAreaAndNoAttract(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-safe-area", "0", "-no-attract"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.SafeAreaSet || opts.SafeAreaPct != 0 || !opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsInputProfile(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-input-profile", "swap-ab"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.InputProfile != "swap-ab" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsTheme(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-theme", "arcade"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Theme != "arcade" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsGFX(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-gfx", "fpga"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "fpga" {
		t.Fatalf("opts = %#v", opts)
	}
	opts, err = parseArgs([]string{"-gfx", "fpga-stub"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "fpga-stub" {
		t.Fatalf("stub opts = %#v", opts)
	}
}

func TestParseArgsLayout(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-layout", "shelf"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.LayoutSet || opts.Layout != "shelf" {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestParseArgsNoAttractFalseIsSet(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-no-attract=false"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("explicit false = %#v", opts)
	}
}

func TestParseArgsHomeRoom(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-home-room", "main-menu"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.HomeRoom != "main-menu" {
		t.Fatalf("home room %#v", opts.HomeRoom)
	}
	opts, err = parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.HomeRoom != "" {
		t.Fatalf("unset home room %#v", opts.HomeRoom)
	}
}

func TestParseArgsWiresSiblingCatalogConfig(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "launcher.json")
	catalog := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(launcher, []byte(`{"api":"http://kit.test:8789","token":"launcher-token-012345678901234567890123456789","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog, []byte("token = \"synthetic\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(string) string { return "" }
	opts, err := parseArgsWithEnv([]string{"-config", launcher, "-gfx", "menu-display"}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if opts.CatalogConfig != catalog || opts.GFX != "menu-display" {
		t.Fatalf("catalog = %q gfx = %q", opts.CatalogConfig, opts.GFX)
	}
	opts, err = parseArgsWithEnv([]string{"-config", launcher, "-catalog-config", filepath.Join(dir, "missing.toml")}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if opts.CatalogConfig != "" {
		t.Fatalf("missing explicit catalog = %q", opts.CatalogConfig)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := parseArgs([]string{"-bogus"})
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseArgsMenuDisplay(t *testing.T) {
	t.Parallel()
	opts, err := parseArgs([]string{"-gfx", "menu-display", "-menu-socket", "/tmp/runtime.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "menu-display" || opts.MenuSocket != "/tmp/runtime.sock" {
		t.Fatalf("opts = %#v", opts)
	}
	opts, err = parseArgs([]string{"-gfx", "menu-display"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "menu-display" || opts.MenuSocket != "" {
		t.Fatalf("default socket opts = %#v", opts)
	}
}

func TestParseArgsFramebuffer(t *testing.T) {
	opts, err := parseArgs([]string{"-gfx", "linuxfb", "-fb", "/dev/fb0", "-input", "/dev/input/event3,/dev/input/event5"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.GFX != "linuxfb" || opts.Framebuffer != "/dev/fb0" || opts.Input != "/dev/input/event3,/dev/input/event5" {
		t.Fatalf("options = %#v", opts)
	}
}
